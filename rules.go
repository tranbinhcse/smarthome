package main

import (
	"sync"
	"time"
)

// Rule: điều kiện ngưỡng -> hành động, có chống dao động (hysteresis + thời gian giữ "for").
// Kích hoạt khi value > Above (hoặc < Below) liên tục For. Nhả khi value <= ClearBelow
// (mặc định = Above) hoặc value >= ClearAbove (mặc định = Below).
type Rule struct {
	ID         string        `yaml:"id" json:"id"`
	Entity     string        `yaml:"entity" json:"entity"`
	Above      *float64      `yaml:"above" json:"above,omitempty"`
	Below      *float64      `yaml:"below" json:"below,omitempty"`
	ClearBelow *float64      `yaml:"clear_below" json:"clear_below,omitempty"`
	ClearAbove *float64      `yaml:"clear_above" json:"clear_above,omitempty"`
	For        time.Duration `yaml:"for" json:"for,omitempty"` // JSON: nano giây
	Then       []Action      `yaml:"then" json:"then,omitempty"`
	OnClear    []Action      `yaml:"on_clear" json:"on_clear,omitempty"`
	Alert      string        `yaml:"alert" json:"alert,omitempty"` // lời cảnh báo: khi kích hoạt, state "alert:<id>" = chuỗi này; khi nhả = ""
}

type Action struct {
	Set string `yaml:"set" json:"set,omitempty"`
	To  string `yaml:"to" json:"to,omitempty"` // on | off

	Notify string `yaml:"notify" json:"notify,omitempty"` // câu cảnh báo, publish lên hubd/alert
	Level  string `yaml:"level" json:"level,omitempty"`   // urgent | warn | info (rỗng = info)
	Kind   string `yaml:"kind" json:"kind,omitempty"`     // loại thông báo (co2, bui, mua, nong...): tắt được từng loại trong Cài đặt (notify.go)

	Alert string `yaml:"-" json:"-"` // engine tự tạo cho luật có alert: id luật; Text rỗng = nhả cảnh báo
	Text  string `yaml:"-" json:"-"`
}

type ruleState struct {
	active  bool
	pending bool
	since   time.Time
}

type Engine struct {
	mu    sync.Mutex
	rules []Rule
	st    map[string]*ruleState
	last  map[string]float64
}

func NewEngine(rules []Rule) *Engine {
	return &Engine{rules: rules, st: map[string]*ruleState{}, last: map[string]float64{}}
}

// Feed ghi nhận giá trị mới của entity rồi đánh giá các luật liên quan.
func (e *Engine) Feed(entity string, v float64, now time.Time) []Action {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.last[entity] = v
	return e.eval(entity, now)
}

// Tick đánh giá lại bằng giá trị cuối (để điều kiện "for" chín dù cảm biến không gửi thêm).
func (e *Engine) Tick(now time.Time) []Action {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.eval("", now)
}

func (e *Engine) eval(only string, now time.Time) []Action {
	var out []Action
	for _, r := range e.rules {
		if only != "" && r.Entity != only {
			continue
		}
		v, ok := e.last[r.Entity]
		if !ok {
			continue
		}
		s := e.st[r.ID]
		if s == nil {
			s = &ruleState{}
			e.st[r.ID] = s
		}
		if s.active {
			if r.cleared(v) {
				s.active, s.pending = false, false
				out = append(out, r.OnClear...)
				if r.Alert != "" {
					out = append(out, Action{Alert: r.ID})
				}
			}
			continue
		}
		if !r.triggered(v) {
			s.pending = false
			continue
		}
		if !s.pending {
			s.pending, s.since = true, now
		}
		if now.Sub(s.since) >= r.For {
			s.active = true
			out = append(out, r.Then...)
			if r.Alert != "" {
				out = append(out, Action{Alert: r.ID, Text: r.Alert})
			}
		}
	}
	return out
}

func (r Rule) triggered(v float64) bool {
	return (r.Above != nil && v > *r.Above) || (r.Below != nil && v < *r.Below)
}

func (r Rule) cleared(v float64) bool {
	if r.Above != nil {
		lim := *r.Above
		if r.ClearBelow != nil {
			lim = *r.ClearBelow
		}
		return v <= lim
	}
	lim := *r.Below
	if r.ClearAbove != nil {
		lim = *r.ClearAbove
	}
	return v >= lim
}
