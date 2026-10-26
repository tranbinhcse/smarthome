package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newDB(t *testing.T) *DB {
	d, err := OpenDB(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestHistoryThrottleQueryPrune(t *testing.T) {
	d := newDB(t)
	for i := 0; i < 10; i++ { // 10 mẫu cách 10 s, gap 30 s -> còn 4
		d.Record("a", float64(i), t0.Add(time.Duration(i*10)*time.Second), 30*time.Second)
	}
	pts, err := d.Query("a", t0, t0.Add(time.Hour), 1000)
	if err != nil || len(pts) != 4 {
		t.Fatalf("cần 4 điểm, được %d (%v)", len(pts), err)
	}
	d.Record("b", 1, t0, 0)
	if p, _ := d.Query("a", t0, t0.Add(time.Hour), 2); len(p) > 3 {
		t.Fatalf("downsample thất bại: %d", len(p))
	}
	d.Prune(time.Hour, t0.Add(3*time.Hour))
	if p, _ := d.Query("a", t0, t0.Add(4*time.Hour), 100); len(p) != 0 {
		t.Fatal("Prune không xoá")
	}
}

func TestAuthFlow(t *testing.T) {
	a := NewAuth("2468", newDB(t))
	h := a.Require(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	do := func(f http.HandlerFunc, body string, c *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		f(w, r)
		return w
	}
	if do(h, "", nil).Code != 401 {
		t.Fatal("không cookie phải 401")
	}
	if do(a.Login, `{"pin":"0000"}`, nil).Code != 401 {
		t.Fatal("sai PIN phải 401")
	}
	w := do(a.Login, `{"pin":"2468"}`, nil)
	if w.Code != 204 || len(w.Result().Cookies()) != 1 || !w.Result().Cookies()[0].HttpOnly {
		t.Fatalf("login: %d", w.Code)
	}
	ck := w.Result().Cookies()[0]
	if do(h, "", ck).Code != 204 {
		t.Fatal("cookie đúng phải qua")
	}
	if do(h, "", &http.Cookie{Name: cookieName, Value: "gia"}).Code != 401 {
		t.Fatal("cookie giả phải 401")
	}
}

func TestAuthLockout(t *testing.T) {
	a := NewAuth("2468", newDB(t))
	now := t0
	a.now = func() time.Time { return now }
	try := func(pin string) int {
		w := httptest.NewRecorder()
		a.Login(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"pin":"`+pin+`"}`)))
		return w.Code
	}
	for i := 0; i < 5; i++ {
		try("x")
	}
	if try("2468") != 429 {
		t.Fatal("đúng PIN trong lúc khoá vẫn phải bị chặn")
	}
	now = now.Add(2 * time.Minute)
	if try("2468") != 204 {
		t.Fatal("hết khoá phải đăng nhập được")
	}
}

func TestAuthDisabledWhenNoPin(t *testing.T) {
	a := NewAuth("", newDB(t))
	if !a.valid(httptest.NewRequest("GET", "/", nil)) {
		t.Fatal("PIN rỗng = mở")
	}
}
