package main

import (
	"encoding/json"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Hub: trạng thái trong RAM + lịch sử + luật. publish được tiêm vào để test không cần MQTT.
type Hub struct {
	cfg     Config
	db      *DB
	engine  *Engine
	publish func(topic, payload string)
	devByID map[string]Device
	routes  map[string][]route // topic MQTT -> các khoá trạng thái nhận từ nó

	mu   sync.RWMutex
	val  map[string]string
	subs map[chan []byte]struct{}

	alerts []Alert // cảnh báo chưa ai xác nhận (assistant.go), khoá bằng mu
}

func NewHub(cfg Config, db *DB, publish func(topic, payload string)) *Hub {
	h := &Hub{cfg: cfg, db: db, engine: NewEngine(cfg.Rules), publish: publish,
		devByID: map[string]Device{}, routes: map[string][]route{},
		val: map[string]string{}, subs: map[chan []byte]struct{}{}}
	add := func(topic string, r route) {
		if topic != "" {
			h.routes[topic] = append(h.routes[topic], r)
		}
	}
	for _, e := range cfg.Entities {
		add(e.Topic, route{id: e.ID, field: e.Field, scale: e.Scale})
	}
	for _, d := range cfg.Devices {
		h.devByID[d.ID] = d
		add(d.StateTopic, route{id: "dev:" + d.ID, field: d.Field})
		add(d.OnlineTopic, route{id: "online:" + d.ID})
	}
	add("tuya/setup/status", route{id: "setup:tuya"}) // tiến độ quét QR Tuya (tuya-bridge), hiện trên trang Cài đặt
	return h
}

type route struct {
	id    string
	field string
	scale float64
}

// Topics: các topic cần đăng ký, mỗi topic một lần dù nhiều mục cùng đọc.
func (h *Hub) Topics() []string {
	out := make([]string, 0, len(h.routes))
	for t := range h.routes {
		out = append(out, t)
	}
	return out
}

// Handle nhận tin MQTT thô và phân cho mọi mục đọc topic đó.
// Mục có field lấy một khoá của payload JSON; payload hỏng hoặc thiếu khoá thì giữ giá trị cũ.
func (h *Hub) Handle(topic, payload string, now time.Time) {
	var obj map[string]any
	parsed := false
	for _, r := range h.routes[topic] {
		if r.field == "" {
			h.Ingest(r.id, payload, now)
			continue
		}
		if !parsed {
			parsed = true
			json.Unmarshal([]byte(payload), &obj) // lỗi -> obj nil -> không khoá nào khớp
		}
		if v, ok := fieldValue(obj[r.field], r.scale); ok {
			h.Ingest(r.id, v, now)
		}
	}
}

func fieldValue(v any, scale float64) (string, bool) {
	switch x := v.(type) {
	case float64:
		if scale != 0 {
			x *= scale
		}
		return strconv.FormatFloat(math.Round(x*1000)/1000, 'f', -1, 64), true
	case bool:
		if x {
			return "1", true
		}
		return "0", true
	case string:
		return x, true
	}
	return "", false
}

var onRe, offRe = regexp.MustCompile(`(?i)^\s*(on|1|true)\s*$`), regexp.MustCompile(`(?i)^\s*(off|0|false)\s*$`)

// Ingest nhận payload từ một topic đã ánh xạ sang khoá id (entity id, "dev:<id>", "online:<id>").
func (h *Hub) Ingest(id, payload string, now time.Time) {
	prev, had := h.set(id, payload)

	var v float64
	var gap time.Duration
	switch {
	case strings.HasPrefix(id, "online:"), strings.HasPrefix(id, "x:"), strings.HasPrefix(id, "setup:"): // x: trạng thái giả định của hồng ngoại; setup: tiến độ cài đặt; không vào lịch sử
		return
	case strings.HasPrefix(id, "dev:"):
		if onRe.MatchString(payload) {
			v = 1
		}
		if had && prev == payload {
			return
		}
	default:
		f, err := strconv.ParseFloat(strings.TrimSpace(payload), 64)
		switch {
		case err == nil && math.IsNaN(f): // ESPHome gửi "nan" khi mất cảm biến: chưa biết, không phải số đo
			return
		case err == nil:
			v, gap = f, 30*time.Second
		case onRe.MatchString(payload): // cảm biến nhị phân (sự kiện camera, binary_sensor ESPHome): ON = 1, OFF = 0
			v = 1
		case offRe.MatchString(payload):
		default:
			return
		}
	}
	if err := h.db.Record(id, v, now, gap); err != nil {
		log.Printf("ghi lịch sử %s: %v", id, err)
	}
	h.run(h.engine.Feed(id, v, now))
}

// set lưu giá trị và đẩy cho giao diện (SSE).
func (h *Hub) set(id, payload string) (prev string, had bool) {
	h.mu.Lock()
	prev, had = h.val[id]
	h.val[id] = payload
	h.mu.Unlock()
	b, _ := json.Marshal(map[string]string{"id": id, "value": payload})
	h.fan(b)
	return prev, had
}

func (h *Hub) Tick(now time.Time) { h.run(h.engine.Tick(now)) }

func (h *Hub) run(acts []Action) {
	for _, a := range acts {
		if a.Alert != "" { // luật có alert: giữ trên chuông dashboard tới khi điều kiện hết
			log.Printf("cảnh báo %s: %q", a.Alert, a.Text)
			h.set("alert:"+a.Alert, a.Text)
			if a.Text != "" { // bật cảnh báo: cũng lên hubd/alert để trợ lý giọng nói đọc; nhả thì thôi
				h.alert("warn", a.Text)
			}
			continue
		}
		if a.Notify != "" {
			h.alert(a.Level, a.Notify)
		}
		if a.Set == "" {
			continue
		}
		d, ok := h.devByID[a.Set]
		switch {
		case !ok:
			log.Printf("luật: thiết bị %q không tồn tại", a.Set)
		case d.Protected && a.To == "off":
			log.Printf("luật: chặn tắt thiết bị bảo vệ %s", d.ID)
		default:
			log.Printf("luật: %s -> %s", d.ID, a.To)
			h.send(d, a.To)
		}
	}
}

// Command dùng cho người bấm nút (đã qua đăng nhập): on/off, hoặc mã nút với thiết bị hồng ngoại.
// Trả false nếu thiết bị không tồn tại hoặc lệnh không hợp lệ.
func (h *Hub) Command(dev, to string) bool {
	d, ok := h.devByID[dev]
	return ok && d.CommandTopic != "" && h.send(d, to)
}

func (h *Hub) Snapshot() map[string]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]string, len(h.val))
	for k, v := range h.val {
		out[k] = v
	}
	return out
}

func (h *Hub) Sub() chan []byte {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}
func (h *Hub) Unsub(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}
func (h *Hub) fan(b []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- b:
		default:
		}
	}
}
