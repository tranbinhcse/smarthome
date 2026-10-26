package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const jkFW = "firmware/jk-bms/"

// Topic mà firmware JK gửi: tổng (jk-total.yaml) + từng pack (mẫu ${n}, nhân theo các n khai báo ở file chính).
func jkFirmwareTopics(t *testing.T, main, pack string) []string {
	t.Helper()
	read := func(f string) string {
		b, err := os.ReadFile(jkFW + f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	topic := regexp.MustCompile(`state_topic:\s*(\S+)`)
	var out []string
	for _, m := range topic.FindAllStringSubmatch(read("jk-total.yaml"), -1) {
		out = append(out, m[1])
	}
	ns := regexp.MustCompile(`file:\s*`+regexp.QuoteMeta(pack)+`,\s*vars:\s*\{\s*n:\s*(\d+)`).FindAllStringSubmatch(read(main), -1)
	if len(ns) == 0 {
		t.Fatalf("%s không include %s cho pack nào", main, pack)
	}
	tpl := topic.FindAllStringSubmatch(read(pack), -1)
	for _, n := range ns {
		for _, m := range tpl {
			out = append(out, strings.ReplaceAll(m[1], "${n}", n[1]))
		}
	}
	sort.Strings(out)
	return out
}

func loadRealConfig(t *testing.T) Config {
	t.Helper()
	cfg, _, err := LoadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Firmware RS485, firmware BLE và config.yaml phải cùng một lược đồ topic: lệch một chữ là số đo mất âm thầm.
func TestJKFirmwareTopicsMatchConfig(t *testing.T) {
	var cfgTopics []string
	for _, e := range loadRealConfig(t).Entities {
		if strings.HasPrefix(e.ID, "jk.") {
			cfgTopics = append(cfgTopics, e.Topic)
		}
	}
	sort.Strings(cfgTopics)
	want := strings.Join(cfgTopics, "\n")
	for _, v := range [][2]string{{"jk-bms.yaml", "jk-pack.yaml"}, {"jk-bms-ble.yaml", "jk-pack-ble.yaml"}} {
		if got := strings.Join(jkFirmwareTopics(t, v[0], v[1]), "\n"); got != want {
			t.Errorf("%s gửi:\n%s\n\nconfig.yaml đọc:\n%s", v[0], got, want)
		}
	}
	if len(cfgTopics) < 30 {
		t.Fatalf("chỉ %d entity jk.* trong config.yaml", len(cfgTopics))
	}
}

// v1 chỉ ĐỌC BMS: firmware JK không được khai báo thực thể ghi được, và bus RS485 chỉ nối chân RX.
// Ngoại lệ duy nhất: header của jk_rs485_bms luôn include mã switch/number nên bản RS485 phải nạp hai nền tảng đó
// ở dạng rỗng — chỉ dòng "- platform: jk_rs485_bms" và "jk_rs485_bms_id"; thêm bất kỳ khoá nào là có thực thể ghi.
func TestJKFirmwareReadOnly(t *testing.T) {
	files, _ := filepath.Glob(jkFW + "*.yaml")
	if len(files) < 5 {
		t.Fatalf("chỉ thấy %d file YAML trong %s", len(files), jkFW)
	}
	write := regexp.MustCompile(`^\s*-?\s*(switch|number|button|select|output|light|fan|climate|lock|valve|cover):(.*)$`)
	stub := regexp.MustCompile(`^\s*(-\s*platform:\s*jk_rs485_bms|jk_rs485_bms_id:\s*\w+)\s*$`)
	comment := regexp.MustCompile(`(?m)#.*$`)
	for _, f := range files {
		b, _ := os.ReadFile(f)
		src := comment.ReplaceAllString(string(b), "")
		lines := strings.Split(src, "\n")
		for i, l := range lines {
			m := write.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			if strings.TrimSpace(m[2]) != "" {
				t.Errorf("%s: %s viết một dòng %q: chỉ được khai báo rỗng dạng khối", f, m[1], strings.TrimSpace(l))
			}
			for _, x := range lines[i+1:] {
				if strings.TrimSpace(x) == "" {
					continue
				}
				if x[0] != ' ' && x[0] != '-' {
					break // khoá cấp đầu tiếp theo
				}
				if !stub.MatchString(x) {
					t.Errorf("%s: %s có %q: firmware JK không được có thực thể ghi được", f, m[1], strings.TrimSpace(x))
				}
			}
		}
		if strings.Contains(src, "tx_pin") || strings.Contains(src, "talk_pin") {
			t.Errorf("%s có tx_pin/talk_pin: ESP32 không được phát lên bus RS485 của các pack", f)
		}
	}
}

// Mỗi file testdata/jk/*.txt là bản tin node gửi lên (dòng "topic payload"), dòng "# alerts: a b" là cảnh báo phải bật.
// Phát lại vào hub dựng từ config.yaml thật: mọi topic phải có entity nhận, đúng giá trị, đúng bộ cảnh báo.
func TestJKFixtures(t *testing.T) {
	cfg := loadRealConfig(t)
	files, _ := filepath.Glob("testdata/jk/*.txt")
	if len(files) == 0 {
		t.Fatal("không có fixture testdata/jk/*.txt")
	}
	byTopic := map[string]string{}
	for _, e := range cfg.Entities {
		byTopic[e.Topic] = e.ID
	}
	covered := map[string]bool{}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			fh, err := os.Open(file)
			if err != nil {
				t.Fatal(err)
			}
			defer fh.Close()
			h := NewHub(cfg, newDB(t), func(string, string) {})
			var want []string
			last := map[string]string{}
			sc := bufio.NewScanner(fh)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if a, ok := strings.CutPrefix(line, "# alerts:"); ok {
					want = strings.Fields(a)
				}
				if line == "" || line[0] == '#' {
					continue
				}
				topic, payload, _ := strings.Cut(line, " ")
				id, ok := byTopic[topic]
				if !ok {
					t.Fatalf("topic %s không có entity trong config.yaml", topic)
				}
				h.Handle(topic, payload, t0)
				last[id] = payload
			}
			h.Tick(t0.Add(10 * time.Minute)) // cho mọi điều kiện "for" kịp chín
			s := h.Snapshot()
			for id, v := range last {
				if s[id] != v {
					t.Errorf("%s = %q, fixture gửi %q", id, s[id], v)
				}
			}
			var got []string
			for k, v := range s {
				if a, ok := strings.CutPrefix(k, "alert:"); ok && v != "" {
					got = append(got, a)
					covered[a] = true
				}
			}
			sort.Strings(got)
			sort.Strings(want)
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("cảnh báo bật %v, fixture cần %v", got, want)
			}
		})
	}
	// Quét hết tập luật pin khai báo trong config.yaml: luật nào chưa fixture nào làm bật là chưa được kiểm.
	n := 0
	for _, r := range cfg.Rules {
		if strings.HasPrefix(r.Entity, "jk.") {
			n++
			if r.Alert == "" {
				t.Errorf("luật %s đọc %s nhưng không có alert", r.ID, r.Entity)
			}
			if !covered[r.ID] {
				t.Errorf("luật %s chưa có fixture nào làm bật", r.ID)
			}
		}
	}
	if n == 0 {
		t.Fatal("config.yaml chưa có luật cảnh báo pin nào")
	}
	t.Logf("%d fixture, %d luật pin đều đã được làm bật", len(files), n)
}

// Hàm gộp 3 pack (jk_total.h) chạy trên ESP32; dịch và chạy test C++ của nó ngay trên máy nếu có trình dịch.
func TestJKTotalCpp(t *testing.T) {
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("không có c++ trên máy này")
	}
	bin := filepath.Join(t.TempDir(), "jk_total_test")
	if out, err := exec.Command(cxx, "-std=c++17", "-Wall", "-Werror", "-o", bin, jkFW+"jk_total_test.cpp").CombinedOutput(); err != nil {
		t.Fatalf("dịch: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("chạy: %v\n%s", err, out)
	}
}
