package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tgDedup   = 30 * time.Minute // cùng chữ trong khoảng này chỉ gửi một lần (khi dedup=true)
	tgExpire  = 24 * time.Hour   // tin chờ quá lâu thì bỏ, không báo "rò nước" của hôm qua
	tgMaxText = 4096             // giới hạn của sendMessage, tính theo ký tự
	tgMaxWait = 50               // giây giữ máy chủ Telegram trong một lần getUpdates
	tgMaxPend = 50               // số yêu cầu chờ duyệt tối đa, người lạ spam /start không làm phình RAM
	tgBadTok  = "Telegram từ chối token: kiểm tra lại token của bot."
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
	db       *DB
	now      func() time.Time
	base     string
	http     *http.Client
	longHTTP *http.Client // getUpdates giữ kết nối tới 50 s nên cần hạn chờ dài hơn http
	sleep    func(time.Duration) <-chan time.Time
	wake     chan struct{}

	mu    sync.Mutex
	q     []*tgItem            // ponytail: hàng đợi nằm trong RAM, khởi động lại hubd thì mất; chuyển sang SQLite nếu thấy mất tin
	last  map[string]time.Time // lần gửi gần nhất theo chữ, để gom trùng
	hold  time.Time            // Telegram bảo chờ (429) tới lúc này
	fails int                  // số lần lỗi tạm liên tiếp, để tăng dần thời gian chờ

	pmu     sync.Mutex // trạng thái vòng hỏi và danh sách chờ duyệt; không giữ khi gọi HTTP
	pending []tgChat   // ponytail: danh sách chờ duyệt nằm trong RAM, khởi động lại hubd thì người ta gõ /start lại
	offset  int64      // update_id kế tiếp cần hỏi
	lastTok string     // token của lần hỏi trước, để biết token đã đổi
	bot     string     // username của bot (getMe)
	errMsg  string     // lỗi hiện trên trang Cài đặt, rỗng = ổn
}

func newTelegram(db *DB, now func() time.Time) *Telegram {
	base := os.Getenv("HUBD_TELEGRAM_API")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &Telegram{db: db, now: now, base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 15 * time.Second},
		longHTTP: &http.Client{Timeout: (tgMaxWait + 15) * time.Second},
		sleep:    time.After, wake: make(chan struct{}, 1), last: map[string]time.Time{}}
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
	go t.pollLoop(ctx)
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
			t.setErr(tok, "")
		case code == 429:
			if retry < time.Second {
				retry = time.Second
			}
			t.hold = t.now().Add(retry)
			t.mu.Unlock()
			return retry
		case code >= 400 && code < 500: // bị chặn, chat không còn: không thử lại
			if code == 401 {
				t.setErr(tok, tgBadTok)
			}
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

// setErr đặt lỗi hiện trên trang Cài đặt, chỉ khi token đang dùng vẫn là tok (kết quả của token cũ bị bỏ).
func (t *Telegram) setErr(tok, m string) {
	t.pmu.Lock()
	defer t.pmu.Unlock()
	if t.token() == tok {
		t.errMsg = m
	}
}

func (t *Telegram) pollLoop(ctx context.Context) {
	for {
		w := t.poll()
		if w == 0 {
			w = time.Second // Telegram giữ kết nối tới 50 s; chặn vòng quay nếu nó trả ngay
		}
		select {
		case <-ctx.Done():
			return
		case <-t.sleep(w):
		}
	}
}

// get gọi một phương thức Bot API bằng GET trên client dài hạn; code 0 = lỗi mạng. Không ghi URL vì token nằm trong đó.
func (t *Telegram) get(tok, method string, q url.Values, out any) int {
	resp, err := t.longHTTP.Get(t.base + "/bot" + tok + "/" + method + "?" + q.Encode())
	if err != nil {
		log.Printf("telegram: %s lỗi mạng", method)
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
	}
	return resp.StatusCode
}

// poll hỏi một lượt (getMe nếu chưa biết tên bot, rồi getUpdates), trả về bao lâu nữa nên hỏi lại (0 = ngay).
// Đọc token mỗi lượt nên token mới có hiệu lực ở lượt kế; token đổi thì bỏ offset, tên bot và lỗi cũ.
func (t *Telegram) poll() time.Duration {
	tok := t.token()
	t.pmu.Lock()
	if tok != t.lastTok {
		t.lastTok, t.offset, t.bot, t.errMsg, t.pending = tok, 0, "", "", nil
	}
	off, bot := t.offset, t.bot
	t.pmu.Unlock()
	if tok == "" {
		return 10 * time.Second
	}
	if bot == "" {
		var me struct {
			Result struct {
				Username string `json:"username"`
			} `json:"result"`
		}
		switch code := t.get(tok, "getMe", url.Values{}, &me); {
		case code == 401:
			t.setErr(tok, tgBadTok)
			return 10 * time.Second
		case code != 200:
			return 5 * time.Second
		}
		t.pmu.Lock()
		if t.token() == tok { // token đã đổi trong lúc chờ thì bỏ kết quả của token cũ
			t.bot = me.Result.Username
		}
		t.pmu.Unlock()
	}
	var up struct {
		Result []struct {
			ID      int64 `json:"update_id"`
			Message struct {
				Text string `json:"text"`
				Chat struct {
					ID        int64  `json:"id"`
					FirstName string `json:"first_name"`
					Username  string `json:"username"`
					Title     string `json:"title"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}
	q := url.Values{"timeout": {strconv.Itoa(tgMaxWait)}, "offset": {strconv.FormatInt(off, 10)}}
	switch code := t.get(tok, "getUpdates", q, &up); {
	case code == 401:
		t.setErr(tok, tgBadTok)
		return 10 * time.Second
	case code != 200:
		return 5 * time.Second
	}
	t.pmu.Lock()
	if t.token() != tok { // token đã đổi trong lúc chờ: bỏ cả lượt, không đụng offset, tên bot, lỗi, danh sách chờ
		t.pmu.Unlock()
		return 0
	}
	t.errMsg = ""
	for _, u := range up.Result {
		t.offset = max(t.offset, u.ID+1)
	}
	t.pmu.Unlock()
	for _, u := range up.Result {
		c := u.Message.Chat
		f := strings.Fields(u.Message.Text) // tin không chữ (ảnh, nhãn dán) hoặc không có message thì f rỗng
		if len(f) == 0 {
			continue
		}
		if cmd, _, _ := strings.Cut(f[0], "@"); cmd == "/start" {
			name := firstNonEmpty(c.FirstName, firstNonEmpty(c.Username, firstNonEmpty(c.Title, strconv.FormatInt(c.ID, 10))))
			t.start(tok, tgChat{c.ID, name})
		}
	}
	return 0
}

// firstNonEmpty trả a nếu không rỗng, không thì b.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// start xử lý /start: người đã duyệt chỉ được báo lại, người mới vào danh sách chờ (mỗi chat một mục).
func (t *Telegram) start(tok string, c tgChat) {
	reply := "Bạn đã nhận cảnh báo rồi."
	if !slices.ContainsFunc(t.chats(), func(a tgChat) bool { return a.ID == c.ID }) {
		reply = "Đã gửi yêu cầu, chờ chủ nhà duyệt trên màn hình nhà."
		t.pmu.Lock()
		if t.token() != tok {
			t.pmu.Unlock()
			return
		}
		if !slices.ContainsFunc(t.pending, func(a tgChat) bool { return a.ID == c.ID }) && len(t.pending) < tgMaxPend {
			t.pending = append(t.pending, c)
		}
		t.pmu.Unlock()
	}
	t.post(tok, c.ID, reply)
}

type tgJSON struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func tgList(cs []tgChat) []tgJSON {
	out := []tgJSON{}
	for _, c := range cs {
		out = append(out, tgJSON{c.ID, c.Name})
	}
	return out
}

func mountTelegram(mux *http.ServeMux, t *Telegram, a *Auth) {
	mux.HandleFunc("GET /api/telegram", a.Require(func(w http.ResponseWriter, r *http.Request) {
		tok := t.token()
		t.pmu.Lock()
		bot, msg, pend := t.bot, t.errMsg, slices.Clone(t.pending)
		t.pmu.Unlock()
		if tok == "" { // vừa xoá token, vòng hỏi chưa kịp dọn
			bot, msg = "", ""
		}
		writeJSON(w, map[string]any{"token_set": tok != "", "bot": bot, "chats": tgList(t.chats()), "pending": tgList(pend), "error": msg})
	}))
	mux.HandleFunc("POST /api/telegram/approve", a.Require(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID  int64  `json:"id"`
			PIN string `json:"pin"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&b) != nil {
			http.Error(w, "cần {id, pin}", 400)
			return
		}
		switch ok, locked := a.CheckPIN(b.PIN); {
		case locked:
			http.Error(w, "nhập sai nhiều lần, thử lại sau 1 phút", http.StatusTooManyRequests)
			return
		case !ok:
			http.Error(w, "sai PIN", http.StatusUnauthorized)
			return
		}
		t.pmu.Lock()
		i := slices.IndexFunc(t.pending, func(c tgChat) bool { return c.ID == b.ID })
		var c tgChat
		if i >= 0 {
			c = t.pending[i]
		}
		t.pmu.Unlock()
		if i < 0 {
			http.Error(w, "không có yêu cầu này", 404)
			return
		}
		if _, err := t.db.sql.Exec(`INSERT OR REPLACE INTO telegram_chats(id,name,added) VALUES(?,?,?)`, c.ID, c.Name, t.now().Unix()); err != nil {
			http.Error(w, "không lưu được", 500)
			return
		}
		t.pmu.Lock() // chỉ bỏ khỏi danh sách chờ sau khi đã lưu, lưu lỗi thì còn thử lại được
		if i := slices.IndexFunc(t.pending, func(p tgChat) bool { return p.ID == c.ID }); i >= 0 {
			t.pending = slices.Delete(t.pending, i, i+1)
		}
		t.pmu.Unlock()
		if code, _ := t.post(t.token(), c.ID, "Đã được duyệt nhận cảnh báo của iNIX Smart."); code != 200 {
			log.Printf("telegram: không gửi được tin xác nhận tới chat %d", c.ID)
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("DELETE /api/telegram/chats/{id}", a.Require(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "id không hợp lệ", 400)
			return
		}
		res, err := t.db.sql.Exec(`DELETE FROM telegram_chats WHERE id=?`, id)
		if err != nil {
			http.Error(w, "không xoá được", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "không có người nhận này", 404)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("POST /api/telegram/test", a.Require(func(w http.ResponseWriter, r *http.Request) {
		if t.token() == "" || len(t.chats()) == 0 {
			http.Error(w, "chưa có token hoặc người nhận", http.StatusConflict)
			return
		}
		t.Send("Tin thử từ iNIX Smart.", false)
		w.WriteHeader(204)
	}))
}
