package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// NAS giả: các chia sẻ là thư mục con của root (thêm IPC$ như máy thật để kiểm việc lọc chia sẻ hệ thống).
// Ghi lại cấu hình mỗi lần nối để kiểm mật khẩu đã lưu được dùng mà không đi qua trình duyệt.
type fakeNAS struct {
	root string
	seen *[]nasCfg
}

func (f fakeNAS) shares() ([]string, error) {
	es, _ := os.ReadDir(f.root)
	out := []string{"IPC$"}
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out, nil
}
func (f fakeNAS) fsys(share, dir string) (fs.FS, error) {
	p := filepath.Join(f.root, share, filepath.FromSlash(dir))
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		return nil, errors.New("không có thư mục")
	}
	return os.DirFS(p), nil
}
func (f fakeNAS) close() {}

func withFakeNAS(t *testing.T, down bool) (string, *[]nasCfg) {
	t.Helper()
	root := t.TempDir()
	b, _ := os.ReadFile("testdata/music/Album/tone44100.mp3")
	for _, p := range []string{"Music/Nhạc Trịnh/Diễm xưa.mp3", "Music/Nhạc Trịnh/Hạ trắng.mp3", "Music/Acoustic/Nàng thơ.mp3", "Photos/a.mp3"} {
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		os.WriteFile(filepath.Join(root, p), b, 0o644)
	}
	seen := &[]nasCfg{}
	old := nasDial
	nasDial = func(c nasCfg) (nasConn, error) {
		*seen = append(*seen, c)
		if down {
			return nil, errors.New("dial tcp 192.168.1.50:445: connection refused")
		}
		return fakeNAS{root, seen}, nil
	}
	t.Cleanup(func() { nasDial = old })
	return root, seen
}

// Chọn thư mục nhạc trên NAS: liệt kê chia sẻ (bỏ IPC$, ADMIN$...), rồi thư mục con; mật khẩu đã lưu dùng ở hub,
// không đi qua trình duyệt; địa chỉ, chia sẻ, đường dẫn lạ bị chặn trước khi nối.
func TestNASBrowse(t *testing.T) {
	_, seen := withFakeNAS(t, false)
	mux, _, ck, _ := musicMux(t, musicLib(t))
	if w := call(mux, "POST", "/api/music/nas", `{"host":"nas.local"}`, ""); w.Code != 401 {
		t.Fatalf("không có PIN: code=%d", w.Code)
	}
	if w := call(mux, "PUT", "/api/settings/music", `{"nas_host":"nas.local","nas_user":"bình","nas_pass":"mk-bi-mat"}`, "", ck...); w.Code != 204 {
		t.Fatalf("lưu cấu hình NAS: %d", w.Code)
	}
	w := call(mux, "POST", "/api/music/nas", `{"host":"nas.local","user":"bình","pass":""}`, "", ck...)
	var r struct {
		Shares, Dirs []string
		MP3          int
	}
	json.Unmarshal(w.Body.Bytes(), &r)
	if w.Code != 200 || strings.Join(r.Shares, ",") != "Music,Photos" || strings.Contains(w.Body.String(), "mk-bi-mat") {
		t.Fatalf("danh sách chia sẻ: code=%d %s", w.Code, w.Body)
	}
	if last := (*seen)[len(*seen)-1]; last.Pass != "mk-bi-mat" || last.User != "bình" {
		t.Fatalf("để trống mật khẩu phải dùng mật khẩu đã lưu: %+v", last)
	}
	w = call(mux, "POST", "/api/music/nas", `{"host":"nas.local","user":"bình","share":"Music","path":""}`, "", ck...)
	json.Unmarshal(w.Body.Bytes(), &r)
	if w.Code != 200 || strings.Join(r.Dirs, ",") != "Acoustic,Nhạc Trịnh" {
		t.Fatalf("thư mục trong chia sẻ: code=%d %s", w.Code, w.Body)
	}
	w = call(mux, "POST", "/api/music/nas", `{"host":"nas.local","user":"bình","share":"Music","path":"Nhạc Trịnh"}`, "", ck...)
	r = struct {
		Shares, Dirs []string
		MP3          int
	}{}
	json.Unmarshal(w.Body.Bytes(), &r)
	if w.Code != 200 || len(r.Dirs) != 0 || r.MP3 != 2 {
		t.Fatalf("thư mục nhạc: code=%d %s", w.Code, w.Body)
	}
	n := len(*seen)
	for _, b := range []string{`{"host":""}`, `{"host":"nas.local:139"}`, `{"host":"a;rm -rf /"}`, `{"host":"nas.local","share":"../x"}`,
		`{"host":"nas.local","share":"Music","path":"../.."}`, `{"host":"nas.local","share":"Music","path":"/etc"}`} {
		if w := call(mux, "POST", "/api/music/nas", b, "", ck...); w.Code != 400 {
			t.Errorf("%s phải bị từ chối: code=%d", b, w.Code)
		}
	}
	if len(*seen) != n {
		t.Fatalf("yêu cầu sai vẫn nối tới NAS: %v", (*seen)[n:])
	}
}

// Thư mục đã chọn trên NAS là một nguồn trong thư viện, phát được như file trên hub.
func TestNASLibraryAndPlay(t *testing.T) {
	withFakeNAS(t, false)
	mux, _, ck, addr := musicMux(t, musicLib(t))
	call(mux, "PUT", "/api/settings/music", `{"nas_host":"nas.local","nas_user":"bình","nas_pass":"mk","nas_share":"Music","nas_path":"Nhạc Trịnh"}`, "", ck...)
	w := call(mux, "GET", "/api/music/library?rescan=1", "", "", ck...)
	var out struct {
		Files []struct{ ID, Title, Folder string }
		Roots []struct {
			Path string
			OK   bool
		}
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	ids := []string{}
	for _, f := range out.Files {
		ids = append(ids, f.ID)
	}
	if !strings.Contains(strings.Join(ids, ","), "1/Diễm xưa.mp3,1/Hạ trắng.mp3") || len(out.Roots) != 2 || out.Roots[1].Path != "NAS //nas.local/Music/Nhạc Trịnh" || !out.Roots[1].OK {
		t.Fatalf("thư viện có thư mục NAS: %s", w.Body)
	}
	f := dialSnap(t, addr, "loa")
	f.read()
	f.read()
	cmd(t, mux, ck, `{"do":"play","files":["1/Diễm xưa.mp3"],"index":0}`, 204)
	if pcm := collect(t, f, 1500*time.Millisecond); len(pcm)/4 < 44100 || !loud(pcm) {
		t.Fatalf("phát file trên NAS: %d khung", len(pcm)/4)
	}
}

// NAS tắt hoặc sai mật khẩu: thư viện vẫn có file trên hub, thư mục NAS báo lỗi, không làm hỏng gì.
func TestNASDown(t *testing.T) {
	withFakeNAS(t, true)
	mux, _, ck, _ := musicMux(t, musicLib(t))
	call(mux, "PUT", "/api/settings/music", `{"nas_host":"192.168.1.50","nas_user":"u","nas_pass":"mk","nas_share":"Music","nas_path":""}`, "", ck...)
	w := call(mux, "GET", "/api/music/library?rescan=1", "", "", ck...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"0/tone48000.mp3"`) || !strings.Contains(w.Body.String(), "connection refused") || strings.Contains(w.Body.String(), `"mk"`) {
		t.Fatalf("NAS tắt: %s", w.Body)
	}
	if w := call(mux, "POST", "/api/music/nas", `{"host":"192.168.1.50","user":"u"}`, "", ck...); w.Code != 502 {
		t.Fatalf("duyệt NAS tắt: code=%d", w.Code)
	}
}
