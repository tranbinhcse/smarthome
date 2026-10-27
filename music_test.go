package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Thư mục nhạc thử: hai file MP3 thật (sóng sin 440 Hz, 1 giây, 44,1 kHz và 48 kHz, tạo bằng lame) cùng những thứ
// thường gặp trên USB và không được hiện: file ._ của máy Mac, thư mục ẩn .Trashes, file không phải MP3, symlink ra ngoài.
func musicLib(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	cp := func(src, dst string) {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(filepath.Join(d, dst)), 0o755)
		os.WriteFile(filepath.Join(d, dst), b, 0o644)
	}
	cp("testdata/music/Album/tone44100.mp3", "Album/tone44100.mp3")
	cp("testdata/music/tone48000.mp3", "tone48000.mp3")
	cp("testdata/music/tone48000.mp3", ".Trashes/xoa.mp3")
	os.WriteFile(filepath.Join(d, "._tone48000.mp3"), []byte("Mac resource fork"), 0o644)
	os.WriteFile(filepath.Join(d, "Album/notes.txt"), []byte("ghi chú"), 0o644)
	if err := os.Symlink("/etc/passwd", filepath.Join(d, "escape.mp3")); err != nil {
		t.Fatal(err)
	}
	return d
}

func musicMux(t *testing.T, dirs string) (*http.ServeMux, *Hub, []*http.Cookie, string) {
	t.Helper()
	t.Setenv("HUBD_MUSIC_DIRS", dirs)
	mux, h, _ := assistantMux(t, Config{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go h.music.snap.serve(ln)
	return mux, h, call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies(), ln.Addr().String()
}

type musicState struct {
	Playing bool
	File    *struct{ ID, Title, Folder string }
	Index   int
	Queue   int
	Pos     int `json:"pos_ms"`
	Dur     int `json:"dur_ms"`
	Zones   []struct {
		ID, Name  string
		Connected bool
		Volume    int
		Muted     bool
	}
}

func getState(t *testing.T, mux http.Handler, ck []*http.Cookie) (s musicState) {
	t.Helper()
	w := call(mux, "GET", "/api/music/state", "", "", ck...)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil {
		t.Fatalf("state: code=%d %s", w.Code, w.Body)
	}
	return
}

func cmd(t *testing.T, mux http.Handler, ck []*http.Cookie, body string, want int) {
	t.Helper()
	if w := call(mux, "POST", "/api/music/cmd", body, "", ck...); w.Code != want {
		t.Fatalf("%s: code=%d muốn %d: %s", body, w.Code, want, w.Body)
	}
}

// collect: gom PCM đầu nhận nhận được cho tới khi im quá `idle` (hết bài). Kiểm dấu thời gian tăng đều 20 ms.
func collect(t *testing.T, f *fakeSnapClient, idle time.Duration) (pcm []byte) {
	t.Helper()
	var last int64 = -1
	for {
		f.c.SetReadDeadline(time.Now().Add(idle))
		h := make([]byte, 26)
		if _, err := io.ReadFull(f.c, h); err != nil {
			return pcm
		}
		body := make([]byte, binary.LittleEndian.Uint32(h[22:]))
		if _, err := io.ReadFull(f.c, body); err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint16(h) != 2 {
			continue
		}
		ts := int64(int32(binary.LittleEndian.Uint32(body)))*1e6 + int64(int32(binary.LittleEndian.Uint32(body[4:])))
		if last >= 0 && ts-last != 20000 {
			t.Fatalf("dấu thời gian đoạn nhạc phải cách đều 20 ms: %d → %d", last, ts)
		}
		last = ts
		pcm = append(pcm, body[12:]...)
	}
}

func loud(pcm []byte) bool {
	for i := 0; i+1 < len(pcm); i += 2 {
		if v := int16(binary.LittleEndian.Uint16(pcm[i:])); v > 3000 || v < -3000 {
			return true
		}
	}
	return false
}

func TestMusicLibrary(t *testing.T) {
	lib := musicLib(t)
	mux, _, ck, _ := musicMux(t, lib+":/khong-co-usb")
	for _, u := range []string{"/api/music/library", "/api/music/state"} {
		if w := call(mux, "GET", u, "", ""); w.Code != 401 {
			t.Errorf("%s không có PIN: code=%d", u, w.Code)
		}
	}
	if w := call(mux, "POST", "/api/music/cmd", `{"do":"pause"}`, ""); w.Code != 401 {
		t.Errorf("lệnh không có PIN: code=%d", w.Code)
	}
	w := call(mux, "GET", "/api/music/library", "", "", ck...)
	var out struct {
		Files []struct{ ID, Title, Folder string }
		Roots []struct {
			Path string
			OK   bool
		}
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	got, _ := json.Marshal(out.Files)
	want := `[{"ID":"0/Album/tone44100.mp3","Title":"tone44100","Folder":"Album"},{"ID":"0/tone48000.mp3","Title":"tone48000","Folder":""}]`
	if w.Code != 200 || string(got) != want {
		t.Fatalf("thư viện (bỏ file ._, thư mục ẩn, file không phải MP3, symlink):\n %s\n muốn %s", got, want)
	}
	if len(out.Roots) != 2 || !out.Roots[0].OK || out.Roots[1].OK || out.Roots[1].Path != "/khong-co-usb" {
		t.Fatalf("thư mục nhạc: %+v", out.Roots)
	}
}

// Phát một bài: đầu nhận nhận đủ 1 giây PCM có tiếng, dấu thời gian cách đều; hết bài thì dừng.
func TestMusicPlayToReceiver(t *testing.T) {
	mux, _, ck, addr := musicMux(t, musicLib(t))
	f := dialSnap(t, addr, "loa-cafe")
	f.read()
	f.read()
	cmd(t, mux, ck, `{"do":"play","files":["0/Album/tone44100.mp3"],"index":0}`, 204)
	if s := getState(t, mux, ck); !s.Playing || s.File == nil || s.File.Title != "tone44100" || s.Dur < 950 || s.Dur > 1150 {
		t.Fatalf("đang phát: %+v", s)
	}
	// MP3 giải mã ra dài hơn: frame thông tin của LAME (go-mp3 giải ra im lặng) + độ trễ mã hoá/giải mã + đệm cho tròn
	// frame, tối đa ~3 frame (3456 khung); đoạn cuối đệm im lặng tới 20 ms (882 khung) → trần 48500
	pcm := collect(t, f, 1500*time.Millisecond)
	if n := len(pcm) / 4; n < 44100 || n > 48500 || !loud(pcm) {
		t.Fatalf("1 giây ở 44,1 kHz: nhận %d khung, có tiếng=%v", n, loud(pcm))
	}
	if s := getState(t, mux, ck); s.Playing {
		t.Fatalf("hết hàng đợi phải dừng: %+v", s)
	}
}

// File 48 kHz được đổi về 44,1 kHz của luồng: 1 giây vẫn là ~44100 khung (không đổi thì ~48000).
func TestMusicResample48k(t *testing.T) {
	mux, _, ck, addr := musicMux(t, musicLib(t))
	f := dialSnap(t, addr, "loa")
	f.read()
	f.read()
	cmd(t, mux, ck, `{"do":"play","files":["0/tone48000.mp3"],"index":0}`, 204)
	pcm := collect(t, f, 1500*time.Millisecond)
	if n := len(pcm) / 4; n < 44100 || n > 48500 || !loud(pcm) { // không đổi tần số: trên 50000 khung
		t.Fatalf("1 giây 48 kHz sau khi đổi: %d khung, có tiếng=%v", n, loud(pcm))
	}
}

// Chỉ phát file có trong thư viện: đường dẫn lạ, thoát thư mục, symlink ra ngoài đều bị chặn, không phát gì.
func TestMusicRejectsUnknownFiles(t *testing.T) {
	mux, _, ck, addr := musicMux(t, musicLib(t))
	f := dialSnap(t, addr, "loa")
	f.read()
	f.read()
	many := `"0/tone48000.mp3"` + strings.Repeat(`,"0/tone48000.mp3"`, 2000)
	for _, files := range []string{`"../../etc/passwd"`, `"0/escape.mp3"`, `"0/../tone48000.mp3"`, `"9/x.mp3"`, `"0/.Trashes/xoa.mp3"`, `"0/._tone48000.mp3"`, many} {
		cmd(t, mux, ck, `{"do":"play","files":[`+files+`],"index":0}`, 400)
	}
	cmd(t, mux, ck, `{"do":"play","files":["0/tone48000.mp3"],"index":5}`, 400)
	cmd(t, mux, ck, `{"do":"xoa"}`, 400)
	if pcm := collect(t, f, 300*time.Millisecond); len(pcm) != 0 {
		t.Fatalf("lệnh bị chặn mà vẫn phát %d byte", len(pcm))
	}
}

// Bài trước / sau / dừng / phát tiếp trong hàng đợi.
func TestMusicTransport(t *testing.T) {
	mux, _, ck, _ := musicMux(t, musicLib(t))
	cmd(t, mux, ck, `{"do":"play","files":["0/Album/tone44100.mp3","0/tone48000.mp3"],"index":0}`, 204)
	cmd(t, mux, ck, `{"do":"next"}`, 204)
	if s := getState(t, mux, ck); s.File == nil || s.File.Title != "tone48000" || s.Index != 1 || s.Queue != 2 || !s.Playing {
		t.Fatalf("bài sau: %+v", s)
	}
	cmd(t, mux, ck, `{"do":"prev"}`, 204)
	if s := getState(t, mux, ck); s.File == nil || s.File.Title != "tone44100" {
		t.Fatalf("bài trước: %+v", s)
	}
	cmd(t, mux, ck, `{"do":"pause"}`, 204)
	if s := getState(t, mux, ck); s.Playing {
		t.Fatalf("dừng: %+v", s)
	}
	cmd(t, mux, ck, `{"do":"play"}`, 204)
	if s := getState(t, mux, ck); !s.Playing || s.File.Title != "tone44100" {
		t.Fatalf("phát tiếp: %+v", s)
	}
}

// Khu = đầu nhận: đặt tên, âm lượng, tắt tiếng; lưu lại nên đầu nhận nối lại vẫn đúng mức cũ.
func TestMusicZones(t *testing.T) {
	mux, _, ck, addr := musicMux(t, musicLib(t))
	f := dialSnap(t, addr, "aa:bb:cc")
	f.read()
	f.read()
	for i := 0; i < 100 && len(getState(t, mux, ck).Zones) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	cmd(t, mux, ck, `{"do":"zone","id":"aa:bb:cc","volume":30,"muted":true,"name":"Khu cafe"}`, 204)
	if st := settingsOf(t, f.readType(3)); st.Volume != 30 || !st.Muted {
		t.Fatalf("đầu nhận phải nhận mức mới: %+v", st)
	}
	z := getState(t, mux, ck).Zones
	if len(z) != 1 || z[0].ID != "aa:bb:cc" || z[0].Name != "Khu cafe" || !z[0].Connected || z[0].Volume != 30 || !z[0].Muted {
		t.Fatalf("khu: %+v", z)
	}
	f.c.Close()
	g := dialSnap(t, addr, "aa:bb:cc")
	if st := settingsOf(t, g.read()); st.Volume != 30 || !st.Muted {
		t.Fatalf("nối lại phải giữ mức đã lưu: %+v", st)
	}
	for _, b := range []string{`{"do":"zone","id":"aa:bb:cc","volume":101}`, `{"do":"zone","id":"","volume":5}`, `{"do":"zone","id":"aa:bb:cc","name":"` + strings.Repeat("x", 81) + `"}`} {
		cmd(t, mux, ck, b, 400)
	}
	if !bytes.Contains(call(mux, "GET", "/api/music/state", "", "", ck...).Body.Bytes(), []byte(`"Khu cafe"`)) {
		t.Fatal("lệnh sai không được đổi khu")
	}
}
