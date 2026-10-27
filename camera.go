package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"hubd/isapi"
)

// Camera Hikvision (mục 15.1): ảnh chụp ISAPI và hình trực tiếp từ go2rtc, cả hai đi qua hubd sau PIN.
// Trình duyệt chỉ thấy /api/cam/...; địa chỉ, tài khoản đầu ghi ở phía máy chủ: trang Cài đặt → Camera (nhóm "nvr"),
// không có thì NVR_HOST/NVR_USER/NVR_PASS trong .env. go2rtc không giữ tài khoản: hubd đưa nguồn RTSP mỗi lần mở luồng.
// Camera = thiết bị có `channel` (số kênh trên đầu ghi); luồng go2rtc tên ch<kênh>.
type cams struct {
	db     *DB
	go2rtc *url.URL
	byID   map[string]Device
	token  string // HUBD_CAMERA_TOKEN: hikbridge đọc cấu hình đầu ghi qua /api/service/nvr; rỗng = tắt

	mu      sync.Mutex
	clients map[string]*http.Client // theo đầu ghi + tài khoản: giữ trạng thái tạm ngừng sau khi bị từ chối mật khẩu
}

func newCams(devs []Device, db *DB) *cams {
	c := &cams{db: db, byID: map[string]Device{}, token: os.Getenv("HUBD_CAMERA_TOKEN"), clients: map[string]*http.Client{}}
	g := os.Getenv("GO2RTC_URL")
	if g == "" {
		g = "http://go2rtc:1984" // chỉ trong mạng Docker, không mở ra LAN
	}
	c.go2rtc, _ = url.Parse(g)
	for _, d := range devs {
		if d.Channel > 0 {
			c.byID[d.ID] = d
		}
	}
	return c
}

// nvrConf: đầu ghi đang dùng (JSON cho hikbridge).
type nvrConf struct {
	On   bool   `json:"enabled"`
	Host string `json:"host"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

var nvrHost = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$`) // IP hay tên[:cổng]; không scheme, không đường dẫn

// conf: nhóm "nvr" có nvr_host thì dùng nhóm đó, không thì .env (cách cũ). enabled=false tắt camera.
// Đọc SQLite mỗi lần gọi nên đổi trên trang Cài đặt là có hiệu lực ngay.
func (c *cams) conf() nvrConf {
	s := c.db.Setting("nvr")
	str := func(k string) string { v, _ := s[k].(string); return v }
	n := nvrConf{Host: strings.TrimSpace(str("nvr_host")), User: strings.TrimSpace(str("nvr_user")), Pass: str("nvr_pass")}
	if n.Host == "" {
		n = nvrConf{Host: os.Getenv("NVR_HOST"), User: os.Getenv("NVR_USER"), Pass: os.Getenv("NVR_PASS")}
	}
	n.On = s["enabled"] != false && nvrHost.MatchString(n.Host)
	return n
}

func (c *cams) client(n nvrConf) *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := n.Host + "\n" + n.User + "\n" + n.Pass
	if c.clients[k] == nil { // ponytail: mỗi tài khoản đã thử một mục, mất khi hubd khởi động lại; đủ cho nhập tay
		c.clients[k] = &http.Client{Timeout: 10 * time.Second, Transport: &isapi.Transport{User: n.User, Pass: n.Pass}}
	}
	return c.clients[k]
}

func (c *cams) register(mux *http.ServeMux, auth *Auth) {
	mux.HandleFunc("GET /api/cam/{id}/snapshot", auth.Require(c.snapshot))
	mux.HandleFunc("GET /api/cam/{id}/ws", auth.Require(c.live))
	mux.HandleFunc("GET /api/cam-js/{file}", auth.Require(c.js))
	mux.HandleFunc("POST /api/setup/nvr/test", auth.Require(c.test))
	mux.HandleFunc("GET /api/service/nvr", c.service)
}

func (c *cams) snapshot(w http.ResponseWriter, r *http.Request) {
	d, ok := c.byID[r.PathValue("id")]
	if !ok {
		http.Error(w, "không có camera này", 404)
		return
	}
	n := c.conf()
	if !n.On {
		http.Error(w, "camera đang tắt hoặc chưa cấu hình đầu ghi", 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/ISAPI/Streaming/channels/%d01/picture", n.Host, d.Channel), nil)
	res, err := c.client(n).Do(req)
	if err != nil {
		log.Printf("ảnh camera %s: %v", d.ID, err)
		http.Error(w, "đầu ghi không trả ảnh", 502)
		return
	}
	defer res.Body.Close()
	// chỉ chuyển ảnh; lỗi XML của đầu ghi ghi log, không đưa ra trình duyệt
	if ct := res.Header.Get("Content-Type"); res.StatusCode != 200 || !strings.HasPrefix(ct, "image/") {
		log.Printf("ảnh camera %s: HTTP %d %s", d.ID, res.StatusCode, ct)
		http.Error(w, "đầu ghi không trả ảnh", 502)
		return
	}
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "private, max-age=30")
	io.Copy(w, io.LimitReader(res.Body, 8<<20))
}

// live chuyển tiếp WebSocket của go2rtc (MSE + báo hiệu WebRTC). Nguồn do hubd dựng từ cấu hình, bỏ mọi tham số
// của trình duyệt: go2rtc nhận src là URL hay lệnh tuỳ ý. name=ch<kênh>: go2rtc giữ một luồng mỗi kênh và chỉ đổi
// nguồn (tài khoản mới có hiệu lực từ lần mở luồng kế tiếp).
func (c *cams) live(w http.ResponseWriter, r *http.Request) {
	d, ok := c.byID[r.PathValue("id")]
	if !ok {
		http.Error(w, "không có camera này", 404)
		return
	}
	n := c.conf()
	if !n.On {
		http.Error(w, "camera đang tắt hoặc chưa cấu hình đầu ghi", 503)
		return
	}
	// go2rtc gửi lại mật khẩu qua RTSP mỗi lần trình phát nối lại: tài khoản vừa bị từ chối (ảnh chụp, Thử kết nối)
	// thì không mở luồng, để đầu ghi không khoá IP của hub.
	if c.client(n).Transport.(*isapi.Transport).Locked() {
		http.Error(w, "đầu ghi vừa từ chối tài khoản, kiểm tra mật khẩu trong Cài đặt → Camera", 503)
		return
	}
	// WebSocket không chịu CORS, và với địa chỉ IP mọi cổng cùng máy là cùng "site" nên cookie PIN vẫn được gửi:
	// chỉ nhận trang của chính hubd.
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err != nil || !strings.EqualFold(u.Host, r.Host) {
			http.Error(w, "nguồn không hợp lệ", 403)
			return
		}
	}
	h := n.Host
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh // cổng trong cấu hình là cổng HTTP (ISAPI); RTSP của đầu ghi ở 554
	}
	src := url.URL{Scheme: "rtsp", User: url.UserPassword(n.User, n.Pass), Host: net.JoinHostPort(h, "554"),
		Path: fmt.Sprintf("/Streaming/Channels/%d02", d.Channel)} // luồng phụ H.264
	c.proxy(w, r, "/api/ws", url.Values{"name": {"ch" + strconv.Itoa(d.Channel)}, "src": {src.String()}}.Encode())
}

// js: trình phát video-rtc.js của go2rtc, lấy từ chính máy chủ go2rtc để luôn cùng phiên bản giao thức.
func (c *cams) js(w http.ResponseWriter, r *http.Request) {
	f := r.PathValue("file")
	if f != "video-rtc.js" {
		http.Error(w, "không có", 404)
		return
	}
	c.proxy(w, r, "/"+f, "")
}

func (c *cams) proxy(w http.ResponseWriter, r *http.Request, path, query string) {
	(&httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(c.go2rtc)
			p.Out.URL.Path, p.Out.URL.RawPath, p.Out.URL.RawQuery = path, "", query
			p.Out.Header.Del("Cookie") // token PIN của hub không đi tiếp
			p.Out.Header.Del("Origin") // go2rtc so Origin với Host của nó; hub đã kiểm PIN
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("go2rtc: %v", err)
			http.Error(w, "go2rtc chưa chạy", 502)
		},
	}).ServeHTTP(w, r)
}

// nvrPatch: đổi IP đầu ghi mà không nhập mật khẩu mới thì bỏ mật khẩu đã lưu, để không gửi nó tới máy khác
// (cùng quy tắc với nút Thử kết nối). Gọi trước khi lưu nhóm "nvr" (settings.go).
func nvrPatch(db *DB, patch map[string]any) {
	h, ok := patch["nvr_host"].(string)
	old, _ := db.Setting("nvr")["nvr_host"].(string)
	if p, _ := patch["nvr_pass"].(string); ok && p == "" && strings.TrimSpace(h) != strings.TrimSpace(old) {
		patch["nvr_pass"] = nil
	}
}

// test: nút "Thử kết nối". Ô để trống lấy cấu hình đang dùng, nhưng mật khẩu đã lưu chỉ gửi tới đúng đầu ghi đã lưu.
func (c *cams) test(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Host string `json:"nvr_host"`
		User string `json:"nvr_user"`
		Pass string `json:"nvr_pass"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil {
		http.Error(w, "cần JSON {nvr_host, nvr_user, nvr_pass}", 400)
		return
	}
	n := c.conf()
	if h := strings.TrimSpace(b.Host); h != "" && h != n.Host {
		n = nvrConf{Host: h}
	}
	if u := strings.TrimSpace(b.User); u != "" {
		n.User = u
	}
	if b.Pass != "" {
		n.Pass = b.Pass
	}
	if !nvrHost.MatchString(n.Host) || n.User == "" || n.Pass == "" {
		http.Error(w, "nhập IP đầu ghi (vd 192.168.1.64), tài khoản và mật khẩu", 400)
		return
	}
	chs, err := nvrChannels(r.Context(), c.client(n), n.Host)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	writeJSON(w, map[string]any{"channels": chs})
}

type nvrChannel struct {
	ID   int    `json:"id" xml:"id"`
	Name string `json:"name" xml:"name"`
}

// nvrChannels: kênh IP có camera trên đầu ghi. Lỗi là câu cho người dùng, không kèm địa chỉ hay tài khoản.
func nvrChannels(ctx context.Context, cl *http.Client, host string) ([]nvrChannel, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/ISAPI/ContentMgmt/InputProxy/channels", nil)
	res, err := cl.Do(req)
	if errors.Is(err, isapi.ErrLocked) {
		return nil, errors.New("đầu ghi vừa từ chối tài khoản này, hub tạm ngừng thử 15 phút để đầu ghi không khoá IP. Kiểm tra lại mật khẩu")
	}
	if err != nil {
		log.Printf("thử kết nối đầu ghi: %v", err)
		return nil, errors.New("không kết nối được đầu ghi: kiểm tra IP; đầu ghi đổi cổng web khác 80 thì nhập IP:cổng (vd 192.168.1.10:8080)")
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, errors.New("sai tài khoản hoặc mật khẩu đầu ghi (hub tạm ngừng thử tài khoản này 15 phút)")
	case http.StatusForbidden:
		return nil, errors.New("tài khoản không có quyền đọc danh sách kênh")
	default:
		return nil, fmt.Errorf("đầu ghi trả HTTP %d", res.StatusCode)
	}
	x := struct {
		Ch []nvrChannel `xml:"InputProxyChannel"`
	}{Ch: []nvrChannel{}}
	if xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&x) != nil {
		return nil, errors.New("đầu ghi trả dữ liệu lạ (không phải ISAPI?)")
	}
	return x.Ch, nil
}

// service: hikbridge đọc cấu hình đầu ghi (cả mật khẩu) bằng token camera riêng. Cookie PIN và token trợ lý không dùng được.
func (c *cams) service(w http.ResponseWriter, r *http.Request) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || c.token == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(c.token)) != 1 {
		http.Error(w, "cần token camera", http.StatusUnauthorized)
		return
	}
	writeJSON(w, c.conf())
}

var chZone = regexp.MustCompile(`^ch([0-9]{1,3})_zone$`)

type camDev struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Zone        string `yaml:"zone"`
	Kind        string `yaml:"kind"`
	Channel     int    `yaml:"channel"`
	OnlineTopic string `yaml:"online_topic"`
}

// camDevices: kênh đã gán khu trên trang Cài đặt (ch<N>_zone, ch<N>_name) thành thiết bị camera, theo số kênh.
func camDevices(s map[string]any) []camDev {
	out := []camDev{}
	if s["enabled"] == false {
		return out
	}
	for k, v := range s {
		m := chZone.FindStringSubmatch(k)
		zone, _ := v.(string)
		if m == nil || zone == "" {
			continue
		}
		ch, _ := strconv.Atoi(m[1])
		if ch < 1 {
			continue
		}
		name, _ := s["ch"+m[1]+"_name"].(string)
		if name = strings.TrimSpace(name); name == "" {
			name = fmt.Sprintf("Camera kênh %d", ch)
		}
		out = append(out, camDev{fmt.Sprintf("cam_ch%d", ch), name, zone, "camera", ch, fmt.Sprintf("hik/%d/online", ch)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out
}

// syncCamGen ghi <HUBD_GEN_DIR>/camera.yaml (gộp như cấu hình Tuya, gendir.go) khi danh sách camera đổi. File đổi thì
// hubd khởi động lại để nạp (main.go), nên đổi mật khẩu hay IP không ghi file. Trả true nếu đã ghi.
func syncCamGen(db *DB) bool {
	dir := os.Getenv("HUBD_GEN_DIR")
	if dir == "" {
		return false
	}
	y, _ := yaml.Marshal(map[string]any{"devices": camDevices(db.Setting("nvr"))})
	b := append([]byte("# Sinh từ trang Cài đặt → Camera (hubd). Không sửa tay: lưu trên dashboard sẽ ghi đè.\n"), y...)
	f := filepath.Join(dir, "camera.yaml")
	if old, err := os.ReadFile(f); err == nil && string(old) == string(b) {
		return false
	}
	if err := os.WriteFile(f+".tmp", b, 0o644); err != nil || os.Rename(f+".tmp", f) != nil {
		log.Printf("không ghi được %s: %v", f, err)
		return false
	}
	return true
}
