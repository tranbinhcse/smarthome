package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// HTTPS cổng 8443, cùng mux với cổng 8080. Dùng chung cho mọi việc cần HTTPS: Safari chỉ cho dùng micro (nút nói)
// trên HTTPS; Spotify chỉ nhận redirect URI HTTPS (https://<IP hub>:8443/...). Một chứng chỉ, một CA cài lên iPad.
// Nguồn chứng chỉ:
//   - HUBD_TLS_CERT/KEY nếu có: chứng chỉ riêng (scripts/tls-selfsigned.sh giữ khoá CA ngoài container, hoặc tên miền thật);
//   - không thì CA riêng của nhà do hubd tạo khi bấm "Bật HTTPS" trên trang Cài đặt. Khoá CA nằm trong SQLite của hubd;
//     CA giới hạn tên (chỉ IP nội bộ và *.home.arpa) nên lộ khoá cũng không giả được trang ngoài internet.
// Tạo/cấp lại có hiệu lực ngay, không phải khởi động lại hubd.

const tlsNS = "sys:tls" // không khớp settingNS: API cấu hình chung (/api/settings/*) không đọc/ghi được nhóm này

var privateNets = func() (out []*net.IPNet) {
	for _, s := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		_, n, _ := net.ParseCIDR(s)
		out = append(out, n)
	}
	return out
}()

func serveTLS(h http.Handler, db *DB) {
	get := tlsSource(db, os.Getenv("HUBD_TLS_CERT"), os.Getenv("HUBD_TLS_KEY"))
	if _, err := get(nil); err != nil {
		log.Printf("HTTPS :8443: %v", err)
	}
	srv := &http.Server{Addr: ":8443", Handler: h, TLSConfig: &tls.Config{GetCertificate: get}}
	log.Printf("HTTPS :8443 dừng: %v", srv.ListenAndServeTLS("", ""))
}

// tlsSource: chứng chỉ cho mỗi lần bắt tay TLS. Chế độ CA đọc lại SQLite để chứng chỉ mới có hiệu lực ngay.
func tlsSource(db *DB, certFile, keyFile string) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	if certFile != "" {
		c, err := tls.LoadX509KeyPair(certFile, keyFile)
		return func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if err != nil {
				return nil, fmt.Errorf("đọc HUBD_TLS_CERT/KEY: %w", err)
			}
			return &c, nil
		}
	}
	var mu sync.Mutex
	var last string
	var cur *tls.Certificate
	return func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		s := db.Setting(tlsNS) // ponytail: một truy vấn SQLite mỗi lần bắt tay; nhà vài thiết bị, kết nối giữ lâu
		cert, _ := s["cert"].(string)
		key, _ := s["key"].(string)
		mu.Lock()
		defer mu.Unlock()
		if cert != last {
			c, err := tls.X509KeyPair([]byte(cert), []byte(key))
			if err != nil {
				return nil, errors.New("chưa có chứng chỉ: bật HTTPS trên trang Cài đặt")
			}
			last, cur = cert, &c
		}
		if cur == nil {
			return nil, errors.New("chưa có chứng chỉ: bật HTTPS trên trang Cài đặt")
		}
		return cur, nil
	}
}

// tlsSign tạo khoá mới và ký chứng chỉ bằng CA. ca nil = CA tự ký, hạn 10 năm; chứng chỉ máy chủ 825 ngày (giới hạn của iOS).
func tlsSign(tmpl, ca *x509.Certificate, caKey crypto.Signer, now time.Time) ([]byte, *pem.Block, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	days := 825
	if ca == nil {
		ca, caKey, days = tmpl, key, 3650
	} else {
		tmpl.KeyUsage = x509.KeyUsageDigitalSignature
	}
	tmpl.SerialNumber, _ = rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl.NotBefore = now.Add(-time.Hour) // lệch đồng hồ giữa hub và iPad
	tmpl.NotAfter = tmpl.NotBefore.Add(time.Duration(days) * 24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	p8, err := x509.MarshalPKCS8PrivateKey(key)
	return der, &pem.Block{Type: "PRIVATE KEY", Bytes: p8}, err
}

func pemCert(der []byte) string { return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) }

func storedCA(db *DB) (*x509.Certificate, crypto.Signer, error) {
	s := db.Setting(tlsNS)
	cert, _ := s["ca_cert"].(string)
	key, _ := s["ca_key"].(string)
	c, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil {
		return nil, nil, err
	}
	return c.Leaf, c.PrivateKey.(crypto.Signer), nil
}

// tlsCA: CA riêng của nhà, tạo lần đầu. Cấp lại chứng chỉ máy chủ giữ nguyên CA nên iPad không phải cài lại.
func tlsCA(db *DB, now time.Time) (*x509.Certificate, crypto.Signer, error) {
	if c, k, err := storedCA(db); err == nil {
		return c, k, nil
	}
	der, key, err := tlsSign(&x509.Certificate{
		Subject:               pkix.Name{CommonName: "iNIX Home CA " + now.Format("2006-01-02"), Organization: []string{"iNIX Home"}},
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		// Giới hạn tên: CA chỉ ký được IP nội bộ và *.home.arpa.
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{"home.arpa"},
		PermittedIPRanges:           privateNets,
	}, nil, nil, now)
	if err != nil {
		return nil, nil, err
	}
	if err := db.PutSetting(tlsNS, map[string]any{"ca_cert": pemCert(der), "ca_key": string(pem.EncodeToMemory(key))}); err != nil {
		return nil, nil, err
	}
	return storedCA(db)
}

func tlsIssue(db *DB, ip net.IP, now time.Time) error {
	ca, caKey, err := tlsCA(db, now)
	if err != nil {
		return err
	}
	der, key, err := tlsSign(&x509.Certificate{Subject: pkix.Name{CommonName: "iNIX Home hub"}, IPAddresses: []net.IP{ip},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, ca, caKey, now)
	if err != nil {
		return err
	}
	return db.PutSetting(tlsNS, map[string]any{"cert": pemCert(der), "key": string(pem.EncodeToMemory(key))})
}

func tlsStatus(db *DB) map[string]any {
	file := os.Getenv("HUBD_TLS_CERT")
	out := map[string]any{"mode": "off", "port": 8443}
	c, err := tlsSource(db, file, os.Getenv("HUBD_TLS_KEY"))(nil)
	if err != nil {
		return out
	}
	ips := []string{}
	for _, ip := range c.Leaf.IPAddresses {
		ips = append(ips, ip.String())
	}
	out["mode"], out["ips"], out["expires"] = "ca", append(ips, c.Leaf.DNSNames...), c.Leaf.NotAfter.Unix()
	if file != "" {
		out["mode"] = "file"
		return out
	}
	if ca, _, err := storedCA(db); err == nil {
		sum := sha256.Sum256(ca.Raw)
		hex := make([]string, len(sum))
		for i, b := range sum {
			hex[i] = fmt.Sprintf("%02X", b)
		}
		out["ca_name"], out["ca_sha256"] = ca.Subject.CommonName, strings.Join(hex, " ") // cùng dạng iOS hiện trong chi tiết chứng chỉ
	}
	return out
}

func mountTLS(mux *http.ServeMux, db *DB, a *Auth) {
	mux.HandleFunc("GET /api/tls", a.Require(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tlsStatus(db)) }))
	mux.HandleFunc("POST /api/tls", a.Require(func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("HUBD_TLS_CERT") != "" {
			http.Error(w, "đang dùng chứng chỉ từ HUBD_TLS_CERT; muốn hub tự cấp thì bỏ biến đó trong .env", http.StatusConflict)
			return
		}
		var b struct {
			IP string `json:"ip"`
		}
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&b) // thân rỗng = IP đang dùng để mở dashboard
		host := b.IP
		if host == "" {
			host = r.Host
			if h, _, err := net.SplitHostPort(r.Host); err == nil {
				host = h
			}
		}
		ip := net.ParseIP(host).To4()
		if ip == nil || !ip.IsPrivate() {
			http.Error(w, "mở dashboard bằng IP nội bộ của hub (vd http://192.168.1.254:8080) rồi bật HTTPS", 400)
			return
		}
		if err := tlsIssue(db, ip, time.Now()); err != nil {
			log.Printf("cấp chứng chỉ HTTPS: %v", err)
			http.Error(w, "không tạo được chứng chỉ", 500)
			return
		}
		writeJSON(w, tlsStatus(db))
	}))
	// Không cần PIN: chứng chỉ CA là thông tin công khai, iPad mới chưa đăng nhập vẫn tải được. DER để iOS mở thành hồ sơ.
	mux.HandleFunc("GET /api/tls/ca.crt", func(w http.ResponseWriter, r *http.Request) {
		ca, _, err := storedCA(db)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Write(ca.Raw)
	})
}
