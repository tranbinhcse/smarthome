package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hubd/isapi"
)

// go2rtc giả: serve trả lời yêu cầu thứ n (từ 1) của /api/stream.mp4; query của từng yêu cầu đi vào kênh trả về.
func fakeG2(t *testing.T, serve func(n int, w http.ResponseWriter, r *http.Request)) (*url.URL, chan url.Values) {
	reqs := make(chan url.Values, 100)
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stream.mp4" {
			http.Error(w, "không có", 404)
			return
		}
		select { // bộ ghi lỗi mà nối dồn dập: bỏ bớt thay vì treo handler (srv.Close sẽ chờ mãi)
		case reqs <- r.URL.Query():
		default:
		}
		serve(int(n.Add(1)), w, r)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return u, reqs
}

// sendHold gửi b rồi giữ kết nối tới khi hubd ngắt.
func sendHold(w http.ResponseWriter, r *http.Request, b []byte) {
	w.Write(b)
	w.(http.Flusher).Flush()
	<-r.Context().Done()
}

type recEnv struct {
	r     *recorder
	db    *DB
	dir   string
	tick  chan time.Time // mỗi lần gửi = một lần kiểm 60 giây; gửi xong nghĩa là lần kiểm trước đã chạy hết
	notes chan string    // "level|kind|text"
	stop  func()
}

func diskOK() recDisk { return recDisk{OK: true, Total: 1e12, Free: 5e11} }

// startRec chạy recorder với chờ nối lại 1 ms; setup sửa recorder trước khi chạy. Dừng và chờ mọi goroutine khi test xong.
func startRec(t *testing.T, g2 *url.URL, srcs []recSource, disk func() recDisk, setup func(*recorder)) *recEnv {
	e := &recEnv{db: newDB(t), dir: t.TempDir(), tick: make(chan time.Time), notes: make(chan string, 100)}
	e.r = newRecorder(e.db, g2, e.dir, disk, func() []recSource { return srcs }, func() map[string]recPolicy { return nil },
		func(level, kind, text string) { e.notes <- level + "|" + kind + "|" + text })
	e.r.tick, e.r.backoff = e.tick, time.Millisecond
	if setup != nil {
		setup(e.r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.r.run(ctx); close(done) }()
	e.stop = func() { cancel(); <-done }
	t.Cleanup(e.stop)
	return e
}

func recv[T any](t *testing.T, c <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("chờ quá lâu: %s", what)
	}
	panic("không tới")
}

func noneIn[T any](t *testing.T, c <-chan T, what string) {
	t.Helper()
	select {
	case v := <-c:
		t.Fatalf("%s: %v", what, v)
	default:
	}
}

// idle: chờ mọi luồng thôi (lần kiểm chỉ mở lại nguồn không còn luồng).
func (e *recEnv) idle(t *testing.T) {
	t.Helper()
	waitFor(t, "luồng thôi", func() bool { return e.runs() == 0 })
}

func (e *recEnv) runs() int {
	e.r.mu.Lock()
	defer e.r.mu.Unlock()
	return len(e.r.runs)
}

// readable: file mở đầu ftyp+moov, toàn box trọn vẹn; trả phần mdat.
func readable(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bx := topBoxes(t, b)
	if len(bx) < 4 || bx[0].typ != "ftyp" || bx[1].typ != "moov" {
		t.Fatalf("%s: không phải MP4 phân mảnh đọc được", path)
	}
	return mdatBytes(t, b)
}

func TestRecorderWritesAndIndexes(t *testing.T) {
	stream := fakeFMP4("hev1", uniformGOPs(6, 5, 1000))
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			w.Write(stream)
			return
		}
		<-r.Context().Done()
	})
	src := recSource{ID: "cam_a", URL: "rtsp://u:p%40ss@10.0.0.9:554/Streaming/Channels/101"}
	e := startRec(t, g2, []recSource{src}, diskOK, nil)
	q := recv(t, reqs, "yêu cầu đầu")
	if q.Get("name") != "rec_cam_a" || q.Get("src") != src.URL {
		t.Fatalf("query %v", q)
	}
	recv(t, reqs, "nối lại sau khi luồng hết") // file của lần đầu đã vào chỉ mục
	e.stop()
	files, err := recAll(e.db)
	if err != nil || len(files) == 0 {
		t.Fatalf("chỉ mục: %v %v", files, err)
	}
	var sum, onDisk, mdat int64
	for _, f := range files {
		s := f.Start.In(tgICT)
		if want := filepath.Join(e.dir, "cam_a", s.Format("20060102"), s.Format("150405")+".mp4"); f.Cam != "cam_a" || f.Path != want {
			t.Errorf("file %+v, muốn %s", f, want)
		}
		st, err := os.Stat(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		sum, onDisk = sum+f.Bytes, onDisk+st.Size()
		mdat += int64(len(readable(t, f.Path)))
	}
	all, _ := filepath.Glob(filepath.Join(e.dir, "*", "*", "*.mp4"))
	if len(all) != len(files) || sum != onDisk || mdat != 6*5*1000 {
		t.Fatalf("trên đĩa %d file %d byte, chỉ mục %d file %d byte, mdat %d", len(all), onDisk, len(files), sum, mdat)
	}
}

func TestRecorderReconnects(t *testing.T) {
	stream := fakeFMP4("hev1", uniformGOPs(2, 3, 500))
	cutMid := append(append([]byte(nil), stream...), fmp4Frame(1, 9e6, true, 500, 1)[:30]...)
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) {
		switch {
		case n <= 2: // đứt giữa box
			w.Write(cutMid)
		case n == 3: // đóng gọn
			w.Write(stream)
		default:
			<-r.Context().Done()
		}
	})
	e := startRec(t, g2, []recSource{{ID: "cam_a", URL: "rtsp://x/y"}}, diskOK, nil)
	for i := 1; i <= 4; i++ {
		recv(t, reqs, "yêu cầu go2rtc")
	}
	e.stop()
	files, _ := recAll(e.db)
	all, _ := filepath.Glob(filepath.Join(e.dir, "*", "*", "*.mp4"))
	if len(files) != 3 || len(all) != 3 {
		t.Fatalf("chỉ mục %d file, trên đĩa %d, muốn 3", len(files), len(all))
	}
	for _, f := range files {
		if n := len(readable(t, f.Path)); n != 2*3*500 {
			t.Errorf("%s: mdat %d byte", f.Path, n)
		}
	}
}

func TestRecorderNeverLogsURL(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	src := recSource{ID: "cam_a", URL: "rtsp://u:p%40ss@10.0.0.9:554/x"}
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) {
		switch n {
		case 1:
			http.Error(w, "không mở được "+src.URL, 500) // go2rtc có thể trả lại nguồn trong thân lỗi
		case 2:
			c, _, _ := w.(http.Hijacker).Hijack()
			c.Close()
		default:
			<-r.Context().Done()
		}
	})
	e := startRec(t, g2, []recSource{src}, diskOK, nil)
	for i := 1; i <= 3; i++ {
		recv(t, reqs, "yêu cầu go2rtc")
		if i < 3 { // lần nối không ra file: nối lại ở lần kiểm kế, sau khi luồng cũ đã thôi
			e.idle(t)
			e.tick <- time.Now()
		}
	}
	e.stop()
	out := buf.String()
	if !strings.Contains(out, "ghi hình cam_a: go2rtc HTTP 500") {
		t.Fatalf("thiếu log lỗi: %q", out)
	}
	for _, s := range []string{"rtsp://", "rtsp%3A", "p%40ss", "p@ss", "p%2540ss", "10.0.0.9"} {
		if strings.Contains(out, s) {
			t.Fatalf("log lộ %q: %s", s, out)
		}
	}
}

func TestRecorderKeepAroundPending(t *testing.T) {
	stream := fakeFMP4("hev1", uniformGOPs(3, 3, 500))
	cut := len(fmp4Head("hev1")) + 6*len(fmp4Frame(1, 0, true, 500, 1)) // hết GOP 2: file đã mở, GOP 1 đã ghi
	release := make(chan struct{})
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			w.Write(stream[:cut])
			w.(http.Flusher).Flush()
			select {
			case <-release:
				w.Write(stream[cut:])
			case <-r.Context().Done():
			}
			return
		}
		<-r.Context().Done()
	})
	created := make(chan string, 10)
	e := startRec(t, g2, []recSource{{ID: "cam_a", URL: "rtsp://x/y"}}, diskOK, func(r *recorder) {
		orig := r.create
		r.create = func(cam string, s time.Time) (io.WriteCloser, string, error) {
			w, p, err := orig(cam, s)
			created <- p
			return w, p, err
		}
	})
	recv(t, created, "file đang ghi")
	e.r.KeepAround("cam_a", time.Now())
	close(release)
	recv(t, reqs, "yêu cầu đầu")
	recv(t, reqs, "nối lại sau khi file xong")
	e.stop()
	files, _ := recAll(e.db)
	if len(files) != 1 || !files[0].Held {
		t.Fatalf("muốn 1 file held: %+v", files)
	}
}

func TestRecorderAdoptsOrphans(t *testing.T) {
	var orphan, known string
	mtime := time.Date(2026, 10, 30, 10, 16, 40, 0, tgICT)
	e := startRec(t, &url.URL{Scheme: "http", Host: "127.0.0.1:1"}, nil, diskOK, func(r *recorder) {
		orphan = filepath.Join(r.dir, "cam_x", "20261030", "101500.mp4")
		known = filepath.Join(r.dir, "cam_x", "20261030", "100000.mp4")
		os.MkdirAll(filepath.Dir(orphan), 0o755)
		os.WriteFile(orphan, make([]byte, 1234), 0o644)
		os.Chtimes(orphan, mtime, mtime)
		os.WriteFile(known, []byte("x"), 0o644)
		recAdd(r.db, recFile{Cam: "cam_x", Path: known, Start: time.Date(2026, 10, 30, 10, 0, 0, 0, tgICT), Bytes: 1})
	})
	e.stop()
	files, _ := recAll(e.db)
	if len(files) != 2 {
		t.Fatalf("muốn 2 dòng: %+v", files)
	}
	f := files[1]
	if f.Cam != "cam_x" || f.Path != orphan || !f.Start.Equal(time.Date(2026, 10, 30, 10, 15, 0, 0, tgICT)) || !f.End.Equal(mtime) ||
		f.Bytes != 1234 || f.Held {
		t.Fatalf("dòng mồ côi sai: %+v", f)
	}
}

func TestRecorderDiskWriteError(t *testing.T) {
	stream := fakeFMP4("hev1", uniformGOPs(3, 3, 500))
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) { sendHold(w, r, stream) })
	var calls atomic.Int32
	e := startRec(t, g2, []recSource{{ID: "cam_a", URL: "rtsp://x/y"}}, diskOK, func(r *recorder) {
		orig := r.create
		r.create = func(cam string, s time.Time) (io.WriteCloser, string, error) {
			if calls.Add(1) == 1 {
				return nil, "", errors.New("ổ bị rút")
			}
			return orig(cam, s)
		}
	})
	recv(t, reqs, "yêu cầu đầu")
	if n := recv(t, e.notes, "báo lỗi ghi"); n != "warn|luu_tru|Không ghi được vào ổ ghi hình, đã dừng ghi." {
		t.Fatalf("báo %q", n)
	}
	select { // chờ nối lại 1 ms: 50 ms đủ để thấy nếu nó nối lại sai
	case <-reqs:
		t.Fatal("nối lại go2rtc trước lần kiểm ổ kế tiếp")
	case <-time.After(50 * time.Millisecond):
	}
	e.idle(t)
	e.tick <- time.Now()
	recv(t, reqs, "ghi lại sau lần kiểm ổ")
	e.stop()
	noneIn(t, e.notes, "báo thêm")
}

func TestRecorderStopAndResume(t *testing.T) {
	var free atomic.Int64
	free.Store(5e8)
	disk := func() recDisk { return recDisk{OK: true, Total: 1e9, Free: free.Load()} }
	ended := make(chan int, 10)
	stream := fakeFMP4("hev1", uniformGOPs(1, 3, 500))
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) {
		sendHold(w, r, stream)
		ended <- n
	})
	e := startRec(t, g2, []recSource{{ID: "cam_a", URL: "rtsp://x/y"}}, disk, nil)
	recv(t, reqs, "yêu cầu đầu")
	free.Store(1e7) // 1%, không có file thường để xoá
	e.tick <- time.Now()
	if n := recv(t, e.notes, "báo dừng"); n != "urgent|luu_tru|Ổ ghi hình gần đầy: đã dừng ghi hình thường, chỉ còn giữ đoạn báo động." {
		t.Fatalf("báo %q", n)
	}
	recv(t, ended, "luồng dừng")
	e.tick <- time.Now()
	e.tick <- time.Now() // lần kiểm vẫn 1% đã chạy xong
	if e.runs() != 0 {
		t.Fatal("mở luồng khi đang dừng")
	}
	noneIn(t, e.notes, "báo dừng lần hai")
	free.Store(2.5e7) // trên 2% nhưng chưa tới 3%: vẫn dừng
	e.tick <- time.Now()
	e.tick <- time.Now()
	if e.runs() != 0 {
		t.Fatal("ghi lại khi mới 2,5%")
	}
	noneIn(t, e.notes, "báo ghi lại khi mới 2,5%")
	noneIn(t, reqs, "yêu cầu go2rtc khi mới 2,5%")
	free.Store(4e7)
	e.tick <- time.Now()
	if n := recv(t, e.notes, "báo ghi lại"); n != "info|luu_tru|Ổ ghi hình đã có chỗ, ghi hình lại." {
		t.Fatalf("báo %q", n)
	}
	recv(t, reqs, "ghi lại")
	e.stop()
	noneIn(t, e.notes, "báo thêm")
}

func TestRecorderDiskNotOKNoRequests(t *testing.T) {
	var ok atomic.Bool
	disk := func() recDisk {
		if ok.Load() {
			return diskOK()
		}
		return recDisk{Reason: recNoDir}
	}
	ended := make(chan int, 10)
	stream := fakeFMP4("hev1", uniformGOPs(1, 3, 500))
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) {
		sendHold(w, r, stream)
		ended <- n
	})
	e := startRec(t, g2, []recSource{{ID: "cam_a", URL: "rtsp://x/y"}}, disk, nil)
	e.tick <- time.Now()
	e.tick <- time.Now() // lần kiểm lúc khởi động và lần kế đã chạy xong
	if e.runs() != 0 {
		t.Fatal("mở luồng khi ổ không đạt")
	}
	noneIn(t, reqs, "yêu cầu go2rtc khi ổ không đạt")
	noneIn(t, e.notes, "báo lúc khởi động") // hubd khởi động lại mỗi khi đổi camera
	// đang ghi mà mất ổ: dừng luồng, báo đúng một lần
	ok.Store(true)
	e.tick <- time.Now()
	recv(t, reqs, "ghi khi ổ đạt")
	ok.Store(false)
	e.tick <- time.Now()
	if n := recv(t, e.notes, "báo mất ổ"); n != "warn|luu_tru|"+recNoDir {
		t.Fatalf("báo %q", n)
	}
	recv(t, ended, "luồng dừng")
	e.tick <- time.Now()
	e.tick <- time.Now()
	e.stop()
	noneIn(t, e.notes, "báo lại")
	noneIn(t, reqs, "yêu cầu go2rtc sau khi mất ổ")
}

func TestNvrRTSP(t *testing.T) {
	n := nvrConf{On: true, Host: "192.168.1.64:8080", User: "admin", Pass: "p@ss"}
	if g := nvrRTSP(n, 3, false); g != "rtsp://admin:p%40ss@192.168.1.64:554/Streaming/Channels/301" {
		t.Fatal(g)
	}
	if g := nvrRTSP(n, 12, true); g != "rtsp://admin:p%40ss@192.168.1.64:554/Streaming/Channels/1202" {
		t.Fatal(g)
	}
}

func TestRecSources(t *testing.T) {
	t.Setenv("NVR_HOST", "")
	db := newDB(t)
	c := newCams(nil, db)
	db.PutSetting("nvr", map[string]any{"ch2_zone": "z1"})
	if s := recSources(db, c); len(s) != 0 {
		t.Fatalf("chưa có IP đầu ghi mà vẫn ghi: %v", s)
	}
	db.PutSetting("nvr", map[string]any{"ch2_zone": "z1", "nvr_host": "192.168.1.64", "nvr_user": "admin", "nvr_pass": "pw"})
	cam, err := camSave(db, goodCam(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rtsp, err := camSave(db, camRec{Name: "Cổng sau", Zone: "z1", Kind: "rtsp", Host: "10.0.0.7", User: "u", Pass: "p", MainPath: "/stream1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := recSources(db, c)
	hp := cam.Host + ":" + strconv.Itoa(cam.HTTPPort)
	if len(s) != 3 || s[0].ID != "cam_ch2" || s[0].URL != "rtsp://admin:pw@192.168.1.64:554/Streaming/Channels/201" ||
		s[0].client != c.client(c.conf()) || s[0].probe != "http://192.168.1.64/ISAPI/System/deviceInfo" {
		t.Fatalf("kênh đầu ghi %+v", s)
	}
	// camList xếp theo tên: "Camera cổng" trước "Cổng sau"
	if s[1].ID != cam.ID || s[1].URL != cam.rtsp(false) || s[1].client != c.clientFor(hp, cam.User, cam.Pass) ||
		s[1].probe != "http://"+hp+"/ISAPI/System/deviceInfo" {
		t.Fatalf("camera Hikvision %+v", s[1])
	}
	if s[2].ID != rtsp.ID || s[2].URL != rtsp.rtsp(false) || s[2].client != nil {
		t.Fatalf("camera RTSP %+v", s[2])
	}
}

// lockedClient: client ISAPI vừa bị camera giả từ chối mật khẩu (khoá 15 phút), như sau một lần Thử kết nối sai.
func lockedClient(t *testing.T, cam *digestCam) *http.Client {
	t.Helper()
	cl := &http.Client{Transport: &isapi.Transport{User: "admin", Pass: "sai"}}
	if res, err := cl.Get(cam.URL + "/ISAPI/System/deviceInfo"); err == nil {
		res.Body.Close()
	}
	if !cl.Transport.(*isapi.Transport).Locked() {
		t.Fatal("client chưa bị khoá")
	}
	return cl
}

func (d *digestCam) bads() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bad
}

func TestRecorderSkipsLockedSource(t *testing.T) {
	cam := newDigestCam(t, "dung")
	cl := lockedClient(t, cam)
	stream := fakeFMP4("hev1", uniformGOPs(1, 3, 500))
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) { sendHold(w, r, stream) })
	e := startRec(t, g2, []recSource{{"cam_a", "rtsp://admin:sai@x/y", cl, cam.URL + "/ISAPI/System/deviceInfo"}}, diskOK, nil)
	e.tick <- time.Now()
	e.tick <- time.Now()
	if e.runs() != 0 {
		t.Fatal("mở luồng khi tài khoản đang bị khoá")
	}
	e.stop()
	noneIn(t, reqs, "yêu cầu go2rtc khi tài khoản đang bị khoá")
	if n := cam.bads(); n != 1 {
		t.Fatalf("camera nhận %d lần sai mật khẩu, muốn 1", n)
	}
}

func TestRecorderFailedAttemptWaitsForCheck(t *testing.T) {
	stream := fakeFMP4("hev1", uniformGOPs(1, 3, 500))
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			http.Error(w, "nguồn lỗi", 500)
			return
		}
		sendHold(w, r, stream)
	})
	e := startRec(t, g2, []recSource{{ID: "cam_a", URL: "rtsp://x/y"}}, diskOK, nil)
	recv(t, reqs, "yêu cầu đầu")
	select { // chờ nối lại 1 ms: 50 ms đủ để thấy nếu nó nối lại sai
	case <-reqs:
		t.Fatal("lần nối không ra file mà vẫn nối lại trước lần kiểm kế")
	case <-time.After(50 * time.Millisecond):
	}
	e.idle(t)
	e.tick <- time.Now()
	recv(t, reqs, "nối lại ở lần kiểm kế")
}

// countRT đếm lượt HTTP tới camera giả (kể cả lượt 401 mở đầu Digest).
type countRT struct{ n atomic.Int32 }

func (c *countRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return http.DefaultTransport.RoundTrip(r)
}

func (d *digestCam) goods() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.good
}

// 5 kênh cùng đầu ghi, mật khẩu sai: thử ISAPI trước khi nối, lần lượt, nên đầu ghi chỉ nhận một lần sai và go2rtc không
// được nối lần nào (không lần gửi mật khẩu sai nào qua RTSP).
func TestRecorderProbesBeforeConnect(t *testing.T) {
	cam := newDigestCam(t, "dung")
	var rt countRT
	tr := &isapi.Transport{User: "admin", Pass: "sai", Base: &rt}
	cl := &http.Client{Transport: tr}
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) { http.Error(w, "RTSP 401", 500) })
	var srcs []recSource
	for ch := 1; ch <= 5; ch++ {
		srcs = append(srcs, recSource{fmt.Sprintf("cam_ch%d", ch), fmt.Sprintf("rtsp://admin:sai@x/Streaming/Channels/%d01", ch), cl,
			cam.URL + "/ISAPI/System/deviceInfo"})
	}
	e := startRec(t, g2, srcs, diskOK, nil)
	e.tick <- time.Now()
	e.tick <- time.Now() // lần kiểm lúc khởi động và lần kế đã chạy xong
	e.idle(t)
	e.stop()
	noneIn(t, reqs, "nối go2rtc với tài khoản sai")
	if n, k := cam.bads(), rt.n.Load(); n != 1 || k > 2 || !tr.Locked() {
		t.Fatalf("đầu ghi nhận %d lần sai, %d lượt HTTP, khoá=%v; muốn 1 lần sai, tối đa 2 lượt", n, k, tr.Locked())
	}
}

func TestRecorderProbeRightPassword(t *testing.T) {
	cam := newDigestCam(t, "dung")
	cl := &http.Client{Transport: &isapi.Transport{User: "admin", Pass: "dung"}}
	stream := fakeFMP4("hev1", uniformGOPs(1, 3, 500))
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) { sendHold(w, r, stream) })
	var srcs []recSource
	for ch := 1; ch <= 5; ch++ {
		srcs = append(srcs, recSource{fmt.Sprintf("cam_ch%d", ch), "rtsp://x/y", cl, cam.URL + "/ISAPI/System/deviceInfo"})
	}
	e := startRec(t, g2, srcs, diskOK, nil)
	for range 5 {
		recv(t, reqs, "mở luồng")
	}
	e.stop()
	if g, b := cam.goods(), cam.bads(); g != 5 || b != 0 {
		t.Fatalf("đầu ghi nhận %d lần đúng, %d lần sai; muốn một deviceInfo mỗi lần nối", g, b)
	}
}

func TestRecorderStalledStream(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	stream := fakeFMP4("hev1", uniformGOPs(2, 3, 500))
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			sendHold(w, r, stream) // gửi 2 GOP rồi đứng
			return
		}
		<-r.Context().Done()
	})
	e := startRec(t, g2, []recSource{{ID: "cam_a", URL: "rtsp://x/y"}}, diskOK, func(r *recorder) { r.idle = 100 * time.Millisecond })
	recv(t, reqs, "yêu cầu đầu")
	recv(t, reqs, "nối lại sau khi luồng đứng") // không cần lần kiểm: lần nối đã ra file
	e.stop()
	files, _ := recAll(e.db)
	if len(files) != 1 || len(readable(t, files[0].Path)) != 2*3*500 {
		t.Fatalf("muốn 1 file đủ 2 GOP: %+v", files)
	}
	if !strings.Contains(buf.String(), "ghi hình cam_a: luồng đứng") {
		t.Fatalf("thiếu log luồng đứng: %q", buf.String())
	}
}

// failAfter: ghi được n byte rồi lỗi, như ổ bị rút giữa lúc ghi.
type failAfter struct {
	io.WriteCloser
	n int
}

func (f *failAfter) Write(b []byte) (int, error) {
	if len(b) > f.n {
		return 0, errors.New("ổ bị rút")
	}
	f.n -= len(b)
	return f.WriteCloser.Write(b)
}

func TestRecorderWriteFailsMidFile(t *testing.T) {
	stream := fakeFMP4("hev1", uniformGOPs(3, 3, 500))
	g2, reqs := fakeG2(t, func(n int, w http.ResponseWriter, r *http.Request) { sendHold(w, r, stream) })
	var calls atomic.Int32
	e := startRec(t, g2, []recSource{{ID: "cam_a", URL: "rtsp://x/y"}}, diskOK, func(r *recorder) {
		orig := r.create
		r.create = func(cam string, s time.Time) (io.WriteCloser, string, error) {
			w, p, err := orig(cam, s)
			if err == nil && calls.Add(1) == 1 {
				return &failAfter{w, len(fmp4Head("hev1"))}, p, nil // đầu file ghi được, GOP đầu thì lỗi
			}
			return w, p, err
		}
	})
	recv(t, reqs, "yêu cầu đầu")
	if n := recv(t, e.notes, "báo lỗi ghi"); n != "warn|luu_tru|Không ghi được vào ổ ghi hình, đã dừng ghi." {
		t.Fatalf("báo %q", n)
	}
	select {
	case <-reqs:
		t.Fatal("nối lại go2rtc trước lần kiểm ổ kế tiếp")
	case <-time.After(50 * time.Millisecond):
	}
	noneIn(t, e.notes, "báo lần hai")
	e.idle(t)
	e.tick <- time.Now()
	recv(t, reqs, "ghi lại sau lần kiểm ổ")
	e.stop()
	noneIn(t, e.notes, "báo thêm")
}
