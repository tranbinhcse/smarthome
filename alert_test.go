package main

import (
	"strings"
	"testing"
	"time"
)

// Luật có `alert`: kích hoạt thì state "alert:<id>" = lời cảnh báo (đẩy qua SSE), nhả thì "".
func TestRuleAlertState(t *testing.T) {
	cfg := Config{Rules: []Rule{{ID: "pin_yeu", Entity: "jk.soc", Below: f(20), ClearAbove: f(25), Alert: "Pin dưới 20%"}}}
	h := NewHub(cfg, newDB(t), func(string, string) { t.Fatal("luật chỉ cảnh báo không được gửi lệnh") })
	ch := h.Sub()
	defer h.Unsub(ch)

	h.Ingest("jk.soc", "50", t0)
	if v, ok := h.Snapshot()["alert:pin_yeu"]; ok {
		t.Fatalf("chưa tới ngưỡng mà đã có cảnh báo %q", v)
	}
	h.Ingest("jk.soc", "18", t0.Add(time.Second))
	if v := h.Snapshot()["alert:pin_yeu"]; v != "Pin dưới 20%" {
		t.Fatalf("alert:pin_yeu = %q", v)
	}
	h.Ingest("jk.soc", "22", t0.Add(2*time.Second)) // chống dao động: chưa nhả
	if v := h.Snapshot()["alert:pin_yeu"]; v == "" {
		t.Fatal("22% chưa qua clear_above 25 mà đã nhả")
	}
	h.Ingest("jk.soc", "26", t0.Add(3*time.Second))
	if v, ok := h.Snapshot()["alert:pin_yeu"]; !ok || v != "" {
		t.Fatalf("nhả phải để chuỗi rỗng, được %q (có=%v)", v, ok)
	}

	var sse []string
	for len(ch) > 0 {
		sse = append(sse, string(<-ch))
	}
	if all := strings.Join(sse, "\n"); !strings.Contains(all, `"id":"alert:pin_yeu","value":"Pin dưới 20%"`) ||
		!strings.Contains(all, `"id":"alert:pin_yeu","value":""`) {
		t.Fatalf("SSE thiếu bật/nhả cảnh báo:\n%s", all)
	}
}

// ESPHome gửi "nan" khi cảm biến mất (pack JK mất kết nối). Đó là "chưa biết", không phải số đo:
// không ghi lịch sử, không làm luật đang đếm "for" bắt đầu lại.
func TestNaNPayloadIgnoredByHistoryAndRules(t *testing.T) {
	cfg := Config{Rules: []Rule{{ID: "nong", Entity: "jk.temp", Above: f(45), For: time.Minute, Alert: "Nóng"}}}
	h := NewHub(cfg, newDB(t), func(string, string) {})
	h.Ingest("jk.temp", "47", t0)
	h.Ingest("jk.temp", "nan", t0.Add(30*time.Second))
	if v := h.Snapshot()["jk.temp"]; v != "nan" {
		t.Fatalf("giao diện vẫn phải thấy giá trị thô, được %q", v)
	}
	h.Tick(t0.Add(time.Minute))
	if v := h.Snapshot()["alert:nong"]; v != "Nóng" {
		t.Fatalf("nan giữa chừng làm luật đếm lại: alert = %q", v)
	}
	if p, _ := h.db.Query("jk.temp", t0, t0.Add(time.Hour), 3600); len(p) != 1 || p[0].V != 47 {
		t.Fatalf("lịch sử chỉ được có mẫu 47: %v", p)
	}
}
