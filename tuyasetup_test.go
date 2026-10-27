package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// Quét QR Tuya từ dashboard: hubd chuyển lệnh (cần PIN) sang tuya-bridge qua MQTT, lưu user code để lần sau
// điền sẵn; trạng thái bridge gửi về (mã QR, tiến độ, danh sách thiết bị) đi thẳng lên giao diện, không vào lịch sử.
func TestTuyaSetupThroughHub(t *testing.T) {
	mux, h, pub := assistantMux(t, Config{})
	if w := call(mux, "POST", "/api/setup/tuya", `{"action":"start","user_code":"abc123"}`, ""); w.Code != 401 {
		t.Fatalf("chưa nhập PIN mà bắt đầu được: code=%d", w.Code)
	}
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	for _, bad := range []string{`{"action":"start","user_code":"  "}`, `{"action":"xoa"}`, `nope`} {
		if w := call(mux, "POST", "/api/setup/tuya", bad, "", ck...); w.Code != 400 {
			t.Errorf("%s: code=%d, muốn 400", bad, w.Code)
		}
	}
	if w := call(mux, "POST", "/api/setup/tuya", `{"action":"start","user_code":" abc123 "}`, "", ck...); w.Code != 204 {
		t.Fatalf("bắt đầu: code=%d %s", w.Code, w.Body)
	}
	call(mux, "POST", "/api/setup/tuya", `{"action":"cancel"}`, "", ck...)
	all := strings.Join(*pub, "\n")
	if !strings.Contains(all, `tuya/setup/cmd {"action":"start","user_code":"abc123"}`) || !strings.Contains(all, `tuya/setup/cmd {"action":"cancel"}`) {
		t.Fatalf("lệnh gửi bridge sai:\n%s", all)
	}
	if h.db.Setting("tuya")["user_code"] != "abc123" {
		t.Fatal("chưa lưu user code để lần sau điền sẵn")
	}
	if !slices.Contains(h.Topics(), "tuya/setup/status") {
		t.Fatal("hubd không đăng ký topic trạng thái quét QR")
	}
	h.Handle("tuya/setup/status", `{"state":"qr"}`, t0)
	if v := h.Snapshot()["setup:tuya"]; v != `{"state":"qr"}` {
		t.Fatalf("setup:tuya = %q", v)
	}
	if p, _ := h.db.Query("setup:tuya", t0.Add(-time.Hour), t0.Add(time.Hour), 10); len(p) != 0 {
		t.Fatalf("trạng thái quét QR không được vào lịch sử: %v", p)
	}
}
