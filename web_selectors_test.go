package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Trình xử lý click dò ngược lên bằng closest('[data-x]'). Nếu thẻ <html> cũng mang data-x
// thì mọi cú click đều khớp nhánh đó và chặn các nhánh sau (điều hướng chết hẳn).
func TestClickSelectorsDoNotMatchRoot(t *testing.T) {
	var js, html []byte
	for _, pat := range []string{"web/*.js", "web/*.html"} {
		files, _ := filepath.Glob(pat)
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			js = append(js, b...) // html cũng có <script> nội tuyến
			if strings.HasSuffix(f, ".html") {
				html = append(html, b...)
			}
		}
	}
	kebab := regexp.MustCompile(`[A-Z]`)
	root := map[string]bool{}
	for _, m := range regexp.MustCompile(`documentElement\.dataset\.(\w+)`).FindAllStringSubmatch(string(js), -1) {
		root[kebab.ReplaceAllStringFunc(m[1], func(c string) string { return "-" + strings.ToLower(c) })] = true
	}
	for _, tag := range regexp.MustCompile(`<html[^>]*>`).FindAllString(string(html), -1) {
		for _, m := range regexp.MustCompile(`data-([\w-]+)`).FindAllStringSubmatch(tag, -1) {
			root[m[1]] = true
		}
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

// CSS không có bước dựng nên một dấu } lệch là âm thầm làm hỏng mọi quy tắc phía sau
// (một khối @container chưa đóng nuốt cả @media màn hẹp). Quét mọi file .css trong web/.
func TestCSSBlocksWellFormed(t *testing.T) {
	files, _ := filepath.Glob("web/*.css")
	if len(files) == 0 {
		t.Fatal("không thấy file CSS nào")
	}
	comment := regexp.MustCompile(`(?s)/\*.*?\*/`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := comment.ReplaceAllString(string(b), "")
		depth, atDepth, line := 0, -1, 1
		for i, ch := range src {
			switch ch {
			case '\n':
				line++
			case '{':
				head := strings.TrimSpace(src[strings.LastIndexAny(src[:i], "{};")+1 : i])
				if strings.HasPrefix(head, "@") && !strings.HasPrefix(head, "@keyframes") {
					if atDepth >= 0 {
						t.Errorf("%s:%d: %q nằm trong một at-rule khác (thiếu dấu } đóng?)", f, line, head)
					}
					atDepth = depth
				}
				depth++
			case '}':
				depth--
				if depth < 0 {
					t.Fatalf("%s:%d: dấu } thừa", f, line)
				}
				if depth == atDepth {
					atDepth = -1
				}
			}
		}
		if depth != 0 {
			t.Errorf("%s: thiếu %d dấu } ở cuối file", f, depth)
		}
	}
}
