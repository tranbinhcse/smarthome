package main

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

// Đọc fixture multipart như luồng thật của đầu ghi: bỏ phần ảnh, XML cụt, kênh không phải số; làm sạch tên sự kiện.
func TestReadAlertsFixture(t *testing.T) {
	f, err := os.Open("testdata/alertstream.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []alert
	err = readAlerts(f, func(a alert) { got = append(got, a) })
	if err == nil {
		t.Error("luồng bị cắt phải trả lỗi để kết nối lại")
	}
	want := []alert{
		{Ch: "1", Type: "videoloss", Active: false},
		{Ch: "2", Type: "vmd", Active: true},
		{Ch: "3", Type: "linedetection", Active: true, Target: "human"},
		{Ch: "4", Type: "vmd", Active: true, Target: "vehicle"},
		{Ch: "5", Type: "fielddetection", Active: true},
		{Ch: "7", Type: "shelteralarm", Active: true},
		{Ch: "6", Type: "vmd", Active: true},
		{Ch: "2", Type: "vmd", Active: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("đọc được\n%+v\nmuốn\n%+v", got, want)
	}
}

type pubs [][2]string

func (p *pubs) pub(topic, payload string) { *p = append(*p, [2]string{topic, payload}) }
func (p *pubs) take() [][2]string         { out := *p; *p = nil; return out }

// ON khi có sự kiện; OFF khi đầu ghi báo inactive hoặc im lặng quá thời gian giữ (đầu ghi thường không gửi inactive cho VMD).
func TestTrackerOnOff(t *testing.T) {
	var p pubs
	tr := newTracker(10*time.Second, p.pub)
	t0 := time.Unix(1000, 0)
	step := func(name string, want ...[2]string) {
		t.Helper()
		if got := p.take(); !reflect.DeepEqual(got, [][2]string(want)) {
			t.Errorf("%s: publish %v, muốn %v", name, got, want)
		}
	}
	tr.alert(alert{Ch: "1", Type: "videoloss"}, t0)
	step("nhịp tim lần đầu: báo OFF để hubd có trạng thái", [2]string{"hik/1/videoloss", "OFF"})
	tr.alert(alert{Ch: "1", Type: "videoloss"}, t0.Add(time.Second))
	step("nhịp tim lặp lại: im")
	tr.alert(alert{Ch: "2", Type: "vmd", Active: true}, t0)
	step("chuyển động", [2]string{"hik/2/vmd", "ON"})
	tr.alert(alert{Ch: "2", Type: "vmd", Active: true}, t0.Add(time.Second))
	step("chuyển động tiếp diễn: im")
	tr.alert(alert{Ch: "3", Type: "linedetection", Active: true, Target: "human"}, t0.Add(3*time.Second))
	step("vượt rào có người", [2]string{"hik/3/linedetection", "ON"}, [2]string{"hik/3/human", "ON"})
	tr.expire(t0.Add(10 * time.Second))
	step("chưa hết thời gian giữ")
	tr.expire(t0.Add(11 * time.Second))
	step("hết thời gian giữ", [2]string{"hik/2/vmd", "OFF"})
	tr.alert(alert{Ch: "3", Type: "linedetection"}, t0.Add(12*time.Second))
	step("inactive", [2]string{"hik/3/linedetection", "OFF"})
	tr.expire(t0.Add(13 * time.Second))
	step("người hết sau thời gian giữ", [2]string{"hik/3/human", "OFF"})
}

func TestChannelStatusOnline(t *testing.T) {
	b, err := os.ReadFile("testdata/channels-status.xml")
	if err != nil {
		t.Fatal(err)
	}
	st, err := parseStatus(b)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]bool{"1": true, "2": false}; !reflect.DeepEqual(st, want) {
		t.Fatalf("trạng thái %v, muốn %v", st, want)
	}
	var p pubs
	tr := newTracker(10*time.Second, p.pub)
	tr.status(st, nil)
	want := [][2]string{{"hik/1/online", "online"}, {"hik/2/online", "offline"}}
	if got := p.take(); !reflect.DeepEqual(got, want) {
		t.Fatalf("publish %v, muốn %v", got, want)
	}
	tr.status(st, nil)
	if got := p.take(); got != nil {
		t.Fatalf("không đổi mà vẫn publish %v", got)
	}
	tr.status(nil, errors.New("mất kết nối đầu ghi"))
	if got := p.take(); !reflect.DeepEqual(got, [][2]string{{"hik/1/online", "offline"}}) {
		t.Fatalf("mất đầu ghi: publish %v, muốn kênh 1 offline", got)
	}
}
