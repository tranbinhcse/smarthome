// Package isapi: HTTP tới đầu ghi Hikvision (ISAPI) bằng Digest, kiểu xác thực mặc định của đầu ghi.
// Dùng chung cho hubd (proxy ảnh chụp) và hikbridge (luồng sự kiện). Chỉ cho request không có body (GET).
package isapi

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrLocked: vừa bị từ chối mật khẩu, tạm ngừng để đầu ghi không khoá IP của trung tâm (Hikvision khoá sau vài lần sai).
var ErrLocked = errors.New("isapi: đầu ghi từ chối tài khoản, tạm ngừng gửi mật khẩu 15 phút")

type Transport struct {
	User, Pass string
	Base       http.RoundTripper // nil = http.DefaultTransport

	mu    sync.Mutex
	until time.Time
	now   func() time.Time
}

func (t *Transport) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	locked := t.clock().Before(t.until)
	t.mu.Unlock()
	if locked {
		return nil, ErrLocked
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	res, err := base.RoundTrip(req)
	if err != nil || res.StatusCode != http.StatusUnauthorized {
		return res, err
	}
	auth := ""
	for _, ch := range res.Header.Values("WWW-Authenticate") {
		if a, ok := digest(ch, req.Method, req.URL.RequestURI(), t.User, t.Pass, ""); ok {
			auth = a
			break
		}
	}
	if auth == "" {
		return res, nil // không phải Digest: trả nguyên 401 cho bên gọi
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	r2 := req.Clone(req.Context())
	r2.Header.Set("Authorization", auth)
	res, err = base.RoundTrip(r2)
	if err == nil && res.StatusCode == http.StatusUnauthorized {
		t.mu.Lock()
		t.until = t.clock().Add(15 * time.Minute)
		t.mu.Unlock()
	}
	return res, err
}

var param = regexp.MustCompile(`(\w+)=(?:"([^"]*)"|([^,\s]*))`)

// digest dựng header Authorization (RFC 2617, MD5, qop=auth) từ một thử thách WWW-Authenticate.
// cnonce rỗng thì sinh ngẫu nhiên.
func digest(challenge, method, uri, user, pass, cnonce string) (string, bool) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(challenge), " ")
	if !strings.EqualFold(scheme, "Digest") {
		return "", false
	}
	p := map[string]string{}
	for _, m := range param.FindAllStringSubmatch(rest, -1) {
		p[strings.ToLower(m[1])] = m[2] + m[3]
	}
	if p["nonce"] == "" || (p["algorithm"] != "" && !strings.EqualFold(p["algorithm"], "MD5")) {
		return "", false
	}
	h := func(s string) string { b := md5.Sum([]byte(s)); return hex.EncodeToString(b[:]) }
	ha1, ha2 := h(user+":"+p["realm"]+":"+pass), h(method+":"+uri)
	out := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s"`, user, p["realm"], p["nonce"], uri)
	if strings.Contains(","+strings.ReplaceAll(p["qop"], " ", "")+",", ",auth,") {
		if cnonce == "" {
			b := make([]byte, 8)
			rand.Read(b)
			cnonce = hex.EncodeToString(b)
		}
		out += fmt.Sprintf(`, qop=auth, nc=00000001, cnonce="%s", response="%s"`, cnonce, h(ha1+":"+p["nonce"]+":00000001:"+cnonce+":auth:"+ha2))
	} else {
		out += fmt.Sprintf(`, response="%s"`, h(ha1+":"+p["nonce"]+":"+ha2))
	}
	if p["opaque"] != "" {
		out += fmt.Sprintf(`, opaque="%s"`, p["opaque"])
	}
	if p["algorithm"] != "" {
		out += ", algorithm=MD5"
	}
	return out, true
}
