package main

import (
	"testing"
	"time"
)

func f(x float64) *float64 { return &x }

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func co2Rule() Rule {
	return Rule{ID: "co2", Entity: "pk.co2", Above: f(1000), ClearBelow: f(900), For: 2 * time.Minute,
		Then: []Action{{Set: "quat", To: "on"}}, OnClear: []Action{{Set: "quat", To: "off"}}}
}

func TestRuleTable(t *testing.T) {
	type step struct {
		at   time.Duration
		v    float64
		want string // "", "on", "off"
	}
	cases := []struct {
		name  string
		rule  Rule
		steps []step
	}{
		{"không đủ thời gian giữ", co2Rule(), []step{{0, 1100, ""}, {60 * time.Second, 1100, ""}}},
		{"đủ for thì bật", co2Rule(), []step{{0, 1100, ""}, {119 * time.Second, 1100, ""}, {120 * time.Second, 1100, "on"}}},
		{"rơi dưới ngưỡng giữa chừng thì đếm lại", co2Rule(), []step{{0, 1100, ""}, {60 * time.Second, 950, ""}, {70 * time.Second, 1100, ""}, {189 * time.Second, 1100, ""}, {190 * time.Second, 1100, "on"}}},
		{"chống dao động: 950 chưa nhả, <=900 mới tắt", co2Rule(), []step{{0, 1100, ""}, {120 * time.Second, 1100, "on"}, {130 * time.Second, 950, ""}, {140 * time.Second, 1050, ""}, {150 * time.Second, 900, "off"}}},
		{"bật chỉ một lần", co2Rule(), []step{{0, 1100, ""}, {120 * time.Second, 1100, "on"}, {180 * time.Second, 1200, ""}}},
		{"dưới ngưỡng (below) với clear_above", Rule{ID: "soc", Entity: "e", Below: f(30), ClearAbove: f(40), Then: []Action{{Set: "x", To: "off"}}, OnClear: []Action{{Set: "x", To: "on"}}},
			[]step{{0, 29, "off"}, {time.Second, 35, ""}, {2 * time.Second, 40, "on"}}},
		{"for=0 kích hoạt ngay", Rule{ID: "r", Entity: "e", Above: f(5), Then: []Action{{Set: "x", To: "on"}}}, []step{{0, 6, "on"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := NewEngine([]Rule{c.rule})
			for i, s := range c.steps {
				got := ""
				if a := e.Feed(c.rule.Entity, s.v, t0.Add(s.at)); len(a) > 0 {
					got = a[0].To
				}
				if got != s.want {
					t.Fatalf("bước %d (v=%v): được %q, cần %q", i, s.v, got, s.want)
				}
			}
		})
	}
}

func TestTickMaturesWithoutNewSample(t *testing.T) {
	e := NewEngine([]Rule{co2Rule()})
	e.Feed("pk.co2", 1100, t0)
	if a := e.Tick(t0.Add(2 * time.Minute)); len(a) != 1 || a[0].To != "on" {
		t.Fatalf("Tick phải kích hoạt, được %v", a)
	}
}

func TestOtherEntityIgnored(t *testing.T) {
	e := NewEngine([]Rule{co2Rule()})
	if a := e.Feed("khac", 5000, t0.Add(time.Hour)); len(a) != 0 {
		t.Fatal(a)
	}
}
