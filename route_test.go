package main

import (
	"encoding/json"
	"net/http/httptest"
	"sort"
	"strings"
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
	exp := []string{"khu1/nhiet", "tuya/cb/dps", "tuya/cb/online", "tuya/cb/state", "tuya/setup/status"} // + tiến độ quét QR, luôn đăng ký
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

// Ổ cắm nhiều lỗ: mỗi lỗ là một thiết bị đọc chung topic dps, mỗi lỗ một khoá; group gom lại trên giao diện.
func TestDeviceStateFieldFromSharedDps(t *testing.T) {
	cfg := Config{Devices: []Device{
		{ID: "oc1", Group: "Ổ cắm quầy", StateTopic: "tuya/oc/dps", Field: "1"},
		{ID: "oc2", Group: "Ổ cắm quầy", StateTopic: "tuya/oc/dps", Field: "2"},
		{ID: "den", StateTopic: "tuya/den/state"},
	}}
	h := NewHub(cfg, newDB(t), func(string, string) {})
	now := time.Now()
	h.Handle("tuya/oc/dps", `{"1": true, "2": false}`, now)
	h.Handle("tuya/den/state", "ON", now)
	s := h.Snapshot()
	for k, v := range map[string]string{"dev:oc1": "1", "dev:oc2": "0", "dev:den": "ON"} {
		if s[k] != v {
			t.Errorf("%s = %q, muốn %q", k, s[k], v)
		}
	}
	if b, _ := json.Marshal(cfg.Devices[0]); !strings.Contains(string(b), `"group":"Ổ cắm quầy"`) {
		t.Errorf("thiếu group trong JSON gửi giao diện: %s", b)
	}
}
