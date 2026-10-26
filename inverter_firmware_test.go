package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Gói ESPHome của inverter (firmware/inverter/*.yaml) được kiểm ở đây, không cần ESP32:
// chỉ đọc, đăng đúng lược đồ MQTT chung, và giải mã bộ thanh ghi mẫu ra đúng số.

// Lược đồ MQTT chung cho mọi hãng: khoá → đơn vị, đăng lên nangluong/inverter/<khoá>.
// Dấu: grid_power + mua / − bán; battery_power + sạc / − xả.
var inverterSchema = map[string]string{
	"pv_power": "W", "load_power": "W", "grid_power": "W", "battery_power": "W", "battery_soc": "%",
	"grid_voltage": "V", "grid_frequency": "Hz", "temperature": "°C",
	"pv_today": "kWh", "load_today": "kWh", "buy_today": "kWh", "sell_today": "kWh",
	"charge_today": "kWh", "discharge_today": "kWh",
}

const inverterTopic = "nangluong/inverter/"

// yamlAny đổi cây YAML sang map/slice/string, bỏ qua tag của ESPHome (!secret, !include).
func yamlAny(n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		return yamlAny(n.Content[0])
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			m[n.Content[i].Value] = yamlAny(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		s := []any{}
		for _, c := range n.Content {
			s = append(s, yamlAny(c))
		}
		return s
	case yaml.AliasNode:
		return yamlAny(n.Alias)
	}
	return n.Value
}

func parseFirmware(src []byte) (map[string]any, error) {
	var n yaml.Node
	if err := yaml.Unmarshal(src, &n); err != nil {
		return nil, err
	}
	if len(n.Content) == 0 {
		return map[string]any{}, nil
	}
	m, _ := yamlAny(&n).(map[string]any)
	return m, nil
}

func items(v any) []map[string]any {
	var out []map[string]any
	if s, ok := v.([]any); ok {
		for _, x := range s {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// Danh sách cho phép (không phải danh sách cấm): khoá nào không có ở đây là vi phạm,
// nên một loại thực thể ghi được mà ESPHome thêm sau này cũng bị chặn.
var (
	firmwareTopKeys = map[string]bool{"substitutions": true, "esphome": true, "esp32": true, "logger": true,
		"wifi": true, "captive_portal": true, "mqtt": true, "ota": true, "packages": true,
		"uart": true, "modbus": true, "modbus_controller": true, "sensor": true}
	firmwareSensorPlatforms = map[string]bool{"modbus_controller": true, "combination": true}
	firmwareRegisterTypes   = map[string]bool{"holding": true, "read": true} // hàm 0x03 và 0x04
)

// readOnlyViolations: những chỗ trong một file firmware có thể làm ESP32 ghi vào inverter.
func readOnlyViolations(cfg map[string]any) []string {
	var bad []string
	for k := range cfg {
		if !firmwareTopKeys[k] {
			bad = append(bad, "khoá cấp đầu không được phép: "+k)
		}
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				if k == "lambda" || k == "write_lambda" || k == "custom_command" || strings.HasPrefix(k, "on_") {
					bad = append(bad, path+"."+k+": tự động hoá/lambda/lệnh tuỳ ý có thể ghi")
				}
				walk(path+"."+k, c)
			}
		case []any:
			for i, c := range x {
				walk(path+"["+strconv.Itoa(i)+"]", c)
			}
		}
	}
	walk("", cfg)
	for _, s := range items(cfg["sensor"]) {
		p := str(s, "platform")
		if !firmwareSensorPlatforms[p] {
			bad = append(bad, "sensor platform không được phép: "+p)
		}
		if p == "modbus_controller" && !firmwareRegisterTypes[str(s, "register_type")] {
			bad = append(bad, "register_type phải là holding hoặc read: "+str(s, "id")+" "+str(s, "register_type"))
		}
	}
	return bad
}

func firmwareFiles(t *testing.T) []string {
	files, _ := filepath.Glob("firmware/inverter/*.yaml")
	if len(files) == 0 {
		t.Fatalf("chỉ thấy %d gói inverter trong firmware/inverter/", len(files))
	}
	return files
}

func loadFirmware(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := parseFirmware(b)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return cfg
}

// v1 chỉ đọc inverter: quét node gốc và mọi gói hãng.
func TestInverterFirmwareReadOnly(t *testing.T) {
	for _, f := range append(firmwareFiles(t), "firmware/node-nangluong.yaml") {
		for _, v := range readOnlyViolations(loadFirmware(t, f)) {
			t.Errorf("%s: %s", f, v)
		}
	}
	// Cài sẵn từng kiểu ghi vào một gói hợp lệ: bộ kiểm phải bắt được tất cả.
	planted := map[string]string{
		"number":         "number:\n  - platform: modbus_controller\n    address: 210\n",
		"select":         "select:\n  - platform: modbus_controller\n    address: 142\n",
		"switch":         "switch:\n  - platform: modbus_controller\n    address: 145\n",
		"output":         "output:\n  - platform: modbus_controller\n    address: 1\n",
		"interval":       "interval:\n  - interval: 1s\n    then: []\n",
		"custom_command": "sensor:\n  - platform: modbus_controller\n    register_type: holding\n    custom_command: [0x01, 0x06, 0x00, 0xD2]\n",
		"coil":           "sensor:\n  - platform: modbus_controller\n    register_type: coil\n",
		"template":       "sensor:\n  - platform: template\n    lambda: return 1;\n",
		"on_value":       "sensor:\n  - platform: combination\n    type: sum\n    on_value: []\n",
	}
	for name, y := range planted {
		cfg, err := parseFirmware([]byte(y))
		if err != nil {
			t.Fatal(name, err)
		}
		if len(readOnlyViolations(cfg)) == 0 {
			t.Errorf("bộ kiểm không bắt được kiểu ghi %q", name)
		}
	}
}

// published: các sensor gói đăng lên MQTT (không internal), theo khoá của lược đồ.
func published(t *testing.T, f string, cfg map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, s := range items(cfg["sensor"]) {
		if str(s, "internal") == "true" {
			continue
		}
		topic := str(s, "state_topic")
		key, ok := strings.CutPrefix(topic, inverterTopic)
		if !ok {
			t.Errorf("%s: sensor %q đăng lên %q, ngoài %s<khoá>", f, str(s, "name"), topic, inverterTopic)
			continue
		}
		if out[key] != nil {
			t.Errorf("%s: khoá %s đăng hai lần", f, key)
		}
		out[key] = s
	}
	return out
}

// Hãng nào cũng đăng đủ và chỉ đúng lược đồ, đúng đơn vị; config.yaml đọc đủ các topic đó.
// Khoá hãng không có thanh ghi thì phải ghi lý do ở fixtures/<gói>.json "missing" (hubd hiện "—").
func TestInverterSchemaCoverage(t *testing.T) {
	files := firmwareFiles(t)
	for _, f := range files {
		pub, missing := published(t, f, loadFirmware(t, f)), loadFixture(t, f).Missing
		for key, unit := range inverterSchema {
			s := pub[key]
			if s == nil && missing[key] == "" {
				t.Errorf("%s: thiếu %s", f, key)
			} else if s != nil && missing[key] != "" {
				t.Errorf("%s: %s có đăng nhưng fixture ghi là thiếu", f, key)
			} else if s == nil {
				t.Logf("%s: không có %s (%s)", f, key, missing[key])
			} else if u := str(s, "unit_of_measurement"); u != unit {
				t.Errorf("%s: %s đơn vị %q, lược đồ là %q", f, key, u, unit)
			}
		}
		for key := range pub {
			if _, ok := inverterSchema[key]; !ok {
				t.Errorf("%s: %s không có trong lược đồ", f, key)
			}
		}
	}
	cfg, _, err := LoadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, e := range cfg.Entities {
		key, ok := strings.CutPrefix(e.Topic, inverterTopic)
		if !ok {
			continue
		}
		seen[key]++
		if unit, ok := inverterSchema[key]; !ok {
			t.Errorf("config.yaml: %s đọc topic ngoài lược đồ %s", e.ID, e.Topic)
		} else if e.Unit != unit {
			t.Errorf("config.yaml: %s đơn vị %q, lược đồ là %q", e.ID, e.Unit, unit)
		}
	}
	for key := range inverterSchema {
		if seen[key] != 1 {
			t.Errorf("config.yaml: %d entity đọc %s%s, cần đúng 1", seen[key], inverterTopic, key)
		}
	}
	t.Logf("%d gói × %d khoá lược đồ", len(files), len(inverterSchema))
}

// Mỗi gói có một bộ thanh ghi mẫu (fixtures/<gói>.json) và số phải ra sau khi giải mã.
type inverterFixture struct {
	Source  string             `json:"source"`
	Holding map[string]uint16  `json:"holding"`
	Read    map[string]uint16  `json:"read"`
	Expect  map[string]float64 `json:"expect"`
	Missing map[string]string  `json:"missing"` // khoá → lý do hãng không có
}

func loadFixture(t *testing.T, firmware string) inverterFixture {
	t.Helper()
	name := strings.TrimSuffix(filepath.Base(firmware), ".yaml")
	raw, err := os.ReadFile(filepath.Join("firmware/inverter/fixtures", name+".json"))
	if err != nil {
		t.Fatalf("thiếu fixture: %v", err)
	}
	var fx inverterFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("%s.json: %v", name, err)
	}
	return fx
}

// decode tính giá trị một sensor theo đúng ngữ nghĩa ESPHome mà gói dùng:
// modbus_controller (value_type, bitmask, filters) và combination (sum, linear).
func decode(t *testing.T, byID map[string]map[string]any, s map[string]any, fx inverterFixture) float64 {
	t.Helper()
	var v float64
	switch str(s, "platform") {
	case "modbus_controller":
		regs := fx.Holding
		if str(s, "register_type") == "read" {
			regs = fx.Read
		}
		addr, err := strconv.ParseInt(str(s, "address"), 0, 32)
		if err != nil {
			t.Fatalf("địa chỉ %q: %v", str(s, "address"), err)
		}
		reg := func(i int64) uint32 {
			r, ok := regs[strconv.FormatInt(addr+i, 10)]
			if !ok {
				t.Fatalf("fixture thiếu thanh ghi %s %d (sensor %s)", str(s, "register_type"), addr+i, str(s, "id"))
			}
			return uint32(r)
		}
		switch vt := str(s, "value_type"); vt {
		case "", "U_WORD":
			v = float64(reg(0))
		case "S_WORD":
			v = float64(int16(reg(0)))
		case "U_DWORD":
			v = float64(reg(0)<<16 | reg(1))
		case "U_DWORD_R":
			v = float64(reg(1)<<16 | reg(0))
		case "S_DWORD":
			v = float64(int32(reg(0)<<16 | reg(1)))
		case "S_DWORD_R":
			v = float64(int32(reg(1)<<16 | reg(0)))
		default:
			t.Fatalf("value_type %s chưa hỗ trợ trong bộ giải mã test", vt)
		}
		if m := str(s, "bitmask"); m != "" {
			mask, _ := strconv.ParseUint(m, 0, 32)
			raw := uint64(v) & mask
			for mask&1 == 0 { // ESPHome dồn kết quả về bit thấp
				mask, raw = mask>>1, raw>>1
			}
			v = float64(raw)
		}
	case "combination":
		for _, src := range items(s["sources"]) {
			c := 1.0
			if str(s, "type") == "linear" {
				c, _ = strconv.ParseFloat(str(src, "coefficient"), 64)
			} else if str(s, "type") != "sum" {
				t.Fatalf("combination type %s chưa hỗ trợ", str(s, "type"))
			}
			dep := byID[str(src, "source")]
			if dep == nil {
				t.Fatalf("nguồn %q không tồn tại", str(src, "source"))
			}
			v += c * decode(t, byID, dep, fx)
		}
	default:
		t.Fatalf("platform %s chưa hỗ trợ", str(s, "platform"))
	}
	for _, flt := range items(s["filters"]) {
		for k, a := range flt {
			x, _ := strconv.ParseFloat(a.(string), 64)
			switch k {
			case "multiply":
				v *= x
			case "offset":
				v += x
			case "throttle", "heartbeat", "delta":
				// chỉ đổi thời điểm gửi, không đổi giá trị
			default:
				t.Fatalf("filter %s chưa hỗ trợ trong bộ giải mã test", k)
			}
		}
	}
	return v
}

func TestInverterDecodeFixtures(t *testing.T) {
	for _, f := range firmwareFiles(t) {
		name := strings.TrimSuffix(filepath.Base(f), ".yaml")
		t.Run(name, func(t *testing.T) {
			fx := loadFixture(t, f)
			cfg := loadFirmware(t, f)
			byID := map[string]map[string]any{}
			for _, s := range items(cfg["sensor"]) {
				byID[str(s, "id")] = s
			}
			pub := published(t, f, cfg)
			keys := make([]string, 0, len(inverterSchema))
			for k := range inverterSchema {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if fx.Missing[k] != "" {
					continue
				}
				want, ok := fx.Expect[k]
				if !ok {
					t.Errorf("fixture thiếu expect.%s", k)
					continue
				}
				if pub[k] == nil {
					continue // TestInverterSchemaCoverage đã báo
				}
				if got := decode(t, byID, pub[k], fx); math.Abs(got-want) > 1e-6 {
					t.Errorf("%s = %v, cần %v", k, got, want)
				}
			}
		})
	}
}

// Dashboard coi mọi entity không gắn thiết bị trong một zone là cảm biến phòng: nhiệt độ inverter
// sẽ thành "nhiệt độ tum kỹ thuật", SOC % thành "độ ẩm". Mọi entity inverter phải nằm trong ENERGY_IDS.
func TestInverterEntitiesHiddenFromRoomSensors(t *testing.T) {
	js, err := os.ReadFile("web/dash.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)const ENERGY_IDS = new Set\(\[(.*?)\]\)`).FindSubmatch(js)
	if m == nil {
		t.Fatal("không thấy ENERGY_IDS trong web/dash.js")
	}
	ids := map[string]bool{}
	for _, q := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(m[1], -1) {
		ids[string(q[1])] = true
	}
	cfg, _, err := LoadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range cfg.Entities {
		if strings.HasPrefix(e.Topic, inverterTopic) && !ids[e.ID] {
			t.Errorf("%s đọc %s nhưng không có trong ENERGY_IDS của web/dash.js", e.ID, e.Topic)
		}
	}
}
