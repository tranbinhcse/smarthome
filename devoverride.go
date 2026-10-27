package main

import (
	"log"
	"os"
	"strings"
	"time"
)

// applyDeviceOverrides áp sắp xếp thiết bị nhập ở trang Cài đặt (nhóm cấu hình "devices"):
// {"<id thiết bị>": {"zone": "<khu>", "name": "<tên>", "area": "<khu được cấp điện>"}}. zone/name đổi chính thiết bị
// (từng kênh của thiết bị nhiều nút là một id riêng); area đưa số đo của thiết bị đó (CB) về khu nó cấp điện.
// Khu không có trong zones hoặc tên rỗng thì giữ nguyên.
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
