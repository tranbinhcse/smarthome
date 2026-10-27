package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Node ESP32 trên trang Cài đặt: danh sách (khu, online, lần gửi cuối) và tài khoản MQTT riêng từng node.
//
// Tài khoản node nằm trong plugin dynamic security của Mosquitto (mosquitto/start.sh), hubd gửi lệnh qua MQTT bằng
// tài khoản quản trị riêng (HUBD_MQTT_ADMIN_USER/PASS). Tài khoản dịch vụ (hubd, tuya...) vẫn ở passwd như cũ.
// Mật khẩu node do hubd sinh, trả về đúng một lần cho trình duyệt; hubd không lưu (plugin chỉ giữ bản băm).

type dynCmd = map[string]any

type dynResp struct {
	Command string          `json:"command"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Corr    string          `json:"correlationData,omitempty"`
}

type nodeSeen struct {
	last time.Time // tin cuối node tự gửi (không tính status: LWT "offline" do broker gửi thay)
	wifi string    // sóng Wi-Fi dBm, common.yaml gửi mỗi 60 giây nên cũng là nhịp sống
}

// Topic chung của mọi node ESPHome (firmware/common.yaml, ir.yaml). Đăng ký wildcard để thấy cả node chưa khai báo
// trong config.yaml. MQTT không cho wildcard giữa chữ (node-+), nên lọc tên node-<khu> trong nodeMsg.
var nodeSubs = []string{"+/status", "+/sensor/wifi/state", "+/ir/learned"}

// Tên node = tài khoản MQTT = tiền tố topic (firmware/common.yaml). Chỉ chữ thường, số, gạch ngang; tối đa 31 ký tự
// (giới hạn tên ESPHome). Tiền tố node- bắt buộc: plugin xét trước passwd, trùng tên tài khoản dịch vụ là chiếm mất.
var nodeNameRe = regexp.MustCompile(`^node-[a-z0-9]+(-[a-z0-9]+)*$`)

func validNode(n string) bool { return len(n) <= 31 && nodeNameRe.MatchString(n) }

func topicNode(topic string) string {
	if n, _, _ := strings.Cut(topic, "/"); validNode(n) {
		return n
	}
	return ""
}

// topicMatch: khớp topic với filter chỉ có '+' (đủ cho nodeSubs).
func topicMatch(filter, topic string) bool {
	f, t := strings.Split(filter, "/"), strings.Split(topic, "/")
	if len(f) != len(t) {
		return false
	}
	for i := range f {
		if f[i] != "+" && f[i] != t[i] {
			return false
		}
	}
	return true
}

// nodeMsg: ghi lần gửi cuối của node; trạng thái kết nối và mã hồng ngoại vừa học lên giao diện (SSE), không vào lịch sử.
func (h *Hub) nodeMsg(topic, payload string, now time.Time) {
	name := topicNode(topic)
	if name == "" {
		return
	}
	rest := topic[len(name)+1:]
	if rest != "status" {
		h.mu.Lock()
		n := h.nodes[name]
		if n == nil {
			n = &nodeSeen{}
			h.nodes[name] = n
		}
		n.last = now
		if rest == "sensor/wifi/state" {
			n.wifi = payload
		}
		h.mu.Unlock()
	}
	switch rest {
	case "status":
		h.set("node:"+name, payload)
	case "ir/learned":
		h.set("irlearn:"+name, payload)
	}
}

type nodeView struct {
	Name    string   `json:"name"`
	Zone    string   `json:"zone"`
	Online  bool     `json:"online"`
	Last    int64    `json:"last"` // unix giây, 0 = chưa thấy
	Wifi    string   `json:"wifi"`
	Account string   `json:"account"` // active | disabled | none; rỗng = không hỏi được plugin
	IR      []string `json:"ir"`      // thiết bị hồng ngoại phát qua node này
}

func (h *Hub) nodeList() map[string]any {
	by := map[string]*nodeView{}
	get := func(n string) *nodeView {
		if by[n] == nil {
			by[n] = &nodeView{Name: n, IR: []string{}}
		}
		return by[n]
	}
	for _, e := range h.cfg.Entities {
		if n := topicNode(e.Topic); n != "" && get(n).Zone == "" {
			by[n].Zone = e.Zone
		}
	}
	for _, d := range h.cfg.Devices {
		n := topicNode(d.CommandTopic)
		if n == "" {
			continue
		}
		if v := get(n); irKinds[d.Kind] {
			v.IR = append(v.IR, d.ID)
		}
		if by[n].Zone == "" {
			by[n].Zone = d.Zone
		}
	}
	h.mu.RLock()
	for n, s := range h.nodes {
		v := get(n)
		v.Last, v.Wifi = s.last.Unix(), s.wifi
	}
	for k, st := range h.val {
		if n, ok := strings.CutPrefix(k, "node:"); ok {
			get(n).Online = st == "online"
		}
	}
	h.mu.RUnlock()

	out := map[string]any{"manage": h.dynsec != nil}
	if h.dynsec != nil {
		if clients, err := h.dynClients(); err != nil {
			out["error"] = err.Error()
		} else {
			for _, v := range by {
				v.Account = "none"
			}
			for _, c := range clients {
				if !validNode(c.Username) {
					continue
				}
				v := get(c.Username)
				v.Account = map[bool]string{true: "disabled", false: "active"}[c.Disabled]
				v.Online = v.Online || len(c.Connections) > 0
			}
		}
	}
	list := make([]*nodeView, 0, len(by))
	for _, v := range by {
		list = append(list, v)
	}
	slices.SortFunc(list, func(a, b *nodeView) int { return strings.Compare(a.Name, b.Name) })
	out["nodes"] = list
	return out
}

type dynClient struct {
	Username    string `json:"username"`
	Disabled    bool   `json:"disabled"`
	Connections []any  `json:"connections"`
}

func (h *Hub) dynClients() ([]dynClient, error) {
	r, err := h.dynsec([]dynCmd{{"command": "listClients", "verbose": true}})
	if err != nil {
		return nil, err
	}
	if len(r) != 1 || r[0].Error != "" {
		return nil, fmt.Errorf("plugin không trả danh sách tài khoản: %+v", r)
	}
	var d struct{ Clients []dynClient }
	err = json.Unmarshal(r[0].Data, &d)
	return d.Clients, err
}

// nodeRole: node chỉ gửi/đăng ký topic của chính nó (và topic tìm IP của ESPHome). Chặn "#" rõ ràng ở mức ưu tiên
// thấp hơn, không dựa vào mặc định của plugin: tài khoản passwd có acl_file "pattern readwrite #" đứng sau plugin.
func nodeRole(name string) dynCmd {
	acl := func(typ, topic string, prio int, allow bool) map[string]any {
		return map[string]any{"acltype": typ, "topic": topic, "priority": prio, "allow": allow}
	}
	return dynCmd{"command": "createRole", "rolename": name, "acls": []map[string]any{
		acl("publishClientSend", name+"/#", 1, true), acl("subscribePattern", name+"/#", 1, true),
		acl("publishClientSend", "esphome/discover/"+name, 1, true), acl("subscribePattern", "esphome/discover", 1, true),
		acl("publishClientSend", "#", 0, false), acl("subscribePattern", "#", 0, false),
	}}
}

func mountNodes(mux *http.ServeMux, h *Hub, a *Auth) {
	mux.HandleFunc("GET /api/nodes", a.Require(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.nodeList())
	}))
	// Tài khoản MQTT của một node: create | reset (sinh mật khẩu mới, trả một lần) | disable | enable | delete.
	mux.HandleFunc("POST /api/nodes/{name}", a.Require(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var b struct{ Action string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&b)
		if !validNode(name) {
			http.Error(w, "tên node phải dạng node-<khu>: chữ thường không dấu, số, gạch ngang, tối đa 31 ký tự", 400)
			return
		}
		with := func(cmd string, kv ...any) dynCmd {
			c := dynCmd{"command": cmd, "username": name}
			for i := 0; i+1 < len(kv); i += 2 {
				c[kv[i].(string)] = kv[i+1]
			}
			return c
		}
		pw := ""
		var cmds []dynCmd
		switch b.Action {
		case "create":
			pw = rand.Text()
			cmds = []dynCmd{nodeRole(name), with("createClient", "password", pw, "roles", []map[string]any{{"rolename": name}})}
		case "reset":
			pw = rand.Text()
			cmds = []dynCmd{with("setClientPassword", "password", pw)}
		case "disable", "enable":
			cmds = []dynCmd{with(b.Action + "Client")}
		case "delete":
			cmds = []dynCmd{with("deleteClient"), {"command": "deleteRole", "rolename": name}}
		default:
			http.Error(w, "action là create, reset, disable, enable hoặc delete", 400)
			return
		}
		if h.dynsec == nil {
			http.Error(w, "chưa bật quản lý tài khoản MQTT: chạy scripts/init-secrets.sh (thêm HUBD_MQTT_ADMIN_PASS vào .env) rồi docker compose up -d", 503)
			return
		}
		resp, err := h.dynsec(cmds)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		for _, x := range resp {
			switch {
			case x.Error == "", x.Command == "createRole" && x.Error == "Role already exists", x.Command == "deleteRole" && x.Error == "Role not found":
			case x.Error == "Client already exists":
				http.Error(w, "đã có tài khoản "+name+", dùng Đổi mật khẩu nếu cần mật khẩu mới", 409)
				return
			case x.Error == "Client not found":
				http.Error(w, "chưa có tài khoản "+name, 404)
				return
			default:
				http.Error(w, "plugin báo lỗi: "+x.Error, 502)
				return
			}
		}
		log.Printf("tài khoản MQTT %s: %s", name, b.Action)
		if pw == "" {
			w.WriteHeader(204)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]string{"password": pw, "secret": "mqtt_pass_" + strings.ReplaceAll(name, "-", "_")})
	}))
}

const dynTopic = "$CONTROL/dynamic-security/v1"

// dynMatch: phản hồi của plugin đi chung một topic cho mọi người quản trị; chỉ nhận phản hồi mang mã của mình.
func dynMatch(payload []byte, corr string) ([]dynResp, bool) {
	var m struct{ Responses []dynResp }
	if json.Unmarshal(payload, &m) != nil || len(m.Responses) == 0 || m.Responses[0].Corr != corr {
		return nil, false
	}
	return m.Responses, true
}

// mqttDynsec: mỗi lô lệnh một kết nối ngắn bằng tài khoản quản trị (việc quản trị hiếm, khỏi giữ kết nối thứ hai).
func mqttDynsec(broker, user, pass string) func([]dynCmd) ([]dynResp, error) {
	var mu sync.Mutex
	const wait = 5 * time.Second
	return func(cmds []dynCmd) ([]dynResp, error) {
		mu.Lock()
		defer mu.Unlock()
		corr := rand.Text()
		for _, c := range cmds {
			c["correlationData"] = corr
		}
		c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID("hubd-admin-" + corr[:8]).
			SetUsername(user).SetPassword(pass).SetConnectTimeout(wait).SetAutoReconnect(false))
		ok := func(t mqtt.Token) error {
			if !t.WaitTimeout(wait) {
				return errors.New("hết giờ chờ broker")
			}
			return t.Error()
		}
		if err := ok(c.Connect()); err != nil {
			return nil, fmt.Errorf("không đăng nhập được tài khoản quản trị MQTT %s: %v", user, err)
		}
		defer c.Disconnect(100)
		got := make(chan []dynResp, 1)
		if err := ok(c.Subscribe(dynTopic+"/response", 1, func(_ mqtt.Client, m mqtt.Message) {
			if r, match := dynMatch(m.Payload(), corr); match {
				select {
				case got <- r:
				default:
				}
			}
		})); err != nil {
			return nil, err
		}
		req, _ := json.Marshal(map[string]any{"commands": cmds})
		if err := ok(c.Publish(dynTopic, 1, false, req)); err != nil {
			return nil, err
		}
		select {
		case r := <-got:
			return r, nil
		case <-time.After(wait):
			return nil, errors.New("plugin dynamic security không trả lời (Mosquitto chưa bật plugin? xem mosquitto/start.sh)")
		}
	}
}
