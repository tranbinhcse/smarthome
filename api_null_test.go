package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hasNull(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		for _, e := range x {
			if hasNull(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if hasNull(e) {
				return true
			}
		}
	}
	return false
}

// Config chưa có thiết bị / cảm biến chưa có dữ liệu: mọi API JSON GET phải trả [] chứ không phải null,
// nếu không dashboard vỡ khi gọi .filter/.map. Quét đủ 3/3 route JSON GET của newMux.
func TestJSONAPINoNullWhenEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte("mqtt:\n  broker: tcp://x:1883\n"), 0o600)
	t.Setenv("HUBD_PIN", "1234")
	cfg, pin, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(cfg, newDB(t), func(string, string) {})
	mux := newMux(h, NewAuth(pin, h.db))
	lw := httptest.NewRecorder()
	mux.ServeHTTP(lw, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"pin":"1234"}`)))
	ck := lw.Result().Cookies()[0]
	for _, u := range []string{"/api/config", "/api/state", "/api/history?id=chua_co"} {
		r := httptest.NewRequest("GET", u, nil)
		r.AddCookie(ck)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || hasNull(v) {
			t.Errorf("%s: code=%d body=%s — không được có null", u, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}
}
