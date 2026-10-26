package main

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	mux, ck := camMux(t)

	if c := get(mux, "/api/cam/cam3/ws?src=exec:rm", nil).Code; c != 401 || len(hits) != 0 {
		t.Fatalf("chưa đăng nhập: code=%d hits=%v", c, hits)
	}
	r := httptest.NewRequest("GET", "/api/cam/cam3/ws?src=exec:rm", nil)
	r.AddCookie(ck)
	r.Header.Set("Origin", "http://192.168.1.254:8080")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || len(hits) != 1 || hits[0] != (hit{"/api/ws", "src=ch3", "", ""}) {
		t.Fatalf("code=%d hits=%+v, muốn /api/ws?src=ch3, không Cookie, không Origin", w.Code, hits)
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
