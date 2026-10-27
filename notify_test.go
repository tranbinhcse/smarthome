package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Tắt từng loại thông báo trong Cài đặt (nhóm "notify", khoá = kind của notify): loại bị tắt không lên hubd/alert
// (trợ lý không đọc), còn lệnh tới thiết bị của luật vẫn chạy. Quét đủ không gian:
// trạng thái công tắc {chưa lưu, bật, tắt} × kind của notify {không có, co2} = 3×2 = 6/6 trường hợp.
func TestNotifyKindMuteSweep(t *testing.T) {
	n := 0
	for _, toggle := range []string{"", `{"co2":true}`, `{"co2":false}`} {
		for _, kind := range []string{"", "co2"} {
			n++
			cfg := Config{
				Devices: []Device{{ID: "quat", CommandTopic: "quat/cmd"}},
				Rules: []Rule{{ID: "co2_cafe", Entity: "cafe.co2", Above: f(1200),
					Then: []Action{{Notify: "CO2 ở khu cafe cao", Level: "warn", Kind: kind}, {Set: "quat", To: "on"}}}},
			}
			mux, h, sent := assistantMux(t, cfg)
			if toggle != "" {
				ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
				if w := call(mux, "PUT", "/api/settings/notify", toggle, "", ck...); w.Code != 204 {
					t.Fatalf("lưu công tắc: %d", w.Code)
				}
			}
			*sent = nil
			h.Ingest("cafe.co2", "1500", t0)
			all := strings.Join(*sent, "\n")
			muted := kind == "co2" && toggle == `{"co2":false}`
			if strings.Contains(all, "hubd/alert ") == muted {
				t.Errorf("công tắc %q, kind %q: tin đã gửi %q, muốn tắt tiếng=%v", toggle, kind, all, muted)
			}
			if !strings.Contains(all, "quat/cmd ON") {
				t.Errorf("công tắc %q, kind %q: tắt thông báo không được chặn lệnh bật quạt: %q", toggle, kind, all)
			}
			if got := len(h.Alerts()); got != map[bool]int{true: 0, false: 1}[muted] {
				t.Errorf("công tắc %q, kind %q: %d cảnh báo chờ xác nhận", toggle, kind, got)
			}
		}
	}
	if n != 6 {
		t.Fatalf("quét %d trường hợp, muốn 6", n)
	}
}

// Trang Cài đặt cần danh sách loại thông báo (gom theo kind, theo thứ tự trong config) để vẽ công tắc.
func TestNotifyKindsAPI(t *testing.T) {
	cfg := Config{Rules: []Rule{
		{ID: "a", Then: []Action{{Notify: "CO2 khu cafe cao", Kind: "co2"}}},
		{ID: "b", Then: []Action{{Notify: "Trời bắt đầu mưa", Kind: "mua"}}},
		{ID: "c", Then: []Action{{Notify: "CO2 phòng làm việc cao", Kind: "co2"}}, OnClear: []Action{{Notify: "Trời tạnh", Kind: "mua"}}},
		{ID: "d", Then: []Action{{Notify: "Có khói"}}}, // không có kind: luôn báo, không có công tắc
	}}
	mux, _, _ := assistantMux(t, cfg)
	if w := call(mux, "GET", "/api/notify", "", ""); w.Code != 401 {
		t.Fatalf("chưa nhập PIN mà xem được: %d", w.Code)
	}
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	w := call(mux, "GET", "/api/notify", "", "", ck...)
	var got []struct {
		Kind  string   `json:"kind"`
		Texts []string `json:"texts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	want := fmt.Sprint([]any{"co2", []string{"CO2 khu cafe cao", "CO2 phòng làm việc cao"}, "mua", []string{"Trời bắt đầu mưa", "Trời tạnh"}})
	var flat []any
	for _, k := range got {
		flat = append(flat, k.Kind, k.Texts)
	}
	if fmt.Sprint(flat) != want {
		t.Fatalf("được %v, muốn %v", flat, want)
	}
	if b := call(newMux(NewHub(Config{}, newDB(t), func(string, string) {}), NewAuth("", nil)), "GET", "/api/notify", "", "").Body.String(); strings.TrimSpace(b) != "[]" {
		t.Fatalf("không có luật notify phải trả [], được %s", b)
	}
}

// Luật notify mặc định trong config.yaml: đủ bốn loại chủ nhà cần (CO₂, bụi mịn, mưa, nóng), mỗi luật đọc một số đo
// có thật, và giá trị vượt ngưỡng trong đủ thời gian "for" thì thật sự phát câu cảnh báo.
// Quét toàn bộ tập luật có notify trong config.yaml (không phải danh sách viết tay).
func TestConfigYAMLNotifyRules(t *testing.T) {
	cfg := loadRealConfig(t)
	ents := map[string]bool{}
	for _, e := range cfg.Entities {
		ents[e.ID] = true
	}
	kinds, n := map[string]int{}, 0
	for _, r := range cfg.Rules {
		var texts []string
		for _, a := range r.Then {
			if a.Notify == "" {
				continue
			}
			texts = append(texts, a.Notify)
			kinds[a.Kind]++
			if a.Kind == "" || !slices.Contains([]string{"info", "warn", "urgent"}, a.Level) {
				t.Errorf("luật %s: notify %q cần kind và level info|warn|urgent", r.ID, a.Notify)
			}
		}
		if len(texts) == 0 {
			continue
		}
		n++
		if !ents[r.Entity] {
			t.Errorf("luật %s đọc %q: không có số đo này trong config.yaml", r.ID, r.Entity)
			continue
		}
		v := 0.0
		switch {
		case r.Above != nil:
			v = *r.Above + 1
		case r.Below != nil:
			v = *r.Below - 1
		}
		var got []string
		h := NewHub(Config{Entities: cfg.Entities, Rules: []Rule{r}}, newDB(t), func(tp, p string) {
			if tp == "hubd/alert" {
				var m map[string]string
				json.Unmarshal([]byte(p), &m)
				got = append(got, m["text"])
			}
		})
		h.Ingest(r.Entity, fmt.Sprint(v), t0)
		h.Tick(t0.Add(r.For))
		if !slices.Equal(got, texts) {
			t.Errorf("luật %s: %s=%v trong %v phát %q, muốn %q", r.ID, r.Entity, v, r.For, got, texts)
		}
	}
	for _, k := range []string{"co2", "bui", "mua", "nong"} {
		if kinds[k] == 0 {
			t.Errorf("config.yaml chưa có luật notify loại %q", k)
		}
	}
	t.Logf("%d luật notify, theo loại %v", n, kinds)
}
