package main

import (
	"testing"
	"time"
)

// Sắp xếp thiết bị vào phòng trên trang Cài đặt (nhóm cấu hình "devices"): đổi phòng, đổi tên từng thiết bị
// hoặc từng kênh; CB thì chọn khu được cấp điện để số đo hiện đúng phòng. Phòng không tồn tại thì bỏ qua.
func TestDeviceOverridesFromSettings(t *testing.T) {
	cfg := Config{
		Zones: []Zone{{ID: "cafe"}, {ID: "kho"}, {ID: "tudien1", Type: "panel"}},
		Devices: []Device{
			{ID: "tuya_a_1", Name: "Nút 1", Zone: "kho", Group: "Đèn Kho"},
			{ID: "tuya_a_2", Name: "Nút 2", Zone: "kho", Group: "Đèn Kho"},
			{ID: "cb", Name: "CB Bếp", Zone: "tudien1", Kind: "breaker"},
		},
		Entities: []Entity{{ID: "cb.w", Device: "cb", Zone: "tudien1"}, {ID: "kho.t", Zone: "kho"}},
	}
	applyDeviceOverrides(&cfg, map[string]any{
		"tuya_a_1": map[string]any{"zone": "cafe", "name": "  Đèn trần  "},
		"tuya_a_2": map[string]any{"zone": "phong-khong-co", "name": ""},
		"cb":       map[string]any{"area": "cafe"},
		"khong_co": map[string]any{"zone": "cafe"},
		"rac":      "khong-phai-object",
	})
	d := map[string]Device{}
	for _, x := range cfg.Devices {
		d[x.ID] = x
	}
	if d["tuya_a_1"].Zone != "cafe" || d["tuya_a_1"].Name != "Đèn trần" {
		t.Fatalf("kênh 1 = %+v", d["tuya_a_1"])
	}
	if d["tuya_a_2"].Zone != "kho" || d["tuya_a_2"].Name != "Nút 2" {
		t.Fatalf("phòng lạ / tên rỗng phải giữ nguyên, được %+v", d["tuya_a_2"])
	}
	if d["cb"].Zone != "tudien1" || cfg.Entities[0].Zone != "cafe" || cfg.Entities[1].Zone != "kho" {
		t.Fatalf("CB: thiết bị ở tủ điện, số đo sang khu được cấp điện: %+v %+v", d["cb"], cfg.Entities)
	}
}

// Lưu sắp xếp thiết bị thì hubd nạp lại (khởi động lại); lưu nhóm khác thì không.
func TestSavingDeviceLayoutRestartsHub(t *testing.T) {
	restarts := make(chan struct{}, 4)
	old := restartHub
	restartHub = func() { restarts <- struct{}{} }
	defer func() { restartHub = old }()
	mux, h, _ := assistantMux(t, Config{})
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	call(mux, "PUT", "/api/settings/tuya", `{"user_code":"x"}`, "", ck...)
	if w := call(mux, "PUT", "/api/settings/devices", `{"tuya_a_1":{"zone":"cafe"}}`, "", ck...); w.Code != 204 {
		t.Fatalf("code=%d", w.Code)
	}
	select {
	case <-restarts:
	case <-time.After(2 * time.Second):
		t.Fatal("lưu sắp xếp thiết bị mà hubd không nạp lại")
	}
	select {
	case <-restarts:
		t.Fatal("lưu nhóm khác cũng làm hubd khởi động lại")
	case <-time.After(100 * time.Millisecond):
	}
	if z := h.db.Setting("devices")["tuya_a_1"].(map[string]any)["zone"]; z != "cafe" {
		t.Fatalf("chưa lưu: %v", z)
	}
}
