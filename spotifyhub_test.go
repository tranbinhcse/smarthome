package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// librespot giả: chính file test này, chạy lại với HUBD_FAKE_LIBRESPOT=1. Cư xử như librespot v0.8.0 ở những chỗ hubd dùng:
// --enable-oauth --oauth-port 0 in "Browse to: <url>" rồi "Provide redirect URL" và đọc địa chỉ trả về từ stdin; đăng nhập
// xong ghi credentials.json vào --system-cache; có thông tin đăng nhập thì ghi PCM S16 44,1 kHz vào --device (FIFO).
// Âm thanh giả là một giá trị không đổi (7777) để test phân biệt với nhạc MP3.
func TestMain(m *testing.M) {
	if os.Getenv("HUBD_FAKE_LIBRESPOT") == "1" {
		fakeLibrespot()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeLibrespot() {
	arg := map[string]string{}
	for i, a := range os.Args {
		if strings.HasPrefix(a, "--") && i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "--") {
			arg[a] = os.Args[i+1]
		} else if strings.HasPrefix(a, "--") {
			arg[a] = "1"
		}
	}
	cred := filepath.Join(arg["--system-cache"], "credentials.json")
	if arg["--enable-oauth"] == "1" {
		fmt.Println("Browse to: https://accounts.spotify.com/authorize?client_id=65b708073fc0480ea92a077233ca87bd&state=s1")
		fmt.Println("Provide redirect URL")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if !strings.Contains(line, "code=") {
			fmt.Fprintln(os.Stderr, "[ERROR librespot] no code")
			os.Exit(1)
		}
		os.MkdirAll(arg["--system-cache"], 0o700)
		os.WriteFile(cred, []byte(`{"username":"thu","auth_type":1}`), 0o600)
	} else if _, err := os.Stat(cred); err != nil {
		fmt.Fprintln(os.Stderr, "[ERROR librespot] Credentials are required if discovery and oauth login are disabled.")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, `[INFO librespot_core::session] Authenticated as 'thu' !`)
	if arg["--name"] != "iNIX Smart" || arg["--backend"] != "pipe" || arg["--disable-discovery"] != "1" {
		fmt.Fprintln(os.Stderr, "[ERROR librespot] tham số sai:", os.Args)
		os.Exit(2)
	}
	secs := 1.0
	fmt.Sscan(os.Getenv("HUBD_FAKE_LIBRESPOT_SECS"), &secs)
	if secs > 0 {
		f, err := os.OpenFile(arg["--device"], os.O_WRONLY, 0)
		if err != nil {
			os.Exit(3)
		}
		buf := make([]byte, 4*441)
		for i := 0; i < len(buf); i += 2 {
			binary.LittleEndian.PutUint16(buf[i:], 7777)
		}
		for n := 0; n < int(secs*100); n++ {
			f.Write(buf)
		}
		f.Close()
	}
	time.Sleep(time.Hour) // giữ tiến trình như librespot đang chờ lệnh (select{} bị Go coi là treo, tiến trình sập)
}

func spotHubMux(t *testing.T, bin string, creds bool, secs string) (*http.ServeMux, *Hub, []*http.Cookie, string, string) {
	t.Helper()
	cache := t.TempDir()
	if creds {
		os.WriteFile(filepath.Join(cache, "credentials.json"), []byte(`{}`), 0o600)
	}
	t.Setenv("HUBD_LIBRESPOT", bin)
	t.Setenv("HUBD_SPOTIFY_CACHE", cache)
	t.Setenv("HUBD_FAKE_LIBRESPOT", "1")
	t.Setenv("HUBD_FAKE_LIBRESPOT_SECS", secs)
	// giữ HUBD_FAKE_LIBRESPOT tới hết test: tiến trình con nào thiếu nó sẽ chạy lại cả bộ test (đệ quy)
	mux, h, ck, addr := musicMux(t, musicLib(t))
	t.Cleanup(h.music.spot.stop)
	return mux, h, ck, addr, cache
}

type hubSpotState struct {
	Available        bool
	LoggedIn         bool `json:"logged_in"`
	State, URL, Name string
}

func hubSpot(t *testing.T, mux http.Handler, ck []*http.Cookie) (s hubSpotState) {
	t.Helper()
	w := call(mux, "GET", "/api/spotify/hub", "", "", ck...)
	json.Unmarshal(w.Body.Bytes(), &s)
	if w.Code != 200 {
		t.Fatalf("GET /api/spotify/hub: %d %s", w.Code, w.Body)
	}
	return
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("chờ quá lâu: %s", what)
}

func TestHubSpotifyUnavailable(t *testing.T) {
	mux, _, ck, _, _ := spotHubMux(t, "/khong-co/librespot", true, "0")
	if w := call(mux, "GET", "/api/spotify/hub", "", ""); w.Code != 401 {
		t.Fatalf("không có PIN: code=%d", w.Code)
	}
	if s := hubSpot(t, mux, ck); s.Available || s.State != "off" {
		t.Fatalf("thiếu librespot phải báo không có: %+v", s)
	}
	if w := call(mux, "POST", "/api/spotify/hub", `{"do":"login"}`, "", ck...); w.Code != 409 {
		t.Fatalf("đăng nhập khi thiếu librespot: code=%d", w.Code)
	}
}

// Đăng nhập một lần qua Cài đặt: hub đưa địa chỉ "Browse to", người dùng dán địa chỉ trang trả về, hub chuyển qua stdin.
func TestHubSpotifyLogin(t *testing.T) {
	mux, _, ck, _, cache := spotHubMux(t, os.Args[0], false, "0")
	if s := hubSpot(t, mux, ck); !s.Available || s.LoggedIn || s.State != "off" {
		t.Fatalf("chưa đăng nhập thì không chạy librespot: %+v", s)
	}
	if w := call(mux, "POST", "/api/spotify/hub", `{"do":"login"}`, ""); w.Code != 401 {
		t.Fatalf("đăng nhập không có PIN: code=%d", w.Code)
	}
	w := call(mux, "POST", "/api/spotify/hub", `{"do":"login"}`, "", ck...)
	var r struct{ URL string }
	json.Unmarshal(w.Body.Bytes(), &r)
	if w.Code != 200 || !strings.HasPrefix(r.URL, "https://accounts.spotify.com/authorize?") {
		t.Fatalf("địa chỉ đăng nhập: code=%d %s", w.Code, w.Body)
	}
	if s := hubSpot(t, mux, ck); s.State != "login" || s.URL != r.URL {
		t.Fatalf("đang chờ đăng nhập: %+v", s)
	}
	// chỉ nhận địa chỉ trả về đúng dạng http://127.0.0.1/login?code=..., không xuống dòng (không chèn thêm lệnh vào stdin)
	for _, u := range []string{"", "https://evil.example/login?code=a", "http://127.0.0.1/login?state=x", "http://127.0.0.1/login?code=a\nrm -rf /",
		"http://127.0.0.1/login?code=" + strings.Repeat("a", 4096)} {
		b, _ := json.Marshal(map[string]string{"do": "code", "url": u})
		if w := call(mux, "POST", "/api/spotify/hub", string(b), "", ck...); w.Code != 400 {
			t.Errorf("địa chỉ %q phải bị từ chối: code=%d", u, w.Code)
		}
	}
	if s := hubSpot(t, mux, ck); s.State != "login" {
		t.Fatalf("địa chỉ sai không được tới librespot: %+v", s)
	}
	if w := call(mux, "POST", "/api/spotify/hub", `{"do":"code","url":"http://127.0.0.1/login?code=ma-thu&state=s1"}`, "", ck...); w.Code != 204 {
		t.Fatalf("dán địa chỉ trả về: code=%d %s", w.Code, w.Body)
	}
	waitFor(t, "librespot đăng nhập xong", func() bool { s := hubSpot(t, mux, ck); return s.State == "running" && s.LoggedIn && s.URL == "" })
	if _, err := os.Stat(filepath.Join(cache, "credentials.json")); err != nil {
		t.Fatal("thông tin đăng nhập phải được lưu cho lần sau")
	}
	if s := hubSpot(t, mux, ck); s.Name != "iNIX Smart" {
		t.Fatalf("tên thiết bị Spotify: %+v", s)
	}
	if w := call(mux, "POST", "/api/spotify/hub", `{"do":"logout"}`, "", ck...); w.Code != 204 {
		t.Fatalf("đăng xuất: code=%d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(cache, "credentials.json")); err == nil {
		t.Fatal("đăng xuất phải xoá thông tin đăng nhập")
	}
	if s := hubSpot(t, mux, ck); s.LoggedIn || s.State != "off" {
		t.Fatalf("sau đăng xuất: %+v", s)
	}
}

func isSpot(pcm []byte) bool {
	return len(pcm) >= 4 && binary.LittleEndian.Uint16(pcm) == 7777 && binary.LittleEndian.Uint16(pcm[len(pcm)-2:]) == 7777
}

// Đã đăng nhập: hubd tự chạy librespot; âm thanh Spotify tới đầu nhận, nhạc MP3 đang phát thì tự dừng.
func TestHubSpotifyAudioToReceivers(t *testing.T) {
	mux, _, ck, addr, _ := spotHubMux(t, os.Args[0], true, "1")
	f := dialSnap(t, addr, "loa")
	f.read()
	f.read()
	waitFor(t, "librespot tự chạy khi đã có thông tin đăng nhập", func() bool { return hubSpot(t, mux, ck).State == "running" })
	pcm := collect(t, f, 1500*time.Millisecond)
	spot := 0
	for i := 0; i+snapFrames*4 <= len(pcm); i += snapFrames * 4 {
		if isSpot(pcm[i : i+snapFrames*4]) {
			spot++
		}
	}
	if spot < 45 { // 1 giây = 50 đoạn 20 ms
		t.Fatalf("âm thanh Spotify tới đầu nhận: %d đoạn", spot)
	}
}

// Người dùng chọn phát MP3 khi Spotify còn đang phát trên hub: MP3 thắng tới khi Spotify ngừng hẳn;
// Spotify phát lại sau đó thì Spotify thắng và MP3 tự dừng.
func TestHubSpotifyAndFilesShareTheStream(t *testing.T) {
	mux, h, ck, addr, _ := spotHubMux(t, os.Args[0], true, "3")
	f := dialSnap(t, addr, "loa")
	f.read()
	f.read()
	waitFor(t, "có tiếng Spotify", func() bool { return h.music.spot.active() })
	if getState(t, mux, ck).Source != "spotify" {
		t.Fatalf("nguồn đang phát phải là spotify: %+v", getState(t, mux, ck))
	}
	cmd(t, mux, ck, `{"do":"play","files":["0/Album/tone44100.mp3"],"index":0}`, 204)
	time.Sleep(300 * time.Millisecond)
	if s := getState(t, mux, ck); !s.Playing || s.Source != "file" {
		t.Fatalf("chọn MP3 thì MP3 phát dù Spotify còn tiếng: %+v", s)
	}
}
