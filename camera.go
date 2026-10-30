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
	"sync/atomic"
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
	direct map[string]bool // id thiết bị camera nối thẳng (không có channel); bản ghi đọc từ camstore mỗi lần dùng
	token  string          // HUBD_CAMERA_TOKEN: hikbridge đọc cấu hình đầu ghi qua /api/service/nvr; rỗng = tắt

	mu      sync.Mutex
	clients map[string]*http.Client                                  // theo đầu ghi + tài khoản: giữ trạng thái tạm ngừng sau khi bị từ chối mật khẩu
	fetch   func(context.Context, camRec) ([]byte, string, *snapErr) // nil = theo loại camera; test thay để mô phỏng lỗi
	flights map[string]*snapFlight                                   // yêu cầu ảnh đang bay theo id
	snaps   map[string]snapEntry                                     // ảnh mới nhất của camera nối thẳng, giữ snapTTL
	snapAt  atomic.Int32                                             // chỉ số trong snapPaths mà đầu ghi trả ảnh lần gần nhất
}

// Đường lấy ảnh chụp, thử lần lượt (đầu ghi trả 403/404 notSupport thì thử đường kế): camera đứng riêng và đầu ghi
// đời mới; kênh camera IP qua proxy của đầu ghi; luồng phụ.
var snapPaths = []string{"/ISAPI/Streaming/channels/%d01/picture", "/ISAPI/ContentMgmt/StreamingProxy/channels/%d01/picture",
	"/ISAPI/Streaming/channels/%d02/picture"}

func newCams(devs []Device, db *DB) *cams {
	c := &cams{db: db, byID: map[string]Device{}, token: os.Getenv("HUBD_CAMERA_TOKEN"), clients: map[string]*http.Client{},
		direct: map[string]bool{}, snaps: map[string]snapEntry{}, flights: map[string]*snapFlight{}}
	g := os.Getenv("GO2RTC_URL")
	if g == "" {
		g = "http://go2rtc:1984" // chỉ trong mạng Docker, không mở ra LAN
	}
	c.go2rtc, _ = url.Parse(g)
	for _, d := range devs {
		if d.Channel > 0 {
			c.byID[d.ID] = d
		} else if d.Kind == "camera" {
			c.direct[d.ID] = true
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

func (c *cams) client(n nvrConf) *http.Client { return c.clientFor(n.Host, n.User, n.Pass) }

// clientFor: một client digest cho mỗi (địa chỉ, tài khoản, mật khẩu), đầu ghi hay camera nối thẳng. Bị từ chối mật
// khẩu thì chỉ client đó tạm ngừng; đầu ghi và camera khác không bị ảnh hưởng.
func (c *cams) clientFor(host, user, pass string) *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := host + "\n" + user + "\n" + pass
	if c.clients[k] == nil { // ponytail: mỗi tài khoản đã thử một mục, mất khi hubd khởi động lại; đủ cho nhập tay
		c.clients[k] = &http.Client{Timeout: 10 * time.Second, Transport: &isapi.Transport{User: user, Pass: pass}}
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
		c.directSnapshot(w, r)
		return
	}
	n := c.conf()
	if !n.On {
		http.Error(w, "camera đang tắt hoặc chưa cấu hình đầu ghi", 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	start := int(c.snapAt.Load())
	var res *http.Response
	for i := range snapPaths {
		k := (start + i) % len(snapPaths)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+n.Host+fmt.Sprintf(snapPaths[k], d.Channel), nil)
		x, err := c.client(n).Do(req)
		if err != nil {
			log.Printf("ảnh camera %s: %v", d.ID, err)
			break
		}
		if ct := x.Header.Get("Content-Type"); x.StatusCode == 200 && strings.HasPrefix(ct, "image/") {
			c.snapAt.Store(int32(k))
			res = x
			break
		}
		// chỉ chuyển ảnh; lỗi XML của đầu ghi ghi log, không đưa ra trình duyệt
		b, _ := io.ReadAll(io.LimitReader(x.Body, 2048))
		x.Body.Close()
		if x.StatusCode != 403 && x.StatusCode != 404 || i == len(snapPaths)-1 { // notSupport thì thử đường kế
			log.Printf("ảnh camera %s: HTTP %d %s %s", d.ID, x.StatusCode, x.Header.Get("Content-Type"), isapiReason(b))
			break
		}
	}
	if res == nil {
		http.Error(w, "đầu ghi không trả ảnh", 502)
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "private, max-age=30")
	io.Copy(w, io.LimitReader(res.Body, 8<<20))
}

// isapiReason: lý do trong <ResponseStatus> lỗi của đầu ghi (vd "Invalid Operation/notSupport"), không chứa tài khoản.
func isapiReason(b []byte) string {
	var s struct {
		Status string `xml:"statusString"`
		Sub    string `xml:"subStatusCode"`
	}
	if xml.Unmarshal(b, &s) != nil {
		return ""
	}
	return s.Status + "/" + s.Sub
}

// live chuyển tiếp WebSocket của go2rtc (MSE + báo hiệu WebRTC). Nguồn do hubd dựng từ cấu hình, bỏ mọi tham số
// của trình duyệt: go2rtc nhận src là URL hay lệnh tuỳ ý. name=ch<kênh>: go2rtc giữ một luồng mỗi kênh và chỉ đổi
// nguồn (tài khoản mới có hiệu lực từ lần mở luồng kế tiếp).
func (c *cams) live(w http.ResponseWriter, r *http.Request) {
	d, ok := c.byID[r.PathValue("id")]
	if !ok {
		c.directLive(w, r)
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
	if !originOK(w, r) {
		return
	}
	h := n.Host
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh // cổng trong cấu hình là cổng HTTP (ISAPI); RTSP của đầu ghi ở 554
	}
	src := url.URL{Scheme: "rtsp", User: url.UserPassword(n.User, n.Pass), Host: net.JoinHostPort(h, "554"),
		Path: fmt.Sprintf("/Streaming/Channels/%d02", d.Channel)} // luồng phụ H.264
	c.proxy(w, r, "/api/ws", url.Values{"name": {"ch" + strconv.Itoa(d.Channel)}, "src": {src.String()}}.Encode())
}

// WebSocket không chịu CORS, và với địa chỉ IP mọi cổng cùng máy là cùng "site" nên cookie PIN vẫn được gửi:
// chỉ nhận trang của chính hubd.
func originOK(w http.ResponseWriter, r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err != nil || !strings.EqualFold(u.Host, r.Host) {
			http.Error(w, "nguồn không hợp lệ", 403)
			return false
		}
	}
	return true
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
	direct, err := camList(db)
	if err != nil { // lỗi DB tạm thời: giữ file cũ, không xoá thiết bị
		log.Printf("không đọc được danh sách camera nối thẳng: %v", err)
		return false
	}
	devs := camDevices(db.Setting("nvr"))
	for _, r := range direct { // sau các kênh đầu ghi; tắt đầu ghi chỉ bỏ kênh
		devs = append(devs, camDev{ID: r.ID, Name: r.Name, Zone: r.Zone, Kind: "camera", OnlineTopic: "cam/" + r.ID + "/online"})
	}
	y, _ := yaml.Marshal(map[string]any{"devices": devs})
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

// ---- Camera nối thẳng (camstore.go): bản ghi đọc từ kho mỗi lần dùng nên mật khẩu mới có hiệu lực ngay ----

const snapTTL = 10 * time.Second

type snapEntry struct {
	b  []byte
	ct string
	at time.Time
}

// snapErr: lỗi kèm mã HTTP và câu tiếng Việt cho trình duyệt (không chứa địa chỉ, tài khoản, mật khẩu, lỗi DB).
type snapErr struct {
	code int
	msg  string
}

func (e *snapErr) Error() string { return e.msg }

// directRec: bản ghi của camera nối thẳng; id không là thiết bị hay đã bị xoá khỏi kho thì 404.
func (c *cams) directRec(id string) (camRec, *snapErr) {
	if !c.direct[id] {
		return camRec{}, &snapErr{404, "không có camera này"}
	}
	rec, ok, err := camGet(c.db, id)
	if err != nil {
		log.Printf("camera %s: không đọc được dữ liệu: %v", id, err)
		return camRec{}, &snapErr{500, "không đọc được dữ liệu camera"}
	}
	if !ok {
		return camRec{}, &snapErr{404, "không có camera này"}
	}
	return rec, nil
}

// freshSnapshot: ảnh mới của camera nối thẳng, bỏ qua bộ đệm (ảnh báo động); cập nhật bộ đệm.
func (c *cams) freshSnapshot(ctx context.Context, id string) ([]byte, string, error) {
	return c.directSnap(ctx, id, true)
}

type snapFlight struct {
	done chan struct{}
	b    []byte
	ct   string
	err  error
}

// directSnap: mỗi id chỉ một yêu cầu tới camera tại một thời điểm; các yêu cầu đến khi cache hết hạn đứng đợi và dùng
// chung kết quả (mật khẩu sai mà gửi hàng loạt thì camera khoá IP trước khi khoá tạm của hub kịp có hiệu lực).
// freshSnapshot cũng nhập vào yêu cầu đang bay (nó vừa bắt đầu hoặc còn tối đa 10 giây), không bắt đầu yêu cầu song song.
func (c *cams) directSnap(ctx context.Context, id string, fresh bool) ([]byte, string, error) {
	rec, se := c.directRec(id)
	if se != nil {
		return nil, "", se
	}
	c.mu.Lock()
	if e, ok := c.snaps[id]; ok && !fresh && time.Since(e.at) < snapTTL {
		c.mu.Unlock()
		return e.b, e.ct, nil
	}
	if f := c.flights[id]; f != nil {
		c.mu.Unlock()
		select {
		case <-f.done:
			return f.b, f.ct, f.err
		case <-ctx.Done():
			return nil, "", &snapErr{504, "hết thời gian chờ camera"}
		}
	}
	f := &snapFlight{done: make(chan struct{})}
	c.flights[id] = f
	c.mu.Unlock()
	// không gắn vào ctx của người gọi: họ ngắt thì những người đang đợi vẫn nhận kết quả
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	f.err = &snapErr{502, "không lấy được ảnh từ camera"} // kết quả mặc định nếu việc lấy ảnh panic
	defer func() {                                        // chạy cả khi panic: không để id kẹt vĩnh viễn với flight không bao giờ đóng
		c.mu.Lock()
		delete(c.flights, id)
		c.mu.Unlock()
		close(f.done)
	}()
	fetch := c.fetch
	if fetch == nil {
		fetch = c.snapHik
		if rec.Kind == "rtsp" {
			fetch = c.snapRTSP
		}
	}
	b, ct, se2 := fetch(fctx, rec)
	if se2 != nil {
		f.err = se2
		return nil, "", se2
	}
	f.b, f.ct, f.err = b, ct, nil
	c.mu.Lock()
	c.snaps[id] = snapEntry{b, ct, time.Now()} // ponytail: không dọn mục camera đã xoá, vài trăm KB mỗi camera đến khi khởi động lại
	c.mu.Unlock()
	return b, ct, nil
}

func (c *cams) snapHik(ctx context.Context, rec camRec) ([]byte, string, *snapErr) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rec.snapURL(), nil)
	res, err := c.clientFor(net.JoinHostPort(rec.Host, strconv.Itoa(rec.HTTPPort)), rec.User, rec.Pass).Do(req)
	if errors.Is(err, isapi.ErrLocked) {
		return nil, "", &snapErr{503, "camera vừa từ chối tài khoản, hub tạm ngừng thử 15 phút để camera không khoá IP. Kiểm tra lại mật khẩu"}
	}
	if err != nil {
		log.Printf("ảnh camera %s: không nối được", rec.ID)
		return nil, "", &snapErr{502, "không kết nối được camera"}
	}
	defer res.Body.Close()
	ct := res.Header.Get("Content-Type")
	if res.StatusCode != 200 || !strings.HasPrefix(ct, "image/") {
		log.Printf("ảnh camera %s: HTTP %d", rec.ID, res.StatusCode)
		if res.StatusCode == 401 {
			return nil, "", &snapErr{502, "camera từ chối tài khoản hoặc mật khẩu (hub tạm ngừng thử 15 phút)"}
		}
		return nil, "", &snapErr{502, "camera không trả ảnh"}
	}
	return readImage(rec.ID, res.Body, ct)
}

// readImage: đọc trọn ảnh; đọc lỗi (cụt), rỗng hay không phải image/* thì không dùng và không đệm.
func readImage(id string, body io.Reader, ct string) ([]byte, string, *snapErr) {
	b, err := io.ReadAll(io.LimitReader(body, 8<<20))
	if err != nil || len(b) == 0 || !strings.HasPrefix(ct, "image/") {
		log.Printf("ảnh camera %s: ảnh hỏng (%d byte, %q)", id, len(b), ct)
		return nil, "", &snapErr{502, "camera trả ảnh hỏng"}
	}
	return b, ct, nil
}

// snapRTSP: nhờ go2rtc nhận luồng phụ rồi chụp một khung. Lỗi mạng của http.Client chứa URL (có mật khẩu) nên không ghi log.
func (c *cams) snapRTSP(ctx context.Context, rec camRec) ([]byte, string, *snapErr) {
	do := func(method, path string, q url.Values) (*http.Response, *snapErr) {
		u := *c.go2rtc
		u.Path, u.RawQuery = path, q.Encode()
		req, _ := http.NewRequestWithContext(ctx, method, u.String(), nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("ảnh camera %s: go2rtc không trả lời", rec.ID)
			return nil, &snapErr{502, "go2rtc chưa chạy"}
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			log.Printf("ảnh camera %s: go2rtc HTTP %d", rec.ID, res.StatusCode)
			return nil, &snapErr{502, "không lấy được ảnh từ camera"}
		}
		return res, nil
	}
	put, se := do(http.MethodPut, "/api/streams", url.Values{"name": {rec.ID}, "src": {rec.rtsp(true)}})
	if se != nil {
		return nil, "", se
	}
	put.Body.Close()
	res, se := do(http.MethodGet, "/api/frame.jpeg", url.Values{"src": {rec.ID}})
	if se != nil {
		return nil, "", se
	}
	defer res.Body.Close()
	return readImage(rec.ID, res.Body, res.Header.Get("Content-Type"))
}

func (c *cams) directSnapshot(w http.ResponseWriter, r *http.Request) {
	b, ct, err := c.directSnap(r.Context(), r.PathValue("id"), false)
	if err != nil {
		var se *snapErr
		errors.As(err, &se)
		http.Error(w, se.msg, se.code)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=10")
	w.Write(b)
}

// directLive: như live của đầu ghi nhưng nguồn là luồng phụ của chính camera; go2rtc giữ luồng tên <id>.
func (c *cams) directLive(w http.ResponseWriter, r *http.Request) {
	rec, se := c.directRec(r.PathValue("id"))
	if se != nil {
		http.Error(w, se.msg, se.code)
		return
	}
	// camera vừa từ chối tài khoản thì không mở luồng: go2rtc sẽ gửi lại mật khẩu sai mỗi lần trình phát nối lại
	if rec.Kind == "hik_cam" && c.clientFor(net.JoinHostPort(rec.Host, strconv.Itoa(rec.HTTPPort)), rec.User, rec.Pass).Transport.(*isapi.Transport).Locked() {
		http.Error(w, "camera vừa từ chối tài khoản, kiểm tra mật khẩu trong Cài đặt → Camera", 503)
		return
	}
	if !originOK(w, r) {
		return
	}
	c.proxy(w, r, "/api/ws", url.Values{"name": {rec.ID}, "src": {rec.rtsp(true)}}.Encode())
}
