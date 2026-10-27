package main

import (
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

var deviceKind = regexp.MustCompile(`^[a-z]{2,12}$`) // loại thiết bị: light, socket, breaker... (dashboard map sang biểu tượng)

// applyDeviceOverrides áp sắp xếp thiết bị nhập ở trang Cài đặt (nhóm cấu hình "devices"):
// {"<id thiết bị>": {"zone": "<khu>", "name": "<tên>", "kind": "<loại>", "group": "<tên thiết bị nhiều nút>",
// "area": "<khu được cấp điện>"}}. zone/name/kind đổi chính thiết bị (từng kênh của thiết bị nhiều nút là một id
// riêng); group đổi tên chung của thiết bị nhiều nút; area đưa số đo của thiết bị đó (CB) về khu nó cấp điện.
// Khu không có trong zones, tên rỗng hay loại không hợp lệ thì giữ nguyên.
func applyDeviceOverrides(cfg *Config, over map[string]any) {
	zones := map[string]bool{}
	for _, z := range cfg.Zones {
		zones[z.ID] = true
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return strings.TrimSpace(s) }
	for i := range cfg.Devices {
		m, ok := over[cfg.Devices[i].ID].(map[string]any)
		if !ok {
			continue
		}
		if z := str(m, "zone"); zones[z] {
			cfg.Devices[i].Zone = z
		}
		if n := str(m, "name"); n != "" {
			cfg.Devices[i].Name = n
		}
		if k := str(m, "kind"); deviceKind.MatchString(k) {
			cfg.Devices[i].Kind = k
		}
		if g := str(m, "group"); g != "" && cfg.Devices[i].Group != "" { // chỉ đổi tên thiết bị nhiều nút, không tự gom nhóm
			cfg.Devices[i].Group = g
		}
		if a := str(m, "area"); zones[a] {
			for j := range cfg.Entities {
				if cfg.Entities[j].Device == cfg.Devices[i].ID {
					cfg.Entities[j].Zone = a
				}
			}
		}
	}
}

// restartHub: thoát để Docker chạy lại hubd và nạp cấu hình mới (dashboard tự tải lại danh sách khi nối lại SSE).
// Chờ một chút để trả xong phản hồi cho trình duyệt. Biến để test thay được.
var restartHub = func() {
	time.Sleep(500 * time.Millisecond)
	log.Print("sắp xếp thiết bị đổi: khởi động lại để nạp")
	os.Exit(0)
}
