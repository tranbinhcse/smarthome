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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Spotify (mục 18 thiết kế): hubd đăng nhập OAuth PKCE và gọi Web API thay trình duyệt. Client ID và refresh token
// nằm trong kho cấu hình nhóm "spotify" (settings.go che refresh_token); access token chỉ ở RAM. Trình duyệt chỉ gọi
// /api/spotify/* sau PIN, không bao giờ thấy token. Spotify chỉ nhận redirect URI HTTPS (trừ 127.0.0.1), nên trang
// trả về nằm ở cổng HTTPS 8443, chung chứng chỉ với micro trợ lý (scripts/tls-selfsigned.sh). Điều khiển phát cần Premium.
// Thêm scope thì tài khoản đã kết nối phải bấm "Kết nối lại": hub so với scope Spotify đã cấp, /api/spotify/player báo reconnect.
const spotifyScopes = "user-read-playback-state user-modify-playback-state user-read-currently-playing" +
	" playlist-read-private playlist-read-collaborative user-library-read" // playlist riêng, cùng sửa, bài đã thích

// Lệnh điều khiển phát dashboard được gọi; ngoài bảng này hubd không chuyển gì tới Spotify.
var spotifyCmds = map[string]struct{ method, path string }{
	"play":     {"PUT", "/me/player/play"},
	"pause":    {"PUT", "/me/player/pause"},
	"next":     {"POST", "/me/player/next"},
	"prev":     {"POST", "/me/player/previous"},
	"volume":   {"PUT", "/me/player/volume"},
	"transfer": {"PUT", "/me/player"}, // chuyển nơi phát sang thiết bị khác, phát tiếp
	"queue":    {"POST", "/me/player/queue"},
	"seek":     {"PUT", "/me/player/seek"},
	"shuffle":  {"PUT", "/me/player/shuffle"},
}

var (
	spotifyDevRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	spotifyURIRe  = regexp.MustCompile(`^spotify:(track|album|playlist|artist):[A-Za-z0-9]{22}$`)
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
	scope   string // scope Spotify đã cấp cho token đang dùng; rỗng = chưa biết
}

type spotifyLogin struct {
	verifier, redirect, client string
	at                         time.Time
}

type spotifyToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
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
	mux.HandleFunc("GET /api/spotify/search", a.Require(s.search))
	mux.HandleFunc("GET /api/spotify/playlists", a.Require(s.playlists))
	mux.HandleFunc("GET /api/spotify/tracks", a.Require(s.tracks))
	mux.HandleFunc("GET /api/spotify/queue", a.Require(s.queue))
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
	if t.Scope != "" {
		s.scope = t.Scope
	}
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
	case bytes.Contains(body, []byte("scope")):
		http.Error(w, "Thiếu quyền Spotify cho mục này: vào Cài đặt, mục Spotify, bấm Kết nối lại", 403)
	case code == 403:
		log.Printf("spotify: HTTP 403 %.300s", body) // lời của Spotify (không chứa token): Premium, User Management...
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
	if s.reconnect() {
		out["reconnect"] = true
	}
	var p struct {
		Playing  bool           `json:"is_playing"`
		Shuffle  bool           `json:"shuffle_state"`
		Progress int            `json:"progress_ms"`
		Device   *spotifyDevice `json:"device"`
		Context  *struct{ URI string }
		Item     *spotifyRaw
	}
	if code == 200 && json.Unmarshal(b, &p) == nil {
		out["playing"], out["device"], out["shuffle"], out["progress_ms"] = p.Playing, p.Device, p.Shuffle, p.Progress
		if p.Context != nil {
			out["context"] = p.Context.URI // playlist/album đang phát: bảng trình phát tô đậm
		}
		if p.Item != nil {
			it := p.Item.item()
			out["track"], out["artist"], out["uri"], out["duration_ms"] = it.Name, it.Sub, it.URI, it.MS
			if p.Item.Album != nil {
				out["art"] = spotifyArt(p.Item.Album.Images, 200) // đủ nét cho đĩa nhạc và bìa lớn trên màn Retina
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
		Do, Device, Context, Offset, URI string
		URIs                             []string
		Volume                           *int
		Position                         *int `json:"position_ms"`
		State                            *bool
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
	case "queue":
		if spotifyKind(b.URI) != "track" {
			http.Error(w, "chọn một bài để thêm vào hàng đợi", 400)
			return
		}
		q.Set("uri", b.URI)
	case "seek":
		if b.Position == nil || *b.Position < 0 {
			http.Error(w, "vị trí tua không hợp lệ", 400)
			return
		}
		q.Set("position_ms", strconv.Itoa(*b.Position))
	case "shuffle":
		if b.State == nil {
			http.Error(w, "cần state true hoặc false", 400)
			return
		}
		q.Set("state", strconv.FormatBool(*b.State))
	case "play":
		var ok bool
		if body, ok = spotifyPlayBody(b.Context, b.Offset, b.URIs); !ok {
			http.Error(w, "bài hoặc danh sách phát không hợp lệ", 400)
			return
		}
	}
	if b.Device != "" && b.Do != "transfer" {
		q.Set("device_id", b.Device)
	}
	code, resp, err := s.call(r.Context(), c.method, c.path, q, body)
	if err == nil && code < 300 {
		w.WriteHeader(204)
		return
	}
	spotifyFail(w, code, resp, err)
}

// Một mục gọn cho dashboard: bài hát, playlist, album hay nghệ sĩ (phân biệt bằng uri spotify:<loại>:<id>).
type spotifyItem struct {
	URI   string `json:"uri"`
	Name  string `json:"name"`
	Sub   string `json:"sub,omitempty"`   // nghệ sĩ (bài, album) hoặc chủ playlist
	Art   string `json:"art,omitempty"`   // ảnh bìa nhỏ
	MS    int    `json:"ms,omitempty"`    // độ dài bài
	Total int    `json:"total,omitempty"` // số bài của playlist
}

// Ảnh nhỏ nhất còn rộng ≥ min px (Spotify xếp ảnh rộng nhất trước); không có ảnh nào đủ rộng thì ảnh đầu.
type spotifyImage struct {
	URL   string
	Width int
}

func spotifyArt(ims []spotifyImage, min int) (art string) {
	for i, im := range ims {
		if i == 0 || im.Width >= min {
			art = im.URL
		}
	}
	return art
}

// Đối tượng Spotify (bài, playlist, album, nghệ sĩ): chỉ đọc các trường cần cho spotifyItem, phần còn lại bỏ.
type spotifyRaw struct {
	URI, Name  string
	DurationMS int `json:"duration_ms"`
	Artists    []struct{ Name string }
	Images     []spotifyImage
	Album      *struct{ Images []spotifyImage }
	Owner      *struct {
		DisplayName string `json:"display_name"`
	}
	Items  *struct{ Total int } // số bài của playlist (khoá mới từ 02/2026)
	Tracks *struct{ Total int } // khoá cũ, Spotify còn trả song song
}

func (r *spotifyRaw) item() spotifyItem {
	it := spotifyItem{URI: r.URI, Name: r.Name, MS: r.DurationMS, Art: spotifyArt(r.Images, 120)}
	if r.Album != nil {
		it.Art = spotifyArt(r.Album.Images, 120)
	}
	var names []string
	for _, a := range r.Artists {
		names = append(names, a.Name)
	}
	it.Sub = strings.Join(names, ", ")
	if r.Owner != nil {
		it.Sub = r.Owner.DisplayName
	}
	if r.Items != nil {
		it.Total = r.Items.Total
	} else if r.Tracks != nil {
		it.Total = r.Tracks.Total
	}
	return it
}

// spotifyItems: bỏ mục null (Spotify trả null cho bài bị gỡ, có lúc cả playlist trong kết quả tìm).
func spotifyItems(raws []*spotifyRaw) []spotifyItem {
	out := []spotifyItem{}
	for _, x := range raws {
		if x != nil && x.URI != "" {
			out = append(out, x.item())
		}
	}
	return out
}

// spotifyKind: loại của một uri hợp lệ (track, album, playlist, artist); rỗng nếu không hợp lệ.
func spotifyKind(u string) string {
	if m := spotifyURIRe.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}

// spotifyPlayBody: thân lệnh phát. Không có gì = phát tiếp; context = album/playlist/nghệ sĩ, offset = bài bắt đầu
// (Spotify chỉ nhận offset với album/playlist); uris = danh sách bài (bài đã thích, kết quả tìm), tối đa 50.
func spotifyPlayBody(ctx, offset string, uris []string) ([]byte, bool) {
	k := spotifyKind(ctx)
	switch {
	case ctx == "" && offset == "" && len(uris) == 0:
		return nil, true
	case ctx != "" && (k == "" || k == "track"),
		offset != "" && (k != "album" && k != "playlist" || spotifyKind(offset) != "track"),
		len(uris) > 0 && (ctx != "" || len(uris) > 50):
		return nil, false
	}
	for _, u := range uris {
		if spotifyKind(u) != "track" {
			return nil, false
		}
	}
	m := map[string]any{}
	if ctx != "" {
		m["context_uri"] = ctx
	}
	if offset != "" {
		m["offset"] = map[string]string{"uri": offset}
	}
	if len(uris) > 0 {
		m["uris"] = uris
	}
	b, _ := json.Marshal(m)
	return b, true
}

// reconnect: token đang dùng thiếu scope hub cần (kết nối từ bản trước): dashboard nhắc Kết nối lại.
func (s *spotify) reconnect() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	have := strings.Fields(s.scope)
	return len(have) > 0 && slices.ContainsFunc(strings.Fields(spotifyScopes), func(want string) bool { return !slices.Contains(have, want) })
}

// get: GET Web API rồi đọc JSON vào v; lỗi thì đã trả lời trình duyệt bằng spotifyFail.
func (s *spotify) get(w http.ResponseWriter, r *http.Request, path string, q url.Values, v any) bool {
	code, b, err := s.call(r.Context(), "GET", path, q, nil)
	if err == nil && code == 200 && json.Unmarshal(b, v) == nil {
		return true
	}
	spotifyFail(w, code, b, err)
	return false
}

// search: bài hát, playlist, album, nghệ sĩ; Spotify từ 02/2026 trả tối đa 10 kết quả mỗi loại.
func (s *spotify) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || utf8.RuneCountInString(q) > 200 {
		http.Error(w, "nhập từ khoá tìm (tối đa 200 ký tự)", 400)
		return
	}
	var res map[string]struct{ Items []*spotifyRaw }
	if !s.get(w, r, "/search", url.Values{"q": {q}, "type": {"track,playlist,album,artist"}, "limit": {"10"}}, &res) {
		return
	}
	out := map[string][]spotifyItem{}
	for _, k := range []string{"tracks", "playlists", "albums", "artists"} {
		out[k] = spotifyItems(res[k].Items)
	}
	writeJSON(w, out)
}

// playlists: playlist của tài khoản (tự tạo, cùng sửa, đang theo dõi).
func (s *spotify) playlists(w http.ResponseWriter, r *http.Request) {
	var res struct{ Items []*spotifyRaw }
	// ponytail: chỉ 50 playlist đầu; nhà có nhiều hơn thì thêm phân trang (offset)
	if s.get(w, r, "/me/playlists", url.Values{"limit": {"50"}}, &res) {
		writeJSON(w, map[string][]spotifyItem{"playlists": spotifyItems(res.Items)})
	}
}

// tracks: bài trong một playlist (spotify:playlist:<id>), album (spotify:album:<id>) hoặc bài đã thích (of=liked); 50 bài đầu.
func (s *spotify) tracks(w http.ResponseWriter, r *http.Request) {
	of := r.URL.Query().Get("of")
	kind, id := spotifyKind(of), of[strings.LastIndex(of, ":")+1:]
	var list []*spotifyRaw
	switch {
	case of == "liked":
		var res struct{ Items []struct{ Track *spotifyRaw } }
		if !s.get(w, r, "/me/tracks", url.Values{"limit": {"50"}}, &res) {
			return
		}
		for _, x := range res.Items {
			list = append(list, x.Track)
		}
	case kind == "album":
		var res struct {
			Images []spotifyImage
			Tracks struct{ Items []*spotifyRaw }
		}
		if !s.get(w, r, "/albums/"+id, nil, &res) {
			return
		}
		out := spotifyItems(res.Tracks.Items)
		for i := range out {
			out[i].Art = spotifyArt(res.Images, 120) // bài trong album không kèm ảnh: dùng bìa album
		}
		writeJSON(w, map[string][]spotifyItem{"tracks": out})
		return
	case kind == "playlist":
		code, b, err := s.call(r.Context(), "GET", "/playlists/"+id+"/items", url.Values{"limit": {"50"}}, nil)
		if err == nil && code == 403 && !bytes.Contains(b, []byte("scope")) {
			http.Error(w, "Spotify chỉ cho xem bài của playlist bạn tạo hoặc cùng sửa. Vẫn phát được cả playlist.", 403)
			return
		}
		var res struct {
			Items []struct {
				IsLocal bool `json:"is_local"`
				Item    *spotifyRaw
			}
		}
		if err != nil || code != 200 || json.Unmarshal(b, &res) != nil {
			spotifyFail(w, code, b, err)
			return
		}
		for _, x := range res.Items {
			if !x.IsLocal { // file trên máy người dùng: API không phát được
				list = append(list, x.Item)
			}
		}
	default:
		http.Error(w, "of là spotify:playlist:<id>, spotify:album:<id> hoặc liked", 400)
		return
	}
	writeJSON(w, map[string][]spotifyItem{"tracks": spotifyItems(list)})
}

// queue: 20 bài kế tiếp.
func (s *spotify) queue(w http.ResponseWriter, r *http.Request) {
	var res struct{ Queue []*spotifyRaw }
	if s.get(w, r, "/me/player/queue", nil, &res) {
		q := spotifyItems(res.Queue)
		writeJSON(w, map[string][]spotifyItem{"queue": q[:min(len(q), 20)]})
	}
}
