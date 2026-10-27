package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// mergeGenerated: thiết bị và số đo trong <dir>/*.yaml (cấu hình sinh tự động ngoài git, vd. Tuya sau khi quét
// QR trên dashboard) thay mục cùng id của config.yaml, mục mới thêm vào cuối. File không đọc được thì bỏ qua.
func mergeGenerated(cfg *Config, dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	sort.Strings(files)
	for _, f := range files {
		var g struct {
			Devices  []Device `yaml:"devices"`
			Entities []Entity `yaml:"entities"`
		}
		raw, err := os.ReadFile(f)
		if err == nil {
			err = yaml.Unmarshal(raw, &g)
		}
		if err != nil {
			log.Printf("bỏ qua cấu hình sinh %s: %v", f, err)
			continue
		}
		cfg.Devices = upsert(dropSplit(cfg.Devices, g.Devices), g.Devices, func(d Device) string { return d.ID })
		cfg.Entities = upsert(cfg.Entities, g.Entities, func(e Entity) string { return e.ID })
	}
}

// dropSplit bỏ thiết bị đã được file sinh tách thành từng kênh: id <gốc>_<n> cùng command_topic (ổ nhiều lỗ, công tắc
// nhiều nút), không thì cả ổ lẫn từng lỗ cùng hiện. Thiết bị khác dùng chung topic (khai tay, tên khác) vẫn giữ.
func dropSplit(base, gen []Device) []Device {
	split := map[string]bool{}
	for _, g := range gen {
		if i := strings.LastIndexByte(g.ID, '_'); i > 0 && g.CommandTopic != "" {
			split[g.ID[:i]+"\n"+g.CommandTopic] = true
		}
	}
	out := base[:0:0]
	for _, d := range base {
		if !split[d.ID+"\n"+d.CommandTopic] {
			out = append(out, d)
		}
	}
	return out
}

func upsert[T any](base, add []T, id func(T) string) []T {
	at := map[string]int{}
	for i, x := range base {
		at[id(x)] = i
	}
	for _, x := range add {
		if i, ok := at[id(x)]; ok {
			base[i] = x
		} else {
			at[id(x)] = len(base)
			base = append(base, x)
		}
	}
	return base
}

// watchFiles gọi onChange khi một file (hoặc một file *.yaml trong thư mục) đổi thời điểm sửa hay kích thước,
// xuất hiện hoặc mất đi. Kiểm tra định kỳ cho đơn giản (vài file, mỗi 5 giây). Trả hàm dừng.
func watchFiles(paths []string, every time.Duration, onChange func(string)) (stop func()) {
	sig := func() map[string]string {
		m := map[string]string{}
		for _, p := range paths {
			list := []string{p}
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				list, _ = filepath.Glob(filepath.Join(p, "*.yaml"))
			}
			for _, f := range list {
				if st, err := os.Stat(f); err == nil {
					m[f] = fmt.Sprint(st.ModTime().UnixNano(), st.Size())
				}
			}
		}
		return m
	}
	diff := func(a, b map[string]string) string {
		for f, v := range b {
			if a[f] != v {
				return f
			}
		}
		for f := range a {
			if _, ok := b[f]; !ok {
				return f
			}
		}
		return ""
	}
	done := make(chan struct{})
	go func() {
		prev, t := sig(), time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				cur := sig()
				if f := diff(prev, cur); f != "" {
					onChange(f)
				}
				prev = cur
			}
		}
	}()
	return func() { close(done) }
}
