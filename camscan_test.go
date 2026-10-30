package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Máy giả theo kiểu: ONVIF (SOAP), Hikvision (401 Digest), máy web thường. Ghi mọi header nhận được.
type scanFakes struct {
	mu      sync.Mutex
	headers []http.Header
	addr    map[string]string // địa chỉ "thật" của dải giả -> máy chủ httptest
	dialed  []string
}

func (f *scanFakes) add(t *testing.T, host string, h http.HandlerFunc) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	f.addr[host] = srv.Listener.Addr().String()
}

func newScanFakes(t *testing.T) *scanFakes {
	f := &scanFakes{addr: map[string]string{}}
	f.add(t, "10.0.0.1", func(w http.ResponseWriter, r *http.Request) { // ONVIF
		if r.URL.Path == "/onvif/device_service" && r.Method == "POST" {
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), "GetSystemDateAndTime") {
				t.Errorf("thiếu GetSystemDateAndTime: %s", b)
			}
			io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`)
			return
		}
		http.NotFound(w, r)
	})
	f.add(t, "10.0.0.2", func(w http.ResponseWriter, r *http.Request) { // Hikvision, cũng nói ONVIF
		if r.URL.Path == "/ISAPI/System/deviceInfo" {
			w.Header().Set("WWW-Authenticate", `Digest realm="DS-2CD", nonce="n"`)
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"/>`)
	})
	f.add(t, "10.0.0.3", func(w http.ResponseWriter, r *http.Request) { // máy web thường
		io.WriteString(w, "<html>router</html>")
	})
	return f
}

func (f *scanFakes) dial(ctx context.Context, _, addr string) (net.Conn, error) {
	h, _, _ := net.SplitHostPort(addr)
	f.mu.Lock()
	f.dialed = append(f.dialed, h)
	real, ok := f.addr[h]
	f.mu.Unlock()
	if !ok {
		return nil, &net.OpError{Op: "dial", Err: context.DeadlineExceeded}
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", real)
}

func scanFor(t *testing.T, f *scanFakes, hosts []string) (*camScan, *cams) {
	db := camTestDB(t)
	c := newCams(nil, db)
	s := newCamScan(c)
	s.dial = f.dial
	s.hostsOf = func(string) ([]string, error) { return hosts, nil }
	return s, c
}

func TestCamScanFindsOnvifAndHikvision(t *testing.T) {
	f := newScanFakes(t)
	s, _ := scanFor(t, f, []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"})
	s.run([]string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"})
	st := s.status()
	if st.Running || st.Error != "" || len(st.Found) != 2 {
		t.Fatalf("found: %+v", st)
	}
	kind := map[string]string{}
	for _, h := range st.Found {
		kind[h.Host] = h.Kind
	}
	if kind["10.0.0.1"] != "onvif" || kind["10.0.0.2"] != "hik_cam" { // máy 2 nói cả hai: ưu tiên hik_cam
		t.Fatalf("kinds: %v", kind)
	}
}

func TestCamScanSkipsKnownHosts(t *testing.T) {
	f := newScanFakes(t)
	s, c := scanFor(t, f, nil)
	if _, err := camSave(c.db, camRec{Name: "Cổng", Zone: "z", Kind: "rtsp", Host: "10.0.0.1", MainPath: "/a"}, nil); err != nil {
		t.Fatal(err)
	}
	c.db.PutSetting("nvr", map[string]any{"nvr_host": "10.0.0.2:8000", "nvr_user": "u", "nvr_pass": "p"})
	got := s.scanHosts([]string{"10.0.0.1", "10.0.0.2", "10.0.0.3"})
	s.run(got)
	if len(s.status().Found) != 0 {
		t.Fatalf("không được hiện lại: %+v", s.status().Found)
	}
	for _, h := range f.dialed {
		if h == "10.0.0.1" || h == "10.0.0.2" {
			t.Fatalf("không được dò %s", h)
		}
	}
}

func TestCamScanRejectsWideRange(t *testing.T) {
	for _, bad := range []string{"192.168.0.0/16", "10.0.0.0/8", "0.0.0.0/0", "fd00::/64", "abc", ""} {
		if h, err := camScanHosts(bad); err == nil || h != nil || !strings.Contains(err.Error(), "ví dụ") {
			t.Errorf("%q phải bị từ chối bằng lời giải thích: %v", bad, err)
		}
	}
	if h, err := camScanHosts("192.168.1.0/24"); err != nil || len(h) != 254 || h[0] != "192.168.1.1" || h[253] != "192.168.1.254" {
		t.Errorf("/24: %d %v", len(h), err)
	}
	if h, err := camScanHosts("192.168.1.16/28"); err != nil || len(h) != 14 || h[0] != "192.168.1.17" {
		t.Errorf("/28: %v %v", h, err)
	}
}

func TestCamScanSendsNoCredentials(t *testing.T) {
	f := newScanFakes(t)
	s, c := scanFor(t, f, nil)
	c.db.PutSetting("nvr", map[string]any{"nvr_host": "10.9.9.9", "nvr_user": "u", "nvr_pass": "p"})
	s.run([]string{"10.0.0.1", "10.0.0.2", "10.0.0.3"})
	if len(f.headers) == 0 {
		t.Fatal("máy giả không nhận yêu cầu nào")
	}
	for _, h := range f.headers {
		if h.Get("Authorization") != "" {
			t.Fatalf("quét không được gửi Authorization: %v", h)
		}
	}
}

func TestCamScanConcurrencyAndTimeout(t *testing.T) {
	var cur, max atomic.Int32
	var bad atomic.Int32
	s, _ := scanFor(t, newScanFakes(t), nil)
	s.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		n := cur.Add(1)
		defer cur.Add(-1)
		for {
			m := max.Load()
			if n <= m || max.CompareAndSwap(m, n) {
				break
			}
		}
		if d, ok := ctx.Deadline(); !ok || time.Until(d) > 400*time.Millisecond {
			bad.Add(1)
		}
		time.Sleep(20 * time.Millisecond)
		return nil, context.DeadlineExceeded
	}
	hosts := make([]string, 100)
	for i := range hosts {
		hosts[i] = "10.1.0." + string(rune('0'+i%10)) + string(rune('0'+i/10%10))
	}
	s.run(hosts)
	if bad.Load() != 0 {
		t.Errorf("%d lần nối không có hạn 400 ms", bad.Load())
	}
	if m := max.Load(); m > 32 || m < 2 {
		t.Errorf("số kết nối đồng thời = %d, cần 2..32", m)
	}
}

// Nhiều cổng cùng trả lời: kết quả không phụ thuộc goroutine nào xong trước; ưu tiên 80, rồi 8080, rồi 2020.
func TestCamScanPrefersPort80Then8080Then2020(t *testing.T) {
	for _, tc := range []struct {
		refuse []int
		want   int
	}{{nil, 80}, {[]int{80}, 8080}, {[]int{80, 8080}, 2020}} {
		f := newScanFakes(t)
		for i := 0; i < 20; i++ {
			s, _ := scanFor(t, f, nil)
			s.dial = func(ctx context.Context, n, addr string) (net.Conn, error) {
				_, p, _ := net.SplitHostPort(addr)
				for _, r := range tc.refuse {
					if p == strconv.Itoa(r) {
						return nil, &net.OpError{Op: "dial", Err: context.DeadlineExceeded}
					}
				}
				return f.dial(ctx, n, addr)
			}
			s.run([]string{"10.0.0.2"}) // Hikvision trả lời trên mọi cổng
			if st := s.status(); len(st.Found) != 1 || st.Found[0].Port != tc.want || st.Found[0].Kind != "hik_cam" {
				t.Fatalf("từ chối %v: %+v, muốn cổng %d", tc.refuse, st.Found, tc.want)
			}
		}
	}
}
