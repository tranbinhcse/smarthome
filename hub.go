package main

import (
	"encoding/json"
	"log"
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

	mu   sync.RWMutex
	val  map[string]string
	subs map[chan []byte]struct{}
}

func NewHub(cfg Config, db *DB, publish func(topic, payload string)) *Hub {
	h := &Hub{cfg: cfg, db: db, engine: NewEngine(cfg.Rules), publish: publish,
		devByID: map[string]Device{}, val: map[string]string{}, subs: map[chan []byte]struct{}{}}
	for _, d := range cfg.Devices {
		h.devByID[d.ID] = d
	}
	return h
}

var onRe = regexp.MustCompile(`(?i)^\s*(on|1|true)\s*$`)

// Ingest nhận payload từ một topic đã ánh xạ sang khoá id (entity id, "dev:<id>", "online:<id>").
func (h *Hub) Ingest(id, payload string, now time.Time) {
	h.mu.Lock()
	prev, had := h.val[id]
	h.val[id] = payload
	h.mu.Unlock()
	b, _ := json.Marshal(map[string]string{"id": id, "value": payload})
	h.fan(b)

	var v float64
	var gap time.Duration
	switch {
	case strings.HasPrefix(id, "online:"):
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
		if err != nil {
			return
		}
		v, gap = f, 30*time.Second
	}
	if err := h.db.Record(id, v, now, gap); err != nil {
		log.Printf("ghi lịch sử %s: %v", id, err)
	}
	h.run(h.engine.Feed(id, v, now))
}

func (h *Hub) Tick(now time.Time) { h.run(h.engine.Tick(now)) }

func (h *Hub) run(acts []Action) {
	for _, a := range acts {
		d, ok := h.devByID[a.Set]
		switch {
		case !ok:
			log.Printf("luật: thiết bị %q không tồn tại", a.Set)
		case d.Protected && a.To == "off":
			log.Printf("luật: chặn tắt thiết bị bảo vệ %s", d.ID)
		default:
			log.Printf("luật: %s -> %s", d.ID, a.To)
			h.publish(d.CommandTopic, d.payload(a.To))
		}
	}
}

// Command dùng cho người bấm nút (đã qua đăng nhập). Trả false nếu thiết bị không tồn tại.
func (h *Hub) Command(dev, to string) bool {
	d, ok := h.devByID[dev]
	if !ok || d.CommandTopic == "" {
		return false
	}
	h.publish(d.CommandTopic, d.payload(to))
	return true
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
