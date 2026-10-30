package main

import (
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

// Ổ ghi hình: chỉ ghi khi thư mục nằm trên thiết bị khác ổ hệ thống (file cấu hình), cùng API dung lượng.

const (
	recNoDir   = "Chưa gắn thư mục ghi hình"
	recOnSys   = "Thư mục ghi hình nằm trên ổ hệ thống: gắn ổ cứng riêng vào /srv/hubd-video"
	recNoWrite = "Không ghi được vào ổ ghi hình"
)

func recDiskCheck(dir, sysRef string) recDisk {
	var d, s syscall.Stat_t
	if dir == "" || syscall.Stat(dir, &d) != nil {
		return recDisk{Reason: recNoDir}
	}
	if syscall.Stat(sysRef, &s) == nil && d.Dev == s.Dev {
		return recDisk{Reason: recOnSys}
	}
	var fs syscall.Statfs_t
	test := filepath.Join(dir, ".hubd-write-test")
	if err := os.WriteFile(test, []byte("x"), 0o600); err != nil || os.Remove(test) != nil || syscall.Statfs(dir, &fs) != nil {
		return recDisk{Reason: recNoWrite}
	}
	return recDisk{OK: true, Total: int64(fs.Blocks) * int64(fs.Bsize), Free: int64(fs.Bavail) * int64(fs.Bsize)}
}

// recNormalPct: phần trăm ổ cho file thường, nhóm camera.rec_max_pct, ép vào 50–95, mặc định 80.
func recNormalPct(db *DB) int {
	v, ok := db.Setting("camera")["rec_max_pct"].(float64)
	if !ok {
		return 80
	}
	return int(math.Max(50, math.Min(95, v)))
}

type recCamUse struct {
	ID    string   `json:"id"`
	Bytes int64    `json:"bytes"`
	Days  *float64 `json:"days"`
}

// mountRecStorage: GET /api/rec/storage. disk() trả tình trạng ổ ở thời điểm gọi.
func mountRecStorage(mux *http.ServeMux, db *DB, a *Auth, disk func() recDisk) {
	mux.HandleFunc("GET /api/rec/storage", a.Require(func(w http.ResponseWriter, r *http.Request) {
		files, err := recAll(db)
		if err != nil {
			http.Error(w, "không đọc được chỉ mục ghi hình", 500)
			return
		}
		d, pct := disk(), recNormalPct(db)
		var normal, held int64
		bytes, rate := map[string]int64{}, map[string]int64{}
		cut := time.Now().Add(-24 * time.Hour)
		for _, f := range files {
			if f.Held {
				held += f.Bytes
			} else {
				normal += f.Bytes
			}
			bytes[f.Cam] += f.Bytes
			if f.End.After(cut) {
				rate[f.Cam] += f.Bytes
			}
		}
		pol := camPolicies(db)
		for id := range pol {
			bytes[id] += 0 // camera nối thẳng chưa có file vẫn được liệt kê
		}
		var sum int64
		for _, v := range rate {
			sum += v
		}
		cams := []recCamUse{}
		for id, b := range bytes {
			u := recCamUse{ID: id, Bytes: b}
			if rt := rate[id]; rt > 0 {
				budget := float64(pol[id].LimitBytes)
				if budget <= 0 {
					budget = float64(d.Total) * float64(pct) / 100 * float64(rt) / float64(sum)
				}
				days := math.Round(budget/float64(rt)*10) / 10
				u.Days = &days
			}
			cams = append(cams, u)
		}
		sort.Slice(cams, func(i, j int) bool { return cams[i].ID < cams[j].ID })
		writeJSON(w, map[string]any{"ok": d.OK, "reason": d.Reason, "total": d.Total, "free": d.Free,
			"normal_bytes": normal, "held_bytes": held, "max_pct": pct, "cams": cams})
	}))
}
