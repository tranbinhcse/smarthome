package main

import (
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

// Nhiều mục đọc chung một topic JSON (dps của CB đo điện): mỗi mục lấy một khoá, nhân hệ số.
func TestHandleSharedJSONTopic(t *testing.T) {
	cfg := Config{
		Entities: []Entity{
			{ID: "cb.w", Topic: "tuya/cb/dps", Field: "19", Scale: 0.1},
			{ID: "cb.v", Topic: "tuya/cb/dps", Field: "20", Scale: 0.1},
			{ID: "cb.a", Topic: "tuya/cb/dps", Field: "18", Scale: 0.001},
			{ID: "cb.on", Topic: "tuya/cb/dps", Field: "1"},
			{ID: "nhiet", Topic: "khu1/nhiet"},
		},
		Devices: []Device{{ID: "cb", StateTopic: "tuya/cb/state", OnlineTopic: "tuya/cb/online"}},
	}
	h := NewHub(cfg, newDB(t), func(string, string) {})
	now := time.Now()
	h.Handle("tuya/cb/dps", `{"1": true, "18": 2831, "19": 4911, "20": 2405}`, now)
	h.Handle("khu1/nhiet", "28.5", now)
	h.Handle("tuya/cb/state", "ON", now)
	h.Handle("tuya/cb/dps", "không phải json", now) // bỏ qua, giữ giá trị cũ
	h.Handle("tuya/cb/dps", `{"9": 0}`, now)        // thiếu khoá: giữ giá trị cũ
	h.Handle("khong/dang/ky", "1", now)

	s := h.Snapshot()
	want := map[string]string{"cb.w": "491.1", "cb.v": "240.5", "cb.a": "2.831", "cb.on": "1",
		"nhiet": "28.5", "dev:cb": "ON"}
	for k, v := range want {
		if s[k] != v {
			t.Errorf("%s = %q, muốn %q", k, s[k], v)
		}
	}
	if len(s) != len(want) {
		t.Errorf("snapshot thừa khoá: %v", s)
	}
	got := h.Topics()
	sort.Strings(got)
	exp := []string{"khu1/nhiet", "tuya/cb/dps", "tuya/cb/online", "tuya/cb/state"}
	if len(got) != len(exp) {
		t.Fatalf("Topics() = %v, muốn %v (mỗi topic đăng ký một lần)", got, exp)
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Fatalf("Topics() = %v, muốn %v", got, exp)
		}
	}
}

// File giao diện không được cache: iPad (nhất là khi thêm vào màn hình chính) phải nhận bản mới sau mỗi lần cập nhật.
func TestStaticNoCache(t *testing.T) {
	h := NewHub(Config{}, newDB(t), func(string, string) {})
	mux := newMux(h, NewAuth("", h.db))
	for _, u := range []string{"/", "/dash.js", "/dash.css"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", u, nil))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: code=%d Cache-Control=%q, muốn 200 + no-cache", u, w.Code, w.Header().Get("Cache-Control"))
		}
	}
}
