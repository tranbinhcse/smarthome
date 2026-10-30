package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
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
	want := fmt.Sprint([]any{"thiet_bi", []string{"Thiết bị mất kết nối quá 10 phút"}, "co2", []string{"CO2 khu cafe cao", "CO2 phòng làm việc cao"}, "mua", []string{"Trời bắt đầu mưa", "Trời tạnh"}})
	var flat []any
	for _, k := range got {
		flat = append(flat, k.Kind, k.Texts)
	}
	if fmt.Sprint(flat) != want {
		t.Fatalf("được %v, muốn %v", flat, want)
	}
	if b := call(newMux(NewHub(Config{}, newDB(t), func(string, string) {}), NewAuth("", nil)), "GET", "/api/notify", "", "").Body.String(); strings.Count(b, `"kind"`) != 1 || !strings.Contains(b, `"thiet_bi"`) {
		t.Fatalf("không có luật notify chỉ còn loại có sẵn thiet_bi, được %s", b)
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

// tgHub: hub có Telegram trỏ vào máy chủ giả (2 chat đã duyệt); sent là các tin MQTT, tgGot đếm tin Telegram đã tới.
func tgHub(t *testing.T, cfg Config) (h *Hub, sent *[]string, tgGot func() int) {
	t.Helper()
	tg, fk, clock := newTgEnv(t, nil)
	var s []string
	h = NewHub(cfg, tg.db, func(tp, p string) { s = append(s, tp+" "+p) })
	h.tg = tg
	return h, &s, func() int { drain(tg, clock); return len(fk.got) }
}

// Cảnh báo của luật tới Telegram theo loại, độc lập với đọc to: tắt đọc (notify[kind]=false) không tắt Telegram,
// tắt Telegram (notify["tg:"+kind]=false) không tắt đọc. Không kind dùng "tg:khac". Luật alert: chỉ báo khi kích hoạt.
func TestRuleNotifyReachesTelegramByKind(t *testing.T) {
	co2 := func(kind string) Config {
		return Config{Rules: []Rule{{ID: "c", Entity: "co2", Above: f(1000), Then: []Action{{Notify: "CO2 cao", Level: "warn", Kind: kind}}}}}
	}
	for _, c := range []struct {
		name     string
		cfg      Config
		set      map[string]any
		wantTG   bool
		wantRead bool
	}{
		{"kind co2", co2("co2"), nil, true, true},
		{"tắt đọc vẫn có Telegram", co2("co2"), map[string]any{"co2": false}, true, false},
		{"tắt Telegram vẫn đọc", co2("co2"), map[string]any{"tg:co2": false}, false, true},
		{"không kind", co2(""), nil, true, true},
		{"không kind, tg:khac tắt", co2(""), map[string]any{"tg:khac": false}, false, true},
		{"tg:khac tắt không chặn kind khác", co2("co2"), map[string]any{"tg:khac": false}, true, true},
	} {
		h, sent, got := tgHub(t, c.cfg)
		if c.set != nil {
			h.db.PutSetting("notify", c.set)
		}
		h.Ingest("co2", "1500", t0)
		if read := strings.Contains(strings.Join(*sent, "\n"), "hubd/alert "); read != c.wantRead {
			t.Errorf("%s: đọc=%v, muốn %v", c.name, read, c.wantRead)
		}
		if tg := got() > 0; tg != c.wantTG {
			t.Errorf("%s: Telegram=%v, muốn %v", c.name, tg, c.wantTG)
		}
	}

	h, _, got := tgHub(t, Config{Rules: []Rule{{ID: "pin", Entity: "soc", Below: f(20), ClearAbove: f(25), Alert: "Pin yếu"}}})
	h.Ingest("soc", "10", t0)
	if n := got(); n == 0 {
		t.Fatal("alert: kích hoạt phải có tin Telegram")
	}
	n := got()
	h.Ingest("soc", "30", t0.Add(time.Minute))
	if got() != n {
		t.Fatal("alert: nhả không được gửi Telegram")
	}
}
