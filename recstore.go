package main

import (
	"sort"
	"strings"
	"time"
)

// Chỉ mục file ghi hình (bảng recordings, thời gian = mili giây Unix) và quy tắc giữ/xoá.

type recFile struct {
	ID         int64
	Cam, Path  string
	Start, End time.Time
	Bytes      int64
	Held       bool
}

type recWindow struct {
	From string `json:"from"` // "HH:MM"; To nhỏ hơn From = qua đêm
	To   string `json:"to"`
}

type recPolicy struct {
	Mode       string
	Windows    []recWindow
	LimitBytes int64
}

// Tập đóng các chế độ ghi; Task 5 kiểm hợp lệ và test quét sinh từ đây.
var recModes = []string{"always", "schedule", "off"}

type recDisk struct {
	OK          bool
	Reason      string
	Total, Free int64
}

type recDecision struct {
	Delete []int64
	Stop   bool
}

const recCols = `id, cam, path, start, end, bytes, held`

func recAdd(db *DB, f recFile) (int64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	r, err := db.sql.Exec(`INSERT INTO recordings(cam, path, start, end, bytes, held) VALUES(?,?,?,?,?,?)`,
		f.Cam, f.Path, f.Start.UnixMilli(), f.End.UnixMilli(), f.Bytes, f.Held)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func recQuery(db *DB, where string, args ...any) ([]recFile, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	rows, err := db.sql.Query(`SELECT `+recCols+` FROM recordings `+where+` ORDER BY start, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recFile{}
	for rows.Next() {
		var f recFile
		var s, e int64
		if err := rows.Scan(&f.ID, &f.Cam, &f.Path, &s, &e, &f.Bytes, &f.Held); err != nil {
			return nil, err
		}
		f.Start, f.End = time.UnixMilli(s), time.UnixMilli(e)
		out = append(out, f)
	}
	return out, rows.Err()
}

func recAll(db *DB) ([]recFile, error) { return recQuery(db, "") }

// recRange: file của camera có phần nằm trong [from, to].
func recRange(db *DB, cam string, from, to time.Time) ([]recFile, error) {
	return recQuery(db, `WHERE cam = ? AND start <= ? AND end >= ?`, cam, to.UnixMilli(), from.UnixMilli())
}

// recRemove chỉ xoá dòng; xoá file là việc của bên gọi.
func recRemove(db *DB, ids []int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	for _, id := range ids {
		if _, err := db.sql.Exec(`DELETE FROM recordings WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

func recHoldRange(db *DB, cam string, from, to time.Time) (int, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	r, err := db.sql.Exec(`UPDATE recordings SET held = 1 WHERE cam = ? AND start <= ? AND end >= ?`,
		cam, to.UnixMilli(), from.UnixMilli())
	if err != nil {
		return 0, err
	}
	n, err := r.RowsAffected()
	return int(n), err
}

// recMinutes: "HH:MM" -> số phút trong ngày.
func recMinutes(s string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// recWanted: file có nên giữ theo chế độ ghi. Chế độ lạ tính như always.
func recWanted(p recPolicy, f recFile) bool {
	switch p.Mode {
	case "off":
		return false
	case "schedule":
		s, e := f.Start.In(tgICT), f.End.In(tgICT)
		day := time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, tgICT).AddDate(0, 0, -1) // khung qua đêm của hôm trước
		for ; !day.After(e); day = day.AddDate(0, 0, 1) {
			for _, w := range p.Windows {
				a, ok1 := recMinutes(w.From)
				b, ok2 := recMinutes(w.To)
				if !ok1 || !ok2 {
					continue
				}
				ws := day.Add(time.Duration(a) * time.Minute)
				we := day.Add(time.Duration(b) * time.Minute)
				if b < a {
					we = we.AddDate(0, 0, 1)
				}
				if !s.After(we) && !e.Before(ws) {
					return true
				}
			}
		}
		return false
	}
	return true
}

// recPlan: quyết định xoá theo thứ tự 4 quy tắc; file held không bao giờ bị xoá.
func recPlan(files []recFile, pol map[string]recPolicy, d recDisk, normalPct int) recDecision {
	var dec recDecision
	if !d.OK {
		return dec
	}
	sorted := append([]recFile(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	newest := map[string]int64{} // cam -> ID file Start lớn nhất (kể cả held)
	for _, f := range sorted {
		newest[f.Cam] = f.ID
	}
	gone := map[int64]bool{}
	var freed int64
	del := func(f recFile) {
		gone[f.ID] = true
		freed += f.Bytes
		dec.Delete = append(dec.Delete, f.ID)
	}
	// oldest: file thường còn lại cũ nhất thoả cam ("" = mọi cam) và điều kiện bỏ file mới nhất.
	oldest := func(cam string, keepNewest bool) (recFile, bool) {
		for _, f := range sorted {
			if f.Held || gone[f.ID] || cam != "" && f.Cam != cam || keepNewest && newest[f.Cam] == f.ID {
				continue
			}
			return f, true
		}
		return recFile{}, false
	}
	normalSum := func(cam string) (n int64) {
		for _, f := range sorted {
			if !f.Held && !gone[f.ID] && (cam == "" || f.Cam == cam) {
				n += f.Bytes
			}
		}
		return
	}
	// 1. không còn được muốn
	cams := []string{}
	for c := range newest {
		cams = append(cams, c)
	}
	sort.Strings(cams)
	for _, f := range sorted {
		p, ok := pol[f.Cam]
		if !f.Held && newest[f.Cam] != f.ID && ok && !recWanted(p, f) {
			del(f)
		}
	}
	// 2. hạn mức từng camera
	for _, c := range cams {
		if lim := pol[c].LimitBytes; lim > 0 {
			for normalSum(c) > lim {
				f, ok := oldest(c, true)
				if !ok {
					break
				}
				del(f)
			}
		}
	}
	// 3. phần trăm ổ cho file thường
	for normalSum("")*100 > int64(normalPct)*d.Total {
		f, ok := oldest("", true)
		if !ok {
			break
		}
		del(f)
	}
	// 4. ổ gần đầy: xoá cả file mới nhất
	for (d.Free+freed)*100 < 2*d.Total {
		f, ok := oldest("", false)
		if !ok {
			dec.Stop = true
			break
		}
		del(f)
	}
	return dec
}
