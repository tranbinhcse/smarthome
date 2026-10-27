package main

import (
	"encoding/json"
	"net/http"
	"regexp"
)

// Cấu hình nhập từ trang Cài đặt (Tuya, AI, đầu ghi camera, Spotify...): mỗi nhóm một object JSON trong SQLite.
// Khoá có tên kết thúc bằng key/pass/password/token/secret là bí mật: API cho trình duyệt chỉ trả "<khoá>_set": true,
// không bao giờ trả giá trị. Lưu là gộp: chuỗi rỗng ở khoá bí mật = giữ giá trị cũ, null = xoá khoá.
var (
	settingNS  = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	secretKey  = regexp.MustCompile(`(?i)(key|pass|password|token|secret)$`)
	serviceNSs = map[string]bool{"ai": true} // nhóm mà dịch vụ dùng token trợ lý (voiced) được đọc đầy đủ
)

func (d *DB) Setting(ns string) map[string]any {
	out := map[string]any{}
	var raw string
	if err := d.sql.QueryRow(`SELECT json FROM settings WHERE ns = ?`, ns).Scan(&raw); err == nil {
		json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func (d *DB) PutSetting(ns string, patch map[string]any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	cur := d.Setting(ns)
	for k, v := range patch {
		switch {
		case v == nil:
			delete(cur, k)
		case v == "" && secretKey.MatchString(k):
			// ô mật khẩu để trống: giữ giá trị đã lưu
		default:
			cur[k] = v
		}
	}
	b, _ := json.Marshal(cur)
	_, err := d.sql.Exec(`INSERT INTO settings(ns, json) VALUES(?, ?) ON CONFLICT(ns) DO UPDATE SET json = excluded.json`, ns, string(b))
	return err
}

// masked: bản cho trình duyệt, bí mật thay bằng "<khoá>_set".
func masked(s map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range s {
		if secretKey.MatchString(k) {
			out[k+"_set"] = v != ""
			continue
		}
		out[k] = v
	}
	return out
}

func mountSettings(mux *http.ServeMux, h *Hub, a *Auth) {
	ns := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		n := r.PathValue("ns")
		if !settingNS.MatchString(n) {
			http.Error(w, "tên nhóm cấu hình không hợp lệ", 400)
			return "", false
		}
		return n, true
	}
	mux.HandleFunc("GET /api/settings/{ns}", a.Require(func(w http.ResponseWriter, r *http.Request) {
		if n, ok := ns(w, r); ok {
			writeJSON(w, masked(h.db.Setting(n)))
		}
	}))
	mux.HandleFunc("PUT /api/settings/{ns}", a.Require(func(w http.ResponseWriter, r *http.Request) {
		n, ok := ns(w, r)
		if !ok {
			return
		}
		var patch map[string]any
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&patch); err != nil || patch == nil {
			http.Error(w, "cần một object JSON", 400)
			return
		}
		if err := h.db.PutSetting(n, patch); err != nil {
			http.Error(w, "không lưu được", 500)
			return
		}
		if n == "nvr" {
			syncCamGen(h.db) // kênh camera đổi: ghi file sinh, hubd tự khởi động lại để nạp (camera.go)
		}
		h.publish("hubd/settings/"+n, "changed") // dịch vụ đọc lại qua API; tin này không kèm giá trị
		if n == "devices" {                      // sắp xếp thiết bị vào phòng: áp lúc khởi động (main.go), nên nạp lại
			go restartHub()
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /api/assistant/settings/{ns}", a.Assistant(func(w http.ResponseWriter, r *http.Request) {
		n, ok := ns(w, r)
		if !ok {
			return
		}
		if !serviceNSs[n] {
			http.Error(w, "token trợ lý không được đọc nhóm này", 403)
			return
		}
		writeJSON(w, h.db.Setting(n))
	}))
}
