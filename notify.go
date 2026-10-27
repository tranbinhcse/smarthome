package main

import (
	"net/http"
	"slices"
)

// Loại thông báo: notify trong luật có `kind` (co2, bui, mua, nong...). Trang Cài đặt tắt từng loại bằng nhóm cấu hình
// "notify" ({"<kind>": false}); loại bị tắt không lên hubd/alert (trợ lý không đọc), lệnh tới thiết bị của luật vẫn chạy.
// notify không có kind (khói, rò nước...) luôn báo.

func (h *Hub) muted(kind string) bool {
	return kind != "" && h.db.Setting("notify")[kind] == false
}

type notifyKind struct {
	Kind  string   `json:"kind"`
	Texts []string `json:"texts"`
}

func mountNotify(mux *http.ServeMux, h *Hub, a *Auth) {
	mux.HandleFunc("GET /api/notify", a.Require(func(w http.ResponseWriter, r *http.Request) {
		out := []notifyKind{}
		for _, ru := range h.cfg.Rules {
			for _, act := range slices.Concat(ru.Then, ru.OnClear) {
				if act.Notify == "" || act.Kind == "" {
					continue
				}
				i := slices.IndexFunc(out, func(k notifyKind) bool { return k.Kind == act.Kind })
				if i < 0 {
					out, i = append(out, notifyKind{Kind: act.Kind}), len(out)
				}
				out[i].Texts = append(out[i].Texts, act.Notify)
			}
		}
		writeJSON(w, out)
	}))
}
