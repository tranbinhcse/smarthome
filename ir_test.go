package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Thiết bị hồng ngoại: máy lạnh có climate_ir trên node (hub chuyển nguyên mã), TV dùng mã học được (hub gửi chuỗi Pronto).
func irHub(t *testing.T) (*Hub, *[][2]string) {
	var sent [][2]string
	cfg := Config{Devices: []Device{
		{ID: "ml", Kind: "ac", CommandTopic: "node-cafe/ir/ac"},
		{ID: "tv", Kind: "tv", CommandTopic: "node-cafe/ir/send", Codes: map[string]string{"power": "P", "vol_up": "V+"}},
		{ID: "ml_kt", Kind: "ac", CommandTopic: "node-kt/ir/ac", Protected: true},
		{ID: "quat", Kind: "fan", CommandTopic: "node-lv/ir/send"}, // chưa học mã nào
		{ID: "den", Kind: "kg", CommandTopic: "tuya/den/cmd"},
	}}
	h := NewHub(cfg, newDB(t), func(tp, p string) { sent = append(sent, [2]string{tp, p}) })
	return h, &sent
}

func TestIRClimatePassThroughAndAssumedState(t *testing.T) {
	h, sent := irHub(t)
	for _, c := range []string{"power", "temp_24", "mode_dry", "fan_2", "swing", "power"} {
		if !h.Command("ml", c) {
			t.Fatalf("%s bị từ chối", c)
		}
	}
	// nút nguồn đổi thành on/off rõ ràng theo trạng thái giả định: node không phải đoán
	want := []string{"on", "temp_24", "mode_dry", "fan_2", "swing", "off"}
	for i, w := range want {
		if (*sent)[i] != [2]string{"node-cafe/ir/ac", w} {
			t.Fatalf("lệnh %d = %v, muốn %s", i, (*sent)[i], w)
		}
	}
	s := h.Snapshot()
	for k, v := range map[string]string{"dev:ml": "OFF", "x:ml:temp": "24", "x:ml:mode": "dry", "x:ml:fan": "2", "x:ml:swing": "1"} {
		if s[k] != v {
			t.Errorf("%s = %q, muốn %q", k, s[k], v)
		}
	}
}

func TestIRLearnedCodes(t *testing.T) {
	h, sent := irHub(t)
	if !h.Command("tv", "vol_up") || !h.Command("tv", "power") {
		t.Fatal("mã đã học phải gửi được")
	}
	if (*sent)[0] != [2]string{"node-cafe/ir/send", "V+"} || (*sent)[1] != [2]string{"node-cafe/ir/send", "P"} {
		t.Fatalf("payload phải là mã học được: %v", *sent)
	}
	if h.Command("tv", "ch_up") || h.Command("quat", "speed_2") {
		t.Fatal("nút chưa học mã phải bị từ chối để dashboard báo lỗi")
	}
	// luật/API cũ gửi on/off: TV chỉ có nút nguồn đảo trạng thái -> bấm khi khác trạng thái giả định
	if !h.Command("tv", "on") || len(*sent) != 2 {
		t.Fatalf("TV đang bật (giả định) thì on không được bấm nguồn lần nữa: %v", *sent)
	}
	if !h.Command("tv", "off") || (*sent)[2][1] != "P" || h.Snapshot()["dev:tv"] != "OFF" {
		t.Fatalf("off phải bấm nguồn: %v", *sent)
	}
}

func TestIRProtectedNeverTurnedOff(t *testing.T) {
	h, sent := irHub(t)
	if !h.Command("ml_kt", "power") || (*sent)[0][1] != "on" {
		t.Fatalf("bật máy lạnh bảo vệ vẫn được: %v", *sent)
	}
	if h.Command("ml_kt", "power") || h.Command("ml_kt", "off") || len(*sent) != 1 {
		t.Fatalf("không lệnh nào được tắt thiết bị bảo vệ: %v", *sent)
	}
	h.Ingest("x", "5", t0)
	h.run([]Action{{Set: "ml_kt", To: "off"}})
	if len(*sent) != 1 {
		t.Fatalf("luật không được tắt thiết bị bảo vệ: %v", *sent)
	}
}

func TestIRRulesUseSamePath(t *testing.T) {
	h, sent := irHub(t)
	h.run([]Action{{Set: "ml", To: "on"}, {Set: "tv", To: "on"}})
	if len(*sent) != 2 || (*sent)[0] != [2]string{"node-cafe/ir/ac", "on"} || (*sent)[1] != [2]string{"node-cafe/ir/send", "P"} {
		t.Fatalf("luật bật thiết bị hồng ngoại phải qua cùng đường dịch mã: %v", *sent)
	}
}

func TestIRRejectsBadCodesAndKeepsOnOffAPI(t *testing.T) {
	h, sent := irHub(t)
	for _, c := range []string{"", "Power", "temp_24;reboot", "x/../y", strings.Repeat("a", 40)} {
		if h.Command("ml", c) {
			t.Errorf("mã %q phải bị từ chối", c)
		}
	}
	if h.Command("den", "temp_24") {
		t.Error("thiết bị không phải hồng ngoại chỉ nhận on/off")
	}
	if !h.Command("den", "off") || (*sent)[len(*sent)-1] != [2]string{"tuya/den/cmd", "OFF"} {
		t.Fatalf("API {device,to:on|off} cũ phải giữ nguyên: %v", *sent)
	}
	// trạng thái giả định không ghi lịch sử, không làm nhiễu biểu đồ
	h.Command("ml", "temp_25")
	if p, _ := h.db.Query("x:ml:temp", t0.AddDate(-1, 0, 0), t0.AddDate(1, 0, 0), 100); len(p) != 0 {
		t.Fatalf("x:* không được vào lịch sử: %v", p)
	}
}

func TestAPICmdAcceptsIRCodes(t *testing.T) {
	h, sent := irHub(t)
	mux := newMux(h, NewAuth("", h.db))
	post := func(body string) int {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/cmd", strings.NewReader(body)))
		return w.Code
	}
	if post(`{"device":"ml","to":"temp_24"}`) != 204 || (*sent)[0] != [2]string{"node-cafe/ir/ac", "temp_24"} {
		t.Fatalf("mã IR qua /api/cmd: %v", *sent)
	}
	if post(`{"device":"tv","to":"ch_up"}`) != 400 || post(`{"device":"ml_kt","to":"off"}`) != 400 {
		t.Fatal("mã chưa học / tắt thiết bị bảo vệ phải 400")
	}
}
