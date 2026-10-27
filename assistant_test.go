package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testTok = "tro-ly-token-thu-0123456789abcdef"

func assistantMux(t *testing.T, cfg Config) (*http.ServeMux, *Hub, *[]string) {
	t.Helper()
	var sent []string
	h := NewHub(cfg, newDB(t), func(tp, p string) { sent = append(sent, tp+" "+p) })
	a := NewAuth("1234", h.db)
	a.assistant = testTok
	return newMux(h, a), h, &sent
}

func call(mux http.Handler, method, url, body, bearer string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// Trợ lý không được lệnh thiết bị cần PIN. Quét đủ không gian cờ: pin × protected × trạng thái = 2×2×2 = 8/8 trường hợp.
func TestAssistantDeviceGuardSweep(t *testing.T) {
	n := 0
	for _, pin := range []bool{false, true} {
		for _, prot := range []bool{false, true} {
			for _, to := range cmdStates {
				n++
				mux, _, sent := assistantMux(t, Config{Devices: []Device{{ID: "d", CommandTopic: "d/cmd", Pin: pin, Protected: prot}}})
				w := call(mux, "POST", "/api/assistant/cmd", `{"device":"d","to":"`+to+`"}`, testTok)
				want, wantSent := 204, 1
				if pin || prot {
					want, wantSent = 403, 0
				}
				if w.Code != want || len(*sent) != wantSent {
					t.Errorf("pin=%v protected=%v to=%s: code=%d sent=%v, muốn %d và %d lệnh", pin, prot, to, w.Code, *sent, want, wantSent)
				}
			}
		}
	}
	if n != 8 {
		t.Fatalf("quét %d trường hợp, muốn 8", n)
	}
}

// Kịch bản cần PIN (tự đặt pin, hoặc có bước đụng thiết bị pin/protected) bị chặn trọn: không bước nào chạy.
// Quét scene.pin × cờ thiết bị trong bước = 2×4 = 8/8 trường hợp.
func TestAssistantSceneGuardSweep(t *testing.T) {
	n := 0
	for _, scenePin := range []bool{false, true} {
		for _, flags := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
			n++
			cfg := Config{
				Devices: []Device{{ID: "thuong", CommandTopic: "thuong/cmd"}, {ID: "d", CommandTopic: "d/cmd", Pin: flags[0], Protected: flags[1]}},
				Scenes:  []Scene{{ID: "s", Name: "Đi ngủ", Pin: scenePin, Steps: []Step{{D: "thuong", To: "off"}, {D: "d", To: "on"}}}},
			}
			mux, _, sent := assistantMux(t, cfg)
			w := call(mux, "POST", "/api/assistant/scene/s", "", testTok)
			locked := scenePin || flags[0] || flags[1]
			if locked && (w.Code != 403 || len(*sent) != 0) {
				t.Errorf("scene.pin=%v dev=%v: code=%d sent=%v, muốn 403 và không lệnh nào", scenePin, flags, w.Code, *sent)
			}
			if !locked && (w.Code != 204 || !slices.Equal(*sent, []string{"thuong/cmd OFF", "d/cmd ON"})) {
				t.Errorf("scene thường: code=%d sent=%v", w.Code, *sent)
			}
		}
	}
	if n != 8 {
		t.Fatalf("quét %d trường hợp, muốn 8", n)
	}
}

// Token trợ lý chỉ mở các route /api/assistant/*; route của người dùng vẫn đòi PIN (mặc định từ chối).
func TestAssistantTokenScope(t *testing.T) {
	mux, _, sent := assistantMux(t, Config{Devices: []Device{{ID: "cb", CommandTopic: "cb/cmd", Protected: true}}})
	for _, c := range []struct{ method, url, body, bearer string }{
		{"POST", "/api/cmd", `{"device":"cb","to":"off"}`, testTok}, // route người dùng: token trợ lý không có tác dụng
		{"GET", "/api/state", "", testTok},
		{"GET", "/api/assistant/state", "", ""},          // thiếu token
		{"GET", "/api/assistant/state", "", "sai-token"}, // sai token
		{"POST", "/api/assistant/alarm/ack", "", "sai-token"},
	} {
		if w := call(mux, c.method, c.url, c.body, c.bearer); w.Code != 401 {
			t.Errorf("%s %s bearer=%q: code=%d, muốn 401", c.method, c.url, c.bearer, w.Code)
		}
	}
	if len(*sent) != 0 {
		t.Fatalf("không được gửi lệnh: %v", *sent)
	}
	// Đã đăng nhập PIN cũng không dùng được route trợ lý (route đó chỉ dành cho token).
	lw := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "")
	if w := call(mux, "GET", "/api/assistant/state", "", "", lw.Result().Cookies()...); w.Code != 401 {
		t.Errorf("cookie người dùng trên route trợ lý: code=%d, muốn 401", w.Code)
	}
}

// HUBD_ASSISTANT_TOKEN trống = tắt trợ lý; "Bearer " rỗng không được khớp chuỗi rỗng.
func TestAssistantDisabledWhenNoToken(t *testing.T) {
	h := NewHub(Config{}, newDB(t), func(string, string) {})
	mux := newMux(h, NewAuth("1234", h.db))
	r := httptest.NewRequest("GET", "/api/assistant/state", nil)
	r.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("code=%d, muốn 401", w.Code)
	}
}

// Luật có notify: publish hubd/alert (hợp đồng {"level","text"}), giữ trong danh sách đang mở tới khi xác nhận.
func TestRuleNotifyAlertAndAck(t *testing.T) {
	cfg := Config{Rules: []Rule{
		{ID: "khoi", Entity: "bep.khoi", Above: f(0), Then: []Action{{Notify: "Có khói ở bếp", Level: "urgent"}}},
		{ID: "mua", Entity: "mua", Above: f(0), Then: []Action{{Notify: "Trời mưa, thu đồ"}}},
	}}
	mux, h, sent := assistantMux(t, cfg)
	h.Ingest("bep.khoi", "1", t0)
	h.Ingest("mua", "1", t0)
	want := []string{`hubd/alert {"level":"urgent","text":"Có khói ở bếp"}`, `hubd/alert {"level":"info","text":"Trời mưa, thu đồ"}`}
	if !slices.Equal(*sent, want) {
		t.Fatalf("sent=%v\nmuốn %v", *sent, want)
	}
	var st struct {
		Values map[string]string
		Alerts []Alert
	}
	json.Unmarshal(call(mux, "GET", "/api/assistant/state", "", testTok).Body.Bytes(), &st)
	if len(st.Alerts) != 2 || st.Alerts[0].Text != "Có khói ở bếp" || st.Values["bep.khoi"] != "1" {
		t.Fatalf("state=%+v", st)
	}
	if w := call(mux, "POST", "/api/assistant/alarm/ack", "", testTok); w.Code != 204 {
		t.Fatalf("ack code=%d", w.Code)
	}
	if (*sent)[len(*sent)-1] != "hubd/alert/ack {}" {
		t.Errorf("thiếu hubd/alert/ack: %v", *sent)
	}
	json.Unmarshal(call(mux, "GET", "/api/assistant/state", "", testTok).Body.Bytes(), &st)
	if len(st.Alerts) != 0 {
		t.Errorf("sau xác nhận vẫn còn cảnh báo: %+v", st.Alerts)
	}
}

// Cấu hình cho trợ lý: thiết bị, kịch bản, danh sách id cần PIN và mọi câu cảnh báo cố định (để tạo sẵn âm thanh).
func TestAssistantConfig(t *testing.T) {
	cfg := Config{
		Devices: []Device{{ID: "den", Name: "Đèn kho"}, {ID: "cb", Name: "CB tổng", Protected: true}, {ID: "bom", Name: "Bơm", Pin: true}},
		Scenes:  []Scene{{ID: "ngu", Name: "Đi ngủ", Steps: []Step{{D: "den", To: "off"}}}, {ID: "di", Name: "Ra khỏi nhà", Steps: []Step{{D: "cb", To: "off"}}}},
		Rules: []Rule{
			{ID: "a", Entity: "x", Then: []Action{{Notify: "Có khói"}}, OnClear: []Action{{Notify: "Hết khói"}}},
			{ID: "b", Entity: "y", Then: []Action{{Notify: "Có khói"}, {Set: "den", To: "on"}}},
		},
	}
	mux, _, _ := assistantMux(t, cfg)
	var got struct {
		Devices []Device
		Scenes  []Scene
		Locked  []string
		Phrases []string
	}
	w := call(mux, "GET", "/api/assistant/config", "", testTok)
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 {
		t.Fatalf("code=%d err=%v", w.Code, err)
	}
	slices.Sort(got.Locked)
	if !slices.Equal(got.Locked, []string{"bom", "cb", "di"}) || !slices.Equal(got.Phrases, []string{"Có khói", "Hết khói"}) ||
		len(got.Devices) != 3 || len(got.Scenes) != 2 {
		t.Fatalf("config=%+v", got)
	}
}

// Cấu hình rỗng: route JSON GET của trợ lý không được trả null (voiced gọi .get/len trên đó).
func TestAssistantJSONNoNull(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte("mqtt:\n  broker: tcp://x:1883\n"), 0o600)
	cfg, _, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	mux, _, _ := assistantMux(t, cfg)
	for _, u := range []string{"/api/assistant/config", "/api/assistant/state", "/api/assistant/history?id=chua_co"} {
		w := call(mux, "GET", u, "", testTok)
		var v any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || hasNull(v) {
			t.Errorf("%s: code=%d body=%s", u, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}
}

// YAML: pin của thiết bị, kịch bản kiểu dashboard ({d, to}), notify/level trong luật.
func TestConfigYAMLScenesPinNotify(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte(`
devices:
  - { id: bom, name: Bơm, pin: true }
scenes:
  - { id: ngu, name: Đi ngủ, icon: moon, steps: [ { d: bom, to: off } ] }
rules:
  - { id: k, entity: x, above: 1, then: [ { notify: Có khói, level: urgent } ] }
`), 0o600)
	cfg, _, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Devices[0].Pin || cfg.Scenes[0].Steps[0] != (Step{D: "bom", To: "off"}) || cfg.Scenes[0].Icon != "moon" ||
		cfg.Rules[0].Then[0] != (Action{Notify: "Có khói", Level: "urgent"}) {
		t.Fatalf("cfg=%+v", cfg)
	}
	// Dashboard đọc cfg.scenes từ /api/config.
	h := NewHub(cfg, newDB(t), func(string, string) {})
	w := call(newMux(h, NewAuth("", h.db)), "GET", "/api/config", "", "")
	if !strings.Contains(w.Body.String(), `"scenes":[{"id":"ngu","name":"Đi ngủ","icon":"moon"`) {
		t.Errorf("/api/config thiếu scenes: %s", w.Body.String())
	}
}

// /voice/* chuyển tới voiced khi đã đăng nhập; cookie của người dùng không được đi tiếp sang voiced.
func TestVoiceProxy(t *testing.T) {
	var gotPath, gotCookie string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotCookie = r.URL.Path, r.Header.Get("Cookie")
		io.WriteString(w, "ok")
	}))
	defer up.Close()
	t.Setenv("HUBD_VOICED_URL", up.URL)
	mux, _, _ := assistantMux(t, Config{})
	if w := call(mux, "POST", "/voice/ask", "x", ""); w.Code != 401 {
		t.Fatalf("chưa đăng nhập: code=%d, muốn 401", w.Code)
	}
	lw := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "")
	w := call(mux, "POST", "/voice/ask", "x", "", lw.Result().Cookies()...)
	if w.Code != 200 || w.Body.String() != "ok" || gotPath != "/voice/ask" || gotCookie != "" {
		t.Fatalf("code=%d body=%q path=%q cookie=%q", w.Code, w.Body.String(), gotPath, gotCookie)
	}
}

// Trợ lý tắt (chưa có token) thì dashboard không hiện nút nói: hubd không chuyển tiếp /voice/* sang voiced
// (dịch vụ đó có thể không chạy, mỗi lần gọi để lại một dòng lỗi) và /voice/enabled trả 404; bật thì 204.
// /voice/enabled không cần PIN: chỉ cho biết bật hay tắt.
func TestVoiceEnabledOnlyWithAssistantToken(t *testing.T) {
	t.Setenv("HUBD_VOICED_URL", "http://voiced:8090")
	h := NewHub(Config{}, newDB(t), func(string, string) {})
	off := newMux(h, NewAuth("1234", h.db))
	if w := call(off, "GET", "/voice/enabled", "", ""); w.Code != 404 {
		t.Fatalf("trợ lý tắt: /voice/enabled code=%d, muốn 404", w.Code)
	}
	lw := call(off, "POST", "/api/login", `{"pin":"1234"}`, "")
	if w := call(off, "GET", "/voice/events", "", "", lw.Result().Cookies()...); w.Code != 404 {
		t.Fatalf("trợ lý tắt mà vẫn chuyển tiếp /voice/events: code=%d", w.Code)
	}
	on, _, _ := assistantMux(t, Config{})
	if w := call(on, "GET", "/voice/enabled", "", ""); w.Code != 204 {
		t.Fatalf("trợ lý bật: /voice/enabled code=%d, muốn 204", w.Code)
	}
}
