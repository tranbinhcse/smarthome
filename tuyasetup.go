package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Quét QR Tuya từ trang Cài đặt: hubd chỉ chuyển lệnh sang tuya-bridge (tuya/setup/cmd), bridge đăng nhập
// Smart Life, gửi mã QR và tiến độ về tuya/setup/status (state "setup:tuya" trên dashboard qua SSE).
func mountTuyaSetup(mux *http.ServeMux, h *Hub, a *Auth) {
	mux.HandleFunc("POST /api/setup/tuya", a.Require(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Action   string `json:"action"`
			UserCode string `json:"user_code"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil {
			http.Error(w, "cần JSON {action, user_code}", 400)
			return
		}
		b.UserCode = strings.TrimSpace(b.UserCode)
		var cmd []byte
		switch {
		case b.Action == "start" && b.UserCode != "":
			if err := h.db.PutSetting("tuya", map[string]any{"user_code": b.UserCode}); err != nil {
				http.Error(w, "không lưu được user code", 500)
				return
			}
			cmd, _ = json.Marshal(map[string]string{"action": "start", "user_code": b.UserCode})
		case b.Action == "cancel":
			cmd = []byte(`{"action":"cancel"}`)
		default:
			http.Error(w, "action là start (kèm user_code) hoặc cancel", 400)
			return
		}
		h.publish("tuya/setup/cmd", string(cmd))
		w.WriteHeader(204)
	}))
}
