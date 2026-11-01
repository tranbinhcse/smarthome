package main

import (
	"database/sql"
	"sync"
	"time"

	_ "modernc.org/sqlite" // thuần Go, không CGO
)

// DB: một file SQLite (WAL) dùng chung cho lịch sử, token đăng nhập và cấu hình nhập từ Cài đặt.
type DB struct {
	sql *sql.DB
	mu  sync.Mutex
	lt  map[string]time.Time // lần ghi cuối theo id, để giảm tải
}

func OpenDB(path string) (*DB, error) {
	d, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	_, err = d.Exec(`
CREATE TABLE IF NOT EXISTS samples(ts INTEGER NOT NULL, id TEXT NOT NULL, v REAL NOT NULL);
CREATE INDEX IF NOT EXISTS samples_id_ts ON samples(id, ts);
CREATE TABLE IF NOT EXISTS tokens(token TEXT PRIMARY KEY, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS settings(ns TEXT PRIMARY KEY, json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS telegram_chats(id INTEGER PRIMARY KEY, name TEXT NOT NULL, added INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS cameras(id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS recordings(id INTEGER PRIMARY KEY, cam TEXT NOT NULL, path TEXT NOT NULL UNIQUE, start INTEGER NOT NULL, end INTEGER NOT NULL, bytes INTEGER NOT NULL, held INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS recordings_cam_start ON recordings(cam, start);
CREATE TABLE IF NOT EXISTS recording_gops(id INTEGER PRIMARY KEY, gops TEXT NOT NULL);`)
	if err != nil {
		return nil, err
	}
	// Chỉ mục GOP (JSON) ở bảng riêng để bảng recordings hẹp: lượt dọn file đọc cả bảng mỗi phút. Bản 2B đầu để chỉ mục
	// ở cột recordings.gops: còn cột đó thì chuyển sang bảng riêng rồi bỏ cột, trong một giao dịch.
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('recordings') WHERE name = 'gops'`).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		tx, err := d.Begin()
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`INSERT OR IGNORE INTO recording_gops(id, gops) SELECT id, gops FROM recordings WHERE gops IS NOT NULL;
ALTER TABLE recordings DROP COLUMN gops;`); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	return &DB{sql: d, lt: map[string]time.Time{}}, nil
}

// Record ghi một mẫu nhưng không dày hơn minGap cho mỗi id.
func (d *DB) Record(id string, v float64, now time.Time, minGap time.Duration) error {
	d.mu.Lock()
	if last, ok := d.lt[id]; ok && now.Sub(last) < minGap {
		d.mu.Unlock()
		return nil
	}
	d.lt[id] = now
	d.mu.Unlock()
	_, err := d.sql.Exec(`INSERT INTO samples(ts,id,v) VALUES(?,?,?)`, now.Unix(), id, v)
	return err
}

type Point struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

// Query trả về tối đa ~maxPoints điểm (trung bình theo khoảng) trong [since, now].
func (d *DB) Query(id string, since, now time.Time, maxPoints int) ([]Point, error) {
	bucket := int64(now.Sub(since).Seconds()) / int64(maxPoints)
	if bucket < 1 {
		bucket = 1
	}
	rows, err := d.sql.Query(`SELECT (ts/?)*?, AVG(v) FROM samples WHERE id=? AND ts>=? GROUP BY 1 ORDER BY 1`,
		bucket, bucket, id, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.T, &p.V); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) Prune(keep time.Duration, now time.Time) error {
	_, err := d.sql.Exec(`DELETE FROM samples WHERE ts<?`, now.Add(-keep).Unix())
	return err
}
