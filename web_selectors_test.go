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

// Trang Cài đặt nhảy lên đầu (2026-10-27): draw() thay nút cũ bằng nút mới mỗi lần có số đo, nhưng chỉ giữ vị trí cuộn
// của vài vùng liệt kê tay (cột phải #side và hộp danh sách phòng không có trong đó), lưu sau khi #stage đã bị vẽ đè,
// và đặt lại trước masonry() (lúc cột phải tạm co lại nên vị trí bị cắt về 0).
// Giữ ba điều: lưu mọi vùng đang cuộn (không liệt kê), lưu trước khi vẽ bất cứ gì, đặt lại sau masonry().
func TestDrawKeepsEveryScrollPosition(t *testing.T) {
	b, err := os.ReadFile("web/dash.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "\nfunction draw() {")
	if i < 0 {
		t.Fatal("không thấy function draw()")
	}
	body := src[i : i+strings.Index(src[i:], "\n}\n")]
	at := func(re string) int {
		loc := regexp.MustCompile(re).FindStringIndex(body)
		if loc == nil {
			t.Fatalf("draw() thiếu %s", re)
		}
		return loc[0]
	}
	save := at(`querySelectorAll\('\*'\)\) if \(e\.scrollTop \|\| e\.scrollLeft\)`)
	if first := at(`innerHTML =`); save > first {
		t.Error("draw() lưu vị trí cuộn sau khi đã vẽ đè một vùng: vùng đó mất vị trí trước khi kịp lưu")
	}
	if restore, m := at(`keep\.get\(skey\(e\)\)`), at(`masonry\(\);`); restore < m {
		t.Error("draw() đặt lại vị trí cuộn trước masonry(): cột phải đang co lại nên vị trí bị cắt về 0")
	}
}

// Biểu đồ trống dù có số (2026-10-27): /api/history trả t theo giây (voiced cũng đọc như vậy), chart.js vẽ theo
// mili giây; dữ liệu mẫu dùng mili giây nên chỉ hỏng với dữ liệu thật. Mọi trang chỉ lấy lịch sử qua chHistory()
// trong chart.js, nơi duy nhất đổi đơn vị. Quét mọi file trong web/.
func TestHistoryFetchedOnlyThroughChHistory(t *testing.T) {
	files, _ := filepath.Glob("web/*")
	n := 0
	for _, f := range files {
		if !strings.HasSuffix(f, ".js") && !strings.HasSuffix(f, ".html") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		n++
		c := strings.Count(string(b), "/api/history")
		if f == filepath.Join("web", "chart.js") {
			if c != 1 || !regexp.MustCompile(`function chHistory\([^)]*\) \{[^\n]*/api/history[^\n]*t: p\.t \* 1e3`).Match(b) {
				t.Errorf("chart.js: chHistory() phải là chỗ duy nhất gọi /api/history và đổi t từ giây sang mili giây")
			}
		} else if c > 0 {
			t.Errorf("%s gọi /api/history %d lần: dùng chHistory() để có t theo mili giây", f, c)
		}
	}
	t.Logf("đã quét %d file .js/.html trong web/", n)
}

// Trang Cài đặt trắng khi mở thẳng #/caidat (2026-10-27): dash.js vẽ ngay khi nạp, lúc settings.js chưa nạp, gọi
// settingsPage() thì ném lỗi và dừng luôn phần khởi động. Mọi file nạp sau dash.js (settings.js, setup-*.js, music.js...)
// đều gặp chuyện này, nên dash.js chỉ khởi động sau DOMContentLoaded (mọi script cuối trang đã chạy).
func TestDashJSStartsAfterAllScripts(t *testing.T) {
	b, err := os.ReadFile("web/dash.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "addEventListener('DOMContentLoaded', init)") || regexp.MustCompile(`(?m)^init\(\);`).Match(b) {
		t.Fatal("dash.js phải gọi init() sau DOMContentLoaded, không gọi ngay khi nạp")
	}
	html, _ := os.ReadFile("web/index.html")
	if i, j := strings.Index(string(html), `src="/dash.js`), strings.Index(string(html), `src="/settings.js`); i < 0 || j < i {
		t.Fatal("index.html phải nạp settings.js sau dash.js")
	}
}

// Mục Telegram của Cài đặt phải được nạp, xếp vào nhóm Thông báo, và mỗi loại thông báo có ô Telegram riêng.
func TestTelegramSettingsWired(t *testing.T) {
	read := func(f string) string {
		b, err := os.ReadFile(filepath.Join("web", f))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if !regexp.MustCompile(`<script src="/setup-telegram\.js\?v=\d+"></script>`).MatchString(read("index.html")) {
		t.Error("index.html chưa nạp setup-telegram.js?v=")
	}
	if !strings.Contains(read("settings.js"), "telegram: 'thongbao'") {
		t.Error("settings.js thiếu telegram: 'thongbao' trong SETUP_SEC")
	}
	if !strings.Contains(read("setup-ai.js"), "notify.tg:") {
		t.Error("setup-ai.js thiếu ô notify.tg:<kind>")
	}
	// PIN duyệt người nhận không được đi qua data-sfield (sẽ vào DRAFT rồi bị "Lưu" ghi xuống cấu hình).
	if strings.Contains(read("setup-telegram.js"), "pin\" data-sfield") || strings.Contains(read("setup-telegram.js"), "telegram.pin") {
		t.Error("setup-telegram.js để PIN đi qua data-sfield")
	}
	// api() coi 401 là phiên hết hạn và gửi lại; PIN sai cũng trả 401 nên lệnh duyệt phải dùng fetch thường.
	for _, l := range strings.Split(read("setup-telegram.js"), "\n") {
		if strings.Contains(l, "/api/telegram/approve") && (strings.Contains(l, "U.api") || !strings.Contains(l, "fetch(")) {
			t.Errorf("lệnh duyệt phải dùng fetch thường: %s", strings.TrimSpace(l))
		}
	}
}

// Chỉ /start mới tạo yêu cầu chờ duyệt: gợi ý trên trang Cài đặt phải nói đúng; nhãn dòng "Khác" phải cho biết có cả pin.
func TestTelegramHintsAccurate(t *testing.T) {
	rd := func(f string) string {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if !strings.Contains(rd("web/setup-telegram.js"), "/start") {
		t.Error("gợi ý Telegram phải nhắc /start")
	}
	for _, f := range []string{"web/setup-ai.js", "README.md"} {
		if !strings.Contains(rd(f), "Pin và cảnh báo khác") {
			t.Errorf("%s: dòng Khác phải ghi \"Pin và cảnh báo khác\"", f)
		}
	}
}

// Camera nối thẳng: mục Camera gọi đúng route, mật khẩu camera không đi qua data-sfield/DRAFT, mọi thẻ ?v= trong index.html cùng một giá trị.
func TestCameraSetupWired(t *testing.T) {
	read := func(f string) string {
		b, err := os.ReadFile(filepath.Join("web", f))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	js := read("setup-camera.js")
	for _, p := range []string{"/api/cameras", "/api/cameras/scan", "/api/cameras/test"} {
		if !strings.Contains(js, "'"+p+"'") {
			t.Errorf("setup-camera.js không gọi %s", p)
		}
	}
	for _, l := range strings.Split(js, "\n") {
		if strings.Contains(l, "cam-pass") && (strings.Contains(l, "data-sfield") || strings.Contains(l, "sinput")) {
			t.Errorf("ô mật khẩu camera đi qua data-sfield: %s", strings.TrimSpace(l))
		}
	}
	if strings.Contains(js, "camera', 'pass") || strings.Contains(js, "camera.pass") {
		t.Error("setup-camera.js để mật khẩu camera vào bản nháp chung")
	}
	if !strings.Contains(js, "{ CAMS = DEMO_CAMS; TPL = TPL0; } else") {
		t.Error("chế độ dữ liệu mẫu phải gán DEMO_CAMS")
	}
	if strings.Count(js, "if (F.busy) return;") < 2 {
		t.Error("Thử kết nối và Lưu phải có chốt chống bấm đúp (F.busy)")
	}
	if strings.Count(js, "finally { F.busy = false; U.soon(); }") < 2 {
		t.Error("Thử kết nối và Lưu phải mở lại nút trong finally khi gọi hub lỗi")
	}
	if strings.Contains(js, "http_port: 0,") {
		t.Error("camera sửa phải giữ http_port đã lưu")
	}
	vs := map[string]bool{}
	for _, m := range regexp.MustCompile(`\?v=(\d+)`).FindAllStringSubmatch(read("index.html"), -1) {
		vs[m[1]] = true
	}
	if len(vs) != 1 {
		t.Errorf("index.html phải dùng một ?v= duy nhất, đang có %d giá trị", len(vs))
	}
}
