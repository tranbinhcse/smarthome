package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// recFx ghi một file ghi hình thật (qua recSplit) vào thư mục tạm rồi thêm vào chỉ mục.
func recFx(t *testing.T, db *DB, cam string, start time.Time, gops [][]int) (recFile, []byte) {
	t.Helper()
	files, _, err := runSplit(bytes.NewReader(fakeFMP4("hev1", gops)), 1<<30)
	if err != nil || len(files) != 1 {
		t.Fatal(err, len(files))
	}
	p := filepath.Join(t.TempDir(), "x.mp4")
	if err := os.WriteFile(p, files[0], 0o600); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range gops {
		n += len(g)
	}
	f := recFile{Cam: cam, Path: p, Start: start, End: start.Add(time.Duration(n) * time.Second / 30), Bytes: int64(len(files[0]))}
	id, err := recAdd(db, f)
	if err != nil {
		t.Fatal(err)
	}
	f.ID = id
	return f, files[0]
}

func playMux(t *testing.T) (*http.ServeMux, *DB, []*http.Cookie) {
	h := NewHub(Config{}, newDB(t), func(string, string) {})
	a := NewAuth("1234", h.db)
	ck := call(newMux(h, a), "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	mux := http.NewServeMux()
	mountRecPlay(mux, h.db, a)
	return mux, h.db, ck
}

func TestRecScan(t *testing.T) {
	files, _, _ := runSplit(bytes.NewReader(fakeFMP4("hev1", uniformGOPs(3, 4, 100))), 1<<30)
	p := filepath.Join(t.TempDir(), "a.mp4")
	os.WriteFile(p, files[0], 0o600)
	initLen, gops, err := recScan(p)
	if err != nil {
		t.Fatal(err)
	}
	bs := topBoxes(t, files[0])
	if want := int64(len(bs[0].b) + len(bs[1].b)); initLen != want {
		t.Fatalf("initLen %d, muốn %d", initLen, want)
	}
	if len(gops) != 3 {
		t.Fatalf("%d GOP", len(gops))
	}
	sum := initLen
	for _, g := range gops {
		if g.Off != sum || math.Abs(g.Dur-4*3000.0/90000) > 1e-9 {
			t.Fatalf("GOP %+v, off muốn %d", g, sum)
		}
		sum += g.Len
	}
	if sum != int64(len(files[0])) {
		t.Fatalf("tổng %d, file %d", sum, len(files[0]))
	}
}

func TestRecScanTruncated(t *testing.T) {
	files, _, _ := runSplit(bytes.NewReader(fakeFMP4("hev1", [][]int{{100, 100}, {100, 100}, {100}})), 1<<30)
	p := filepath.Join(t.TempDir(), "a.mp4")
	os.WriteFile(p, files[0][:len(files[0])-10], 0o600) // cụt giữa mdat cuối: GOP cuối (một khung) không đủ
	_, gops, err := recScan(p)
	if err != nil || len(gops) != 2 {
		t.Fatalf("err=%v gops=%d, muốn 2 GOP đủ", err, len(gops))
	}
}

var reRange = regexp.MustCompile(`#EXT-X-BYTERANGE:(\d+)@(\d+)`)

func TestRecPlaylist(t *testing.T) {
	mux, db, ck := playMux(t)
	t0 := time.Date(2026, 10, 30, 10, 0, 0, 0, tgICT)
	f1, _ := recFx(t, db, "cam1", t0, uniformGOPs(3, 30, 100))
	f2, _ := recFx(t, db, "cam1", t0.Add(3*time.Second), uniformGOPs(2, 30, 100))
	u := fmt.Sprintf("/api/cam/cam1/rec.m3u8?from=%d&to=%d", t0.UnixMilli(), f2.End.UnixMilli())
	w := call(mux, "GET", u, "", "", ck...)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	for s, n := range map[string]int{"#EXT-X-DISCONTINUITY": 1, "#EXT-X-MAP": 2, "#EXT-X-ENDLIST": 1, "#EXTINF": 5} {
		if c := strings.Count(body, s); c != n {
			t.Errorf("%s: %d, muốn %d\n%s", s, c, n, body)
		}
	}
	if !strings.HasPrefix(body, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:1\n") || !strings.HasSuffix(body, "#EXT-X-ENDLIST\n") {
		t.Errorf("đầu/cuối sai:\n%s", body)
	}
	if strings.Contains(body, "http") || strings.Contains(body, os.TempDir()) || strings.Contains(body, `"/rec/`) || strings.Contains(body, "\n/") {
		t.Errorf("playlist có đường dẫn tuyệt đối:\n%s", body)
	}
	if !strings.Contains(body, fmt.Sprintf("URI=\"rec/%d.mp4\"", f1.ID)) || !strings.Contains(body, fmt.Sprintf("\nrec/%d.mp4\n", f2.ID)) {
		t.Errorf("thiếu URI file:\n%s", body)
	}
	var want []string
	for _, f := range []recFile{f1, f2} {
		_, gs, _ := recScan(f.Path)
		for _, g := range gs {
			want = append(want, fmt.Sprintf("%d@%d", g.Len, g.Off))
		}
	}
	var got []string
	for _, m := range reRange.FindAllStringSubmatch(body, -1) {
		got = append(got, m[1]+"@"+m[2])
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BYTERANGE %v, muốn %v", got, want)
	}
	// from giữa GOP thứ hai của file 1, to trong GOP thứ hai: chỉ lấy GOP chứa from tới GOP chứa to
	u = fmt.Sprintf("/api/cam/cam1/rec.m3u8?from=%d&to=%d", t0.Add(1500*time.Millisecond).UnixMilli(), t0.Add(1700*time.Millisecond).UnixMilli())
	if c := strings.Count(call(mux, "GET", u, "", "", ck...).Body.String(), "#EXTINF"); c != 1 {
		t.Errorf("cắt theo from/to: %d đoạn, muốn 1", c)
	}
	// khoảng sai
	for _, bad := range []string{"from=5&to=5", "from=x&to=9", fmt.Sprintf("from=0&to=%d", 25*3600*1000), "from=-1&to=5", "from=0&to=9223372036854775807", ""} {
		if c := call(mux, "GET", "/api/cam/cam1/rec.m3u8?"+bad, "", "", ck...).Code; c != 400 {
			t.Errorf("%q: %d, muốn 400", bad, c)
		}
	}
}

func TestRecSpansMerge(t *testing.T) {
	mux, db, ck := playMux(t)
	t0 := time.Date(2026, 10, 30, 10, 0, 0, 0, tgICT)
	for _, s := range []int{0, 15, 55} { // mỗi file 10 giây: khoảng cách 5 giây rồi 30 giây
		recAdd(db, recFile{Cam: "c", Path: fmt.Sprint("p", s), Start: t0.Add(time.Duration(s) * time.Second), End: t0.Add(time.Duration(s+10) * time.Second)})
	}
	w := call(mux, "GET", "/api/cam/c/rec?day=2026-10-30", "", "", ck...)
	want := fmt.Sprintf(`{"spans":[[%d,%d],[%d,%d]]}`, t0.UnixMilli(), t0.Add(25*time.Second).UnixMilli(), t0.Add(55*time.Second).UnixMilli(), t0.Add(65*time.Second).UnixMilli())
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Fatalf("%s\nmuốn %s", got, want)
	}
	if got := strings.TrimSpace(call(mux, "GET", "/api/cam/khong/rec?day=2026-10-30", "", "", ck...).Body.String()); got != `{"spans":[]}` {
		t.Errorf("camera lạ: %s", got)
	}
	if got := strings.TrimSpace(call(mux, "GET", "/api/cam/c/rec?day=2026-10-29", "", "", ck...).Body.String()); got != `{"spans":[]}` {
		t.Errorf("ngày khác: %s", got)
	}
	if c := call(mux, "GET", "/api/cam/c/rec?day=hom-nay", "", "", ck...).Code; c != 400 {
		t.Errorf("ngày sai: %d", c)
	}
}

func TestRecPlaybackAuth(t *testing.T) {
	mux, db, _ := playMux(t)
	f, _ := recFx(t, db, "c", time.Now(), uniformGOPs(1, 5, 50))
	for _, u := range []string{"/api/cam/c/rec?day=2026-10-30", "/api/cam/c/rec.m3u8?from=1&to=2", fmt.Sprintf("/api/cam/c/rec/%d.mp4", f.ID)} {
		if c := call(mux, "GET", u, "", "").Code; c != 401 {
			t.Errorf("%s không cookie: %d", u, c)
		}
	}
}

func TestRecFileOtherCamera(t *testing.T) {
	mux, db, ck := playMux(t)
	f, _ := recFx(t, db, "cam_a", time.Now(), uniformGOPs(1, 5, 50))
	if c := call(mux, "GET", fmt.Sprintf("/api/cam/cam_b/rec/%d.mp4", f.ID), "", "", ck...).Code; c != 404 {
		t.Errorf("camera khác: %d", c)
	}
	for _, bad := range []string{"abc.mp4", "999999.mp4", strconv.FormatInt(f.ID, 10)} {
		if c := call(mux, "GET", "/api/cam/cam_a/rec/"+bad, "", "", ck...).Code; c != 404 {
			t.Errorf("%s: %d", bad, c)
		}
	}
	if c := call(mux, "GET", fmt.Sprintf("/api/cam/cam_a/rec/%d.mp4", f.ID), "", "", ck...).Code; c != 200 {
		t.Errorf("đúng camera: %d", c)
	}
}

func TestRecFileRange(t *testing.T) {
	mux, db, ck := playMux(t)
	f, data := recFx(t, db, "c", time.Now(), uniformGOPs(1, 5, 500))
	r := httptest.NewRequest("GET", fmt.Sprintf("/api/cam/c/rec/%d.mp4", f.ID), nil)
	r.Header.Set("Range", "bytes=0-99")
	r.AddCookie(ck[0])
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Body)
	if w.Code != 206 || len(b) != 100 || !bytes.Equal(b, data[:100]) {
		t.Fatalf("%d, %d byte", w.Code, len(b))
	}
}

func TestDashReplayWired(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("web", "dash.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, s := range []string{"data-replay", "Giờ này không có hình", "[data-replay]", "rec.m3u8", "Xem lại chạy trên Safari"} {
		if !strings.Contains(js, s) {
			t.Errorf("dash.js thiếu %q", s)
		}
	}
	i := strings.Index(js, "function liveFill")
	if i < 0 || !strings.Contains(js[i:i+900], "[data-replay]") {
		t.Error("liveFill không giữ [data-replay]")
	}
}

// trunFrame dựng cặp moof+mdat một mẫu, thời lượng khai theo từng nguồn: "tfhd" (default_sample_duration, như go2rtc),
// "trun" (cờ 0x100 từng mẫu) hoặc "trex" (không khai trong moof).
func trunFrame(src string, dts uint64, key bool, dur uint32) []byte {
	flags := uint32(0x01010000)
	if key {
		flags = 0x02000000
	}
	tfhdFl, tfhd, trunFl, trun := uint32(0x020020), u32(1, flags), uint32(1), u32(0)
	switch src {
	case "tfhd":
		tfhdFl, tfhd = 0x020028, u32(1, dur, flags)
	case "trun":
		trunFl, trun = 0x101, u32(0, dur)
	}
	moof := mkBox("moof", mkBox("mfhd", u32(0, 1)), mkBox("traf", mkBox("tfhd", append(u32(tfhdFl), tfhd...)),
		mkBox("tfdt", u32(1<<24), binary.BigEndian.AppendUint64(nil, dts)), mkBox("trun", append(u32(trunFl, 1), trun...))))
	return append(moof, mkBox("mdat", make([]byte, 50))...)
}

func TestRecScanDurationSources(t *testing.T) {
	trexDur := uint32(4500)
	for _, src := range []string{"tfhd", "trun", "trex"} {
		head := fmp4Head("hev1")
		if src == "trex" { // trex: track 1, desc 1, duration 4500, size 0, flags 0
			head = fmp4HeadTrexDur("hev1", trexDur)
		}
		in := head
		for i := 0; i < 6; i++ {
			in = append(in, trunFrame(src, uint64(i)*4500, i%3 == 0, 4500)...)
		}
		p := filepath.Join(t.TempDir(), src+".mp4")
		files, _, err := runSplit(bytes.NewReader(in), 1<<30)
		if err != nil || len(files) != 1 {
			t.Fatal(src, err)
		}
		os.WriteFile(p, files[0], 0o600)
		_, gops, err := recScan(p)
		var sum float64
		for _, g := range gops {
			sum += g.Dur
		}
		if err != nil || len(gops) != 2 || math.Abs(sum-6*4500.0/90000) > 1e-9 {
			t.Errorf("%s: err=%v gops=%d tổng Dur %v, muốn 2 GOP và 0.3", src, err, len(gops), sum)
		}
	}
}

// fmp4HeadTrexDur như fmp4Head, thêm mvex/trex có default_sample_duration.
func fmp4HeadTrexDur(codec string, dur uint32) []byte {
	h := fmp4Head(codec)
	bs := topBoxes(&testing.T{}, h)
	moov := append([]byte(nil), bs[1].b...)
	moov = append(moov, mkBox("mvex", mkBox("trex", u32(0, 1, 1, dur, 0, 0)))...)
	binary.BigEndian.PutUint32(moov, uint32(len(moov)))
	return append(append([]byte(nil), bs[0].b...), moov...)
}

func TestRecScanTruncatedMidGOP(t *testing.T) {
	files, _, _ := runSplit(bytes.NewReader(fakeFMP4("hev1", [][]int{{100, 100, 100}, {100, 100, 100}})), 1<<30)
	p := filepath.Join(t.TempDir(), "a.mp4")
	os.WriteFile(p, files[0][:len(files[0])-10], 0o600) // cụt giữa mdat khung thứ 3 của GOP 2
	_, gops, err := recScan(p)
	last := int64(len(fmp4Frame(1, 0, false, 100, 1)))
	if err != nil || len(gops) != 2 {
		t.Fatalf("err=%v gops=%d", err, len(gops))
	}
	if end := gops[1].Off + gops[1].Len; end != int64(len(files[0]))-last || math.Abs(gops[1].Dur-2*3000.0/90000) > 1e-9 {
		t.Errorf("GOP cuối kết thúc %d (muốn %d), Dur %v (muốn 2 khung)", end, int64(len(files[0]))-last, gops[1].Dur)
	}
}
