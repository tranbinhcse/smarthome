package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Spotify giả: một máy chủ thử đóng cả accounts.spotify.com (/authorize, /api/token) lẫn api.spotify.com (/v1).
// Test không bao giờ gọi Spotify thật: spotifyMux trỏ hubd vào máy giả qua SPOTIFY_ACCOUNTS_URL / SPOTIFY_API_URL.
type fakeSpotify struct {
	*httptest.Server
	mu        sync.Mutex
	challenge string   // code_challenge trình duyệt mang tới /authorize (test chép từ Location sang)
	grants    []string // grant_type đã nhận ở /api/token
	calls     []string // "METHOD /v1/path?query body" tới Web API
	access    string   // access token đang hợp lệ
	refresh   string   // refresh token đang hợp lệ
	issued    []string // mọi token đã cấp: trình duyệt không được thấy cái nào
	n         int
	expire    bool // trả 401 cho lần gọi Web API kế tiếp (token hết hạn sớm)
	free      bool // tài khoản không Premium
	noDevice  bool // không có thiết bị nào đang mở Spotify
	idle      bool // /me/player trả 204: không phát gì
	oldScope  bool // cấp scope như bản đầu (chưa có quyền xem playlist, bài đã thích): cần Kết nối lại
}

const (
	spCID      = "cid-thu"
	spRedirect = "https://hub.test:8443/api/spotify/callback"
	spHub      = "https://hub.test:8443"
	// mã Spotify thật có dạng base62 22 ký tự
	spT1, spT2  = "4uLU6hMCjMI75M1A2tKUQC", "7ouMYWpwJ422jRcDASZB7P"
	spPlOwn     = "37i9dQZF1DXcBWIGoYBM5M" // playlist tự tạo: xem được bài
	spPlOther   = "37i9dQZF1DX0XUsuxWHRQd" // playlist theo dõi của người khác: Spotify trả 403 khi xem bài
	spAlbum     = "6akEvsycLGftJxYudPjmqK"
	spArtist    = "0TnOYISbd1XYRBk9myaseg"
	spOldScope  = "user-read-playback-state user-modify-playback-state user-read-currently-playing"
	spFullScope = spOldScope + " playlist-read-private playlist-read-collaborative user-library-read"
)

// Bài hát kiểu Spotify (kèm trường thừa mà hubd không được chuyển ra trình duyệt: href, available_markets...).
func spTrackJSON(id, name string) string {
	return fmt.Sprintf(`{"uri":"spotify:track:%s","id":"%s","name":"%s","duration_ms":200000,"href":"https://api/x","available_markets":["VN"],
		"artists":[{"name":"Lê Hiếu","href":"https://api/a"},{"name":"Soobin"}],
		"album":{"name":"Album thử","images":[{"url":"https://i.scdn.co/640","width":640},{"url":"https://i.scdn.co/300","width":300},{"url":"https://i.scdn.co/64","width":64}]}}`, id, id, name)
}

// Các lệnh điều khiển phát theo tài liệu Spotify Web API (oracle độc lập với bảng spotifyCmds của hubd).
var spEndpoints = map[string]bool{
	"PUT /v1/me/player/play": true, "PUT /v1/me/player/pause": true, "POST /v1/me/player/next": true,
	"POST /v1/me/player/previous": true, "PUT /v1/me/player/volume": true, "PUT /v1/me/player": true,
	"POST /v1/me/player/queue": true, "PUT /v1/me/player/seek": true, "PUT /v1/me/player/shuffle": true,
}

func newFakeSpotify(t *testing.T) *fakeSpotify {
	f := &fakeSpotify{}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeSpotify) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/api/token" {
		f.token(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.access || f.access == "" || f.expire {
		f.expire = false
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"status":401,"message":"The access token expired"}}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	ep := r.Method + " " + r.URL.Path
	f.calls = append(f.calls, strings.TrimSpace(ep+"?"+r.URL.RawQuery+" "+string(body)))
	switch {
	case ep == "GET /v1/me/player" && f.idle:
		w.WriteHeader(204)
	case ep == "GET /v1/me/player":
		io.WriteString(w, `{"is_playing":true,"progress_ms":1000,"shuffle_state":true,"context":{"uri":"spotify:playlist:`+spPlOwn+`","href":"https://api/p"},
			"device":{"id":"dev-cafe","name":"Khu cafe","type":"Speaker","volume_percent":40,"supports_volume":true,"is_active":true},
			"item":{"uri":"spotify:track:`+spT1+`","name":"Ngày Mai Em Đi","duration_ms":200000,"artists":[{"name":"Lê Hiếu"},{"name":"Soobin"}],
			"album":{"name":"A","images":[{"url":"https://i.scdn.co/640","width":640},{"url":"https://i.scdn.co/300","width":300},{"url":"https://i.scdn.co/64","width":64}]}}}`)
	case ep == "GET /v1/me/player/devices":
		io.WriteString(w, `{"devices":[{"id":"dev-cafe","is_active":true,"name":"Khu cafe","type":"Speaker","volume_percent":40,"supports_volume":true},
			{"id":"dev-ngu1","is_active":false,"name":"Phòng ngủ 1","type":"Speaker","volume_percent":null,"supports_volume":false}]}`)
	case strings.HasPrefix(ep, "GET /v1/") && f.browse(w, r, ep):
	case !spEndpoints[ep]:
		w.WriteHeader(404)
		io.WriteString(w, `{"error":{"status":404,"message":"Service not found"}}`)
	case f.free:
		w.WriteHeader(403)
		io.WriteString(w, `{"error":{"status":403,"message":"Player command failed: Premium required","reason":"PREMIUM_REQUIRED"}}`)
	case f.noDevice:
		w.WriteHeader(404)
		io.WriteString(w, `{"error":{"status":404,"message":"Player command failed: No active device found","reason":"NO_ACTIVE_DEVICE"}}`)
	default:
		w.WriteHeader(204)
	}
}

// browse: tìm kiếm, playlist, album, bài đã thích, hàng đợi. Trả false nếu không phải các đường này.
func (f *fakeSpotify) browse(w http.ResponseWriter, r *http.Request, ep string) bool {
	q := r.URL.Query()
	scope := func() bool {
		if f.oldScope {
			w.WriteHeader(403)
			io.WriteString(w, `{"error":{"status":403,"message":"Insufficient client scope"}}`)
		}
		return f.oldScope
	}
	switch ep {
	case "GET /v1/search":
		// Spotify từ 02/2026: tối đa 10 kết quả mỗi loại
		if n, err := strconv.Atoi(q.Get("limit")); q.Get("q") == "" || err != nil || n < 1 || n > 10 || q.Get("type") != "track,playlist,album,artist" {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"status":400,"message":"Invalid limit"}}`)
			return true
		}
		io.WriteString(w, `{"tracks":{"href":"https://api/s","items":[`+spTrackJSON(spT1, "Ngày Mai Em Đi")+`]},
			"playlists":{"items":[null,{"uri":"spotify:playlist:`+spPlOther+`","name":"Nhạc chill","owner":{"display_name":"Spotify"},"images":[{"url":"https://i.scdn.co/p300","width":300}],"items":{"total":80},"external_urls":{"spotify":"x"}}]},
			"albums":{"items":[{"uri":"spotify:album:`+spAlbum+`","name":"Album thử","artists":[{"name":"Lê Hiếu"}],"images":[{"url":"https://i.scdn.co/a300","width":300}]}]},
			"artists":{"items":[{"uri":"spotify:artist:`+spArtist+`","name":"Soobin","images":[]}]}}`)
	case "GET /v1/me/playlists":
		if !scope() {
			io.WriteString(w, `{"items":[{"uri":"spotify:playlist:`+spPlOwn+`","name":"Nhạc quán sáng","owner":{"display_name":"Bình"},"images":[],"items":{"total":2},"tracks":{"total":2}},
				{"uri":"spotify:playlist:`+spPlOther+`","name":"Nhạc chill","owner":{"display_name":"Spotify"},"images":[{"url":"https://i.scdn.co/p300","width":300}],"items":{"total":80}}]}`)
		}
	case "GET /v1/playlists/" + spPlOwn + "/items":
		io.WriteString(w, `{"total":4,"items":[{"added_at":"x","is_local":false,"item":`+spTrackJSON(spT1, "Bài một")+`},{"is_local":false,"item":null},
			{"is_local":true,"item":{"uri":"spotify:local:a:b:c:1","name":"File trên máy"}},{"is_local":false,"item":`+spTrackJSON(spT2, "Bài hai")+`}]}`)
	case "GET /v1/playlists/" + spPlOther + "/items":
		w.WriteHeader(403)
		io.WriteString(w, `{"error":{"status":403,"message":"Forbidden"}}`)
	case "GET /v1/albums/" + spAlbum:
		io.WriteString(w, `{"uri":"spotify:album:`+spAlbum+`","name":"Album thử","images":[{"url":"https://i.scdn.co/a300","width":300}],
			"tracks":{"items":[{"uri":"spotify:track:`+spT1+`","name":"Bài một","duration_ms":1000,"artists":[{"name":"Lê Hiếu"}]}]}}`)
	case "GET /v1/me/tracks":
		if !scope() {
			io.WriteString(w, `{"items":[{"added_at":"x","track":`+spTrackJSON(spT2, "Bài đã thích")+`}]}`)
		}
	case "GET /v1/me/player/queue":
		io.WriteString(w, `{"currently_playing":`+spTrackJSON(spT1, "Đang phát")+`,"queue":[`+spTrackJSON(spT2, "Kế tiếp")+`,`+spTrackJSON(spT1, "Sau nữa")+`]}`)
	default:
		return false
	}
	return true
}

func (f *fakeSpotify) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	g := r.PostForm.Get("grant_type")
	f.grants = append(f.grants, g)
	bad := r.PostForm.Get("client_id") != spCID
	switch g {
	case "authorization_code":
		v := r.PostForm.Get("code_verifier")
		sum := sha256.Sum256([]byte(v))
		bad = bad || r.PostForm.Get("code") != "ma-thu" || r.PostForm.Get("redirect_uri") != spRedirect ||
			len(v) < 43 || len(v) > 128 || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge
	case "refresh_token":
		bad = bad || r.PostForm.Get("refresh_token") != f.refresh
	default:
		bad = true
	}
	if bad {
		w.WriteHeader(400)
		io.WriteString(w, `{"error":"invalid_grant","error_description":"Invalid refresh token"}`)
		return
	}
	f.n++
	f.access, f.refresh = fmt.Sprintf("at-bi-mat-%d", f.n), fmt.Sprintf("rt-bi-mat-%d", f.n)
	f.issued = append(f.issued, f.access, f.refresh)
	json.NewEncoder(w).Encode(map[string]any{"access_token": f.access, "token_type": "Bearer", "expires_in": 3600,
		"refresh_token": f.refresh, "scope": map[bool]string{false: spFullScope, true: spOldScope}[f.oldScope]})
}

func (f *fakeSpotify) snapshot() (grants, calls []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.grants...), append([]string(nil), f.calls...)
}

func spotifyMux(t *testing.T, f *fakeSpotify) (*http.ServeMux, *Hub, []*http.Cookie) {
	t.Helper()
	t.Setenv("SPOTIFY_ACCOUNTS_URL", f.URL)
	t.Setenv("SPOTIFY_API_URL", f.URL+"/v1")
	mux, h, _ := assistantMux(t, Config{})
	return mux, h, call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
}

// Đã kết nối sẵn: có client_id và refresh token còn hiệu lực ở máy giả.
func spotifyConnected(t *testing.T) (*fakeSpotify, *http.ServeMux, *Hub, []*http.Cookie) {
	f := newFakeSpotify(t)
	mux, h, ck := spotifyMux(t, f)
	f.refresh = "rt-bi-mat-0"
	f.issued = append(f.issued, f.refresh)
	h.db.PutSetting("spotify", map[string]any{"client_id": spCID, "refresh_token": f.refresh})
	return f, mux, h, ck
}

// Trình duyệt không bao giờ thấy access/refresh token: kiểm mọi phản hồi gửi về trình duyệt, cả header.
func noTokens(t *testing.T, f *fakeSpotify, w *httptest.ResponseRecorder) {
	t.Helper()
	all := w.Body.String()
	for k, v := range w.Header() {
		all += "\n" + k + ": " + strings.Join(v, ",")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, tok := range f.issued {
		if strings.Contains(all, tok) {
			t.Fatalf("token %q lộ ra trình duyệt: %s", tok, all)
		}
	}
}

func TestSpotifyLoginPKCE(t *testing.T) {
	f := newFakeSpotify(t)
	mux, h, ck := spotifyMux(t, f)
	login := func(base string, ck ...*http.Cookie) *httptest.ResponseRecorder {
		return call(mux, "POST", base+"/api/spotify/login", "", "", ck...)
	}
	if w := login(spHub); w.Code != 401 {
		t.Fatalf("chưa nhập PIN mà đăng nhập Spotify được: code=%d", w.Code)
	}
	if w := login(spHub, ck...); w.Code != 400 || !strings.Contains(w.Body.String(), "Client ID") {
		t.Fatalf("chưa nhập Client ID phải báo: code=%d %s", w.Code, w.Body)
	}
	h.db.PutSetting("spotify", map[string]any{"client_id": spCID})
	// Spotify chỉ nhận redirect URI HTTPS (trừ 127.0.0.1): hub chưa bật HTTPS thì phải nói rõ.
	t.Setenv("HUBD_TLS_CERT", "")
	if w := login("http://hub.test:8080", ck...); w.Code != 400 || !strings.Contains(w.Body.String(), "HTTPS") {
		t.Fatalf("hub chưa bật HTTPS phải báo: code=%d %s", w.Code, w.Body)
	}
	// Đã bật HTTPS (chung cổng 8443 với micro trợ lý): bấm từ trang HTTP 8080 vẫn được, Spotify trả về cổng 8443.
	t.Setenv("HUBD_TLS_CERT", "/tls/hub.crt")
	var urls []string
	for _, base := range []string{"http://hub.test:8080", spHub} {
		w := login(base, ck...)
		var b struct{ URL string }
		json.Unmarshal(w.Body.Bytes(), &b)
		loc, _ := url.Parse(b.URL)
		q := loc.Query()
		if w.Code != 200 || !strings.HasPrefix(b.URL, f.URL+"/authorize?") || q.Get("client_id") != spCID ||
			q.Get("response_type") != "code" || q.Get("redirect_uri") != spRedirect || q.Get("code_challenge_method") != "S256" ||
			q.Get("code_challenge") == "" || q.Get("state") == "" ||
			q.Get("scope") != spFullScope {
			t.Fatalf("từ %s: địa chỉ đăng nhập Spotify sai: code=%d %s", base, w.Code, w.Body)
		}
		urls = append(urls, b.URL)
	}
	loc, _ := url.Parse(urls[1])
	q := loc.Query()
	f.challenge = q.Get("code_challenge")
	cb := spHub + "/api/spotify/callback?code=ma-thu&state="

	if w := call(mux, "GET", cb+"state-gia", "", ""); w.Code != 400 {
		t.Fatalf("state lạ phải bị từ chối: code=%d", w.Code)
	}
	if g, _ := f.snapshot(); len(g) != 0 {
		t.Fatalf("state sai mà vẫn đổi mã lấy token: %v", g)
	}
	// Callback không cần cookie PIN: app trên màn hình chính iPad có kho cookie riêng, trang Spotify mở ở trình
	// duyệt khác. state (ngẫu nhiên, một lần, chỉ cấp sau PIN) là thứ xác thực lượt đăng nhập.
	w := call(mux, "GET", cb+url.QueryEscape(q.Get("state")), "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Đã kết nối Spotify") {
		t.Fatalf("đăng nhập xong phải báo đã kết nối: code=%d %s", w.Code, w.Body)
	}
	noTokens(t, f, w)
	if rt := h.db.Setting("spotify")["refresh_token"]; rt != "rt-bi-mat-1" {
		t.Fatalf("refresh token phải lưu ở hub: %v", rt)
	}
	if w := call(mux, "GET", cb+url.QueryEscape(q.Get("state")), "", ""); w.Code != 400 {
		t.Fatalf("state chỉ dùng một lần: code=%d", w.Code)
	}
	w = call(mux, "GET", "/api/settings/spotify", "", "", ck...)
	noTokens(t, f, w)
	if !strings.Contains(w.Body.String(), `"refresh_token_set":true`) {
		t.Fatalf("trang Cài đặt phải biết đã kết nối: %s", w.Body)
	}
	// Người dùng bấm "Không đồng ý" trên Spotify: báo lỗi, bỏ lượt đăng nhập đó.
	loc, _ = url.Parse(urls[0])
	st := url.QueryEscape(loc.Query().Get("state"))
	if w := call(mux, "GET", spHub+"/api/spotify/callback?error=access_denied&state="+st, "", ""); w.Code != 400 {
		t.Fatalf("người dùng từ chối trên Spotify: code=%d", w.Code)
	}
	if w := call(mux, "GET", cb+st, "", ""); w.Code != 400 {
		t.Fatalf("lượt đã bị từ chối không được dùng lại: code=%d", w.Code)
	}
}

func TestSpotifyPlayerRefreshesTokenAtHub(t *testing.T) {
	f, mux, h, ck := spotifyConnected(t)
	w := call(mux, "GET", "/api/spotify/player", "", "", ck...)
	noTokens(t, f, w)
	var p struct {
		Connected, Playing, Shuffle, Reconnect bool
		Track, Artist, Art, URI, Context       string
		Progress                               int `json:"progress_ms"`
		Duration                               int `json:"duration_ms"`
		Device                                 struct {
			ID, Name string
			Volume   int `json:"volume_percent"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 200 || !p.Connected || !p.Playing || p.Track != "Ngày Mai Em Đi" || p.Artist != "Lê Hiếu, Soobin" ||
		p.Art != "https://i.scdn.co/300" || p.Device.ID != "dev-cafe" || p.Device.Name != "Khu cafe" || p.Device.Volume != 40 ||
		p.URI != "spotify:track:"+spT1 || p.Context != "spotify:playlist:"+spPlOwn || !p.Shuffle || p.Progress != 1000 || p.Duration != 200000 || p.Reconnect {
		t.Fatalf("tóm tắt bài đang phát sai: code=%d %s", w.Code, w.Body)
	}
	// Spotify xoay vòng refresh token: bản mới phải được lưu, không thì lần làm mới sau bị từ chối.
	if g, _ := f.snapshot(); len(g) != 1 || g[0] != "refresh_token" || h.db.Setting("spotify")["refresh_token"] != "rt-bi-mat-1" {
		t.Fatalf("làm mới token sai: grants=%v lưu=%v", g, h.db.Setting("spotify")["refresh_token"])
	}
	call(mux, "GET", "/api/spotify/player", "", "", ck...)
	if g, _ := f.snapshot(); len(g) != 1 {
		t.Fatalf("access token còn hạn mà vẫn làm mới: %v", g)
	}
	f.mu.Lock()
	f.expire = true
	f.mu.Unlock()
	if w := call(mux, "GET", "/api/spotify/player", "", "", ck...); w.Code != 200 || !strings.Contains(w.Body.String(), `"connected":true`) {
		t.Fatalf("401 từ Spotify phải làm mới token rồi thử lại: code=%d %s", w.Code, w.Body)
	}
	if g, _ := f.snapshot(); len(g) != 2 {
		t.Fatalf("sau 401 phải làm mới đúng một lần: %v", g)
	}
	f.mu.Lock()
	f.idle = true
	f.mu.Unlock()
	if w := call(mux, "GET", "/api/spotify/player", "", "", ck...); !strings.Contains(w.Body.String(), `"connected":true`) || !strings.Contains(w.Body.String(), `"playing":false`) {
		t.Fatalf("không phát gì (204): %s", w.Body)
	}
	w = call(mux, "GET", "/api/spotify/devices", "", "", ck...)
	noTokens(t, f, w)
	var d struct {
		Devices []struct {
			ID, Name string
			Active   bool `json:"is_active"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	if w.Code != 200 || len(d.Devices) != 2 || d.Devices[0].ID != "dev-cafe" || !d.Devices[0].Active || d.Devices[1].Name != "Phòng ngủ 1" {
		t.Fatalf("danh sách thiết bị Spotify Connect sai: %s", w.Body)
	}
}

func TestSpotifyNotConnected(t *testing.T) {
	f := newFakeSpotify(t)
	mux, h, ck := spotifyMux(t, f)
	for _, r := range [][2]string{{"GET", "/api/spotify/player"}, {"GET", "/api/spotify/devices"}, {"POST", "/api/spotify/cmd"}} {
		if w := call(mux, r[0], r[1], `{"do":"pause"}`, ""); w.Code != 401 {
			t.Errorf("%s %s không có PIN: code=%d", r[0], r[1], w.Code)
		}
	}
	if w := call(mux, "GET", "/api/spotify/player", "", "", ck...); w.Code != 200 || w.Body.String() != "{\"connected\":false}\n" {
		t.Fatalf("chưa kết nối: code=%d %s", w.Code, w.Body)
	}
	if w := call(mux, "POST", "/api/spotify/cmd", `{"do":"pause"}`, "", ck...); w.Code != 409 {
		t.Fatalf("lệnh khi chưa kết nối: code=%d", w.Code)
	}
	// Refresh token bị thu hồi (đổi mật khẩu, gỡ quyền app): xoá để trang Cài đặt hiện "chưa kết nối".
	f.refresh = "rt-khac"
	h.db.PutSetting("spotify", map[string]any{"client_id": spCID, "refresh_token": "rt-da-thu-hoi"})
	if w := call(mux, "GET", "/api/spotify/player", "", "", ck...); w.Body.String() != "{\"connected\":false}\n" {
		t.Fatalf("token bị thu hồi phải thành chưa kết nối: %s", w.Body)
	}
	if _, ok := h.db.Setting("spotify")["refresh_token"]; ok {
		t.Fatal("refresh token bị thu hồi phải bị xoá")
	}
	if _, c := f.snapshot(); len(c) != 0 {
		t.Fatalf("chưa kết nối mà vẫn gọi Web API: %v", c)
	}
}

// Quét đủ tập lệnh khai báo trong spotifyCmds: mỗi lệnh tới đúng một endpoint điều khiển phát có thật của Spotify.
func TestSpotifyCmdSweep(t *testing.T) {
	f, mux, _, ck := spotifyConnected(t)
	n := 0
	for do := range spotifyCmds {
		n++
		_, before := f.snapshot()
		w := call(mux, "POST", "/api/spotify/cmd", `{"do":"`+do+`","device":"dev-ngu1","volume":30,"uri":"spotify:track:`+spT1+`","position_ms":5000,"state":true}`, "", ck...)
		noTokens(t, f, w)
		_, after := f.snapshot()
		if w.Code != 204 || len(after) != len(before)+1 {
			t.Errorf("%s: code=%d %s, gọi Spotify %v", do, w.Code, w.Body, after[len(before):])
			continue
		}
		got := after[len(before)]
		if !spEndpoints[strings.SplitN(got, "?", 2)[0]] {
			t.Errorf("%s gọi endpoint lạ: %s", do, got)
		}
		if !strings.Contains(got, "dev-ngu1") {
			t.Errorf("%s không gửi thiết bị đã chọn: %s", do, got)
		}
		want := map[string]string{"volume": "volume_percent=30", "queue": "uri=spotify%3Atrack%3A" + spT1, "seek": "position_ms=5000", "shuffle": "state=true"}[do]
		if !strings.Contains(got, want) {
			t.Errorf("%s: thiếu %q: %s", do, want, got)
		}
	}
	if n < 9 {
		t.Fatalf("chỉ quét %d lệnh, muốn đủ phát/dừng/trước/sau/âm lượng/chuyển thiết bị/hàng đợi/tua/trộn bài", n)
	}
	t.Logf("quét %d/%d lệnh", n, len(spotifyCmds))

	_, before := f.snapshot()
	for _, body := range []string{`{"do":"xoa_tai_khoan"}`, `{"do":"volume","volume":101}`, `{"do":"volume","volume":-1}`, `{"do":"volume"}`,
		`{"do":"transfer"}`, `{"do":"play","device":"a&b=c"}`, `[1]`,
		`{"do":"queue"}`, `{"do":"queue","uri":"spotify:album:` + spAlbum + `"}`, `{"do":"seek"}`, `{"do":"seek","position_ms":-1}`, `{"do":"shuffle"}`,
		`{"do":"play","context":"spotify:track:` + spT1 + `"}`, `{"do":"play","context":"spotify:playlist:x&y=z"}`,
		`{"do":"play","context":"spotify:artist:` + spArtist + `","offset":"spotify:track:` + spT1 + `"}`,
		`{"do":"play","offset":"spotify:track:` + spT1 + `"}`, `{"do":"play","uris":["spotify:album:` + spAlbum + `"]}`,
		`{"do":"play","uris":[` + strings.TrimSuffix(strings.Repeat(`"spotify:track:`+spT1+`",`, 51), ",") + `]}`} {
		if w := call(mux, "POST", "/api/spotify/cmd", body, "", ck...); w.Code != 400 {
			t.Errorf("%s phải bị từ chối: code=%d", body, w.Code)
		}
	}
	if _, after := f.snapshot(); len(after) != len(before) {
		t.Fatalf("lệnh không hợp lệ vẫn tới Spotify: %v", after[len(before):])
	}
	f.mu.Lock()
	f.free = true
	f.mu.Unlock()
	if w := call(mux, "POST", "/api/spotify/cmd", `{"do":"pause"}`, "", ck...); w.Code != 403 || !strings.Contains(w.Body.String(), "Premium") {
		t.Fatalf("tài khoản thường phải báo cần Premium: code=%d %s", w.Code, w.Body)
	}
	f.mu.Lock()
	f.free, f.noDevice = false, true
	f.mu.Unlock()
	if w := call(mux, "POST", "/api/spotify/cmd", `{"do":"play"}`, "", ck...); w.Code != 409 || !strings.Contains(w.Body.String(), "thiết bị") {
		t.Fatalf("không có thiết bị đang mở phải bảo chọn thiết bị: code=%d %s", w.Code, w.Body)
	}
}

// Bảng trình phát đầy đủ: tìm kiếm, playlist, bài trong playlist/album, bài đã thích, hàng đợi. Đều sau PIN.
var spBrowse = []string{"/api/spotify/search?q=a", "/api/spotify/playlists", "/api/spotify/tracks?of=liked", "/api/spotify/queue"}

func TestSpotifyBrowseNeedsPIN(t *testing.T) {
	_, mux, _, _ := spotifyConnected(t)
	for _, u := range spBrowse {
		if w := call(mux, "GET", u, "", ""); w.Code != 401 {
			t.Errorf("%s không có PIN: code=%d", u, w.Code)
		}
	}
}

// Trả trường gọn {uri, name, sub, art, ms, total}, không chuyển nguyên phản hồi Spotify (href, available_markets...).
func compact(t *testing.T, f *fakeSpotify, w *httptest.ResponseRecorder) map[string][]spotifyItem {
	t.Helper()
	noTokens(t, f, w)
	for _, raw := range []string{"href", "available_markets", "external_urls", "is_local", "added_at"} {
		if strings.Contains(w.Body.String(), raw) {
			t.Fatalf("chuyển nguyên trường %q của Spotify ra trình duyệt: %s", raw, w.Body)
		}
	}
	var out map[string][]spotifyItem
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("code=%d %s", w.Code, w.Body)
	}
	return out
}

func TestSpotifySearchCompact(t *testing.T) {
	f, mux, _, ck := spotifyConnected(t)
	out := compact(t, f, call(mux, "GET", "/api/spotify/search?q="+url.QueryEscape("ngày mai"), "", "", ck...))
	tr, pl, al, ar := out["tracks"], out["playlists"], out["albums"], out["artists"]
	if len(tr) != 1 || tr[0] != (spotifyItem{URI: "spotify:track:" + spT1, Name: "Ngày Mai Em Đi", Sub: "Lê Hiếu, Soobin", Art: "https://i.scdn.co/300", MS: 200000}) {
		t.Errorf("bài hát: %+v", tr)
	}
	if len(pl) != 1 || pl[0] != (spotifyItem{URI: "spotify:playlist:" + spPlOther, Name: "Nhạc chill", Sub: "Spotify", Art: "https://i.scdn.co/p300", Total: 80}) {
		t.Errorf("playlist (bỏ mục null Spotify hay trả): %+v", pl)
	}
	if len(al) != 1 || al[0].Sub != "Lê Hiếu" || al[0].Art != "https://i.scdn.co/a300" || len(ar) != 1 || ar[0].URI != "spotify:artist:"+spArtist {
		t.Errorf("album/nghệ sĩ: %+v %+v", al, ar)
	}
	_, calls := f.snapshot()
	if last := calls[len(calls)-1]; !strings.Contains(last, "q=ng%C3%A0y+mai") {
		t.Errorf("gửi sai câu tìm: %s", last)
	}
	_, before := f.snapshot()
	for _, q := range []string{"", "%20%20", strings.Repeat("a", 201)} {
		if w := call(mux, "GET", "/api/spotify/search?q="+q, "", "", ck...); w.Code != 400 {
			t.Errorf("câu tìm %q phải bị từ chối: code=%d", q, w.Code)
		}
	}
	if _, after := f.snapshot(); len(after) != len(before) {
		t.Errorf("câu tìm không hợp lệ vẫn tới Spotify: %v", after[len(before):])
	}
}

func TestSpotifyPlaylistsAndTracks(t *testing.T) {
	f, mux, _, ck := spotifyConnected(t)
	pl := compact(t, f, call(mux, "GET", "/api/spotify/playlists", "", "", ck...))["playlists"]
	if len(pl) != 2 || pl[0] != (spotifyItem{URI: "spotify:playlist:" + spPlOwn, Name: "Nhạc quán sáng", Sub: "Bình", Total: 2}) {
		t.Fatalf("playlist của tài khoản: %+v", pl)
	}
	// bỏ bài null (bị gỡ) và file trên máy (không phát qua API được)
	tr := compact(t, f, call(mux, "GET", "/api/spotify/tracks?of=spotify:playlist:"+spPlOwn, "", "", ck...))["tracks"]
	if len(tr) != 2 || tr[0].Name != "Bài một" || tr[1].URI != "spotify:track:"+spT2 || tr[1].Art != "https://i.scdn.co/300" {
		t.Errorf("bài trong playlist: %+v", tr)
	}
	// playlist của người khác: Spotify không cho xem bài; báo rõ, vẫn phát được cả playlist
	if w := call(mux, "GET", "/api/spotify/tracks?of=spotify:playlist:"+spPlOther, "", "", ck...); w.Code != 403 || !strings.Contains(w.Body.String(), "playlist bạn tạo") {
		t.Errorf("playlist không xem được bài: code=%d %s", w.Code, w.Body)
	}
	// album: bài của album không kèm ảnh, lấy ảnh bìa album
	tr = compact(t, f, call(mux, "GET", "/api/spotify/tracks?of=spotify:album:"+spAlbum, "", "", ck...))["tracks"]
	if len(tr) != 1 || tr[0] != (spotifyItem{URI: "spotify:track:" + spT1, Name: "Bài một", Sub: "Lê Hiếu", Art: "https://i.scdn.co/a300", MS: 1000}) {
		t.Errorf("bài trong album: %+v", tr)
	}
	tr = compact(t, f, call(mux, "GET", "/api/spotify/tracks?of=liked", "", "", ck...))["tracks"]
	if len(tr) != 1 || tr[0].Name != "Bài đã thích" {
		t.Errorf("bài đã thích: %+v", tr)
	}
	q := compact(t, f, call(mux, "GET", "/api/spotify/queue", "", "", ck...))["queue"]
	if len(q) != 2 || q[0].Name != "Kế tiếp" {
		t.Errorf("hàng đợi: %+v", q)
	}
	_, before := f.snapshot()
	for _, of := range []string{"", "spotify:artist:" + spArtist, "spotify:track:" + spT1, "spotify:playlist:a/../../me", "spotify:album:x?y=1"} {
		if w := call(mux, "GET", "/api/spotify/tracks?of="+url.QueryEscape(of), "", "", ck...); w.Code != 400 {
			t.Errorf("of=%q phải bị từ chối: code=%d", of, w.Code)
		}
	}
	if _, after := f.snapshot(); len(after) != len(before) {
		t.Errorf("of không hợp lệ vẫn tới Spotify: %v", after[len(before):])
	}
}

// Phát từ một bài trong playlist/album (context + offset), danh sách bài (bài đã thích, kết quả tìm), cả nghệ sĩ.
func TestSpotifyPlayContext(t *testing.T) {
	f, mux, _, ck := spotifyConnected(t)
	for body, want := range map[string]string{
		`{"do":"play","context":"spotify:playlist:` + spPlOwn + `","offset":"spotify:track:` + spT2 + `"}`: `PUT /v1/me/player/play? {"context_uri":"spotify:playlist:` + spPlOwn + `","offset":{"uri":"spotify:track:` + spT2 + `"}}`,
		`{"do":"play","context":"spotify:album:` + spAlbum + `"}`:                                          `PUT /v1/me/player/play? {"context_uri":"spotify:album:` + spAlbum + `"}`,
		`{"do":"play","context":"spotify:artist:` + spArtist + `","device":"dev-cafe"}`:                    `PUT /v1/me/player/play?device_id=dev-cafe {"context_uri":"spotify:artist:` + spArtist + `"}`,
		`{"do":"play","uris":["spotify:track:` + spT2 + `","spotify:track:` + spT1 + `"]}`:                 `PUT /v1/me/player/play? {"uris":["spotify:track:` + spT2 + `","spotify:track:` + spT1 + `"]}`,
	} {
		w := call(mux, "POST", "/api/spotify/cmd", body, "", ck...)
		_, calls := f.snapshot()
		if w.Code != 204 || calls[len(calls)-1] != want {
			t.Errorf("%s: code=%d %s\n gửi %s\n muốn %s", body, w.Code, w.Body, calls[len(calls)-1], want)
		}
	}
}

// Kết nối từ bản đầu thiếu quyền xem playlist: hub báo cần Kết nối lại thay vì lỗi khó hiểu.
func TestSpotifyScopeReconnect(t *testing.T) {
	f, mux, _, ck := spotifyConnected(t)
	f.mu.Lock()
	f.oldScope = true
	f.mu.Unlock()
	if w := call(mux, "GET", "/api/spotify/player", "", "", ck...); !strings.Contains(w.Body.String(), `"reconnect":true`) {
		t.Errorf("scope cũ phải báo cần kết nối lại: %s", w.Body)
	}
	for _, u := range []string{"/api/spotify/playlists", "/api/spotify/tracks?of=liked"} {
		if w := call(mux, "GET", u, "", "", ck...); w.Code != 403 || !strings.Contains(w.Body.String(), "Kết nối lại") {
			t.Errorf("%s thiếu quyền: code=%d %s", u, w.Code, w.Body)
		}
	}
}
