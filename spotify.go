package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Spotify (mục 18 thiết kế): hubd đăng nhập OAuth PKCE và gọi Web API thay trình duyệt. Client ID và refresh token
// nằm trong kho cấu hình nhóm "spotify" (settings.go che refresh_token); access token chỉ ở RAM. Trình duyệt chỉ gọi
// /api/spotify/* sau PIN, không bao giờ thấy token. Spotify chỉ nhận redirect URI HTTPS (trừ 127.0.0.1), nên trang
// trả về nằm ở cổng HTTPS 8443, chung chứng chỉ với micro trợ lý (scripts/tls-selfsigned.sh). Điều khiển phát cần Premium.
const spotifyScopes = "user-read-playback-state user-modify-playback-state user-read-currently-playing"

// Lệnh điều khiển phát dashboard được gọi; ngoài bảng này hubd không chuyển gì tới Spotify.
var spotifyCmds = map[string]struct{ method, path string }{
	"play":     {"PUT", "/me/player/play"},
	"pause":    {"PUT", "/me/player/pause"},
	"next":     {"POST", "/me/player/next"},
	"prev":     {"POST", "/me/player/previous"},
	"volume":   {"PUT", "/me/player/volume"},
	"transfer": {"PUT", "/me/player"}, // chuyển nơi phát sang thiết bị khác, phát tiếp
}

var (
	spotifyDevRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	errSpotifyOff = errors.New("chưa kết nối Spotify")
)

type spotify struct {
	db            *DB
	accounts, api string // https://accounts.spotify.com, https://api.spotify.com/v1; test trỏ vào Spotify giả
	client        *http.Client

	mu      sync.Mutex
	pending map[string]spotifyLogin // state -> lượt đăng nhập đang chờ Spotify trả về
	access  string
	expires time.Time
	rt      string // refresh token đã đổi ra access (xoá/đổi trong Cài đặt thì lấy lại)
}

type spotifyLogin struct {
	verifier, redirect, client string
	at                         time.Time
}

type spotifyToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// Thiết bị Spotify Connect: giữ tên trường của Spotify, chỉ lấy các trường dashboard cần.
type spotifyDevice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Active   bool   `json:"is_active"`
	Volume   *int   `json:"volume_percent"`
	VolumeOK bool   `json:"supports_volume"`
}

func mountSpotify(mux *http.ServeMux, h *Hub, a *Auth) {
	env := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	s := &spotify{db: h.db, client: &http.Client{Timeout: 10 * time.Second}, pending: map[string]spotifyLogin{},
		accounts: env("SPOTIFY_ACCOUNTS_URL", "https://accounts.spotify.com"), api: env("SPOTIFY_API_URL", "https://api.spotify.com/v1")}
	mux.HandleFunc("POST /api/spotify/login", a.Require(s.login))
	mux.HandleFunc("GET /api/spotify/callback", s.callback) // không cần cookie: state một lần xác thực lượt đăng nhập
	mux.HandleFunc("GET /api/spotify/player", a.Require(s.player))
	mux.HandleFunc("GET /api/spotify/devices", a.Require(s.devices))
	mux.HandleFunc("POST /api/spotify/cmd", a.Require(s.cmd))
}

func (s *spotify) setting(k string) string {
	v, _ := s.db.Setting("spotify")[k].(string)
	return v
}

func spotifyRandom() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b) // 43 ký tự: đủ độ dài code_verifier của PKCE
}

// login: tạo code_verifier + state, trả địa chỉ trang đồng ý của Spotify để trình duyệt mở.
func (s *spotify) login(w http.ResponseWriter, r *http.Request) {
	cid := s.setting("client_id")
	if cid == "" {
		http.Error(w, "Nhập Client ID của app Spotify rồi lưu trước", 400)
		return
	}
	host := (&url.URL{Host: r.Host}).Hostname()
	if r.TLS == nil && os.Getenv("HUBD_TLS_CERT") == "" {
		http.Error(w, "Spotify chỉ trả về địa chỉ HTTPS: bật HTTPS cho hub (cổng 8443, README mục Trợ lý giọng nói) rồi thử lại", 400)
		return
	}
	l := spotifyLogin{verifier: spotifyRandom(), redirect: "https://" + host + ":8443/api/spotify/callback", client: cid, at: time.Now()}
	state := spotifyRandom()
	s.mu.Lock()
	for k, v := range s.pending {
		if time.Since(v.at) > 10*time.Minute {
			delete(s.pending, k)
		}
	}
	s.pending[state] = l
	s.mu.Unlock()
	sum := sha256.Sum256([]byte(l.verifier))
	q := url.Values{"client_id": {cid}, "response_type": {"code"}, "redirect_uri": {l.redirect}, "state": {state},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "scope": {spotifyScopes}}
	writeJSON(w, map[string]string{"url": s.accounts + "/authorize?" + q.Encode()})
}

// callback: Spotify chuyển trình duyệt về đây kèm code; hubd đổi code lấy token, lưu refresh token.
func (s *spotify) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	l, ok := s.pending[q.Get("state")]
	delete(s.pending, q.Get("state"))
	s.mu.Unlock()
	if !ok || time.Since(l.at) > 10*time.Minute {
		http.Error(w, "Lượt đăng nhập Spotify không hợp lệ hoặc đã quá 10 phút: bấm Kết nối lại trong Cài đặt", 400)
		return
	}
	if e := q.Get("error"); e != "" {
		http.Error(w, "Spotify chưa cho phép ("+e+"): bấm Kết nối lại và chọn Đồng ý", 400)
		return
	}
	t, _, err := s.grant(r.Context(), url.Values{"grant_type": {"authorization_code"}, "code": {q.Get("code")},
		"redirect_uri": {l.redirect}, "client_id": {l.client}, "code_verifier": {l.verifier}})
	if err == nil {
		s.mu.Lock()
		err = s.keep(t, "")
		s.mu.Unlock()
	}
	if err != nil {
		log.Printf("spotify: đổi code lấy token: %v", err)
		http.Error(w, "Không lấy được quyền từ Spotify, thử kết nối lại", 502)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Spotify</title>
<body style="font:17px system-ui;background:#0a1119;color:#e8eef4;padding:32px"><p>Đã kết nối Spotify. Đóng trang này và quay lại Hub Center.</p>
<p><a style="color:#f6b93b" href="/#/caidat">Về trang Cài đặt</a></p>`)
}

// grant: gọi /api/token của Spotify (PKCE: không có client secret). Lỗi kèm mã HTTP; nội dung lỗi của Spotify không chứa token.
func (s *spotify) grant(ctx context.Context, form url.Values) (spotifyToken, int, error) {
	var t spotifyToken
	req, _ := http.NewRequestWithContext(ctx, "POST", s.accounts+"/api/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.client.Do(req)
	if err != nil {
		return t, 0, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != 200 {
		return t, res.StatusCode, fmt.Errorf("HTTP %d %s", res.StatusCode, b)
	}
	if json.Unmarshal(b, &t) != nil || t.AccessToken == "" {
		return t, res.StatusCode, errors.New("phản hồi token không đọc được")
	}
	return t, 200, nil
}

// keep: giữ access token ở RAM; Spotify có thể đổi refresh token mỗi lần làm mới, bản mới phải lưu lại. Gọi khi giữ s.mu.
func (s *spotify) keep(t spotifyToken, rt string) error {
	if t.RefreshToken != "" && t.RefreshToken != rt {
		rt = t.RefreshToken
		if err := s.db.PutSetting("spotify", map[string]any{"refresh_token": rt}); err != nil {
			return err
		}
	}
	s.access, s.rt = t.AccessToken, rt
	s.expires = time.Now().Add(time.Duration(t.ExpiresIn)*time.Second - time.Minute)
	return nil
}

// token: access token còn hạn, hoặc làm mới bằng refresh token. stale = token vừa bị Spotify trả 401.
// Giữ s.mu cả lúc gọi Spotify để hai yêu cầu cùng lúc không làm mới hai lần (refresh token xoay vòng).
func (s *spotify) token(ctx context.Context, stale string) (string, error) {
	cid, rt := s.setting("client_id"), s.setting("refresh_token")
	s.mu.Lock()
	defer s.mu.Unlock()
	if cid == "" || rt == "" {
		s.access = ""
		return "", errSpotifyOff
	}
	if s.access != "" && s.access != stale && s.rt == rt && time.Now().Before(s.expires) {
		return s.access, nil
	}
	t, code, err := s.grant(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}, "client_id": {cid}})
	if code == 400 && strings.Contains(err.Error(), "invalid_grant") {
		// bị thu hồi (gỡ quyền app, đổi mật khẩu): xoá để trang Cài đặt hiện chưa kết nối
		log.Printf("spotify: refresh token bị từ chối, cần kết nối lại")
		s.access = ""
		s.db.PutSetting("spotify", map[string]any{"refresh_token": nil})
		return "", errSpotifyOff
	}
	if err != nil {
		return "", err
	}
	return t.AccessToken, s.keep(t, rt)
}

// call: gọi Web API bằng token của hub; 401 (token hết hạn sớm) thì làm mới một lần rồi thử lại.
func (s *spotify) call(ctx context.Context, method, path string, q url.Values, body []byte) (int, []byte, error) {
	stale := ""
	for try := 0; ; try++ {
		tok, err := s.token(ctx, stale)
		if err != nil {
			return 0, nil, err
		}
		u := s.api + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		req, _ := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := s.client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if res.StatusCode == 401 && try == 0 {
			stale = tok
			continue
		}
		return res.StatusCode, b, nil
	}
}

// spotifyFail: đổi lỗi thành lời dễ hiểu cho dashboard; không chuyển nguyên phản hồi của Spotify ra trình duyệt.
func spotifyFail(w http.ResponseWriter, code int, body []byte, err error) {
	switch {
	case errors.Is(err, errSpotifyOff):
		http.Error(w, "Chưa kết nối Spotify: vào Cài đặt, mục Spotify", 409)
	case err != nil:
		log.Printf("spotify: %v", err)
		http.Error(w, "Không gọi được Spotify", 502)
	case bytes.Contains(body, []byte("PREMIUM_REQUIRED")):
		http.Error(w, "Cần tài khoản Spotify Premium để điều khiển phát", 403)
	case bytes.Contains(body, []byte("NO_ACTIVE_DEVICE")):
		http.Error(w, "Chưa có thiết bị nào đang mở Spotify: chọn thiết bị phát", 409)
	case code == 403:
		http.Error(w, "Spotify từ chối: tài khoản cần Premium và phải có trong User Management của app", 403)
	default:
		log.Printf("spotify: HTTP %d %.300s", code, body)
		http.Error(w, "Spotify báo lỗi, thử lại sau", 502)
	}
}

// player: bài đang phát, gọn cho trình phát trên dashboard.
func (s *spotify) player(w http.ResponseWriter, r *http.Request) {
	code, b, err := s.call(r.Context(), "GET", "/me/player", nil, nil)
	if errors.Is(err, errSpotifyOff) {
		writeJSON(w, map[string]bool{"connected": false})
		return
	}
	if err != nil || code != 200 && code != 204 {
		spotifyFail(w, code, b, err)
		return
	}
	out := map[string]any{"connected": true, "playing": false}
	var p struct {
		Playing bool           `json:"is_playing"`
		Device  *spotifyDevice `json:"device"`
		Item    *struct {
			Name    string
			Artists []struct{ Name string }
			Album   struct {
				Images []struct {
					URL   string
					Width int
				}
			}
		}
	}
	if code == 200 && json.Unmarshal(b, &p) == nil {
		out["playing"], out["device"] = p.Playing, p.Device
		if it := p.Item; it != nil {
			var names []string
			for _, a := range it.Artists {
				names = append(names, a.Name)
			}
			out["track"], out["artist"] = it.Name, strings.Join(names, ", ")
			// ảnh bìa nhỏ nhất còn rộng ≥ 200 px (Spotify xếp ảnh rộng nhất trước): đủ nét cho đĩa nhạc trên màn Retina
			for i, im := range it.Album.Images {
				if i == 0 || im.Width >= 200 {
					out["art"] = im.URL
				}
			}
		}
	}
	writeJSON(w, out)
}

func (s *spotify) devices(w http.ResponseWriter, r *http.Request) {
	code, b, err := s.call(r.Context(), "GET", "/me/player/devices", nil, nil)
	var d struct {
		Devices []spotifyDevice `json:"devices"`
	}
	if err != nil || code != 200 || json.Unmarshal(b, &d) != nil {
		spotifyFail(w, code, b, err)
		return
	}
	writeJSON(w, d)
}

// cmd: {do, device?, volume?}; do thuộc spotifyCmds. device = id Spotify Connect (bỏ trống = thiết bị đang phát).
func (s *spotify) cmd(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Do, Device string
		Volume     *int
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil {
		http.Error(w, "cần JSON {do, device, volume}", 400)
		return
	}
	c, ok := spotifyCmds[b.Do]
	if !ok || b.Device != "" && !spotifyDevRe.MatchString(b.Device) {
		http.Error(w, "lệnh Spotify không hợp lệ", 400)
		return
	}
	q, body := url.Values{}, []byte(nil)
	switch b.Do {
	case "volume":
		if b.Volume == nil || *b.Volume < 0 || *b.Volume > 100 {
			http.Error(w, "âm lượng 0–100", 400)
			return
		}
		q.Set("volume_percent", strconv.Itoa(*b.Volume))
	case "transfer":
		if b.Device == "" {
			http.Error(w, "chọn thiết bị để chuyển nhạc sang", 400)
			return
		}
		body, _ = json.Marshal(map[string]any{"device_ids": []string{b.Device}, "play": true})
	}
	if b.Device != "" && body == nil {
		q.Set("device_id", b.Device)
	}
	code, resp, err := s.call(r.Context(), c.method, c.path, q, body)
	if err == nil && code < 300 {
		w.WriteHeader(204)
		return
	}
	spotifyFail(w, code, resp, err)
}
