package main

import (
	"log"
	"os"
	"regexp"
	"sort"
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

var zoneID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)

// applyZoneOverrides áp phòng sửa / thêm trên trang Cài đặt (nhóm cấu hình "zones"):
// {"<id phòng>": {"name": "<tên>", "floor": <tầng 1..9>, "desc": "<mô tả>"}}. Phòng đã có thì đổi các trường hợp lệ;
// id chưa có thì thêm phòng mới (cần tên và tầng). Gọi trước applyDeviceOverrides để chuyển được thiết bị vào phòng mới.
func applyZoneOverrides(cfg *Config, over map[string]any) {
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return strings.TrimSpace(s) }
	floor := func(m map[string]any) int {
		if f, ok := m["floor"].(float64); ok && f >= 1 && f <= 9 && f == float64(int(f)) {
			return int(f)
		}
		return 0
	}
	at := map[string]int{}
	for i, z := range cfg.Zones {
		at[z.ID] = i
	}
	var added []string
	for id, v := range over {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		i, ok := at[id]
		if !ok {
			if zoneID.MatchString(id) && str(m, "name") != "" && floor(m) != 0 {
				added = append(added, id)
			}
			continue
		}
		if n := str(m, "name"); n != "" {
			cfg.Zones[i].Name = n
		}
		if f := floor(m); f != 0 {
			cfg.Zones[i].Floor = f
		}
		if _, ok := m["desc"].(string); ok {
			cfg.Zones[i].Desc = str(m, "desc")
		}
	}
	sort.Strings(added) // thứ tự ổn định giữa các lần khởi động
	for _, id := range added {
		m := over[id].(map[string]any)
		cfg.Zones = append(cfg.Zones, Zone{ID: id, Name: str(m, "name"), Floor: floor(m), Desc: str(m, "desc")})
	}
}

// restartHub: thoát để Docker chạy lại hubd và nạp cấu hình mới (dashboard tự tải lại danh sách khi nối lại SSE).
// Chờ một chút để trả xong phản hồi cho trình duyệt. Biến để test thay được.
var restartHub = func() {
	time.Sleep(500 * time.Millisecond)
	log.Print("sắp xếp thiết bị đổi: khởi động lại để nạp")
	os.Exit(0)
}
