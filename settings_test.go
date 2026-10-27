package main

import (
	"strings"
	"testing"
)

// Cấu hình nhập từ trang Cài đặt: lưu theo nhóm, gộp khi lưu, bí mật không bao giờ trả lại trình duyệt.
func TestSettingsMaskSecretsAndMerge(t *testing.T) {
	mux, h, pub := assistantMux(t, Config{})
	if w := call(mux, "PUT", "/api/settings/ai", `{"url":"http://x/v1"}`, ""); w.Code != 401 {
		t.Fatalf("chưa nhập PIN mà lưu được: code=%d", w.Code)
	}
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	if w := call(mux, "PUT", "/api/settings/ai", `{"url":"http://x/v1","api_key":"sk-bi-mat","model":"m1"}`, "", ck...); w.Code != 204 {
		t.Fatalf("lưu: code=%d %s", w.Code, w.Body)
	}
	body := call(mux, "GET", "/api/settings/ai", "", "", ck...).Body.String()
	if strings.Contains(body, "sk-bi-mat") || !strings.Contains(body, `"api_key_set":true`) || !strings.Contains(body, `"url":"http://x/v1"`) {
		t.Fatalf("trình duyệt phải thấy url, chỉ biết key đã lưu, không thấy key: %s", body)
	}
	// Gửi key rỗng = giữ key cũ (ô mật khẩu để trống khi chỉ đổi model); null = xoá.
	call(mux, "PUT", "/api/settings/ai", `{"model":"m2","api_key":""}`, "", ck...)
	if s := h.db.Setting("ai"); s["api_key"] != "sk-bi-mat" || s["model"] != "m2" || s["url"] != "http://x/v1" {
		t.Fatalf("gộp sai: %v", s)
	}
	// Dịch vụ dùng token trợ lý đọc được giá trị đầy đủ của nhóm "ai", không đọc được nhóm khác.
	if b := call(mux, "GET", "/api/assistant/settings/ai", "", testTok).Body.String(); !strings.Contains(b, `"api_key":"sk-bi-mat"`) {
		t.Fatalf("voiced không đọc được cấu hình AI: %s", b)
	}
	call(mux, "PUT", "/api/settings/nvr", `{"nvr_pass":"p"}`, "", ck...)
	if w := call(mux, "GET", "/api/assistant/settings/nvr", "", testTok); w.Code != 403 {
		t.Fatalf("token trợ lý đọc được mật khẩu đầu ghi: code=%d", w.Code)
	}
	// Lưu thì báo đổi qua MQTT để dịch vụ đọc lại, không kèm giá trị.
	all := strings.Join(*pub, "\n")
	if !strings.Contains(all, "hubd/settings/ai changed") || strings.Contains(all, "sk-bi-mat") {
		t.Fatalf("thông báo đổi cấu hình sai: %q", all)
	}
	call(mux, "PUT", "/api/settings/ai", `{"api_key":null}`, "", ck...)
	if _, ok := h.db.Setting("ai")["api_key"]; ok {
		t.Fatal("null phải xoá khoá")
	}
}

func TestSettingsNamespaceAndPersistence(t *testing.T) {
	mux, h, _ := assistantMux(t, Config{})
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	for _, ns := range []string{"Ai", "a.b", "x%2Fy", strings.Repeat("a", 33)} {
		if w := call(mux, "PUT", "/api/settings/"+ns, `{}`, "", ck...); w.Code != 400 && w.Code != 404 {
			t.Errorf("nhóm %q phải bị từ chối, code=%d", ns, w.Code)
		}
	}
	if w := call(mux, "PUT", "/api/settings/tuya", `[1,2]`, "", ck...); w.Code != 400 {
		t.Errorf("thân không phải object phải bị từ chối, code=%d", w.Code)
	}
	call(mux, "PUT", "/api/settings/tuya", `{"user_code":"abc"}`, "", ck...)
	h2 := NewHub(Config{}, h.db, func(string, string) {}) // khởi động lại hubd: đọc lại từ SQLite
	if h2.db.Setting("tuya")["user_code"] != "abc" {
		t.Fatal("cấu hình không còn sau khi khởi động lại")
	}
}
