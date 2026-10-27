package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const nvrPass = "bimat-nvr-123"

var jpeg = []byte("\xff\xd8\xff\xe0 anh chup \xff\xd9")

// Đầu ghi giả: đòi Digest, trả JPEG cho kênh 3, XML lỗi cho kênh khác.
func fakeNVR(t *testing.T, paths *[]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("Authorization")
		if a == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="NVR", nonce="n1", qop="auth"`)
			w.WriteHeader(401)
			return
		}
		if !strings.Contains(a, `username="viewer"`) || strings.Contains(a, nvrPass) {
			t.Errorf("Authorization sai: %s", a)
		}
		*paths = append(*paths, r.URL.Path)
		if r.URL.Path != "/ISAPI/Streaming/channels/301/picture" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(403)
			io.WriteString(w, "<ResponseStatus><statusString>Invalid Operation</statusString></ResponseStatus>")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpeg)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func camMux(t *testing.T) (http.Handler, *http.Cookie) {
	cfg := Config{Devices: []Device{
		{ID: "cam3", Name: "Camera quầy", Kind: "camera", Zone: "cafe", Channel: 3},
		{ID: "cam4", Name: "Camera kho", Kind: "camera", Zone: "kho", Channel: 4},
		{ID: "den", Kind: "kg", CommandTopic: "den/cmd"},
	}}
	h := NewHub(cfg, newDB(t), func(string, string) {})
	mux := newMux(h, NewAuth("1234", h.db))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"pin":"1234"}`)))
	return mux, w.Result().Cookies()[0]
}

func get(mux http.Handler, u string, ck *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", u, nil)
	if ck != nil {
		r.AddCookie(ck)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// Ảnh chụp đi qua hubd: cần PIN, hubd tự xác thực với đầu ghi, trình duyệt không thấy tài khoản hay địa chỉ đầu ghi.
func TestCamSnapshotProxy(t *testing.T) {
	var paths []string
	nvr := fakeNVR(t, &paths)
	host := strings.TrimPrefix(nvr.URL, "http://")
	t.Setenv("NVR_HOST", host)
	t.Setenv("NVR_USER", "viewer")
	t.Setenv("NVR_PASS", nvrPass)
	mux, ck := camMux(t)

	if w := get(mux, "/api/cam/cam3/snapshot", nil); w.Code != 401 || len(paths) != 0 {
		t.Fatalf("chưa đăng nhập: code=%d, đầu ghi bị gọi %v", w.Code, paths)
	}
	w := get(mux, "/api/cam/cam3/snapshot", ck)
	if w.Code != 200 || w.Body.String() != string(jpeg) || w.Header().Get("Content-Type") != "image/jpeg" ||
		!strings.Contains(w.Header().Get("Cache-Control"), "private") {
		t.Fatalf("code=%d type=%q cache=%q body=%q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"), w.Body)
	}
	for _, x := range []string{"/api/cam/den/snapshot", "/api/cam/khong-co/snapshot"} {
		if c := get(mux, x, ck).Code; c != 404 {
			t.Errorf("%s: code=%d, muốn 404", x, c)
		}
	}
	bad := get(mux, "/api/cam/cam4/snapshot", ck)
	if bad.Code != 502 || strings.Contains(bad.Body.String(), "ResponseStatus") {
		t.Errorf("đầu ghi trả lỗi: code=%d body=%q (không chuyển nguyên văn lỗi của đầu ghi)", bad.Code, bad.Body)
	}
	for _, r := range []*httptest.ResponseRecorder{w, bad, get(mux, "/api/config", ck)} {
		dump := r.Body.String() + strings.Join(r.Header().Values("Content-Type"), "") + strings.Join(r.Header().Values("Location"), "")
		if strings.Contains(dump, nvrPass) || strings.Contains(dump, host) || strings.Contains(dump, "viewer") {
			t.Errorf("lộ thông tin đầu ghi ra trình duyệt: %q", dump)
		}
	}
	if len(paths) != 2 || paths[0] != "/ISAPI/Streaming/channels/301/picture" {
		t.Errorf("đường dẫn ISAPI: %v", paths)
	}
}

func TestCamSnapshotNotConfigured(t *testing.T) {
	t.Setenv("NVR_HOST", "")
	mux, ck := camMux(t)
	if c := get(mux, "/api/cam/cam3/snapshot", ck).Code; c != 503 {
		t.Fatalf("chưa cấu hình đầu ghi: code=%d, muốn 503", c)
	}
}

// Xem trực tiếp: hubd chuyển tiếp WebSocket của go2rtc sau PIN. Tên luồng do cấu hình quyết định,
// không lấy từ trình duyệt (go2rtc nhận src là URL/lệnh tuỳ ý).
func TestCamLiveProxyPinsStream(t *testing.T) {
	type hit struct{ path, query, cookie, origin string }
	var hits []hit
	g2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, hit{r.URL.Path, r.URL.RawQuery, r.Header.Get("Cookie"), r.Header.Get("Origin")})
		if r.Header.Get("Upgrade") == "websocket" {
			c, rw, _ := w.(http.Hijacker).Hijack()
			rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\nxin chao")
			rw.Flush()
			c.Close()
			return
		}
		io.WriteString(w, "ok")
	}))
	defer g2.Close()
	t.Setenv("GO2RTC_URL", g2.URL)
	t.Setenv("NVR_HOST", "")
	mux, ck := camMux(t)
	if c := get(mux, "/api/cam/cam3/ws", ck).Code; c != 503 || len(hits) != 0 {
		t.Fatalf("chưa cấu hình đầu ghi: code=%d hits=%v, muốn 503", c, hits)
	}
	t.Setenv("NVR_HOST", "192.168.1.64:80") // cổng HTTP của đầu ghi; RTSP luôn 554
	t.Setenv("NVR_USER", "viewer")
	t.Setenv("NVR_PASS", nvrPass)

	if c := get(mux, "/api/cam/cam3/ws?src=exec:rm", nil).Code; c != 401 || len(hits) != 0 {
		t.Fatalf("chưa đăng nhập: code=%d hits=%v", c, hits)
	}
	// WebSocket không chịu CORS: trang khác nguồn (kể cả dịch vụ khác trên cùng máy, cùng "site" nên cookie vẫn đi)
	// không được mở luồng.
	r := httptest.NewRequest("GET", "/api/cam/cam3/ws", nil)
	r.AddCookie(ck)
	r.Header.Set("Origin", "http://192.168.1.254:9000")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 || len(hits) != 0 {
		t.Fatalf("Origin lạ: code=%d hits=%v, muốn 403", w.Code, hits)
	}
	r = httptest.NewRequest("GET", "/api/cam/cam3/ws?src=exec:rm", nil)
	r.AddCookie(ck)
	r.Header.Set("Origin", "http://example.com") // cùng Host của request
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	// go2rtc không giữ tài khoản đầu ghi: hubd đưa nguồn RTSP (cấu hình hiện hành) kèm tên luồng ch<kênh> mỗi lần mở.
	if len(hits) != 1 {
		t.Fatalf("code=%d hits=%+v", w.Code, hits)
	}
	q, _ := url.ParseQuery(hits[0].query)
	src := "rtsp://viewer:" + nvrPass + "@192.168.1.64:554/Streaming/Channels/302"
	if w.Code != 200 || len(hits) != 1 || hits[0].path != "/api/ws" || len(q) != 2 || q.Get("name") != "ch3" || q.Get("src") != src ||
		hits[0].cookie != "" || hits[0].origin != "" {
		t.Fatalf("code=%d hits=%+v, muốn /api/ws?name=ch3&src=%s, không Cookie, không Origin", w.Code, hits, src)
	}
	if strings.Contains(w.Body.String(), nvrPass) {
		t.Fatal("lộ mật khẩu đầu ghi ra trình duyệt")
	}
	for u, want := range map[string]int{"/api/cam-js/video-rtc.js": 200, "/api/cam-js/video-stream.js": 404,
		"/api/cam-js/stream.html": 404, "/api/cam-js/..%2Fapi%2Fstreams": 404, "/api/cam/den/ws": 404} {
		if c := get(mux, u, ck).Code; c != want {
			t.Errorf("%s: code=%d, muốn %d", u, c, want)
		}
	}
	if last := hits[len(hits)-1]; last.path != "/video-rtc.js" {
		t.Errorf("go2rtc nhận đường dẫn lạ: %+v", hits)
	}

	// Nâng cấp WebSocket thật qua hubd (cần Hijack xuyên qua lớp kiểm PIN).
	srv := httptest.NewServer(mux)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/cam/cam3/ws", nil)
	req.AddCookie(ck)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 101 {
		t.Fatalf("upgrade: err=%v res=%v", err, res)
	}
	line, _ := bufio.NewReader(res.Body.(io.ReadWriteCloser)).ReadString('\n')
	res.Body.Close()
	if line != "xin chao" {
		t.Fatalf("dữ liệu sau upgrade: %q", line)
	}
}

// Sự kiện camera (hik/<kênh>/vmd) và binary_sensor ESPHome gửi ON/OFF: là cảm biến thường, có lịch sử và chạy được luật.
func TestBinaryEntityFeedsRules(t *testing.T) {
	var sent [][2]string
	cfg := Config{
		Entities: []Entity{{ID: "cam3.vmd", Topic: "hik/3/vmd"}},
		Devices:  []Device{{ID: "den_san", CommandTopic: "den/cmd"}},
		Rules:    []Rule{{ID: "co_nguoi", Entity: "cam3.vmd", Above: f(0.5), Then: []Action{{Set: "den_san", To: "on"}}}},
	}
	h := NewHub(cfg, newDB(t), func(tp, p string) { sent = append(sent, [2]string{tp, p}) })
	h.Handle("hik/3/vmd", "OFF", t0)
	h.Handle("hik/3/vmd", "ON", t0.Add(time.Second))
	h.Handle("hik/3/vmd", "OFF", t0.Add(2*time.Second))
	if len(sent) != 1 || sent[0] != [2]string{"den/cmd", "ON"} {
		t.Fatalf("luật: %v", sent)
	}
	if s := h.Snapshot()["cam3.vmd"]; s != "OFF" {
		t.Fatalf("trạng thái = %q", s)
	}
	p, _ := h.db.Query("cam3.vmd", t0, t0.Add(time.Hour), 3600)
	if len(p) != 3 || p[0].V != 0 || p[1].V != 1 || p[2].V != 0 {
		t.Fatalf("lịch sử: %v", p)
	}
}

// Dòng camera trong config.yaml (khối đề xuất, bỏ #) phải đọc ra đúng kênh.
func TestConfigCameraChannel(t *testing.T) {
	p := t.TempDir() + "/config.yaml"
	os.WriteFile(p, []byte("devices:\n  - { id: cam_cafe1, name: \"Camera cafe 1\", zone: cafe, kind: camera, channel: 2, online_topic: hik/2/online }\n"), 0o600)
	cfg, _, err := LoadConfig(p)
	if err != nil || len(cfg.Devices) != 1 || cfg.Devices[0].Channel != 2 || cfg.Devices[0].OnlineTopic != "hik/2/online" {
		t.Fatalf("err=%v devices=%+v", err, cfg.Devices)
	}
}

// Đầu ghi giả cho "Thử kết nối": danh sách kênh IP. Tài khoản "sai" bị từ chối như sai mật khẩu.
func fakeNVRChannels(t *testing.T, hits *int) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if a := r.Header.Get("Authorization"); a == "" || strings.Contains(a, `username="sai"`) {
			w.Header().Set("WWW-Authenticate", `Digest realm="NVR", nonce="n1", qop="auth"`)
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/ISAPI/ContentMgmt/InputProxy/channels" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write(must(os.ReadFile("testdata/nvr-channels.xml")))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// Cấu hình nhập trên trang Cài đặt (nhóm nvr) thắng .env cũ; enabled=false tắt camera.
func TestCamSnapshotUsesSettings(t *testing.T) {
	var paths []string
	nvr := fakeNVR(t, &paths)
	t.Setenv("NVR_HOST", "127.0.0.1:1")
	t.Setenv("NVR_USER", "cu")
	t.Setenv("NVR_PASS", "cu")
	mux, ck := camMux(t)
	call(mux, "PUT", "/api/settings/nvr", `{"nvr_host":"`+strings.TrimPrefix(nvr.URL, "http://")+`","nvr_user":"viewer","nvr_pass":"`+nvrPass+`"}`, "", ck)
	if w := get(mux, "/api/cam/cam3/snapshot", ck); w.Code != 200 || w.Body.String() != string(jpeg) {
		t.Fatalf("ảnh theo cấu hình đã lưu: code=%d body=%q", w.Code, w.Body)
	}
	call(mux, "PUT", "/api/settings/nvr", `{"enabled":false}`, "", ck)
	if c := get(mux, "/api/cam/cam3/snapshot", ck).Code; c != 503 || len(paths) != 1 {
		t.Fatalf("đã tắt camera: code=%d, đầu ghi bị gọi %v", c, paths)
	}
}

// "Thử kết nối": hubd hỏi đầu ghi danh sách kênh. Mật khẩu đã lưu chỉ gửi tới đúng đầu ghi đã lưu; đầu ghi từ chối
// thì không gửi lại cùng tài khoản (không dò mật khẩu, không để đầu ghi khoá IP của hub).
func TestNVRTestConnection(t *testing.T) {
	t.Setenv("NVR_HOST", "")
	var hits int
	nvr := fakeNVRChannels(t, &hits)
	host := strings.TrimPrefix(nvr.URL, "http://")
	mux, ck := camMux(t)
	post := func(body string) *httptest.ResponseRecorder {
		return call(mux, "POST", "/api/setup/nvr/test", body, "", ck)
	}
	good := `{"nvr_host":"` + host + `","nvr_user":"viewer","nvr_pass":"` + nvrPass + `"}`
	if w := call(mux, "POST", "/api/setup/nvr/test", good, ""); w.Code != 401 || hits != 0 {
		t.Fatalf("chưa đăng nhập: code=%d hits=%d", w.Code, hits)
	}
	type ch struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	list := func(w *httptest.ResponseRecorder) []ch {
		var got struct{ Channels []ch }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
			t.Fatalf("code=%d body=%s", w.Code, w.Body)
		}
		return got.Channels
	}
	if got := list(post(good)); len(got) != 2 || got[0] != (ch{1, "Cổng trước"}) || got[1] != (ch{3, "Quầy"}) {
		t.Fatalf("kênh = %+v", got)
	}
	// Đã lưu: ô mật khẩu để trống (trình duyệt không có mật khẩu) thì dùng mật khẩu đã lưu; {} = thử cấu hình đã lưu.
	call(mux, "PUT", "/api/settings/nvr", good, "", ck)
	for _, b := range []string{`{"nvr_host":"` + host + `","nvr_user":"viewer","nvr_pass":""}`, `{}`} {
		if got := list(post(b)); len(got) != 2 {
			t.Fatalf("%s: kênh = %+v", b, got)
		}
	}
	n := hits
	for _, b := range []string{`{"nvr_host":"10.0.0.9","nvr_user":"viewer","nvr_pass":""}`, // đầu ghi khác: không gửi mật khẩu đã lưu
		`{"nvr_host":"http://x","nvr_user":"a","nvr_pass":"b"}`, `{"nvr_host":"1.2.3.4/x?","nvr_user":"a","nvr_pass":"b"}`,
		`{"nvr_host":"a b","nvr_user":"a","nvr_pass":"b"}`, `[1]`} {
		if w := post(b); w.Code != 400 {
			t.Errorf("%s: code=%d, muốn 400", b, w.Code)
		}
	}
	if hits != n {
		t.Fatalf("đầu ghi bị gọi với yêu cầu sai: %d lần", hits-n)
	}
	bad := `{"nvr_host":"` + host + `","nvr_user":"sai","nvr_pass":"p1"}`
	if w := post(bad); w.Code != 502 || !strings.Contains(w.Body.String(), "sai tài khoản") {
		t.Fatalf("sai mật khẩu: code=%d body=%s", w.Code, w.Body)
	}
	n = hits
	if w := post(bad); w.Code != 502 || hits != n {
		t.Fatalf("thử lại tài khoản vừa bị từ chối: code=%d, đầu ghi bị gọi %d lần", w.Code, hits-n)
	}
	if w := post(good); w.Code != 200 {
		t.Fatalf("tài khoản đúng không bị chặn theo: code=%d", w.Code)
	}
}

const camTok = "camera-token-thu-0123456789abcdef"

// hikbridge đọc cấu hình đầu ghi (cả mật khẩu) qua hubd bằng token dịch vụ camera riêng.
// Trình duyệt (PIN) và token trợ lý không đọc được.
func TestCamServiceConfig(t *testing.T) {
	t.Setenv("HUBD_CAMERA_TOKEN", camTok)
	t.Setenv("NVR_HOST", "192.168.1.64")
	t.Setenv("NVR_USER", "viewer")
	t.Setenv("NVR_PASS", nvrPass)
	mux, _, _ := assistantMux(t, Config{})
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	for _, tok := range []string{"", testTok, "sai"} {
		if w := call(mux, "GET", "/api/service/nvr", "", tok, ck...); w.Code != 401 || strings.Contains(w.Body.String(), nvrPass) {
			t.Errorf("token %q: code=%d", tok, w.Code)
		}
	}
	type conf struct {
		On               bool `json:"enabled"`
		Host, User, Pass string
	}
	read := func() (c conf) {
		w := call(mux, "GET", "/api/service/nvr", "", camTok)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &c) != nil {
			t.Fatalf("code=%d body=%s", w.Code, w.Body)
		}
		return c
	}
	if c := read(); c != (conf{true, "192.168.1.64", "viewer", nvrPass}) {
		t.Fatalf(".env cũ: %+v", c)
	}
	call(mux, "PUT", "/api/settings/nvr", `{"nvr_host":"10.0.0.5","nvr_user":"xem","nvr_pass":"moi","enabled":false}`, "", ck...)
	if c := read(); c != (conf{false, "10.0.0.5", "xem", "moi"}) {
		t.Fatalf("cấu hình trang Cài đặt: %+v", c)
	}

	t.Setenv("HUBD_CAMERA_TOKEN", "") // không đặt token: không ai đọc được, kể cả "Bearer " rỗng
	mux, _, _ = assistantMux(t, Config{})
	r := httptest.NewRequest("GET", "/api/service/nvr", nil)
	r.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("token rỗng: code=%d", w.Code)
	}
}

// Kênh gán trên trang Cài đặt sinh thiết bị camera vào thư mục cấu hình sinh (như Tuya), khỏi bỏ # trong config.yaml.
// Chỉ ghi lại khi danh sách đổi: đổi mật khẩu không làm hubd khởi động lại.
func TestCamGeneratedDevices(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HUBD_GEN_DIR", dir)
	mux, ck := camMux(t)
	put := func(b string) {
		if w := call(mux, "PUT", "/api/settings/nvr", b, "", ck); w.Code != 204 {
			t.Fatalf("lưu %s: code=%d", b, w.Code)
		}
	}
	main := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(main, []byte("devices:\n  - {id: den, kind: kg}\n"), 0o644)
	devs := func() []Device {
		cfg, _, err := LoadConfig(main)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Devices
	}
	put(`{"nvr_host":"192.168.1.64","ch12_zone":"kho","ch12_name":"","ch3_zone":"cafe","ch3_name":" Quầy ","ch5_zone":"","ch7_name":"Không khu"}`)
	want := []Device{{ID: "den", Kind: "kg"},
		{ID: "cam_ch3", Name: "Quầy", Zone: "cafe", Kind: "camera", Channel: 3, OnlineTopic: "hik/3/online"},
		{ID: "cam_ch12", Name: "Camera kênh 12", Zone: "kho", Kind: "camera", Channel: 12, OnlineTopic: "hik/12/online"}}
	if got := devs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("thiết bị = %+v\nmuốn %+v", got, want)
	}
	f := filepath.Join(dir, "camera.yaml")
	old := time.Unix(1700000000, 0)
	os.Chtimes(f, old, old)
	put(`{"nvr_pass":"moi","nvr_user":"xem"}`)
	if st, err := os.Stat(f); err != nil || !st.ModTime().Equal(old) {
		t.Fatalf("danh sách không đổi mà file sinh bị ghi lại: %v %v", err, st)
	}
	if b, _ := os.ReadFile(f); strings.Contains(string(b), "moi") || strings.Contains(string(b), "192.168") {
		t.Fatalf("file sinh chứa thông tin đầu ghi: %s", b)
	}
	put(`{"enabled":false}`)
	if got := devs(); len(got) != 1 {
		t.Fatalf("đã tắt camera mà còn thiết bị: %+v", got)
	}
}

// Đầu ghi vừa từ chối tài khoản (ảnh chụp bị 401) thì không mở luồng xem: go2rtc sẽ gửi lại mật khẩu sai qua RTSP mỗi
// lần trình phát nối lại và đầu ghi khoá IP của hub.
func TestCamLiveBlockedAfterRejectedPassword(t *testing.T) {
	var nvrHits, g2Hits int
	nvr := fakeNVRChannels(t, &nvrHits)
	g2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { g2Hits++ }))
	defer g2.Close()
	t.Setenv("GO2RTC_URL", g2.URL)
	t.Setenv("NVR_HOST", "")
	mux, ck := camMux(t)
	call(mux, "PUT", "/api/settings/nvr", `{"nvr_host":"`+strings.TrimPrefix(nvr.URL, "http://")+`","nvr_user":"sai","nvr_pass":"p"}`, "", ck)
	if c := get(mux, "/api/cam/cam3/snapshot", ck).Code; c != 502 {
		t.Fatalf("ảnh chụp với tài khoản sai: code=%d", c)
	}
	if w := get(mux, "/api/cam/cam3/ws", ck); w.Code != 503 || g2Hits != 0 || !strings.Contains(w.Body.String(), "từ chối") {
		t.Fatalf("tài khoản vừa bị từ chối mà vẫn mở luồng: code=%d go2rtc=%d body=%q", w.Code, g2Hits, w.Body)
	}
	call(mux, "PUT", "/api/settings/nvr", `{"nvr_user":"viewer"}`, "", ck) // sửa tài khoản: xem được ngay
	if get(mux, "/api/cam/cam3/ws", ck); g2Hits != 1 {
		t.Fatalf("tài khoản mới vẫn bị chặn: go2rtc=%d", g2Hits)
	}
}
