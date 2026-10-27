package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

// Cấu hình nhập từ trang Cài đặt (Tuya, AI, đầu ghi camera, Spotify...): mỗi nhóm một object JSON trong SQLite.
// Khoá có tên kết thúc bằng key/pass/password/token/secret là bí mật: API cho trình duyệt chỉ trả "<khoá>_set": true,
// không bao giờ trả giá trị. Lưu là gộp: chuỗi rỗng ở khoá bí mật = giữ giá trị cũ (trừ khi đổi url), null = xoá khoá.
var (
	settingNS  = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	secretKey  = regexp.MustCompile(`(?i)(key|pass|password|token|secret)$`)
	serviceNSs = map[string]bool{"ai": true, "voice": true} // nhóm mà dịch vụ dùng token trợ lý (voiced) được đọc đầy đủ
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
	if u, ok := patch["url"]; ok && u != cur["url"] {
		// Bí mật gắn với url lưu cùng nó: đổi url mà không nhập lại thì xoá, không gửi khoá cũ tới máy chủ khác.
		for k := range cur {
			if v, _ := patch[k].(string); secretKey.MatchString(k) && v == "" {
				delete(cur, k)
			}
		}
	}
	for k, v := range patch {
		switch {
		case v == nil:
			delete(cur, k)
		case v == "" && secretKey.MatchString(k):
			// ô mật khẩu để trống: giữ giá trị đã lưu
		default:
			// object (vd. từng thiết bị trong "devices") trộn theo trường: chỉ gửi trường vừa đổi không làm mất
			// trường đã lưu trước; null xoá đúng trường đó
			o, ok1 := cur[k].(map[string]any)
			p, ok2 := v.(map[string]any)
			if !ok1 || !ok2 {
				cur[k] = v
				continue
			}
			for f, fv := range p {
				if fv == nil {
					delete(o, f)
				} else {
					o[f] = fv
				}
			}
		}
	}
	b, _ := json.Marshal(cur)
	_, err := d.sql.Exec(`INSERT INTO settings(ns, json) VALUES(?, ?) ON CONFLICT(ns) DO UPDATE SET json = excluded.json`, ns, string(b))
	return err
}

// putSetting lưu một nhóm như PUT /api/settings/{ns}: đầu ghi đổi IP thì bỏ mật khẩu cũ và ghi lại file camera sinh,
// báo dịch vụ đọc lại qua MQTT (tin không kèm giá trị). Trả true khi nhóm đó chỉ áp lúc khởi động (thiết bị, phòng).
func putSetting(h *Hub, n string, patch map[string]any) (restart bool, err error) {
	if n == "nvr" {
		nvrPatch(h.db, patch)
	}
	if err := h.db.PutSetting(n, patch); err != nil {
		return false, err
	}
	if n == "nvr" {
		syncCamGen(h.db)
	}
	if n == "soloist" && h.music != nil {
		h.music.spot.restart() // khoá API Soloist vừa đổi: chạy lại với khoá mới (xoá khoá thì dừng)
	}
	h.publish("hubd/settings/"+n, "changed")
	return n == "devices" || n == "zones", nil
}

// SettingNSs: tên mọi nhóm cài đặt đã lưu.
func (d *DB) SettingNSs() []string {
	var out []string
	rows, err := d.sql.Query(`SELECT ns FROM settings ORDER BY ns`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

func withoutSecrets(s map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range s {
		if !secretKey.MatchString(k) {
			out[k] = v
		}
	}
	return out
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
		restart, err := putSetting(h, n, patch)
		if err != nil {
			http.Error(w, "không lưu được", 500)
			return
		}
		if restart {
			go restartHub()
		}
		w.WriteHeader(204)
	}))
	// Sao lưu: mọi nhóm cài đặt, bỏ hẳn trường bí mật (khoá API, mật khẩu, token) nên tệp tải về không chứa bí mật.
	mux.HandleFunc("GET /api/backup", a.Require(func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{}
		for _, n := range h.db.SettingNSs() {
			out[n] = withoutSecrets(h.db.Setting(n))
		}
		now := time.Now()
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="inix-home-%s.json"`, now.Format("20060102-1504")))
		writeJSON(w, map[string]any{"app": "iNIX Home", "version": 1, "at": now.Unix(), "settings": out})
	}))
	// Khôi phục: ghi lại từng nhóm như lưu tay (trộn theo trường, bí mật đang có giữ nguyên), rồi nạp lại hubd.
	mux.HandleFunc("POST /api/restore", a.Require(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Settings map[string]map[string]any `json:"settings"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&b) != nil || len(b.Settings) == 0 {
			http.Error(w, "không phải tệp sao lưu iNIX Home", 400)
			return
		}
		for n := range b.Settings {
			if !settingNS.MatchString(n) {
				http.Error(w, "tệp sao lưu có nhóm cài đặt lạ: "+n, 400)
				return
			}
		}
		for n, s := range b.Settings {
			if _, err := putSetting(h, n, withoutSecrets(s)); err != nil {
				http.Error(w, "không lưu được", 500)
				return
			}
		}
		go restartHub()
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
