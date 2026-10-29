package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
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

	// token Bearer của trợ lý giọng nói (HUBD_ASSISTANT_TOKEN), rỗng = tắt. Xem assistant.go.
	assistant string
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

// CheckPIN so PIN nhập lại (đăng nhập, duyệt người nhận Telegram, tắt còi) và dùng chung bộ đếm khoá:
// sai 5 lần thì khoá 1 phút; đang khoá thì không so, trả locked=true.
func (a *Auth) CheckPIN(pin string) (ok bool, locked bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.now().Before(a.lock) {
		return false, true
	}
	if a.pin != "" && subtle.ConstantTimeCompare([]byte(pin), []byte(a.pin)) == 1 {
		a.fail = 0
		return true, false
	}
	if a.fail++; a.fail >= 5 {
		a.fail, a.lock = 0, a.now().Add(time.Minute)
	}
	return false, false
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var body struct{ PIN string }
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body) != nil {
		http.Error(w, "bad json", 400)
		return
	}
	ok, locked := a.CheckPIN(body.PIN)
	if locked {
		http.Error(w, "thử lại sau ít phút", http.StatusTooManyRequests)
		return
	}
	if !ok {
		http.Error(w, "sai PIN", http.StatusUnauthorized)
		return
	}
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

// mountSessions: Cài đặt → Tài khoản & chia sẻ. Liệt kê máy đã đăng nhập theo mã rút gọn từ token (sha256, không gửi
// token ra trình duyệt), đăng xuất từ xa một máy hoặc mọi máy khác máy đang dùng.
func mountSessions(mux *http.ServeMux, a *Auth) {
	sid := func(tok string) string { s := sha256.Sum256([]byte(tok)); return hex.EncodeToString(s[:6]) }
	mine := func(r *http.Request) string {
		if c, err := r.Cookie(cookieName); err == nil {
			return c.Value
		}
		return ""
	}
	tokens := func() map[string]int64 {
		out := map[string]int64{}
		rows, err := a.db.sql.Query(`SELECT token, created FROM tokens WHERE created>?`, a.now().Add(-90*24*time.Hour).Unix())
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var t string
			var c int64
			if rows.Scan(&t, &c) == nil {
				out[t] = c
			}
		}
		return out
	}
	type session struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
		Current bool   `json:"current"`
	}
	mux.HandleFunc("GET /api/sessions", a.Require(func(w http.ResponseWriter, r *http.Request) {
		out := []session{}
		for t, c := range tokens() {
			out = append(out, session{sid(t), c, t == mine(r)})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
		writeJSON(w, out)
	}))
	mux.HandleFunc("DELETE /api/sessions/{id}", a.Require(func(w http.ResponseWriter, r *http.Request) {
		for t := range tokens() {
			if sid(t) == r.PathValue("id") {
				a.db.sql.Exec(`DELETE FROM tokens WHERE token=?`, t)
			}
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("DELETE /api/sessions", a.Require(func(w http.ResponseWriter, r *http.Request) {
		a.db.sql.Exec(`DELETE FROM tokens WHERE token<>?`, mine(r))
		w.WriteHeader(204)
	}))
}
