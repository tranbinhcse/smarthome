package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func camTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := OpenDB(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func zoneIs(zs ...string) func(string) bool {
	return func(z string) bool {
		for _, x := range zs {
			if x == z {
				return true
			}
		}
		return false
	}
}

func goodCam() camRec {
	return camRec{Name: "Camera cổng", Zone: "z1", Kind: "hik_cam", Host: "192.168.1.50", User: "admin", Pass: "s3cret"}
}

func TestCamStoreCRUD(t *testing.T) {
	d, ok := camTestDB(t), zoneIs("z1")
	c, err := camSave(d, goodCam(), ok)
	if err != nil || c.ID != "cam_camera_cong" || c.HTTPPort != 80 || c.RTSPPort != 554 {
		t.Fatalf("save: %+v %v", c, err)
	}
	if l, err := camList(d); err != nil || len(l) != 1 || l[0].ID != c.ID {
		t.Fatalf("list: %+v", l)
	}
	// sửa không nhập mật khẩu: giữ mật khẩu cũ
	e := c
	e.Pass, e.Name = "", "Cổng chính"
	if e, err = camSave(d, e, ok); err != nil || e.Pass != "s3cret" || e.ID != c.ID {
		t.Fatalf("edit: %+v %v", e, err)
	}
	// đổi host không nhập mật khẩu: bỏ mật khẩu cũ
	e.Pass, e.Host = "", "192.168.1.51"
	if e, err = camSave(d, e, ok); err != nil || e.Pass != "" {
		t.Fatalf("host change: %+v %v", e, err)
	}
	if g, found, err := camGet(d, c.ID); err != nil || !found || g.Pass != "" || g.Host != "192.168.1.51" {
		t.Fatalf("get: %+v %v", g, found)
	}
	del1, e1 := camDelete(d, c.ID)
	del2, e2 := camDelete(d, c.ID)
	if l, _ := camList(d); !del1 || del2 || e1 != nil || e2 != nil || len(l) != 0 {
		t.Fatal("delete")
	}
	if _, err := camSave(d, camRec{ID: "cam_nope", Name: "x", Zone: "z1", Kind: "rtsp", Host: "h", MainPath: "/a"}, ok); err == nil {
		t.Fatal("sửa id không tồn tại phải lỗi")
	}
}

func TestCamStoreUniqueIDs(t *testing.T) {
	d, ok := camTestDB(t), zoneIs("z1")
	a, _ := camSave(d, goodCam(), ok)
	b, _ := camSave(d, goodCam(), ok)
	if a.ID != "cam_camera_cong" || b.ID != "cam_camera_cong_2" {
		t.Fatalf("ids: %s %s", a.ID, b.ID)
	}
	// không bao giờ trùng mẫu id của đầu ghi cam_ch<số>
	n := goodCam()
	n.Name = "ch1"
	c, _ := camSave(d, n, ok)
	if nvrCamID.MatchString(c.ID) {
		t.Fatalf("id đụng id đầu ghi: %s", c.ID)
	}
}

func TestCamStoreRejectsInvalid(t *testing.T) {
	d, ok := camTestDB(t), zoneIs("z1")
	rtsp := func(m func(*camRec)) camRec {
		c := camRec{Name: "A", Zone: "z1", Kind: "rtsp", Host: "10.0.0.2", MainPath: "/s1", SubPath: "/s2"}
		m(&c)
		return c
	}
	cases := []struct {
		name string
		c    camRec
		want string
	}{
		{"tên rỗng", rtsp(func(c *camRec) { c.Name = "" }), "tên"},
		{"tên 41", rtsp(func(c *camRec) { c.Name = strings.Repeat("a", 41) }), "tên"},
		{"host http", rtsp(func(c *camRec) { c.Host = "http://1.2.3.4" }), "địa chỉ"},
		{"host /", rtsp(func(c *camRec) { c.Host = "1.2.3.4/x" }), "địa chỉ"},
		{"host space", rtsp(func(c *camRec) { c.Host = "1.2.3 4" }), "địa chỉ"},
		{"host có cổng", rtsp(func(c *camRec) { c.Host = "1.2.3.4:554" }), "địa chỉ"},
		{"http port 0", rtsp(func(c *camRec) { c.HTTPPort = -1 }), "HTTP"},
		{"http port 70000", rtsp(func(c *camRec) { c.HTTPPort = 70000 }), "HTTP"},
		{"rtsp port 70000", rtsp(func(c *camRec) { c.RTSPPort = 70000 }), "RTSP"},
		{"kind abc", rtsp(func(c *camRec) { c.Kind = "abc" }), "loại"},
		{"zone lạ", rtsp(func(c *camRec) { c.Zone = "zx" }), "khu vực"},
		{"main không /", rtsp(func(c *camRec) { c.MainPath = "s1" }), "đường dẫn"},
		{"main rtsp://", rtsp(func(c *camRec) { c.MainPath = "/rtsp://x" }), "đường dẫn"},
		{"main rỗng", rtsp(func(c *camRec) { c.MainPath = "" }), "đường dẫn"},
		{"sub space", rtsp(func(c *camRec) { c.SubPath = "/a b" }), "đường dẫn"},
	}
	for _, tc := range cases {
		_, err := camSave(d, tc.c, ok)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
			t.Errorf("%s: err=%v, cần chứa %q", tc.name, err, tc.want)
		}
	}
	if l, _ := camList(d); len(l) != 0 {
		t.Fatal("bản ghi sai không được lưu")
	}
	// cổng 0 nghĩa là mặc định, không phải lỗi
	if _, err := camSave(d, rtsp(func(c *camRec) { c.HTTPPort = 0 }), ok); err != nil {
		t.Fatal(err)
	}
}

func TestCamViewHidesPassword(t *testing.T) {
	v := camRec{ID: "cam_a", Name: "A", Pass: "hunter2xyz", Host: "h"}.view()
	if _, has := v["pass"]; has || v["pass_set"] != true {
		t.Fatalf("view: %v", v)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "hunter2xyz") {
		t.Fatal("lộ mật khẩu: " + string(b))
	}
	if (camRec{}).view()["pass_set"] != false {
		t.Fatal("pass_set phải false khi chưa có mật khẩu")
	}
}

func TestCamRTSPURL(t *testing.T) {
	h := camRec{Kind: "hik_cam", Host: "10.0.0.5", RTSPPort: 8554, HTTPPort: 8080, User: "admin", Pass: "pw"}
	if g := h.rtsp(false); g != "rtsp://admin:pw@10.0.0.5:8554/Streaming/Channels/101" {
		t.Fatal(g)
	}
	if g := h.rtsp(true); g != "rtsp://admin:pw@10.0.0.5:8554/Streaming/Channels/102" {
		t.Fatal(g)
	}
	if g := h.snapURL(); g != "http://10.0.0.5:8080/ISAPI/Streaming/channels/101/picture" {
		t.Fatal(g)
	}
	r := camRec{Kind: "rtsp", Host: "10.0.0.6", RTSPPort: 554, User: "u", Pass: "p", MainPath: "/stream1", SubPath: "/stream2"}
	if r.rtsp(false) != "rtsp://u:p@10.0.0.6:554/stream1" || r.rtsp(true) != "rtsp://u:p@10.0.0.6:554/stream2" || r.snapURL() != "" {
		t.Fatal(r.rtsp(false), r.rtsp(true))
	}
	r.SubPath = ""
	if r.rtsp(true) != "rtsp://u:p@10.0.0.6:554/stream1" {
		t.Fatal("thiếu SubPath phải dùng MainPath")
	}
	if len(camTemplates) < 3 || camTemplates[0].MainPath == "" {
		t.Fatal("camTemplates")
	}
}

// Review Focus 1: mật khẩu có ký tự đặc biệt phải được mã hoá trong URL và lấy lại đúng.
func TestCamRTSPURLEscapesPassword(t *testing.T) {
	const pw = "p@ss:w/rd#%"
	for _, c := range []camRec{
		{Kind: "hik_cam", Host: "10.0.0.5", RTSPPort: 554, User: "ad min", Pass: pw},
		{Kind: "rtsp", Host: "10.0.0.5", RTSPPort: 554, User: "ad min", Pass: pw, MainPath: "/s"},
	} {
		s := c.rtsp(false)
		if strings.Contains(s, pw) {
			t.Fatalf("mật khẩu chưa mã hoá: %s", s)
		}
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		gp, _ := u.User.Password()
		if u.User.Username() != "ad min" || gp != pw || u.Hostname() != "10.0.0.5" || u.Port() != "554" {
			t.Fatalf("lấy lại sai: %q %q %s", u.User.Username(), gp, s)
		}
	}
}

// Dòng hỏng (JSON không đọc được) không được bị camera mới ghi đè; camList bỏ qua dòng hỏng, camGet báo lỗi.
func TestCamStoreCorruptRowNotOverwritten(t *testing.T) {
	d, ok := camTestDB(t), zoneIs("z1")
	const bad = "{not json"
	if _, err := d.sql.Exec(`INSERT INTO cameras(id, data) VALUES('cam_camera_cong', ?)`, bad); err != nil {
		t.Fatal(err)
	}
	c, err := camSave(d, goodCam(), ok)
	if err != nil || c.ID != "cam_camera_cong_2" {
		t.Fatalf("save: %+v %v", c, err)
	}
	var raw string
	d.sql.QueryRow(`SELECT data FROM cameras WHERE id='cam_camera_cong'`).Scan(&raw)
	if raw != bad {
		t.Fatalf("dòng hỏng bị ghi đè: %q", raw)
	}
	if l, err := camList(d); err != nil || len(l) != 1 || l[0].ID != c.ID {
		t.Fatalf("list: %+v %v", l, err)
	}
	if _, found, err := camGet(d, "cam_camera_cong"); found || err == nil {
		t.Fatalf("camGet dòng hỏng phải lỗi: %v %v", found, err)
	}
	e := goodCam()
	e.ID = "cam_camera_cong"
	if _, err := camSave(d, e, ok); err == nil {
		t.Fatal("sửa dòng hỏng phải lỗi")
	}
}

func TestCamStoreDBErrorsSurface(t *testing.T) {
	d := camTestDB(t)
	d.sql.Close()
	if _, err := camList(d); err == nil {
		t.Error("camList")
	}
	if _, _, err := camGet(d, "x"); err == nil {
		t.Error("camGet")
	}
	if _, err := camDelete(d, "x"); err == nil {
		t.Error("camDelete")
	}
	if _, err := camSave(d, goodCam(), nil); err == nil {
		t.Error("camSave")
	}
}

// ---- API camera nối thẳng (mountCameras) ----

// apiEnv: hub có hai phòng, đăng nhập bằng PIN, token trợ lý bật.
func apiEnv(t *testing.T) (*Hub, http.Handler, *http.Cookie) {
	t.Helper()
	h := NewHub(Config{Zones: []Zone{{ID: "kho", Name: "Kho"}, {ID: "cafe", Name: "Quán"}}}, newDB(t), func(string, string) {})
	a := NewAuth("1234", h.db)
	a.assistant = "tok-tro-ly"
	mux := newMux(h, a)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"pin":"1234"}`)))
	return h, mux, w.Result().Cookies()[0]
}

func TestCamerasAPI(t *testing.T) {
	t.Setenv("HUBD_GEN_DIR", t.TempDir())
	restarts := make(chan struct{}, 4)
	old := restartHub
	restartHub = func() { restarts <- struct{}{} }
	defer func() { restartHub = old }()
	h, mux, ck := apiEnv(t)

	// chưa đăng nhập, và token trợ lý: 401 cho mọi route
	for _, rt := range [][2]string{{"GET", "/api/cameras"}, {"PUT", "/api/cameras"}, {"DELETE", "/api/cameras/x"},
		{"POST", "/api/cameras/test"}, {"POST", "/api/cameras/scan"}, {"GET", "/api/cameras/scan"}} {
		if w := call(mux, rt[0], rt[1], "{}", ""); w.Code != 401 {
			t.Errorf("%v không cookie: %d", rt, w.Code)
		}
		if w := call(mux, rt[0], rt[1], "{}", "tok-tro-ly"); w.Code != 401 {
			t.Errorf("%v token trợ lý: %d", rt, w.Code)
		}
	}

	// thêm
	w := call(mux, "PUT", "/api/cameras", `{"name":"Camera cổng","zone":"kho","kind":"hik_cam","host":"192.168.1.50","user":"admin","pass":"`+camPass+`"}`, "", ck)
	var saved map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved["id"] != "cam_camera_cong" || saved["pass_set"] != true || saved["http_port"] != float64(80) {
		t.Fatalf("thêm: %d %s", w.Code, w.Body)
	}
	if _, has := saved["pass"]; has || strings.Contains(w.Body.String(), camPass) {
		t.Fatalf("PUT lộ mật khẩu: %s", w.Body)
	}
	select {
	case <-restarts:
	case <-time.After(time.Second):
		t.Fatal("thêm camera phải khởi động lại hub (camera.yaml đổi)")
	}
	// sửa, bỏ trống mật khẩu = giữ mật khẩu cũ
	w = call(mux, "PUT", "/api/cameras", `{"id":"cam_camera_cong","name":"Cổng chính","zone":"cafe","kind":"hik_cam","host":"192.168.1.50","user":"admin"}`, "", ck)
	if w.Code != 200 {
		t.Fatalf("sửa: %d %s", w.Code, w.Body)
	}
	if g, _, _ := camGet(h.db, "cam_camera_cong"); g.Name != "Cổng chính" || g.Pass != camPass {
		t.Fatalf("sau sửa: %+v", g)
	}
	// GET không lộ mật khẩu, có mẫu đường dẫn
	w = call(mux, "GET", "/api/cameras", "", "", ck)
	var list struct {
		Cameras   []map[string]any `json:"cameras"`
		Templates []map[string]any `json:"templates"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Cameras) != 1 || len(list.Templates) != 3 ||
		strings.Contains(w.Body.String(), camPass) || list.Cameras[0]["pass"] != nil || list.Cameras[0]["pass_set"] != true {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	// sai: khu không có, tên rỗng, id lạ, JSON hỏng; lời báo không lộ lỗi DB
	for body, want := range map[string]int{
		`{"name":"A","zone":"khong-co","kind":"rtsp","host":"h","main_path":"/a"}`:                400,
		`{"name":"","zone":"kho","kind":"rtsp","host":"h","main_path":"/a"}`:                      400,
		`{"id":"cam_khong_co","name":"A","zone":"kho","kind":"rtsp","host":"h","main_path":"/a"}`: 404,
		`khong phai json`: 400,
	} {
		if w := call(mux, "PUT", "/api/cameras", body, "", ck); w.Code != want || strings.Contains(strings.ToLower(w.Body.String()), "sql") {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	for len(restarts) > 0 {
		<-restarts
	}
	// xoá
	if w := call(mux, "DELETE", "/api/cameras/cam_camera_cong", "", "", ck); w.Code != 204 {
		t.Fatalf("xoá: %d", w.Code)
	}
	select {
	case <-restarts:
	case <-time.After(time.Second):
		t.Fatal("xoá camera phải khởi động lại hub")
	}
	if w := call(mux, "DELETE", "/api/cameras/cam_camera_cong", "", "", ck); w.Code != 404 {
		t.Fatalf("xoá lần hai: %d", w.Code)
	}
}

// Camera Hikvision giả đòi Digest và chỉ nhận đúng mật khẩu; đếm số lần đúng/sai.
type digestCam struct {
	*httptest.Server
	mu        sync.Mutex
	good, bad int
}

var digRe = regexp.MustCompile(`(\w+)="?([^",]*)"?`)

func newDigestCam(t *testing.T, pass string) *digestCam {
	d := &digestCam{}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("Authorization")
		if a == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="CAM", nonce="n1", qop="auth"`)
			w.WriteHeader(401)
			return
		}
		p := map[string]string{}
		for _, m := range digRe.FindAllStringSubmatch(strings.TrimPrefix(a, "Digest "), -1) {
			p[m[1]] = m[2]
		}
		h := func(s string) string { b := md5.Sum([]byte(s)); return hex.EncodeToString(b[:]) }
		want := h(h(p["username"]+":CAM:"+pass) + ":n1:" + p["nc"] + ":" + p["cnonce"] + ":auth:" + h("GET:"+r.URL.RequestURI()))
		d.mu.Lock()
		defer d.mu.Unlock()
		if p["response"] != want {
			d.bad++
			w.Header().Set("WWW-Authenticate", `Digest realm="CAM", nonce="n1", qop="auth"`)
			w.WriteHeader(401)
			return
		}
		d.good++
		io.WriteString(w, "<DeviceInfo><model>DS-2CD2143</model><firmwareVersion>V5.7.1</firmwareVersion></DeviceInfo>")
	}))
	t.Cleanup(d.Close)
	return d
}

func (d *digestCam) port() int {
	u, _ := url.Parse(d.URL)
	p, _ := strconv.Atoi(u.Port())
	return p
}

func testBody(host string, port int, extra string) string {
	return fmt.Sprintf(`{"kind":"hik_cam","host":%q,"http_port":%d,"user":"admin"%s}`, host, port, extra)
}

func TestCamerasTestKeepsSecretWithHost(t *testing.T) {
	h, mux, ck := apiEnv(t)
	a, b := newDigestCam(t, camPass), newDigestCam(t, camPass)
	s, err := camSave(h.db, camRec{Name: "Cổng", Zone: "kho", Kind: "hik_cam", Host: "127.0.0.1", HTTPPort: a.port(), User: "admin", Pass: camPass}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// cùng host, pass rỗng: dùng mật khẩu đã lưu
	w := call(mux, "POST", "/api/cameras/test", testBody("127.0.0.1", a.port(), `,"id":"`+s.ID+`"`), "", ck)
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out["ok"] != true || out["model"] != "DS-2CD2143" || out["firmware"] != "V5.7.1" || a.good != 1 {
		t.Fatalf("cùng host: %s good=%d", w.Body, a.good)
	}
	// đổi host (cùng máy, tên khác), pass rỗng: không gửi mật khẩu cũ. Camera B chỉ nhận mật khẩu cũ nên phải thấy sai.
	w = call(mux, "POST", "/api/cameras/test", testBody("localhost", b.port(), `,"id":"`+s.ID+`"`), "", ck)
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out["ok"] != false || b.good != 0 || b.bad != 1 {
		t.Fatalf("đổi host: %s good=%d bad=%d", w.Body, b.good, b.bad)
	}
	// mật khẩu nhập tay luôn được dùng
	w = call(mux, "POST", "/api/cameras/test", testBody("localhost", b.port(), `,"pass":"`+camPass+`"`), "", ck)
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out["ok"] != true {
		t.Fatalf("pass nhập: %s", w.Body)
	}
	// rtsp: chỉ nối TCP
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	w = call(mux, "POST", "/api/cameras/test", fmt.Sprintf(`{"kind":"rtsp","host":"127.0.0.1","rtsp_port":%d}`, l.Addr().(*net.TCPAddr).Port), "", ck)
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out["ok"] != true {
		t.Fatalf("rtsp: %s", w.Body)
	}
}

func TestCamerasTestMessages(t *testing.T) {
	_, mux, ck := apiEnv(t)
	cam := newDigestCam(t, "mat-khau-that")
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := l.Addr().(*net.TCPAddr).Port
	l.Close()
	secret := "mk-sai-bi-mat-77"
	run := func(port int, kind string) string {
		w := call(mux, "POST", "/api/cameras/test", fmt.Sprintf(`{"kind":%q,"host":"127.0.0.1","http_port":%d,"rtsp_port":%d,"user":"admin","pass":%q}`, kind, port, port, secret), "", ck)
		var out map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out["ok"] != false {
			t.Fatalf("%s: %d %s", kind, w.Code, w.Body)
		}
		for _, leak := range []string{"127.0.0.1", secret, strconv.Itoa(port)} {
			if strings.Contains(w.Body.String(), leak) {
				t.Errorf("câu báo lộ %q: %s", leak, w.Body)
			}
		}
		return out["message"].(string)
	}
	if m := run(cam.port(), "hik_cam"); !strings.Contains(m, "Sai tài khoản hoặc mật khẩu") {
		t.Errorf("sai mật khẩu: %s", m)
	}
	if m := run(cam.port(), "hik_cam"); !strings.Contains(m, "15 phút") || strings.Contains(m, "Sai tài khoản") {
		t.Errorf("bị khoá: %s", m)
	}
	if m := run(dead, "hik_cam"); !strings.Contains(m, "Không kết nối được camera") {
		t.Errorf("không nối được: %s", m)
	}
	if m := run(dead, "rtsp"); !strings.Contains(m, "RTSP") {
		t.Errorf("rtsp không nối được: %s", m)
	}
}

func TestBackupExcludesCameraPasswords(t *testing.T) {
	h, mux, ck := apiEnv(t)
	if _, err := camSave(h.db, camRec{Name: "Cổng", Zone: "kho", Kind: "hik_cam", Host: "192.168.1.50", User: "admin", Pass: camPass}, nil); err != nil {
		t.Fatal(err)
	}
	h.db.PutSetting("camera", map[string]any{"lan_cidr": "192.168.1.0/24"})
	w := call(mux, "GET", "/api/backup", "", "", ck)
	if w.Code != 200 || strings.Contains(w.Body.String(), camPass) || !strings.Contains(w.Body.String(), "192.168.1.0/24") {
		t.Fatalf("backup: %d %s", w.Code, w.Body)
	}
}

func TestCamerasScanRoutes(t *testing.T) {
	h, mux, ck := apiEnv(t)
	if w := call(mux, "POST", "/api/cameras/scan", "", "", ck); w.Code != 400 || !strings.Contains(w.Body.String(), "192.168.1.0/24") {
		t.Fatalf("chưa nhập dải: %d %s", w.Code, w.Body)
	}
	h.db.PutSetting("camera", map[string]any{"lan_cidr": "10.0.0.0/8"})
	if w := call(mux, "POST", "/api/cameras/scan", "", "", ck); w.Code != 400 || !strings.Contains(w.Body.String(), "quá rộng") {
		t.Fatalf("dải rộng: %d %s", w.Code, w.Body)
	}
	if w := call(mux, "GET", "/api/cameras/scan", "", "", ck); w.Code != 200 || !strings.Contains(w.Body.String(), `"found":[]`) {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
}
