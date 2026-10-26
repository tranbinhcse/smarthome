package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testHub(t *testing.T) (*Hub, *[][2]string) {
	var sent [][2]string
	cfg := Config{
		Devices: []Device{
			{ID: "quat", CommandTopic: "q/cmd"},
			{ID: "tulanh", CommandTopic: "t/cmd", Protected: true},
		},
		Rules: []Rule{
			{ID: "r1", Entity: "co2", Above: f(1000), ClearBelow: f(900), Then: []Action{{Set: "quat", To: "on"}}, OnClear: []Action{{Set: "quat", To: "off"}}},
			{ID: "r2", Entity: "x", Above: f(1), Then: []Action{{Set: "tulanh", To: "off"}, {Set: "khong_co", To: "on"}}},
		},
	}
	h := NewHub(cfg, newDB(t), func(tp, p string) { sent = append(sent, [2]string{tp, p}) })
	return h, &sent
}

func TestHubRuleFiresAndRecords(t *testing.T) {
	h, sent := testHub(t)
	h.Ingest("co2", "1200", t0)
	if len(*sent) != 1 || (*sent)[0] != [2]string{"q/cmd", "ON"} {
		t.Fatalf("sent=%v", *sent)
	}
	h.Ingest("co2", "850", t0.Add(time.Minute))
	if len(*sent) != 2 || (*sent)[1][1] != "OFF" {
		t.Fatalf("sent=%v", *sent)
	}
	if p, _ := h.db.Query("co2", t0, t0.Add(time.Hour), 100); len(p) != 2 {
		t.Fatalf("lịch sử: %v", p)
	}
}

func TestHubProtectedAndUnknownNeverCommanded(t *testing.T) {
	h, sent := testHub(t)
	h.Ingest("x", "5", t0)
	if len(*sent) != 0 {
		t.Fatalf("luật không được tắt thiết bị bảo vệ: %v", *sent)
	}
}

func TestHubDeviceStateHistoryOnChangeOnly(t *testing.T) {
	h, _ := testHub(t)
	h.Ingest("dev:quat", "ON", t0)
	h.Ingest("dev:quat", "ON", t0.Add(time.Second))
	h.Ingest("dev:quat", "OFF", t0.Add(2*time.Second))
	p, _ := h.db.Query("dev:quat", t0, t0.Add(time.Hour), 3600)
	if len(p) != 2 || p[0].V != 1 || p[1].V != 0 {
		t.Fatalf("%v", p)
	}
}

func TestHubCommand(t *testing.T) {
	h, sent := testHub(t)
	if !h.Command("quat", "on") || (*sent)[0] != [2]string{"q/cmd", "ON"} {
		t.Fatal(*sent)
	}
	if h.Command("la", "on") {
		t.Fatal("thiết bị lạ phải bị từ chối")
	}
}

func TestAPIRequiresLoginAndRejectsBadCmd(t *testing.T) {
	h, sent := testHub(t)
	a := NewAuth("1234", h.db)
	mux := newMux(h, a)
	req := func(method, url, body string, c *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, u := range []string{"/api/config", "/api/state", "/api/history?id=a", "/events"} {
		if req("GET", u, "", nil).Code != 401 {
			t.Fatalf("%s phải đòi đăng nhập", u)
		}
	}
	if req("POST", "/api/cmd", `{"device":"quat","to":"on"}`, nil).Code != 401 || len(*sent) != 0 {
		t.Fatal("lệnh khi chưa đăng nhập phải bị chặn")
	}
	ck := req("POST", "/api/login", `{"pin":"1234"}`, nil).Result().Cookies()[0]
	if req("POST", "/api/cmd", `{"device":"quat","to":"on"}`, ck).Code != 204 || len(*sent) != 1 {
		t.Fatal("lệnh hợp lệ phải chạy")
	}
	if req("POST", "/api/cmd", `{"device":"quat","to":"nổ"}`, ck).Code != 400 {
		t.Fatal("to không hợp lệ phải 400")
	}
	if req("GET", "/", "", nil).Code != 200 {
		t.Fatal("trang tĩnh phải mở được để hiện ô nhập PIN")
	}
}
