package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	tgDedup   = 30 * time.Minute // cùng chữ trong khoảng này chỉ gửi một lần (khi dedup=true)
	tgExpire  = 24 * time.Hour   // tin chờ quá lâu thì bỏ, không báo "rò nước" của hôm qua
	tgMaxText = 4096             // giới hạn của sendMessage, tính theo ký tự
)

type tgChat struct {
	ID   int64
	Name string
}

type tgItem struct {
	text string
	at   time.Time
	to   []int64 // người nhận còn phải gửi, theo thứ tự
}

// Telegram gửi cảnh báo tới các chat đã duyệt. Token đọc từ cấu hình mỗi lần dùng nên đổi trên trang Cài đặt có hiệu lực ngay.
type Telegram struct {
	db    *DB
	now   func() time.Time
	base  string
	http  *http.Client
	sleep func(time.Duration) <-chan time.Time
	wake  chan struct{}

	mu    sync.Mutex
	q     []*tgItem            // ponytail: hàng đợi nằm trong RAM, khởi động lại hubd thì mất; chuyển sang SQLite nếu thấy mất tin
	last  map[string]time.Time // lần gửi gần nhất theo chữ, để gom trùng
	hold  time.Time            // Telegram bảo chờ (429) tới lúc này
	fails int                  // số lần lỗi tạm liên tiếp, để tăng dần thời gian chờ
}

func newTelegram(db *DB, now func() time.Time) *Telegram {
	base := os.Getenv("HUBD_TELEGRAM_API")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &Telegram{db: db, now: now, base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 15 * time.Second},
		sleep: time.After, wake: make(chan struct{}, 1), last: map[string]time.Time{}}
}

func (t *Telegram) token() string {
	s, _ := t.db.Setting("telegram")["bot_token"].(string)
	return s
}

func (t *Telegram) chats() []tgChat {
	rows, err := t.db.sql.Query(`SELECT id, name FROM telegram_chats ORDER BY added, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []tgChat
	for rows.Next() {
		var c tgChat
		if rows.Scan(&c.ID, &c.Name) == nil {
			out = append(out, c)
		}
	}
	return out
}

// Send xếp tin vào hàng đợi cho mọi chat đã duyệt, không chặn. Chưa có token hoặc chưa có chat thì bỏ luôn
// (không để dành tin cũ để rồi dội vào lúc mới cài).
func (t *Telegram) Send(text string, dedup bool) {
	if t.token() == "" {
		return
	}
	var to []int64
	for _, c := range t.chats() {
		to = append(to, c.ID)
	}
	if len(to) == 0 {
		return
	}
	if r := []rune(text); len(r) > tgMaxText {
		text = string(r[:tgMaxText-1]) + "…"
	}
	now := t.now()
	t.mu.Lock()
	for k, at := range t.last { // chữ quá 30 phút không còn tác dụng gom, bỏ cho khỏi phình
		if now.Sub(at) >= tgDedup {
			delete(t.last, k)
		}
	}
	if dedup {
		if at, ok := t.last[text]; ok && now.Sub(at) < tgDedup {
			t.mu.Unlock()
			return
		}
		t.last[text] = now
	}
	t.q = append(t.q, &tgItem{text: text, at: now, to: to})
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *Telegram) run(ctx context.Context) {
	for {
		w := t.deliver()
		if w == 0 {
			w = time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-t.wake:
		case <-t.sleep(w):
		}
	}
}

// deliver gửi một lượt mọi thứ đang đến hạn, trả về bao lâu nữa nên gọi lại (0 = hàng đợi trống).
// Lỗi tạm hoặc 429 dừng lượt để giữ thứ tự; chat bị chặn (4xx) chỉ bị gạt khỏi tin đó.
func (t *Telegram) deliver() time.Duration {
	for {
		// Lấy việc kế tiếp dưới khoá, nhả khoá khi gọi HTTP để Send không bị chặn.
		t.mu.Lock()
		now := t.now()
		if now.Before(t.hold) {
			defer t.mu.Unlock()
			return t.hold.Sub(now)
		}
		tok := t.token()
		for len(t.q) > 0 && (now.Sub(t.q[0].at) > tgExpire || tok == "" || len(t.q[0].to) == 0) {
			t.q = t.q[1:]
		}
		if len(t.q) == 0 {
			t.mu.Unlock()
			return 0
		}
		it, chat := t.q[0], t.q[0].to[0] // chỉ deliver bỏ phần tử đầu hàng nên it còn nguyên khi ta quay lại
		t.mu.Unlock()

		code, retry := t.post(tok, chat, it.text)

		t.mu.Lock()
		switch {
		case code == 200:
			t.fails = 0
		case code == 429:
			if retry < time.Second {
				retry = time.Second
			}
			t.hold = t.now().Add(retry)
			t.mu.Unlock()
			return retry
		case code >= 400 && code < 500: // bị chặn, chat không còn: không thử lại
			log.Printf("telegram: sendMessage %d, bỏ chat %d", code, chat)
		default:
			t.fails++
			w := 5 * time.Second << min(t.fails-1, 6)
			t.hold = t.now().Add(w)
			t.mu.Unlock()
			return w
		}
		it.to = it.to[1:]
		t.mu.Unlock()
	}
}

// post gọi sendMessage; code 0 = lỗi mạng. Không ghi URL vì token nằm trong đó.
func (t *Telegram) post(tok string, chat int64, text string) (int, time.Duration) {
	b, _ := json.Marshal(map[string]any{"chat_id": chat, "text": text})
	resp, err := t.http.Post(t.base+"/bot"+tok+"/sendMessage", "application/json", bytes.NewReader(b))
	if err != nil {
		log.Printf("telegram: sendMessage lỗi mạng")
		return 0, 0
	}
	defer resp.Body.Close()
	var r struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	json.Unmarshal(raw, &r)
	if resp.StatusCode != 200 {
		log.Printf("telegram: sendMessage trả %d", resp.StatusCode)
	}
	return resp.StatusCode, time.Duration(r.Parameters.RetryAfter) * time.Second
}
