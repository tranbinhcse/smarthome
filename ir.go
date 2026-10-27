package main

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Hồng ngoại: một chiều, không đọc được trạng thái thật. Hub giữ "trạng thái giả định" theo lệnh gần nhất
// (dev:<id>, x:<id>:<khoá>) và đẩy qua SSE để mọi màn hình thấy giống nhau, kể cả sau khi nạp lại trang.
var irKinds = map[string]bool{"tv": true, "speaker": true, "fan": true, "ac": true}

// Mã nút của dashboard (act() trong web/dash.js): power, temp_24, mode_cool, vol_up, ok... Chỉ nhận chữ thường,
// số, gạch dưới để không đẩy payload tuỳ ý ra node.
var irCodeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)

// Khoá đặt giá trị (speed_2 -> x:id:speed = 2) và nút bật/tắt tính năng, khớp act() của dashboard.
var irValueKeys = map[string]bool{"speed": true, "mode": true, "fan": true, "temp": true, "timer": true, "src": true}
var irToggles = map[string]bool{"play": true, "swing": true, "sleep": true, "turbo": true}

// send: thiết bị thường nhận on/off; thiết bị hồng ngoại nhận mã nút. Dùng chung cho người bấm và luật.
func (h *Hub) send(d Device, to string) bool {
	if irKinds[d.Kind] {
		return h.ir(d, to)
	}
	if to != "on" && to != "off" {
		return false
	}
	h.publish(d.CommandTopic, d.payload(to))
	return true
}

// ir: máy lạnh không có mã học được -> chuyển nguyên mã cho climate_ir trên node (firmware/ir.yaml);
// còn lại -> gửi chuỗi Pronto đã học trong codes. Nút nguồn đổi thành on/off theo trạng thái giả định.
var irMu sync.Mutex // đọc trạng thái giả định rồi ghi lại: người bấm và luật (goroutine khác) không được xen giữa

func (h *Hub) ir(d Device, code string) bool {
	irMu.Lock()
	defer irMu.Unlock()
	h.mu.RLock()
	on := onRe.MatchString(h.val["dev:"+d.ID])
	h.mu.RUnlock()
	if code == "power" {
		code = map[bool]string{true: "off", false: "on"}[on]
	}
	payload := code
	if len(d.Codes) > 0 || d.Kind != "ac" {
		payload = d.Codes[code]
		if payload == "" && (code == "on" || code == "off") {
			if on == (code == "on") {
				return true // đã đúng trạng thái giả định: bấm nguồn lúc này sẽ đảo ngược
			}
			payload = d.Codes["power"]
		}
	}
	if !irCodeRe.MatchString(code) || payload == "" || d.Protected && code == "off" {
		return false
	}
	h.publish(d.CommandTopic, payload)
	h.assume(d.ID, code)
	return true
}

func (h *Hub) assume(id, code string) {
	now := time.Now()
	key, v, kv := strings.Cut(code, "_")
	x := "x:" + id + ":"
	switch {
	case code == "on" || code == "off":
		h.Ingest("dev:"+id, strings.ToUpper(code), now)
	case kv && irValueKeys[key]:
		if key == "temp" {
			n, _ := strconv.Atoi(v)
			v = strconv.Itoa(min(30, max(16, n)))
		}
		h.Ingest(x+key, v, now)
		if key == "speed" {
			h.Ingest("dev:"+id, "ON", now)
		}
	case irToggles[code]:
		h.mu.RLock()
		cur := h.val[x+code]
		h.mu.RUnlock()
		h.Ingest(x+code, map[bool]string{true: "0", false: "1"}[cur == "1"], now)
	}
}
