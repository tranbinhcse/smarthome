package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeDynsec: plugin dynamic security trong bộ nhớ, đủ các lệnh nodes.go dùng; không có broker thật.
// Lỗi trả về đúng chuỗi của Mosquitto 2.1 (đã đo bằng broker thử: "Client already exists", "Role not found"...).
type fakeDynsec struct {
	clients map[string]*fakeClient
	roles   map[string][]map[string]any
	sent    []dynCmd
}
type fakeClient struct {
	password string
	disabled bool
	roles    []string
	online   bool
}

func newFakeDynsec() *fakeDynsec {
	return &fakeDynsec{clients: map[string]*fakeClient{}, roles: map[string][]map[string]any{}}
}

func (f *fakeDynsec) call(cmds []dynCmd) ([]dynResp, error) {
	var out []dynResp
	for _, c := range cmds {
		f.sent = append(f.sent, c)
		name, _ := c["command"].(string)
		u, _ := c["username"].(string)
		role, _ := c["rolename"].(string)
		r := dynResp{Command: name}
		cl := f.clients[u]
		switch {
		case name == "createRole" && f.roles[role] != nil:
			r.Error = "Role already exists"
		case name == "createRole":
			b, _ := json.Marshal(c["acls"])
			var acls []map[string]any
			json.Unmarshal(b, &acls)
			f.roles[role] = append(acls, nil)[:len(acls)] // khác nil kể cả khi rỗng
		case name == "deleteRole" && f.roles[role] == nil:
			r.Error = "Role not found"
		case name == "deleteRole":
			delete(f.roles, role)
		case name == "createClient" && cl != nil:
			r.Error = "Client already exists"
		case name == "createClient":
			b, _ := json.Marshal(c["roles"])
			var rs []struct{ Rolename string }
			json.Unmarshal(b, &rs)
			nc := &fakeClient{password: c["password"].(string)}
			for _, x := range rs {
				nc.roles = append(nc.roles, x.Rolename)
			}
			f.clients[u] = nc
		case name == "listClients":
			var list []map[string]any
			for n, x := range f.clients {
				m := map[string]any{"username": n, "connections": []any{}}
				if x.disabled {
					m["disabled"] = true
				}
				if x.online {
					m["connections"] = []any{map[string]any{"address": "192.168.1.50"}}
				}
				list = append(list, m)
			}
			r.Data, _ = json.Marshal(map[string]any{"totalCount": len(list), "clients": list})
		case cl == nil:
			r.Error = "Client not found"
		case name == "setClientPassword":
			cl.password = c["password"].(string)
		case name == "disableClient":
			cl.disabled = true
		case name == "enableClient":
			cl.disabled = false
		case name == "deleteClient":
			delete(f.clients, u)
		default:
			r.Error = "Unknown command"
		}
		out = append(out, r)
	}
	return out, nil
}

func login(t *testing.T, cfg Config) (*Hub, func(method, url, body string) (int, string), *[]string) {
	t.Helper()
	mux, h, pub := assistantMux(t, cfg)
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	return h, func(method, url, body string) (int, string) {
		w := call(mux, method, url, body, "", ck...)
		return w.Code, w.Body.String()
	}, pub
}

func nodeOf(t *testing.T, body, name string) map[string]any {
	t.Helper()
	var r struct{ Nodes []map[string]any }
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("GET /api/nodes không phải JSON: %v %s", err, body)
	}
	for _, n := range r.Nodes {
		if n["name"] == name {
			return n
		}
	}
	return nil
}

// Tài khoản MQTT của node do hubd quản lý qua plugin dynamic security: tạo (mật khẩu hiện một lần), đổi mật khẩu,
// khoá, mở, xoá. Mật khẩu không được lưu ở hubd, không lên MQTT thường, không hiện lại trong danh sách.
func TestNodeAccountLifecycle(t *testing.T) {
	h, req, pub := login(t, Config{})
	mux, _, _ := assistantMux(t, Config{})
	if w := call(mux, "POST", "/api/nodes/node-cafe", `{"action":"create"}`, ""); w.Code != 401 {
		t.Fatalf("chưa nhập PIN mà tạo được tài khoản: code=%d", w.Code)
	}
	if w := call(mux, "POST", "/api/nodes/node-cafe", `{"action":"create"}`, testTok); w.Code != 401 {
		t.Fatalf("token trợ lý giọng nói tạo được tài khoản MQTT: code=%d", w.Code)
	}
	if code, body := req("POST", "/api/nodes/node-cafe", `{"action":"create"}`); code != 503 || !strings.Contains(body, "HUBD_MQTT_ADMIN_PASS") {
		t.Fatalf("chưa cấu hình tài khoản quản trị: code=%d %s, muốn 503 kèm hướng dẫn", code, body)
	}
	if code, body := req("GET", "/api/nodes", ""); code != 200 || !strings.Contains(body, `"manage":false`) {
		t.Fatalf("danh sách khi chưa cấu hình quản trị: code=%d %s", code, body)
	}

	f := newFakeDynsec()
	h.dynsec = f.call
	code, body := req("POST", "/api/nodes/node-cafe", `{"action":"create"}`)
	var got struct{ Password, Secret string }
	json.Unmarshal([]byte(body), &got)
	if code != 200 || len(got.Password) < 20 || got.Secret != "mqtt_pass_node_cafe" {
		t.Fatalf("tạo: code=%d %s", code, body)
	}
	c := f.clients["node-cafe"]
	if c == nil || c.password != got.Password || !slices.Equal(c.roles, []string{"node-cafe"}) {
		t.Fatalf("plugin chưa có tài khoản đúng mật khẩu và role riêng: %+v", c)
	}
	if code, _ := req("POST", "/api/nodes/node-cafe", `{"action":"create"}`); code != 409 {
		t.Fatalf("tạo trùng: code=%d, muốn 409 (giữ mật khẩu cũ, dùng đổi mật khẩu)", code)
	}
	if f.clients["node-cafe"].password != got.Password {
		t.Fatal("tạo trùng đã đổi mật khẩu")
	}

	_, body = req("GET", "/api/nodes", "")
	if n := nodeOf(t, body, "node-cafe"); n == nil || n["account"] != "active" {
		t.Fatalf("danh sách thiếu node-cafe đang hoạt động: %s", body)
	}
	if strings.Contains(body, got.Password) {
		t.Fatal("danh sách lộ mật khẩu")
	}

	_, body = req("POST", "/api/nodes/node-cafe", `{"action":"reset"}`)
	var again struct{ Password string }
	json.Unmarshal([]byte(body), &again)
	if again.Password == "" || again.Password == got.Password || f.clients["node-cafe"].password != again.Password {
		t.Fatalf("đổi mật khẩu: %s", body)
	}

	for _, step := range []struct{ action, want string }{{"disable", "disabled"}, {"enable", "active"}, {"delete", "none"}} {
		if code, body := req("POST", "/api/nodes/node-cafe", `{"action":"`+step.action+`"}`); code != 204 {
			t.Fatalf("%s: code=%d %s", step.action, code, body)
		}
		_, body := req("GET", "/api/nodes", "")
		n := nodeOf(t, body, "node-cafe")
		if step.want == "none" && n != nil || step.want != "none" && (n == nil || n["account"] != step.want) {
			t.Fatalf("sau %s: %v", step.action, n)
		}
	}
	if f.roles["node-cafe"] != nil {
		t.Fatal("xoá tài khoản mà còn role")
	}
	if code, _ := req("POST", "/api/nodes/node-cafe", `{"action":"disable"}`); code != 404 {
		t.Fatalf("khoá tài khoản không có: code=%d, muốn 404", code)
	}
	if code, _ := req("POST", "/api/nodes/node-cafe", `{"action":"xoa-het"}`); code != 400 {
		t.Fatalf("việc lạ: code=%d, muốn 400", code)
	}

	all := strings.Join(*pub, "\n")
	for k, v := range h.db.Setting("nodes") {
		all += fmt.Sprint(k, v)
	}
	if strings.Contains(all, got.Password) || strings.Contains(all, again.Password) {
		t.Fatal("mật khẩu node bị lưu ở hubd hoặc gửi lên MQTT thường")
	}
}

// Role của node chỉ cho đi topic của chính nó (node-<khu>/#) và topic tìm IP của ESPHome; còn lại chặn rõ ràng ở
// mức ưu tiên thấp hơn, không dựa vào mặc định của plugin (đã đo: acl_file "pattern readwrite #" đứng sau plugin).
func TestNodeRoleOnlyOwnTopics(t *testing.T) {
	h, req, _ := login(t, Config{})
	f := newFakeDynsec()
	h.dynsec = f.call
	req("POST", "/api/nodes/node-ro", `{"action":"create"}`)
	type acl struct {
		typ, topic string
		allow      bool
	}
	var got []acl
	prio := map[bool][]float64{}
	for _, a := range f.roles["node-ro"] {
		got = append(got, acl{a["acltype"].(string), a["topic"].(string), a["allow"].(bool)})
		prio[a["allow"].(bool)] = append(prio[a["allow"].(bool)], a["priority"].(float64))
	}
	want := []acl{
		{"publishClientSend", "node-ro/#", true}, {"subscribePattern", "node-ro/#", true},
		{"publishClientSend", "esphome/discover/node-ro", true}, {"subscribePattern", "esphome/discover", true},
		{"publishClientSend", "#", false}, {"subscribePattern", "#", false},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ACL role node-ro = %v, muốn %v", got, want)
	}
	if slices.Min(prio[true]) <= slices.Max(prio[false]) {
		t.Fatalf("luật cho phép phải đứng trước luật chặn: %v", prio)
	}
}

// Tên tài khoản do hubd tạo chỉ có dạng node-<khu>. Plugin xét trước passwd (đã đo): tạo trùng tên tài khoản dịch vụ
// (hubd, tuya...) là chiếm mất tài khoản đó. Quét: mọi tài khoản passwd mà scripts/init-secrets.sh tạo, và đủ 128 ký tự
// ASCII chen vào giữa tên.
func TestNodeNameGuard(t *testing.T) {
	h, req, _ := login(t, Config{})
	f := newFakeDynsec()
	h.dynsec = f.call
	raw, err := os.ReadFile("scripts/init-secrets.sh")
	if err != nil {
		t.Fatal(err)
	}
	svc := map[string]bool{}
	for _, re := range []string{`mqtt_user ([a-z][a-z0-9-]*) `, `\b([a-z][a-z0-9-]*):\$[A-Z_]+_PASS\b`, `_MQTT_ADMIN_USER=([a-z0-9-]+)`} {
		for _, m := range regexp.MustCompile(re).FindAllStringSubmatch(string(raw), -1) {
			svc[m[1]] = true
		}
	}
	if len(svc) < 6 {
		t.Fatalf("chỉ đọc được %d tài khoản dịch vụ từ init-secrets.sh (%v), biểu thức dò đã lỗi thời", len(svc), svc)
	}
	for u := range svc {
		if code, _ := req("POST", "/api/nodes/"+u, `{"action":"create"}`); code != 400 {
			t.Errorf("tạo được tài khoản trùng tài khoản dịch vụ %q: code=%d", u, code)
		}
	}
	for _, bad := range []string{"node-", "node--a", "node-a-", "node-Cafe", strings.Repeat("node-a", 6)} {
		if code, _ := req("POST", "/api/nodes/"+bad, `{"action":"create"}`); code != 400 {
			t.Errorf("tên %q: code=%d, muốn 400", bad, code)
		}
	}
	if len(f.sent) != 0 {
		t.Fatalf("tên sai vẫn gửi lệnh cho plugin: %v", f.sent)
	}
	n := 0
	for b := 0; b < 128; b++ {
		name := "node-a" + string(rune(b)) + "b"
		ok := b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-'
		if validNode(name) != ok {
			t.Errorf("validNode(%q) = %v, muốn %v", name, !ok, ok)
		}
		n++
	}
	if n != 128 {
		t.Fatalf("quét %d ký tự, muốn 128", n)
	}
}

// Danh sách node: lấy từ config.yaml (khu, thiết bị hồng ngoại), từ tin node gửi (cả node chưa khai báo) và từ plugin.
// hubd đăng ký topic chung của mọi node bằng wildcard; topic cụ thể trùng wildcard không đăng ký lại (tin đến hai lần).
func TestNodeListFromConfigAndTraffic(t *testing.T) {
	cfg := Config{
		Entities: []Entity{{ID: "cafe.co2", Zone: "cafe", Topic: "node-cafe/sensor/co2/state"}, {ID: "ro.tds", Zone: "cafe", Topic: "node-ro/sensor/tds_sau_loc/state"}},
		Devices: []Device{{ID: "tv_cafe", Zone: "cafe", Kind: "tv", CommandTopic: "node-cafe/ir/send", OnlineTopic: "node-cafe/status"},
			{ID: "den", Kind: "light", CommandTopic: "tuya/x/cmd"}},
	}
	h, req, _ := login(t, cfg)
	h.Handle("node-cafe/status", "online", t0)
	h.Handle("node-cafe/sensor/wifi/state", "-61", t0.Add(time.Minute))
	h.Handle("node-cafe/status", "online", t0.Add(2*time.Minute)) // trạng thái không tính là lần gửi số đo
	h.Handle("node-moi/status", "online", t0)
	h.Handle("obk-quat/status", "online", t0) // không phải node ESPHome

	_, body := req("GET", "/api/nodes", "")
	cafe := nodeOf(t, body, "node-cafe")
	if cafe == nil || cafe["zone"] != "cafe" || cafe["online"] != true || cafe["wifi"] != "-61" ||
		cafe["last"] != float64(t0.Add(time.Minute).Unix()) || fmt.Sprint(cafe["ir"]) != "[tv_cafe]" {
		t.Fatalf("node-cafe = %v", cafe)
	}
	if ro := nodeOf(t, body, "node-ro"); ro == nil || ro["online"] != false || ro["last"] != float64(0) || ro["zone"] != "cafe" {
		t.Fatalf("node-ro (đã khai báo, chưa gửi gì) = %v", ro)
	}
	if nodeOf(t, body, "node-moi") == nil || nodeOf(t, body, "obk-quat") != nil {
		t.Fatalf("node mới chưa khai báo phải hiện, thiết bị khác thì không: %s", body)
	}
	if v := h.Snapshot()["online:tv_cafe"]; v != "online" {
		t.Fatalf("topic trạng thái vẫn phải tới thiết bị dùng nó: online:tv_cafe = %q", v)
	}
	if h.Snapshot()["node:node-cafe"] != "online" {
		t.Fatal("trạng thái node phải lên giao diện qua SSE (node:<tên>)")
	}
	if p, _ := h.db.Query("node:node-cafe", t0.Add(-time.Hour), t0.Add(time.Hour), 10); len(p) != 0 {
		t.Fatalf("trạng thái node không được vào lịch sử: %v", p)
	}

	topics := h.Topics()
	for _, want := range []string{"+/status", "+/sensor/wifi/state", "+/ir/learned", "node-cafe/sensor/co2/state"} {
		if !slices.Contains(topics, want) {
			t.Errorf("Topics() thiếu %s: %v", want, topics)
		}
	}
	if slices.Contains(topics, "node-cafe/status") {
		t.Errorf("node-cafe/status trùng +/status, đăng ký hai lần thì mỗi tin đến hai lần: %v", topics)
	}

	f := newFakeDynsec()
	f.clients["node-ro"] = &fakeClient{online: true}
	f.clients["node-kho"] = &fakeClient{disabled: true}
	f.clients["hubd-admin"] = &fakeClient{}
	h.dynsec = f.call
	_, body = req("GET", "/api/nodes", "")
	if ro := nodeOf(t, body, "node-ro"); ro["account"] != "active" || ro["online"] != true {
		t.Fatalf("node-ro đang nối broker = %v", ro)
	}
	if k := nodeOf(t, body, "node-kho"); k == nil || k["account"] != "disabled" {
		t.Fatalf("node chỉ có trong plugin phải hiện: %s", body)
	}
	if nodeOf(t, body, "hubd-admin") != nil || nodeOf(t, body, "node-cafe")["account"] != "none" {
		t.Fatalf("chỉ liệt kê tài khoản node-*, node chưa có tài khoản ghi none: %s", body)
	}
}

// Học mã hồng ngoại từ dashboard: node gửi mã vừa thu lên <node>/ir/learned, giao diện thấy ngay (irlearn:<node>);
// gán vào nút của thiết bị bằng nhóm cấu hình nodes (ir:<thiết bị>:<nút>), đè lên codes trong config.yaml.
// Chỉ phát chuỗi Pronto: nhóm cấu hình nhận gì cũng lưu, không để chuỗi tuỳ ý ra node.
func TestLearnedIRCodesFromSettings(t *testing.T) {
	tv := Device{ID: "tv_cafe", Kind: "tv", CommandTopic: "node-cafe/ir/send", Codes: map[string]string{"vol_up": "0000 006D 0001 0001", "mute": "0000 0000 0001"}}
	h, req, pub := login(t, Config{Devices: []Device{tv, {ID: "ml", Kind: "ac", CommandTopic: "node-cafe/ir/ac"}}})
	h.Handle("node-cafe/ir/learned", "0000 006D 0022 0000 0158 00AC ", t0)
	if v := h.Snapshot()["irlearn:node-cafe"]; v != "0000 006D 0022 0000 0158 00AC " {
		t.Fatalf("irlearn:node-cafe = %q", v)
	}
	if p, _ := h.db.Query("irlearn:node-cafe", t0.Add(-time.Hour), t0.Add(time.Hour), 10); len(p) != 0 {
		t.Fatal("mã học không được vào lịch sử")
	}
	if h.Command("tv_cafe", "power") {
		t.Fatal("nút chưa học mà gửi được")
	}
	req("PUT", "/api/settings/nodes", `{"ir:tv_cafe:power":"0000 006D 0022 0000 0158 00AC ","ir:tv_cafe:mute":"0000 006D 0002 0002","ir:tv_cafe:ok":"rm -rf /","ir:ml:on":"0000 006D 0003 0003"}`)
	*pub = nil
	for _, c := range []string{"power", "vol_up", "mute", "ok"} {
		h.Command("tv_cafe", c)
	}
	h.Command("ml", "temp_24") // máy lạnh vẫn đi climate_ir khi không có codes trong config.yaml
	want := []string{"node-cafe/ir/send 0000 006D 0022 0000 0158 00AC", "node-cafe/ir/send 0000 006D 0001 0001", "node-cafe/ir/send 0000 006D 0002 0002", "node-cafe/ir/ac temp_24"}
	if !slices.Equal(*pub, want) {
		t.Fatalf("lệnh gửi = %q\nmuốn %q", *pub, want)
	}
}

// Phản hồi của plugin đi chung một topic cho mọi người quản trị (cả mosquitto_ctrl): chỉ nhận đúng phản hồi của mình.
func TestDynsecResponseMatch(t *testing.T) {
	if _, ok := dynMatch([]byte(`{"responses":[{"command":"listClients","correlationData":"cua-nguoi-khac"}]}`), "cua-minh"); ok {
		t.Fatal("nhận nhầm phản hồi của người khác")
	}
	for _, bad := range []string{``, `nope`, `{"responses":[]}`} {
		if _, ok := dynMatch([]byte(bad), "cua-minh"); ok {
			t.Fatalf("nhận phản hồi hỏng %q", bad)
		}
	}
	r, ok := dynMatch([]byte(`{"responses":[{"command":"createRole","correlationData":"cua-minh"},{"command":"createClient","error":"Client already exists","correlationData":"cua-minh"}]}`), "cua-minh")
	if !ok || len(r) != 2 || r[1].Error != "Client already exists" {
		t.Fatalf("phản hồi = %+v %v", r, ok)
	}
}

// Hãng máy lạnh chọn trên trang Cài đặt là ac_platform của ESPHome: phải đúng danh sách ở đầu firmware/ir.yaml,
// gõ sai một chữ thì lệnh nạp firmware dashboard đưa ra sẽ lỗi. Quét hai chiều: cả danh sách của ir.yaml.
func TestACPlatformsMatchFirmware(t *testing.T) {
	irYaml, err := os.ReadFile("firmware/ir.yaml")
	if err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile("web/setup-nodes.js")
	if err != nil {
		t.Fatal(err)
	}
	fw := map[string]bool{}
	if m := regexp.MustCompile(`(?s)từng phòng\):(.*?)\.\s`).FindSubmatch(irYaml); m != nil {
		for _, p := range strings.Split(string(regexp.MustCompile(`\([^)]*\)|#`).ReplaceAll(m[1], nil)), ",") {
			if p = strings.TrimSpace(p); !regexp.MustCompile(`^[a-z][a-z0-9_]+$`).MatchString(p) {
				t.Fatalf("danh sách ac_platform của ir.yaml có mục lạ %q", p)
			}
			fw[p] = true
		}
	}
	acList := regexp.MustCompile(`(?s)const AC = \[(.*?)\];`).FindSubmatch(js)
	if len(fw) < 10 || acList == nil {
		t.Fatalf("không đọc được danh sách hãng: ir.yaml %v, setup-nodes.js có AC=%v", fw, acList != nil)
	}
	ui := map[string]bool{}
	for _, m := range regexp.MustCompile(`\['([a-z0-9_]*)', `).FindAllSubmatch(acList[1], -1) {
		if p := string(m[1]); p != "" {
			ui[p] = true
			if !fw[p] {
				t.Errorf("dashboard có hãng %q không nằm trong danh sách ac_platform của firmware/ir.yaml", p)
			}
		}
	}
	for p := range fw {
		if !ui[p] {
			t.Errorf("firmware/ir.yaml có %q mà dashboard không cho chọn", p)
		}
	}
}
