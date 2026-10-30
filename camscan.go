package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Dò camera trong mạng nhà (mục camera nối thẳng): thử cổng 80/8080/2020 từng địa chỉ trong dải camera.lan_cidr,
// nhận ra ONVIF (SOAP không cần đăng nhập) và Hikvision (ISAPI đòi Digest). Không bao giờ gửi tài khoản khi quét.
// Kết quả lần quét gần nhất giữ trong RAM.
const (
	scanDialTimeout = 400 * time.Millisecond
	scanMaxConc     = 32
	scanProbeTimout = 3 * time.Second
	scanCIDRHint    = "Nhập dải mạng của nhà, ví dụ 192.168.1.0/24"
)

type scanHit struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Kind  string `json:"kind"` // hik_cam | onvif
	Model string `json:"model"`
}

type scanStatus struct {
	Running bool      `json:"running"`
	Found   []scanHit `json:"found"`
	Error   string    `json:"error"`
}

type camScan struct {
	c     *cams
	ports []int
	// test thay: dial để dùng máy chủ giả, hostsOf để không cần dải thật
	dial    func(ctx context.Context, network, addr string) (net.Conn, error)
	hostsOf func(cidr string) ([]string, error)

	mu sync.Mutex
	st scanStatus
}

func newCamScan(c *cams) *camScan {
	return &camScan{c: c, ports: []int{80, 8080, 2020}, dial: (&net.Dialer{}).DialContext, hostsOf: camScanHosts,
		st: scanStatus{Found: []scanHit{}}}
}

// camScanHosts: các địa chỉ máy trong dải IPv4, tối đa /24 (256 địa chỉ); rộng hơn bị từ chối.
func camScanHosts(cidr string) ([]string, error) {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return nil, errors.New(scanCIDRHint)
	}
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, errors.New("Dải mạng không hợp lệ. " + scanCIDRHint)
	}
	ones, bits := n.Mask.Size()
	if ip.To4() == nil || bits != 32 {
		return nil, errors.New("Chỉ dò được dải IPv4. " + scanCIDRHint)
	}
	if ones < 24 {
		return nil, errors.New("Dải mạng quá rộng: chỉ dò tối đa 256 địa chỉ (từ /24 trở lại, ví dụ 192.168.1.0/24)")
	}
	base := n.IP.To4()
	size := 1 << (32 - ones)
	lo, hi := 1, size-2 // bỏ địa chỉ mạng và địa chỉ quảng bá
	if size <= 2 {
		lo, hi = 0, size-1
	}
	out := []string{}
	for i := lo; i <= hi; i++ {
		a := make(net.IP, 4)
		copy(a, base)
		a[3] += byte(i) // /24 trở lại: chỉ byte cuối đổi
		out = append(out, a.String())
	}
	return out, nil
}

func (s *camScan) status() scanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.st
	st.Found = append([]scanHit{}, st.Found...)
	return st
}

// scanHosts: bỏ camera đã lưu và đầu thu khỏi danh sách cần dò.
func (s *camScan) scanHosts(all []string) []string {
	skip := map[string]bool{}
	if h, _, err := net.SplitHostPort(s.c.conf().Host); err == nil {
		skip[h] = true
	} else if h := s.c.conf().Host; h != "" {
		skip[h] = true
	}
	if l, err := camList(s.c.db); err == nil {
		for _, r := range l {
			skip[r.Host] = true
		}
	}
	out := []string{}
	for _, h := range all {
		if !skip[h] {
			out = append(out, h)
		}
	}
	return out
}

// start: kiểm dải mạng rồi quét nền. Trả mã HTTP và câu báo (nếu lỗi).
func (s *camScan) start() (int, string) {
	cidr, _ := s.c.db.Setting("camera")["lan_cidr"].(string)
	hosts, err := s.hostsOf(cidr)
	if err != nil {
		s.mu.Lock()
		if !s.st.Running {
			s.st = scanStatus{Found: []scanHit{}, Error: err.Error()}
		}
		s.mu.Unlock()
		return 400, err.Error()
	}
	s.mu.Lock()
	if s.st.Running {
		s.mu.Unlock()
		return 409, "Đang dò camera, đợi một lát"
	}
	s.st = scanStatus{Running: true, Found: []scanHit{}}
	s.mu.Unlock()
	go func() {
		defer func() { // panic cũng phải gỡ cờ Running, không thì không quét lại được
			if r := recover(); r != nil {
				log.Printf("dò camera: lỗi %v", r)
				s.mu.Lock()
				s.st = scanStatus{Found: []scanHit{}, Error: "Dò camera gặp lỗi, thử lại"}
				s.mu.Unlock()
			}
		}()
		s.run(s.scanHosts(hosts))
	}()
	return 202, ""
}

// run: quét đồng bộ danh sách địa chỉ, ghi kết quả vào s.st.
func (s *camScan) run(hosts []string) {
	s.mu.Lock()
	s.st = scanStatus{Running: true, Found: []scanHit{}}
	s.mu.Unlock()
	cl := &http.Client{Timeout: scanProbeTimout,
		Transport:     &http.Transport{DialContext: s.dialTimeout, DisableKeepAlives: true, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var wg sync.WaitGroup
	sem := make(chan struct{}, scanMaxConc)
	var mu sync.Mutex
	var failed atomic.Bool
	best := map[string]scanHit{} // mỗi máy một kết quả: hik_cam hơn onvif
	for _, h := range hosts {
		for _, p := range s.ports {
			wg.Add(1)
			sem <- struct{}{}
			go func(h string, p int) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("dò camera: lỗi %v", r)
						failed.Store(true)
					}
					<-sem
					wg.Done()
				}()
				hit, ok := s.probe(cl, h, p)
				if !ok {
					return
				}
				mu.Lock()
				if old, had := best[h]; !had || old.Kind == "onvif" && hit.Kind == "hik_cam" ||
					old.Kind == hit.Kind && s.rank(hit.Port) < s.rank(old.Port) { // nhiều cổng cùng trả lời: cổng đứng trước trong s.ports (80, 8080, 2020)
					best[h] = hit
				}
				mu.Unlock()
			}(h, p)
		}
	}
	wg.Wait()
	found := []scanHit{}
	for _, h := range hosts { // giữ thứ tự dải
		if hit, ok := best[h]; ok {
			found = append(found, hit)
		}
	}
	s.mu.Lock()
	s.st = scanStatus{Found: found}
	if failed.Load() {
		s.st.Error = "Dò camera gặp lỗi, thử lại"
	}
	s.mu.Unlock()
}

func (s *camScan) rank(port int) int {
	for i, p := range s.ports {
		if p == port {
			return i
		}
	}
	return len(s.ports)
}

func (s *camScan) dialTimeout(ctx context.Context, network, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, scanDialTimeout)
	defer cancel()
	return s.dial(ctx, network, addr)
}

const onvifTimeReq = `<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl"/></s:Body></s:Envelope>`

// probe: một địa chỉ:cổng. Không có Authorization trong mọi yêu cầu.
func (s *camScan) probe(cl *http.Client, host string, port int) (scanHit, bool) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	hit := scanHit{Host: host, Port: port}
	conn, err := s.dialTimeout(context.Background(), "tcp", addr) // cổng không mở thì thôi, khỏi gửi HTTP
	if err != nil {
		return hit, false
	}
	conn.Close()
	base := "http://" + addr
	if r, err := http.NewRequest("GET", base+"/ISAPI/System/deviceInfo", nil); err == nil {
		if res, err := cl.Do(r); err == nil {
			b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
			res.Body.Close()
			if res.StatusCode == 401 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(res.Header.Get("WWW-Authenticate"))), "digest") ||
				res.StatusCode == 200 && strings.Contains(string(b), "<DeviceInfo") {
				var di struct {
					Model string `xml:"model"`
				}
				xml.Unmarshal(b, &di)
				hit.Kind, hit.Model = "hik_cam", di.Model
				return hit, true // hik_cam hơn onvif
			}
		}
	}
	r, err := http.NewRequest("POST", base+"/onvif/device_service", strings.NewReader(onvifTimeReq))
	if err != nil {
		return hit, false
	}
	r.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	res, err := cl.Do(r)
	if err != nil {
		return hit, false
	}
	defer res.Body.Close()
	d := xml.NewDecoder(io.LimitReader(res.Body, 64<<10))
	for {
		tok, err := d.Token()
		if err != nil {
			return hit, false
		}
		if se, ok := tok.(xml.StartElement); ok { // phần tử gốc phải là SOAP Envelope
			hit.Kind = "onvif"
			return hit, se.Name.Local == "Envelope"
		}
	}
}

func (s *camScan) post(w http.ResponseWriter, r *http.Request) {
	if code, msg := s.start(); code != 202 {
		log.Printf("dò camera: %d %s", code, msg)
		http.Error(w, msg, code)
		return
	}
	w.WriteHeader(202)
}

func (s *camScan) get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.status())
}
