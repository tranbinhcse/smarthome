package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Soloist giả: chính file test này, chạy lại qua một script nằm trong gói soloist_release_*.tar.gz mà CDN giả trả về
// (script đặt HUBD_FAKE=soloist: hubd chỉ đưa cho Soloist môi trường tối thiểu). Cư xử như Soloist 1.3.10 ở những chỗ hubd
// dùng: in phiên bản và số ngày còn lại; ghi ws.port vào --data-dir; API WebSocket (auth_state, playback_state, lệnh →
// command_result + sự kiện); ZeroConf HTTP /zc ở cổng ngẫu nhiên trên 0.0.0.0; phát qua PulseAudio, ở đây ghi thẳng vào
// FIFO của module-pipe-sink: vài đoạn toàn số 0 rồi một giá trị không đổi (7777) để test phân biệt với nhạc MP3.
// PulseAudio giả (HUBD_FAKE=pulse) chỉ mở socket rồi chờ.
func TestMain(m *testing.M) {
	switch os.Getenv("HUBD_FAKE") {
	case "soloist":
		fakeSoloist()
		os.Exit(0)
	case "pulse":
		fakePulse()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakePulse() {
	for _, a := range os.Args {
		if i := strings.Index(a, "socket="); i >= 0 {
			if _, err := net.Listen("unix", strings.Fields(a[i+7:])[0]); err != nil {
				os.Exit(3)
			}
		}
	}
	time.Sleep(time.Hour) // select{} bị Go coi là treo, tiến trình sập
}

func fakeSoloist() {
	arg := map[string]string{}
	for i := 1; i+1 < len(os.Args); i += 2 { // mọi tham số hubd dùng đều có giá trị
		arg[os.Args[i]] = os.Args[i+1]
	}
	fail := func(f string, a ...any) { fmt.Fprintf(os.Stderr, "soloist giả: "+f+"\n", a...); os.Exit(2) }
	key := os.Getenv("HUBD_FAKE_KEY")
	if os.Getenv("SOLOIST_API_KEY") != key || arg["-k"] != "@SOLOIST_API_KEY" {
		fail("khoá phải qua biến môi trường SOLOIST_API_KEY, -k chỉ là chữ giữ chỗ")
	}
	for _, a := range os.Args {
		if strings.Contains(a, key) {
			fail("khoá lộ trên dòng lệnh")
		}
	}
	if os.Getenv("HUBD_PIN") != "" || os.Getenv("HUBD_MQTT_PASS") != "" {
		fail("Soloist không được thừa hưởng môi trường của hubd")
	}
	if arg["-n"] != "iNIX Smart" || arg["--ws"] != "127.0.0.1:0" || !strings.HasPrefix(os.Getenv("PULSE_SERVER"), "unix:") {
		fail("tham số sai: %v", os.Args)
	}
	fmt.Println("2026-10-27 10:46:15.929: soloist 1.3.10.19 build 1793080877 (20261027) (gtest) (linux/x86_64)")
	fmt.Println("2026-10-27 10:46:15.929: client expires in 89 days")
	if os.Getenv("HUBD_FAKE_MODE") == "expired" {
		fmt.Println("2026-10-27 10:46:15.930: client expired")
		os.Exit(10)
	}

	zc, _ := net.Listen("tcp4", "0.0.0.0:0")
	go http.Serve(zc, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zc" || r.URL.Query().Get("action") != "getInfo" {
			http.Error(w, "?", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"remoteName": arg["-n"], "status": 101, "deviceType": "SPEAKER"})
	}))

	item := func(name string) map[string]any {
		return map[string]any{"uri": "spotify:track:" + name, "entity_type": "track", "decorations": map[string]any{
			"identity":        map[string]any{"name": name},
			"visual_identity": map[string]any{"cover": []any{map[string]any{"url": "https://i.scdn.co/image/nho", "size": "small"}, map[string]any{"url": "https://i.scdn.co/image/lon", "size": "large"}}},
			"parent":          map[string]any{"entity": map[string]any{"uri": "spotify:album:a", "decorations": map[string]any{"identity": map[string]any{"name": "Album thử"}}}},
			"creators":        []any{map[string]any{"entity": map[string]any{"decorations": map[string]any{"identity": map[string]any{"name": "Ca sĩ thử"}}}}},
			"playback":        map[string]any{"duration_ms": 210000},
		}}
	}
	ms := func() int64 { return time.Now().UnixMilli() }
	ws, _ := net.Listen("tcp", "127.0.0.1:0")
	go http.Serve(ws, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c.WriteJSON(map[string]any{"type": "auth_state", "logged_in": true, "is_active": true, "device_name": arg["-n"]})
		c.WriteJSON(map[string]any{"type": "playback_state", "status": "playing", "item": item("Bài thử"), "volume": 65, "is_active": true,
			"position": map[string]any{"position_ms": 45000, "timestamp_ms": ms(), "speed": 1.0}, "options": map[string]any{"shuffle": false, "repeat": "off"}})
		for {
			var m map[string]any
			if c.ReadJSON(&m) != nil {
				return
			}
			if m["type"] != "command" {
				c.WriteJSON(map[string]any{"type": "error", "message": "unknown message type"})
				continue
			}
			ev := map[string]any{}
			switch m["command"] {
			case "pause", "play":
				st := map[string]string{"pause": "paused", "play": "playing"}[m["command"].(string)]
				ev = map[string]any{"type": "playback_changed", "status": st}
			case "set_volume":
				ev = map[string]any{"type": "volume_changed", "volume": m["volume"]}
			case "seek":
				ev = map[string]any{"type": "position_sync", "position": map[string]any{"position_ms": m["position_ms"], "timestamp_ms": ms(), "speed": 1.0}}
			case "skip_next":
				ev = map[string]any{"type": "track_changed", "item": item("Bài thứ hai")}
			case "skip_prev":
				ev = map[string]any{"type": "track_changed", "item": item("Bài thử")}
			default:
				c.WriteJSON(map[string]any{"type": "error", "message": "unknown command"})
				continue
			}
			c.WriteJSON(map[string]any{"type": "command_result", "command": m["command"]})
			c.WriteJSON(ev)
		}
	}))
	os.WriteFile(filepath.Join(arg["-D"], "ws.addr"), []byte("127.0.0.1"), 0o644)
	os.WriteFile(filepath.Join(arg["-D"], "ws.port"), []byte(strconv.Itoa(ws.Addr().(*net.TCPAddr).Port)), 0o644)
	fmt.Println("2026-10-27 10:46:15.962: running")

	secs := 0.0
	fmt.Sscan(os.Getenv("HUBD_FAKE_SECS"), &secs)
	if secs > 0 {
		f, err := os.OpenFile(filepath.Join(filepath.Dir(strings.TrimPrefix(os.Getenv("PULSE_SERVER"), "unix:")), "audio.fifo"), os.O_WRONLY, 0)
		if err != nil {
			os.Exit(3)
		}
		buf := make([]byte, 4*441) // 10 ms
		f.Write(make([]byte, 5*len(buf)))
		for i := 0; i < len(buf); i += 2 {
			binary.LittleEndian.PutUint16(buf[i:], 7777)
		}
		for n := 0; n < int(secs*100); n++ {
			f.Write(buf)
		}
		f.Close()
	}
	time.Sleep(time.Hour)
}

// fakeCDN: soloist-builds.spotifycdn.com giả. Gói chứa script chạy file test này như Soloist; Last-Modified/304 như CDN thật.
type fakeCDN struct {
	mu   sync.Mutex
	mod  time.Time
	mode string   // HUBD_FAKE_MODE của bản dựng đang phát hành
	ims  []string // If-Modified-Since của từng lần hub hỏi
}

func (c *fakeCDN) publish(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode, c.mod = mode, c.mod.Add(24*time.Hour)
}

func (c *fakeCDN) hits() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.ims...)
}

func (c *fakeCDN) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ims = append(c.ims, r.Header.Get("If-Modified-Since"))
	if t, err := http.ParseTime(r.Header.Get("If-Modified-Since")); err == nil && !c.mod.After(t) {
		w.WriteHeader(304)
		return
	}
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	add := func(name, body string, mode int64) {
		tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	add("CHANGELOG.md", "# Soloist Changelog\n", 0o644)
	add("soloist", fmt.Sprintf("#!/bin/sh\nHUBD_FAKE=soloist HUBD_FAKE_MODE=%s HUBD_FAKE_KEY=%s HUBD_FAKE_SECS=%s exec '%s' \"$@\"\n",
		c.mode, testKey, os.Getenv("HUBD_FAKE_SECS"), os.Args[0]), 0o555)
	tw.Close()
	gz.Close()
	w.Header().Set("Last-Modified", c.mod.UTC().Format(http.TimeFormat))
	w.Write(b.Bytes())
}

const testKey = "sk-THU-bi-mat-8f3a"

// soloistHub: hub thử với CDN giả và PulseAudio giả; secs = số giây Soloist giả phát (0 = không phát).
func soloistHub(t *testing.T, secs string) (*http.ServeMux, *Hub, []*http.Cookie, string, *fakeCDN) {
	t.Helper()
	cdn := &fakeCDN{mod: time.Date(2026, 10, 27, 6, 24, 49, 0, time.UTC)}
	srv := httptest.NewServer(cdn)
	t.Cleanup(srv.Close)
	pulse := filepath.Join(t.TempDir(), "pulseaudio")
	os.WriteFile(pulse, []byte(fmt.Sprintf("#!/bin/sh\nHUBD_FAKE=pulse exec '%s' \"$@\"\n", os.Args[0])), 0o755)
	t.Setenv("HUBD_SOLOIST_URL", srv.URL+"/soloist_release_x86_64.tar.gz")
	t.Setenv("HUBD_SOLOIST_DIR", t.TempDir())
	t.Setenv("HUBD_PULSEAUDIO", pulse)
	t.Setenv("HUBD_SOLOIST_PRELOAD", "")
	t.Setenv("HUBD_FAKE_SECS", secs)
	t.Setenv("HUBD_MQTT_PASS", "mat-khau-mqtt") // Soloist giả thấy biến này = hubd đưa nguyên môi trường của nó cho Soloist
	mux, h, ck, addr := musicMux(t, musicLib(t))
	t.Cleanup(h.music.spot.stop)
	return mux, h, ck, addr, cdn
}

type hubSpotState struct {
	State, Error, Version, Name string
	Days                        int
	KeySet                      bool `json:"key_set"`
	LoggedIn                    bool `json:"logged_in"`
}

func hubSpot(t *testing.T, mux http.Handler, ck []*http.Cookie) (s hubSpotState) {
	t.Helper()
	w := call(mux, "GET", "/api/spotify/hub", "", "", ck...)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil {
		t.Fatalf("GET /api/spotify/hub: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), testKey) {
		t.Fatal("API tình trạng Soloist lộ khoá")
	}
	return
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("chờ quá lâu: %s", what)
}

func putKey(t *testing.T, mux http.Handler, ck []*http.Cookie, v any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"api_key": v})
	if w := call(mux, "PUT", "/api/settings/soloist", string(b), "", ck...); w.Code != 204 {
		t.Fatalf("lưu khoá: %d %s", w.Code, w.Body)
	}
}

// pids: tiến trình Soloist và PulseAudio hubd đang giữ (0 = không có).
func pids(s *hubSpotify) (sol, pa int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sol != nil {
		sol = s.sol.Process.Pid
	}
	if s.pa != nil {
		pa = s.pa.Process.Pid
	}
	return
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Chưa có khoá API: không tải, không chạy gì.
func TestSoloistWaitsForKey(t *testing.T) {
	mux, h, ck, _, cdn := soloistHub(t, "0")
	if w := call(mux, "GET", "/api/spotify/hub", "", ""); w.Code != 401 {
		t.Fatalf("không có PIN: code=%d", w.Code)
	}
	time.Sleep(200 * time.Millisecond)
	if s := hubSpot(t, mux, ck); s.State != "nokey" || s.KeySet {
		t.Fatalf("chưa có khoá: %+v", s)
	}
	if sol, pa := pids(h.music.spot); sol != 0 || pa != 0 || len(cdn.hits()) != 0 {
		t.Fatalf("chưa có khoá mà đã tải/chạy: soloist=%d pulse=%d cdn=%v", sol, pa, cdn.hits())
	}
	if w := call(mux, "POST", "/api/spotify/hub", `{"do":"pause"}`, "", ck...); w.Code != 409 {
		t.Fatalf("lệnh khi Soloist chưa chạy: code=%d", w.Code)
	}
}

// Nhập khoá → hub tải Soloist, chạy với khoá qua biến môi trường (Soloist giả tự thoát nếu khoá ở dòng lệnh), khoá không
// lộ ra API hay log. Cập nhật Soloist: hỏi lại CDN bằng If-Modified-Since. Xoá khoá: dừng Soloist và PulseAudio.
func TestSoloistKeyStaysSecret(t *testing.T) {
	var logs lockedBuf
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	mux, h, ck, _, cdn := soloistHub(t, "0")
	putKey(t, mux, ck, testKey)
	waitFor(t, "Soloist chạy và đăng nhập", func() bool { s := hubSpot(t, mux, ck); return s.State == "running" && s.LoggedIn })
	s := hubSpot(t, mux, ck)
	if !s.KeySet || s.Version != "1.3.10.19" || s.Days != 89 || s.Name != "iNIX Smart" {
		t.Fatalf("tình trạng: %+v", s)
	}
	w := call(mux, "GET", "/api/settings/soloist", "", "", ck...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"api_key_set":true`) || strings.Contains(w.Body.String(), testKey) {
		t.Fatalf("cài đặt soloist chỉ được báo đã có khoá: %d %s", w.Code, w.Body)
	}
	if b := call(mux, "GET", "/api/music/state", "", "", ck...).Body.String(); strings.Contains(b, testKey) {
		t.Fatal("trạng thái nhạc lộ khoá")
	}
	if hs := cdn.hits(); len(hs) != 1 || hs[0] != "" {
		t.Fatalf("lần đầu tải không điều kiện: %q", hs)
	}

	sol, _ := pids(h.music.spot)
	if w := call(mux, "POST", "/api/spotify/hub", `{"do":"update"}`, "", ck...); w.Code != 202 {
		t.Fatalf("Cập nhật Soloist: code=%d", w.Code)
	}
	waitFor(t, "chạy lại sau khi cập nhật", func() bool {
		n, _ := pids(h.music.spot)
		return n != 0 && n != sol && hubSpot(t, mux, ck).State == "running"
	})
	if hs := cdn.hits(); len(hs) != 2 || hs[1] != "Tue, 27 Oct 2026 06:24:49 GMT" {
		t.Fatalf("cập nhật phải hỏi CDN bằng If-Modified-Since theo Last-Modified lần trước: %q", hs)
	}

	sol, pa := pids(h.music.spot)
	putKey(t, mux, ck, nil)
	waitFor(t, "xoá khoá thì dừng", func() bool { return hubSpot(t, mux, ck).State == "nokey" })
	waitFor(t, "Soloist và PulseAudio thoát", func() bool {
		return syscall.Kill(sol, 0) != nil && syscall.Kill(pa, 0) != nil
	})
	if strings.Contains(logs.String(), testKey) {
		t.Fatal("log lộ khoá")
	}
	if !strings.Contains(logs.String(), "soloist: ") {
		t.Fatalf("log của Soloist phải vào log hubd: %s", logs.String())
	}
}

// Bản dựng hết hạn (thoát mã 10): báo "expired"; Spotify có bản mới thì bấm Cập nhật Soloist là chạy lại.
func TestSoloistExpired(t *testing.T) {
	mux, _, ck, _, cdn := soloistHub(t, "0")
	cdn.publish("expired")
	putKey(t, mux, ck, testKey)
	waitFor(t, "báo hết hạn", func() bool { return hubSpot(t, mux, ck).State == "expired" })
	cdn.publish("")
	call(mux, "POST", "/api/spotify/hub", `{"do":"update"}`, "", ck...)
	waitFor(t, "chạy bản mới", func() bool { return hubSpot(t, mux, ck).State == "running" })
}

type hubSpotNow struct {
	Track, Artist, Album, Art, Status string
	Volume                            int
	Dur                               int64 `json:"dur_ms"`
	Pos                               int64 `json:"pos_ms"`
}

func spotNow(t *testing.T, mux http.Handler, ck []*http.Cookie) (n hubSpotNow) {
	t.Helper()
	var s struct{ Spotify *hubSpotNow }
	w := call(mux, "GET", "/api/music/state", "", "", ck...)
	if json.Unmarshal(w.Body.Bytes(), &s) != nil {
		t.Fatalf("state: %s", w.Body)
	}
	if s.Spotify != nil {
		n = *s.Spotify
	}
	return
}

// Trạng thái và lệnh qua WebSocket của Soloist; ghép đôi qua /zc của hubd.
func TestSoloistControl(t *testing.T) {
	mux, _, ck, _, _ := soloistHub(t, "0")
	if w := call(mux, "GET", "/zc?action=getInfo&version=2.10.0", "", ""); w.Code != 503 {
		t.Fatalf("/zc khi Soloist chưa chạy: code=%d", w.Code)
	}
	putKey(t, mux, ck, testKey)
	waitFor(t, "có bài đang phát", func() bool { return spotNow(t, mux, ck).Track != "" })
	n := spotNow(t, mux, ck)
	if n.Track != "Bài thử" || n.Artist != "Ca sĩ thử" || n.Album != "Album thử" || n.Art != "https://i.scdn.co/image/lon" ||
		n.Status != "playing" || n.Volume != 65 || n.Dur != 210000 || n.Pos < 45000 {
		t.Fatalf("trạng thái Spotify trên hub: %+v", n)
	}
	// app Spotify trong LAN gọi ZeroConf qua hubd (không có PIN, như loa Spotify Connect thật)
	w := call(mux, "GET", "/zc?action=getInfo&version=2.10.0", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remoteName":"iNIX Smart"`) {
		t.Fatalf("ZeroConf qua hubd: %d %s", w.Code, w.Body)
	}
	if w := call(mux, "POST", "/api/spotify/hub", `{"do":"pause"}`, ""); w.Code != 401 {
		t.Fatalf("lệnh không có PIN: code=%d", w.Code)
	}
	for _, c := range []struct {
		body string
		ok   func(hubSpotNow) bool
	}{
		{`{"do":"pause"}`, func(n hubSpotNow) bool { return n.Status == "paused" }},
		{`{"do":"play"}`, func(n hubSpotNow) bool { return n.Status == "playing" }},
		{`{"do":"volume","volume":30}`, func(n hubSpotNow) bool { return n.Volume == 30 }},
		{`{"do":"seek","position_ms":120000}`, func(n hubSpotNow) bool { return n.Pos >= 120000 }},
		{`{"do":"next"}`, func(n hubSpotNow) bool { return n.Track == "Bài thứ hai" }},
		{`{"do":"prev"}`, func(n hubSpotNow) bool { return n.Track == "Bài thử" }},
	} {
		if w := call(mux, "POST", "/api/spotify/hub", c.body, "", ck...); w.Code != 204 {
			t.Fatalf("%s: code=%d %s", c.body, w.Code, w.Body)
		}
		waitFor(t, c.body, func() bool { return c.ok(spotNow(t, mux, ck)) })
	}
	for _, b := range []string{`{"do":"volume","volume":101}`, `{"do":"volume"}`, `{"do":"seek","position_ms":-1}`, `{"do":"rm"}`, `x`} {
		if w := call(mux, "POST", "/api/spotify/hub", b, "", ck...); w.Code != 400 {
			t.Errorf("%s phải bị từ chối: code=%d", b, w.Code)
		}
	}
}

func isSpot(pcm []byte) bool {
	return len(pcm) >= 4 && binary.LittleEndian.Uint16(pcm) == 7777 && binary.LittleEndian.Uint16(pcm[len(pcm)-2:]) == 7777
}

// Đoạn toàn số 0 (sink PulseAudio đang im) không tính là Spotify đang phát.
func TestSoloistSilenceIsNotPlaying(t *testing.T) {
	s := &hubSpotify{pcm: make(chan []byte, 2)}
	s.pcm <- make([]byte, snapFrames*4)
	if s.take() != nil || s.active() {
		t.Fatal("đoạn toàn số 0 không phải tiếng Spotify")
	}
	b := make([]byte, snapFrames*4)
	b[7] = 1
	s.pcm <- b
	if s.take() == nil || !s.active() {
		t.Fatal("đoạn có tiếng phải tới các khu")
	}
}

// Đã có khoá: âm thanh Soloist (qua FIFO của PulseAudio) tới đầu nhận.
func TestSoloistAudioToReceivers(t *testing.T) {
	mux, _, ck, addr, _ := soloistHub(t, "1")
	f := dialSnap(t, addr, "loa")
	f.read()
	f.read()
	putKey(t, mux, ck, testKey)
	waitFor(t, "Soloist chạy", func() bool { return hubSpot(t, mux, ck).State == "running" })
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

// Người dùng chọn phát MP3 khi Spotify còn đang phát trên hub: MP3 thắng tới khi Spotify ngừng hẳn.
func TestHubSpotifyAndFilesShareTheStream(t *testing.T) {
	mux, h, ck, addr, _ := soloistHub(t, "3")
	f := dialSnap(t, addr, "loa")
	f.read()
	f.read()
	putKey(t, mux, ck, testKey)
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

// Máy Mac mất tiếng hẳn khi Spotify vẫn phát (2026-10-27): bấm phát ở trình phát của hub lúc Spotify đang phát qua hub mà
// lệnh không phát được MP3 (chưa chọn bài → 409) vẫn đặt cờ "MP3 thắng", nên tiếng Spotify bị bỏ trong khi MP3 không chạy:
// mọi khu im tới khi Spotify ngừng. "MP3 thắng" chỉ có tác dụng khi MP3 thật sự đang phát; MP3 hết bài thì Spotify ra lại.
func TestSpotifyNotSilencedWhenFilesAreNotPlaying(t *testing.T) {
	mux, h, ck, addr, _ := soloistHub(t, "3")
	f := dialSnap(t, addr, "loa")
	f.read()
	f.read()
	putKey(t, mux, ck, testKey)
	waitFor(t, "có tiếng Spotify", func() bool { return h.music.spot.active() })
	cmd(t, mux, ck, `{"do":"play"}`, 409) // chưa chọn bài
	time.Sleep(300 * time.Millisecond)
	if s := getState(t, mux, ck); s.Source != "spotify" {
		t.Fatalf("lệnh phát hỏng không được làm im tiếng Spotify: %+v", s)
	}
	cmd(t, mux, ck, `{"do":"play","files":["0/tone48000.mp3"],"index":0}`, 204)
	waitFor(t, "MP3 thắng khi đang phát", func() bool { return getState(t, mux, ck).Source == "file" })
	waitFor(t, "MP3 hết bài thì Spotify ra lại", func() bool { return getState(t, mux, ck).Source == "spotify" })
}
