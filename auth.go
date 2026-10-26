package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Auth: PIN dùng chung. Đăng nhập đúng PIN -> token ngẫu nhiên lưu SQLite, gửi qua cookie HttpOnly.
// Pin rỗng = tắt xác thực (chỉ để dev; main sẽ cảnh báo).
type Auth struct {
	pin  string
	db   *DB
	mu   sync.Mutex
	fail int
	lock time.Time
	now  func() time.Time
}

const cookieName = "hub_token"

func NewAuth(pin string, db *DB) *Auth { return &Auth{pin: pin, db: db, now: time.Now} }

func (a *Auth) valid(r *http.Request) bool {
	if a.pin == "" {
		return true
	}
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}
	var n int
	a.db.sql.QueryRow(`SELECT COUNT(1) FROM tokens WHERE token=? AND created>?`,
		c.Value, a.now().Add(-90*24*time.Hour).Unix()).Scan(&n)
	return n == 1
}

// Require bọc handler cần đăng nhập.
func (a *Auth) Require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.valid(r) {
			http.Error(w, "cần đăng nhập", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var body struct{ PIN string }
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body) != nil {
		http.Error(w, "bad json", 400)
		return
	}
	a.mu.Lock()
	if a.now().Before(a.lock) {
		a.mu.Unlock()
		http.Error(w, "thử lại sau ít phút", http.StatusTooManyRequests)
		return
	}
	ok := a.pin != "" && subtle.ConstantTimeCompare([]byte(body.PIN), []byte(a.pin)) == 1
	if !ok {
		if a.fail++; a.fail >= 5 {
			a.fail, a.lock = 0, a.now().Add(time.Minute)
		}
		a.mu.Unlock()
		http.Error(w, "sai PIN", http.StatusUnauthorized)
		return
	}
	a.fail = 0
	a.mu.Unlock()
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	if _, err := a.db.sql.Exec(`INSERT INTO tokens(token,created) VALUES(?,?)`, tok, a.now().Unix()); err != nil {
		http.Error(w, "lỗi", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 90 * 24 * 3600})
	w.WriteHeader(204)
}
