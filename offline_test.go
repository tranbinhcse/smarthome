package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// offHub: hub một thiết bị có online_topic; got là các chữ đã đọc to (hubd/alert).
func offHub(t *testing.T) (*Hub, *[]string) {
	var got []string
	h := NewHub(Config{Devices: []Device{{ID: "den", Name: "Đèn bếp", OnlineTopic: "den/online"}}}, newDB(t), func(tp, p string) {
		if tp == "hubd/alert" {
			got = append(got, p)
		}
	})
	return h, &got
}

func TestOfflineNotifiesAfter10Minutes(t *testing.T) {
	h, got := offHub(t)
	w := newOfflineWatch(h)
	step := func(at time.Duration, state string) {
		h.Ingest("online:den", state, t0.Add(at))
		w.check(t0.Add(at))
	}
	step(0, "offline")
	step(9*time.Minute, "offline")
	if len(*got) != 0 {
		t.Fatalf("9 phút chưa được báo: %v", *got)
	}
	step(10*time.Minute+time.Second, "offline")
	if len(*got) != 1 || !strings.Contains((*got)[0], "Đèn bếp mất kết nối hơn 10 phút.") || !strings.Contains((*got)[0], `"warn"`) {
		t.Fatalf("cần 1 tin mất kết nối: %v", *got)
	}
	step(70*time.Minute, "offline")
	if len(*got) != 1 {
		t.Fatalf("không báo lặp: %v", *got)
	}
	step(71*time.Minute, "online")
	if len(*got) != 2 || !strings.Contains((*got)[1], "Đèn bếp đã kết nối lại.") || !strings.Contains((*got)[1], `"info"`) {
		t.Fatalf("cần tin kết nối lại: %v", *got)
	}
	step(80*time.Minute, "offline")
	step(91*time.Minute, "offline")
	if len(*got) != 3 {
		t.Fatalf("mất lần nữa phải báo lại: %v", *got)
	}
}

func TestOfflineShortBlipsIgnored(t *testing.T) {
	h, got := offHub(t)
	w := newOfflineWatch(h)
	h.Ingest("online:den", "offline", t0)
	w.check(t0)
	h.Ingest("online:den", "online", t0.Add(5*time.Minute))
	w.check(t0.Add(5 * time.Minute))
	w.check(t0.Add(30 * time.Minute))
	if len(*got) != 0 {
		t.Fatalf("mất ngắn không được báo: %v", *got)
	}
}

func TestNotifyKindsIncludeThietBi(t *testing.T) {
	h, sent, tgGot := tgHub(t, Config{})
	mux := http.NewServeMux()
	mountNotify(mux, h, NewAuth("", h.db))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/notify", nil))
	if !strings.Contains(rec.Body.String(), `"kind":"thiet_bi"`) {
		t.Fatalf("thiếu thiet_bi: %s", rec.Body.String())
	}
	h.db.PutSetting("notify", map[string]any{"tg:thiet_bi": false})
	h.notify("warn", "thiet_bi", "Đèn mất kết nối hơn 10 phút.")
	if tgGot() != 0 {
		t.Fatal("tắt tg:thiet_bi mà vẫn gửi Telegram")
	}
	if !strings.Contains(strings.Join(*sent, "\n"), "hubd/alert ") {
		t.Fatal("tắt Telegram vẫn phải đọc thành tiếng")
	}
}

func TestDirectCamOnlineProbe(t *testing.T) {
	h, _ := offHub(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	cams := []camRec{{ID: "c1", Host: "127.0.0.1", RTSPPort: port}}
	probeCams(h, cams, t0)
	if h.Snapshot()["online:c1"] != "online" {
		t.Fatalf("cổng mở phải online: %v", h.Snapshot())
	}
	ln.Close()
	probeCams(h, cams, t0.Add(time.Minute))
	if h.Snapshot()["online:c1"] != "offline" {
		t.Fatalf("cổng đóng phải offline: %v", h.Snapshot())
	}
}
