package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Trợ lý giọng nói (voiced, thiết kế mục 15.2) gọi hubd bằng "Authorization: Bearer <HUBD_ASSISTANT_TOKEN>",
// chỉ qua các route /api/assistant/* dưới đây; route của người dùng không nhận token này.
// Quyền bằng người chưa nhập PIN: đọc trạng thái/lịch sử, bật tắt thiết bị thường, chạy kịch bản thường,
// xác nhận cảnh báo. Thiết bị pin/protected và kịch bản đụng tới chúng bị chặn tại đây, không dựa vào prompt.

var cmdStates = []string{"on", "off"}

// Alert: cảnh báo do luật phát ra (notify), giữ tới khi có người xác nhận.
type Alert struct {
	Level string `json:"level"`
	Text  string `json:"text"`
	At    int64  `json:"at"`
}

// locked: lệnh tới thiết bị này cần PIN.
func (d Device) locked() bool { return d.Pin || d.Protected }

func (h *Hub) sceneLocked(s Scene) bool {
	return s.Pin || slices.ContainsFunc(s.Steps, func(st Step) bool { return h.devByID[st.D].locked() })
}

func (h *Hub) scene(id string) (Scene, bool) {
	i := slices.IndexFunc(h.cfg.Scenes, func(s Scene) bool { return s.ID == id })
	if i < 0 {
		return Scene{}, false
	}
	return h.cfg.Scenes[i], true
}

// RunScene gửi lần lượt các bước, trả số bước không gửi được.
func (h *Hub) RunScene(s Scene) (failed int) {
	for _, st := range s.Steps {
		if !slices.Contains(cmdStates, st.To) || !h.Command(st.D, st.To) {
			failed++
		}
	}
	return failed
}

// alert: publish hubd/alert cho phần mở rộng (voiced đọc thành tiếng), giữ tới khi có người xác nhận.
func (h *Hub) alert(level, text string) {
	if level == "" {
		level = "info"
	}
	b, _ := json.Marshal(map[string]string{"level": level, "text": text})
	h.publish("hubd/alert", string(b))
	h.mu.Lock()
	h.alerts = append(h.alerts, Alert{level, text, time.Now().Unix()})
	if len(h.alerts) > 20 { // ponytail: giữ 20 cảnh báo mới nhất trong RAM; lưu SQLite khi làm Telegram/gửi bù
		h.alerts = h.alerts[len(h.alerts)-20:]
	}
	h.mu.Unlock()
}

func (h *Hub) Alerts() []Alert {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]Alert{}, h.alerts...)
}

// AckAlerts: có người xác nhận (tắt còi/thôi đọc). Không cần PIN.
func (h *Hub) AckAlerts() {
	h.mu.Lock()
	h.alerts = nil
	h.mu.Unlock()
	h.publish("hubd/alert/ack", "{}")
}

func (a *Auth) isAssistant(r *http.Request) bool {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && a.assistant != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(a.assistant)) == 1
}

// Assistant bọc handler chỉ dành cho token trợ lý.
func (a *Auth) Assistant(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.isAssistant(r) {
			http.Error(w, "cần token trợ lý", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func mountAssistant(mux *http.ServeMux, h *Hub, a *Auth) {
	mux.HandleFunc("GET /api/assistant/config", a.Assistant(func(w http.ResponseWriter, r *http.Request) {
		locked, phrases := []string{}, []string{}
		for _, d := range h.cfg.Devices {
			if d.locked() {
				locked = append(locked, d.ID)
			}
		}
		for _, s := range h.cfg.Scenes {
			if h.sceneLocked(s) {
				locked = append(locked, s.ID)
			}
		}
		for _, ru := range h.cfg.Rules {
			for _, act := range slices.Concat(ru.Then, ru.OnClear) {
				if act.Notify != "" && !slices.Contains(phrases, act.Notify) {
					phrases = append(phrases, act.Notify)
				}
			}
		}
		writeJSON(w, map[string]any{"zones": h.cfg.Zones, "entities": h.cfg.Entities, "devices": h.cfg.Devices,
			"scenes": h.cfg.Scenes, "locked": locked, "phrases": phrases})
	}))
	mux.HandleFunc("GET /api/assistant/state", a.Assistant(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"values": h.Snapshot(), "alerts": h.Alerts()})
	}))
	mux.HandleFunc("GET /api/assistant/history", a.Assistant(func(w http.ResponseWriter, r *http.Request) {
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		if hours < 1 || hours > 24*180 {
			hours = 24
		}
		now := time.Now()
		pts, err := h.db.Query(r.URL.Query().Get("id"), now.Add(-time.Duration(hours)*time.Hour), now, 300)
		if err != nil {
			http.Error(w, "lỗi", 500)
			return
		}
		writeJSON(w, pts)
	}))
	mux.HandleFunc("POST /api/assistant/cmd", a.Assistant(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Device, To string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || !slices.Contains(cmdStates, body.To) {
			http.Error(w, "lệnh không hợp lệ", 400)
			return
		}
		d, ok := h.devByID[body.Device]
		switch {
		case !ok:
			http.Error(w, "không có thiết bị này", 404)
		case d.locked():
			http.Error(w, "thiết bị cần PIN", http.StatusForbidden)
		case !h.Command(d.ID, body.To):
			http.Error(w, "thiết bị không nhận lệnh", 400)
		default:
			w.WriteHeader(204)
		}
	}))
	mux.HandleFunc("POST /api/assistant/scene/{id}", a.Assistant(func(w http.ResponseWriter, r *http.Request) {
		s, ok := h.scene(r.PathValue("id"))
		switch {
		case !ok:
			http.Error(w, "không có kịch bản này", 404)
		case h.sceneLocked(s):
			http.Error(w, "kịch bản cần PIN", http.StatusForbidden)
		default:
			if n := h.RunScene(s); n > 0 {
				http.Error(w, fmt.Sprintf("%d bước không gửi được", n), http.StatusBadGateway)
				return
			}
			w.WriteHeader(204)
		}
	}))
	mux.HandleFunc("POST /api/assistant/alarm/ack", a.Assistant(func(w http.ResponseWriter, r *http.Request) {
		h.AckAlerts()
		w.WriteHeader(204)
	}))

	// Nút nói trên dashboard: /voice/* chuyển sang voiced (cùng origin, dùng PIN của dashboard).
	// voiced không nhận cookie người dùng; nó tự gọi lại hubd bằng token trợ lý.
	// Chỉ chuyển tiếp khi trợ lý bật (có token): tắt thì voiced có thể không chạy. /voice/enabled (không cần PIN)
	// cho dashboard biết để ẩn nút nói.
	u, err := url.Parse(os.Getenv("HUBD_VOICED_URL"))
	on := err == nil && u.Host != "" && a.assistant != ""
	mux.HandleFunc("GET /voice/enabled", func(w http.ResponseWriter, r *http.Request) {
		if !on {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if on {
		p := httputil.NewSingleHostReverseProxy(u)
		dir := p.Director
		p.Director = func(r *http.Request) { dir(r); r.Header.Del("Cookie") }
		mux.Handle("/voice/", a.Require(p.ServeHTTP))
	}
}
