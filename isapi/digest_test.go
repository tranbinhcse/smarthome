package isapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Ví dụ chuẩn trong RFC 2617 mục 3.5.
func TestDigestRFC2617Vector(t *testing.T) {
	ch := `Digest realm="testrealm@host.com", qop="auth,auth-int", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", opaque="5ccc069c403ebaf9f0171e9517f40e41"`
	got, ok := digest(ch, "GET", "/dir/index.html", "Mufasa", "Circle Of Life", "0a4f113b")
	if !ok {
		t.Fatal("không đọc được thử thách Digest")
	}
	for _, want := range []string{`response="6629fae49393a05397450978507c4ef1"`, `qop=auth`, `nc=00000001`,
		`cnonce="0a4f113b"`, `opaque="5ccc069c403ebaf9f0171e9517f40e41"`, `uri="/dir/index.html"`, `username="Mufasa"`} {
		if !strings.Contains(got, want) {
			t.Errorf("thiếu %s trong %s", want, got)
		}
	}
}

// Đầu ghi giả: đòi Digest, chấp nhận khi phản hồi khớp mật khẩu thật.
func fakeNVR(t *testing.T, pass string, hits *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("Authorization")
		if a == "" {
			w.Header().Add("WWW-Authenticate", `Basic realm="x"`)
			w.Header().Add("WWW-Authenticate", `Digest realm="DS-7608", nonce="abc123", qop="auth"`)
			w.WriteHeader(401)
			return
		}
		*hits++
		cn := between(a, `cnonce="`, `"`)
		want, _ := digest(`Digest realm="DS-7608", nonce="abc123", qop="auth"`, r.Method, r.URL.RequestURI(), "viewer", pass, cn)
		if a != want {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, "ok")
	}))
}

func between(s, a, b string) string {
	_, s, _ = strings.Cut(s, a)
	s, _, _ = strings.Cut(s, b)
	return s
}

func TestTransportAnswersChallenge(t *testing.T) {
	hits := 0
	srv := fakeNVR(t, "dung", &hits)
	defer srv.Close()
	c := &http.Client{Transport: &Transport{User: "viewer", Pass: "dung"}}
	res, err := c.Get(srv.URL + "/ISAPI/Streaming/channels/101/picture?x=1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(b) != "ok" {
		t.Fatalf("code=%d body=%q", res.StatusCode, b)
	}
}

// Sai mật khẩu: đầu ghi Hikvision khoá IP sau vài lần sai, nên ngừng gửi mật khẩu 15 phút.
func TestTransportBacksOffAfterWrongPassword(t *testing.T) {
	hits := 0
	srv := fakeNVR(t, "dung", &hits)
	defer srv.Close()
	now := time.Unix(1000, 0)
	tr := &Transport{User: "viewer", Pass: "sai", now: func() time.Time { return now }}
	c := &http.Client{Transport: tr}
	res, err := c.Get(srv.URL + "/a")
	if err != nil || res.StatusCode != 401 {
		t.Fatalf("lần đầu: err=%v res=%v", err, res)
	}
	for i := 0; i < 3; i++ {
		if _, err := c.Get(srv.URL + "/a"); !errors.Is(err, ErrLocked) {
			t.Fatalf("lần %d: err=%v, muốn ErrLocked", i+2, err)
		}
	}
	if hits != 1 {
		t.Fatalf("đầu ghi nhận %d lần gửi mật khẩu, muốn 1", hits)
	}
	now = now.Add(15*time.Minute + time.Second)
	tr.Pass = "dung"
	if res, err := c.Get(srv.URL + "/a"); err != nil || res.StatusCode != 200 {
		t.Fatalf("sau 15 phút: err=%v res=%v", err, res)
	}
}
