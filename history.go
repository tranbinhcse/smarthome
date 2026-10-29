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
CREATE TABLE IF NOT EXISTS telegram_chats(id INTEGER PRIMARY KEY, name TEXT NOT NULL, added INTEGER NOT NULL);`)
	if err != nil {
		return nil, err
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
