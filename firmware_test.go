package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Topic node-<khu>/sensor/<tên>/state trong config.yaml phải khớp một sensor "name: <tên>" trong firmware/node-<khu>.yaml
// (kể cả gói !include). Gõ sai một chữ là ô trên dashboard hiện "—" mãi mà không báo lỗi. Quét mọi entity node-*.
func TestConfigTopicsMatchFirmware(t *testing.T) {
	cfg, _, err := LoadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	topicRe := regexp.MustCompile(`^(node-[a-z0-9-]+)/sensor/([a-z0-9_]+)/state$`)
	names := map[string]map[string]bool{} // node -> tên sensor trong firmware
	read := func(node string) map[string]bool {
		if n, ok := names[node]; ok {
			return n
		}
		n := map[string]bool{}
		files, seen := []string{node + ".yaml"}, map[string]bool{node + ".yaml": true}
		for i := 0; i < len(files); i++ {
			b, err := os.ReadFile(filepath.Join("firmware", files[i]))
			if err != nil {
				t.Errorf("thiếu firmware/%s: %v", files[i], err)
				continue
			}
			for _, m := range regexp.MustCompile(`!include (\S+\.yaml)`).FindAllStringSubmatch(string(b), -1) {
				if !seen[m[1]] { // common.yaml nhắc lại chính nó trong chú thích
					seen[m[1]] = true
					files = append(files, m[1])
				}
			}
			for _, m := range regexp.MustCompile(`(?m)^\s+name: ([a-z0-9_]+)\s*(#.*)?$`).FindAllStringSubmatch(string(b), -1) {
				n[m[1]] = true
			}
		}
		names[node] = n
		return n
	}
	checked := 0
	for _, e := range cfg.Entities {
		if !strings.HasPrefix(e.Topic, "node-") {
			continue
		}
		m := topicRe.FindStringSubmatch(e.Topic)
		if m == nil {
			t.Errorf("%s: topic %q sai dạng node-<khu>/sensor/<tên>/state", e.ID, e.Topic)
			continue
		}
		if !read(m[1])[m[2]] {
			t.Errorf("%s: firmware/%s.yaml không có sensor name: %s", e.ID, m[1], m[2])
		}
		checked++
	}
	if checked < 20 {
		t.Fatalf("chỉ thấy %d entity node-*, config.yaml hoặc biểu thức dò đã đổi", checked)
	}
	t.Logf("%d/%d entity node-* khớp firmware", checked, checked)
}
