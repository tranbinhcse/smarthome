package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Trang Cài đặt mới (2026-10-27): sửa phòng (tên, tầng, mô tả), thêm phòng; thiết bị chuyển được vào phòng mới
// vì nhóm "zones" áp trước nhóm "devices". Id phòng mới phải hợp lệ, tên rỗng hay tầng ngoài 1..9 thì bỏ qua.
func TestZoneOverridesRenameAndAdd(t *testing.T) {
	cfg := Config{
		Zones:   []Zone{{ID: "cafe", Name: "Khu cafe", Floor: 1}, {ID: "kho", Name: "Kho", Floor: 1}},
		Devices: []Device{{ID: "den", Name: "Đèn", Zone: "kho"}},
	}
	applyZoneOverrides(&cfg, map[string]any{
		"cafe":       map[string]any{"name": "  Quán cafe ", "floor": 2.0, "desc": "Khu bán nước"},
		"kho":        map[string]any{"name": "", "floor": 12.0},
		"san-thuong": map[string]any{"name": "Sân thượng", "floor": 3.0},
		"Sai Id!":    map[string]any{"name": "Lỗi", "floor": 1.0},
		"rong":       map[string]any{"floor": 1.0},
	})
	applyDeviceOverrides(&cfg, map[string]any{"den": map[string]any{"zone": "san-thuong"}})
	if want := []Zone{{ID: "cafe", Name: "Quán cafe", Floor: 2, Desc: "Khu bán nước"}, {ID: "kho", Name: "Kho", Floor: 1},
		{ID: "san-thuong", Name: "Sân thượng", Floor: 3}}; !reflect.DeepEqual(cfg.Zones, want) {
		t.Fatalf("phòng = %+v\nmuốn %+v", cfg.Zones, want)
	}
	if cfg.Devices[0].Zone != "san-thuong" {
		t.Fatalf("thiết bị không chuyển được vào phòng mới: %+v", cfg.Devices[0])
	}
}

// Lưu nhóm "zones" thì hub tự nạp lại như nhóm "devices".
func TestSavingZonesRestartsHub(t *testing.T) {
	restarts := make(chan struct{}, 1)
	old := restartHub
	restartHub = func() { restarts <- struct{}{} }
	defer func() { restartHub = old }()
	mux, _, _ := assistantMux(t, Config{})
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	if w := call(mux, "PUT", "/api/settings/zones", `{"kho":{"name":"Kho mới"}}`, "", ck...); w.Code != 204 {
		t.Fatalf("lưu phòng: code=%d", w.Code)
	}
	<-restarts
}

// Trang Tự động hoá đọc luật từ /api/config (trước đây không gửi ra).
func TestConfigExposesRules(t *testing.T) {
	above := 1000.0
	mux, _, _ := assistantMux(t, Config{Rules: []Rule{{ID: "co2", Entity: "cafe.co2", Above: &above, Alert: "CO₂ cao",
		Then: []Action{{Set: "quat", To: "on"}, {Notify: "Mở cửa", Kind: "co2"}}}}})
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	var got struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(call(mux, "GET", "/api/config", "", "", ck...).Body.Bytes(), &got); err != nil || len(got.Rules) != 1 {
		t.Fatalf("rules = %+v (%v)", got.Rules, err)
	}
	r := got.Rules[0]
	then, _ := r["then"].([]any)
	if r["id"] != "co2" || r["entity"] != "cafe.co2" || r["above"] != 1000.0 || r["alert"] != "CO₂ cao" || len(then) != 2 {
		t.Fatalf("luật gửi ra thiếu trường: %v", r)
	}
	if a, _ := then[0].(map[string]any); a["set"] != "quat" || a["to"] != "on" {
		t.Fatalf("hành động: %v", then[0])
	}
}

// Tài khoản: liệt kê máy đã đăng nhập (không lộ token), đăng xuất từ xa một máy hoặc mọi máy khác.
func TestSessionsListAndRevoke(t *testing.T) {
	mux, _, _ := assistantMux(t, Config{})
	login := func() []*http.Cookie { return call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies() }
	a, b, c := login(), login(), login()
	w := call(mux, "GET", "/api/sessions", "", "", a...)
	var list []struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
		Current bool   `json:"current"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 3 {
		t.Fatalf("danh sách = %s (%v)", w.Body, err)
	}
	for _, x := range []*http.Cookie{a[0], b[0], c[0]} {
		if strings.Contains(w.Body.String(), x.Value) {
			t.Fatalf("lộ token ra trình duyệt: %s", w.Body)
		}
	}
	cur := 0
	var other string
	for _, s := range list {
		if s.Current {
			cur++
		} else if other == "" {
			other = s.ID
		}
	}
	if cur != 1 || len(list[0].ID) < 8 {
		t.Fatalf("phải đánh dấu đúng một máy hiện tại: %+v", list)
	}
	if call(mux, "DELETE", "/api/sessions/"+other, "", "", a...).Code != 204 {
		t.Fatal("không đăng xuất được một máy")
	}
	alive := 0
	for _, x := range [][]*http.Cookie{b, c} {
		if call(mux, "GET", "/api/state", "", "", x...).Code == 200 {
			alive++
		}
	}
	if alive != 1 {
		t.Fatalf("sau khi đăng xuất một máy còn %d/2 máy khác dùng được", alive)
	}
	if call(mux, "DELETE", "/api/sessions", "", "", a...).Code != 204 {
		t.Fatal("không đăng xuất được mọi máy khác")
	}
	for _, x := range [][]*http.Cookie{b, c} {
		if call(mux, "GET", "/api/state", "", "", x...).Code != 401 {
			t.Fatal("máy khác vẫn đăng nhập sau khi đăng xuất mọi máy khác")
		}
	}
	if call(mux, "GET", "/api/state", "", "", a...).Code != 200 {
		t.Fatal("máy đang dùng bị đăng xuất theo")
	}
	if call(mux, "GET", "/api/sessions", "", "").Code != 401 {
		t.Fatal("chưa đăng nhập mà xem được danh sách")
	}
}

// Sao lưu: tải mọi nhóm cài đặt nhưng bỏ hẳn trường bí mật (khoá API, mật khẩu, token); khôi phục ghi lại các nhóm đó
// (trộn theo trường, không xoá bí mật đang có) rồi hub tự nạp lại.
func TestBackupOmitsSecretsAndRestores(t *testing.T) {
	restarts := make(chan struct{}, 2)
	old := restartHub
	restartHub = func() { restarts <- struct{}{} }
	defer func() { restartHub = old }()
	mux, h, _ := assistantMux(t, Config{})
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	h.db.PutSetting("ai", map[string]any{"url": "http://ai/v1", "api_key": "sk-bi-mat", "model": "m"})
	h.db.PutSetting("devices", map[string]any{"den": map[string]any{"zone": "kho"}})
	h.db.PutSetting("nvr", map[string]any{"nvr_host": "10.0.0.5", "nvr_pass": "mk-dau-ghi"})
	w := call(mux, "GET", "/api/backup", "", "", ck...)
	body := w.Body.String()
	if w.Code != 200 || strings.Contains(body, "sk-bi-mat") || strings.Contains(body, "mk-dau-ghi") ||
		strings.Contains(body, "api_key") || !strings.Contains(body, `"nvr_host":"10.0.0.5"`) || !strings.Contains(body, `"zone":"kho"`) {
		t.Fatalf("bản sao lưu: code=%d %s", w.Code, body)
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("phải tải về thành tệp: %q", w.Header().Get("Content-Disposition"))
	}

	mux2, h2, _ := assistantMux(t, Config{})
	ck2 := call(mux2, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	// Máy mới đã nhập khoá cho đúng địa chỉ trong bản sao lưu: giữ khoá. Khác địa chỉ thì khoá bị bỏ như khi lưu tay
	// (khoá gắn với địa chỉ, không gửi sang máy chủ khác).
	h2.db.PutSetting("ai", map[string]any{"url": "http://ai/v1", "api_key": "sk-may-moi"})
	h2.db.PutSetting("voice", map[string]any{"url": "http://tts-cu", "api_key": "sk-tts"})
	restore := strings.Replace(body, `"settings":{`, `"settings":{"voice":{"url":"http://tts-moi"},`, 1)
	if r := call(mux2, "POST", "/api/restore", restore, "", ck2...); r.Code != 204 {
		t.Fatalf("khôi phục: code=%d %s", r.Code, r.Body)
	}
	<-restarts
	if s := h2.db.Setting("ai"); s["url"] != "http://ai/v1" || s["api_key"] != "sk-may-moi" || s["model"] != "m" {
		t.Fatalf("ai sau khôi phục = %v", s)
	}
	if s := h2.db.Setting("voice"); s["url"] != "http://tts-moi" || s["api_key"] != nil {
		t.Fatalf("đổi địa chỉ khi khôi phục phải bỏ khoá cũ: %v", s)
	}
	if d, _ := h2.db.Setting("devices")["den"].(map[string]any); d["zone"] != "kho" {
		t.Fatalf("devices sau khôi phục = %v", h2.db.Setting("devices"))
	}
	for _, bad := range []string{`{}`, `{"settings":{"Sai Ten!":{"a":1}}}`, `không phải json`} {
		if r := call(mux2, "POST", "/api/restore", bad, "", ck2...); r.Code != 400 {
			t.Fatalf("tệp sai %q: code=%d", bad, r.Code)
		}
	}
	if call(mux2, "POST", "/api/restore", body, "").Code != 401 {
		t.Fatal("chưa đăng nhập mà khôi phục được")
	}
}
