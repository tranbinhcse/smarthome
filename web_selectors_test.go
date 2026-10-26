package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Trình xử lý click dò ngược lên bằng closest('[data-x]'). Nếu thẻ <html> cũng mang data-x
// thì mọi cú click đều khớp nhánh đó và chặn các nhánh sau (điều hướng chết hẳn).
func TestClickSelectorsDoNotMatchRoot(t *testing.T) {
	js, err := os.ReadFile("web/dash.js")
	if err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	kebab := regexp.MustCompile(`[A-Z]`)
	root := map[string]bool{}
	for _, m := range regexp.MustCompile(`documentElement\.dataset\.(\w+)`).FindAllStringSubmatch(string(js), -1) {
		root[kebab.ReplaceAllStringFunc(m[1], func(c string) string { return "-" + strings.ToLower(c) })] = true
	}
	for _, m := range regexp.MustCompile(`data-([\w-]+)`).FindAllStringSubmatch(regexp.MustCompile(`<html[^>]*>`).FindString(string(html)), -1) {
		root[m[1]] = true
	}
	sels := regexp.MustCompile(`(?:pick|closest\??\.?)\('\[data-([\w-]+)\]'\)`).FindAllStringSubmatch(string(js), -1)
	if len(sels) < 10 {
		t.Fatalf("chỉ thấy %d bộ chọn data-*, biểu thức dò có thể đã lỗi thời", len(sels))
	}
	for _, m := range sels {
		if root[m[1]] {
			t.Errorf("bộ chọn [data-%s] trùng thuộc tính đặt trên <html>: mọi cú click sẽ khớp", m[1])
		}
	}
	t.Logf("%d bộ chọn data-* đối chiếu với %d thuộc tính của <html>", len(sels), len(root))
}
