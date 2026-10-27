package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type tlsState struct {
	Mode     string   `json:"mode"`
	IPs      []string `json:"ips"`
	Expires  int64    `json:"expires"`
	CAName   string   `json:"ca_name"`
	CASHA256 string   `json:"ca_sha256"`
}

func tlsCall(t *testing.T, mux http.Handler, method, host, body string, ck []*http.Cookie) (*httptest.ResponseRecorder, tlsState) {
	t.Helper()
	r := httptest.NewRequest(method, "/api/tls", strings.NewReader(body))
	r.Host = host
	for _, c := range ck {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var s tlsState
	json.Unmarshal(w.Body.Bytes(), &s)
	return w, s
}

// Bật HTTPS từ trang Cài đặt: hubd tự tạo CA riêng của nhà (giới hạn tên: chỉ IP nội bộ và home.arpa) và chứng chỉ
// cho IP đang mở dashboard; iPad tải CA ở /api/tls/ca.crt (không cần PIN), cổng 8443 dùng ngay không phải khởi động lại.
func TestTLSFromSettings(t *testing.T) {
	mux, h, _ := assistantMux(t, Config{})
	get := tlsSource(h.db, "", "")
	if _, err := get(nil); err == nil {
		t.Fatal("chưa tạo chứng chỉ mà cổng 8443 đã có chứng chỉ")
	}
	if w, _ := tlsCall(t, mux, "POST", "192.168.1.254:8080", "", nil); w.Code != 401 {
		t.Fatalf("chưa nhập PIN mà tạo được chứng chỉ: %d", w.Code)
	}
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	if _, s := tlsCall(t, mux, "GET", "192.168.1.254:8080", "", ck); s.Mode != "off" {
		t.Fatalf("mode = %q, muốn off", s.Mode)
	}
	// Tên máy, IP công cộng, IPv6: CA bị giới hạn tên nên không cấp được, báo người dùng mở bằng IP nội bộ.
	for _, host := range []string{"hub.local:8080", "8.8.8.8:8080", "[fd00::1]:8080", "localhost"} {
		if w, _ := tlsCall(t, mux, "POST", host, "", ck); w.Code != 400 {
			t.Errorf("Host %s: code=%d, muốn 400", host, w.Code)
		}
	}
	w, s := tlsCall(t, mux, "POST", "192.168.1.254:8080", "", ck)
	if w.Code != 200 || s.Mode != "ca" || strings.Join(s.IPs, ",") != "192.168.1.254" || !strings.HasPrefix(s.CAName, "iNIX Home CA") || len(s.CASHA256) != 95 {
		t.Fatalf("tạo chứng chỉ: %d %s", w.Code, w.Body)
	}

	// CA tải công khai, dạng DER để iOS mở thành hồ sơ cài đặt.
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest("GET", "/api/tls/ca.crt", nil))
	if r.Code != 200 || r.Header().Get("Content-Type") != "application/x-x509-ca-cert" {
		t.Fatalf("tải CA: %d %v", r.Code, r.Header())
	}
	ca, err := x509.ParseCertificate(r.Body.Bytes())
	if err != nil || !ca.IsCA || !ca.PermittedDNSDomainsCritical || len(ca.PermittedIPRanges) != 3 || strings.Join(ca.PermittedDNSDomains, ",") != "home.arpa" {
		t.Fatalf("CA sai: %v %+v", err, ca)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	// Cổng 8443 phục vụ ngay chứng chỉ mới; trình duyệt tin CA thì kết nối được tới đúng IP.
	// (cấu hình như serveTLS: chỉ GetCertificate; trình duyệt không gửi SNI khi mở bằng IP)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{GetCertificate: get})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: roots, ServerName: "192.168.1.254"})
	if err != nil {
		t.Fatalf("bắt tay TLS với IP hub: %v", err)
	}
	leaf := conn.ConnectionState().PeerCertificates[0]
	conn.Close()
	// iOS chỉ nhận chứng chỉ máy chủ có hạn tối đa 825 ngày, có SAN, có serverAuth.
	if d := leaf.NotAfter.Sub(leaf.NotBefore); d > 825*24*time.Hour || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth || s.Expires != leaf.NotAfter.Unix() {
		t.Fatalf("chứng chỉ máy chủ: hạn %v, eku %v", d, leaf.ExtKeyUsage)
	}

	// Cấp lại (đổi IP) giữ nguyên CA: iPad không phải cài lại. Khởi động lại hubd vẫn còn.
	_, s2 := tlsCall(t, mux, "POST", "192.168.1.254:8080", `{"ip":"10.0.0.5"}`, ck)
	if s2.CASHA256 != s.CASHA256 || strings.Join(s2.IPs, ",") != "10.0.0.5" {
		t.Fatalf("cấp lại: %+v (trước %+v)", s2, s)
	}
	c, err := tlsSource(h.db, "", "")(nil)
	if err != nil || c.Leaf == nil || c.Leaf.IPAddresses[0].String() != "10.0.0.5" {
		t.Fatalf("sau khởi động lại: %v %v", err, c)
	}
}

// Giới hạn tên của CA có hiệu lực: lộ khoá CA (nằm trong SQLite của hubd) cũng không giả được trang ngoài internet.
// Quét đủ các loại tên: 3 dải IP nội bộ + home.arpa được; IP công cộng, tên miền thật, IPv6 bị từ chối.
func TestTLSCANameConstraints(t *testing.T) {
	db := newDB(t)
	ca, key, err := tlsCA(db, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for name, ok := range map[string]bool{"10.1.2.3": true, "172.20.0.1": true, "192.168.1.254": true, "hub.home.arpa": true,
		"8.8.8.8": false, "172.32.0.1": false, "google.com": false, "fd00::1": false} {
		tmpl := &x509.Certificate{ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		if ip := net.ParseIP(name); ip != nil {
			tmpl.IPAddresses = []net.IP{ip}
		} else {
			tmpl.DNSNames = []string{name}
		}
		der, _, err := tlsSign(tmpl, ca, key, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		leaf, _ := x509.ParseCertificate(der)
		_, err = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: name})
		if (err == nil) != ok {
			t.Errorf("%s: verify err=%v, muốn hợp lệ=%v", name, err, ok)
		}
	}
}

// Có HUBD_TLS_CERT/KEY (chứng chỉ riêng: scripts/tls-selfsigned.sh hoặc tên miền thật): dùng tệp, trang Cài đặt
// chỉ hiện trạng thái, không tạo đè.
func TestTLSFileMode(t *testing.T) {
	dir := t.TempDir()
	ca, key, _ := tlsCA(newDB(t), time.Now())
	der, k, _ := tlsSign(&x509.Certificate{IPAddresses: []net.IP{net.ParseIP("192.168.1.254")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, ca, key, time.Now())
	crt, kf := filepath.Join(dir, "hub.crt"), filepath.Join(dir, "hub.key")
	os.WriteFile(crt, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(kf, pem.EncodeToMemory(k), 0o600)
	t.Setenv("HUBD_TLS_CERT", crt)
	t.Setenv("HUBD_TLS_KEY", kf)
	mux, h, _ := assistantMux(t, Config{})
	if c, err := tlsSource(h.db, crt, kf)(nil); err != nil || c == nil {
		t.Fatalf("không đọc được chứng chỉ tệp: %v", err)
	}
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	if _, s := tlsCall(t, mux, "GET", "192.168.1.254:8080", "", ck); s.Mode != "file" || strings.Join(s.IPs, ",") != "192.168.1.254" {
		t.Fatalf("trạng thái chế độ tệp: %+v", s)
	}
	if w, _ := tlsCall(t, mux, "POST", "192.168.1.254:8080", "", ck); w.Code != 409 {
		t.Fatalf("chế độ tệp mà vẫn tạo đè: %d", w.Code)
	}
}
