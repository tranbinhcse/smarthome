package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type tgSent struct {
	Chat int64
	Text string
	At   time.Time
}

// tgFake: máy chủ Telegram giả; reply quyết định phản hồi theo (chat, lần gọi thứ n của chat đó, tổng lần gọi).
type tgFake struct {
	mu    sync.Mutex
	calls []tgSent // mọi lần gọi, kể cả lỗi
	got   []tgSent // chỉ lần trả 200
	reply func(chat int64, n int) (int, string)
}

func newTgEnv(t *testing.T, reply func(chat int64, n int) (int, string)) (*Telegram, *tgFake, *time.Time) {
	f := &tgFake{reply: reply}
	clock := t0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/botTOK/sendMessage") {
			t.Errorf("đường dẫn lạ %s", r.URL.Path)
		}
		var b struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &b)
		f.mu.Lock()
		defer f.mu.Unlock()
		s := tgSent{b.ChatID, b.Text, clock}
		f.calls = append(f.calls, s)
		code, body := 200, `{"ok":true}`
		if f.reply != nil {
			code, body = f.reply(b.ChatID, len(f.calls))
		}
		if code == 200 {
			f.got = append(f.got, s)
		}
		w.WriteHeader(code)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HUBD_TELEGRAM_API", srv.URL)
	db := newDB(t)
	db.PutSetting("telegram", map[string]any{"bot_token": "TOK"})
	for _, id := range []int{111, 222} {
		db.sql.Exec(`INSERT INTO telegram_chats(id,name,added) VALUES(?,?,0)`, id, "n")
	}
	return newTelegram(db, func() time.Time { return clock }), f, &clock
}

// drain chạy các lượt gửi, nhảy đồng hồ theo thời gian chờ mà lượt trả về, tới khi hết việc.
func drain(tg *Telegram, clock *time.Time) {
	for i := 0; i < 1000; i++ {
		w := tg.deliver()
		if w == 0 {
			return
		}
		*clock = clock.Add(w)
	}
}

func TestTelegramSendsOnlyToApprovedChats(t *testing.T) {
	tg, f, clock := newTgEnv(t, nil)
	tg.Send("Rò nước ở bếp", true)
	drain(tg, clock)
	if len(f.got) != 2 || f.got[0].Chat != 111 || f.got[1].Chat != 222 || f.got[0].Text != "Rò nước ở bếp" {
		t.Fatalf("cần 2 tin tới 111, 222: %+v", f.got)
	}
	tg2, f2, c2 := newTgEnv(t, nil)
	tg2.db.sql.Exec(`DELETE FROM settings WHERE ns='telegram'`)
	tg2.Send("x", false)
	drain(tg2, c2)
	if len(f2.calls) != 0 {
		t.Fatalf("không token mà vẫn gọi: %+v", f2.calls)
	}
}

func TestTelegramDedup30Minutes(t *testing.T) {
	tg, f, clock := newTgEnv(t, nil)
	tg.Send("a", true)
	drain(tg, clock)
	*clock = clock.Add(10 * time.Minute)
	tg.Send("a", true)
	drain(tg, clock)
	if len(f.got) != 2 { // 1 tin x 2 người
		t.Fatalf("lần 2 phải bị gom: %d", len(f.got))
	}
	*clock = t0.Add(31 * time.Minute)
	tg.Send("a", true)
	drain(tg, clock)
	if len(f.got) != 4 {
		t.Fatalf("lần 3 phải gửi: %d", len(f.got))
	}
	tg.Send("c", true)
	tg.mu.Lock()
	n := len(tg.last)
	tg.mu.Unlock()
	if n != 2 { // "a" (lần 3) và "c"; không còn khoá cũ
		t.Fatalf("khoá gom trùng cũ phải bị dọn: %d", n)
	}
	drain(tg, clock)
	tg.Send("b", false)
	tg.Send("b", false)
	drain(tg, clock)
	if len(f.got) != 10 {
		t.Fatalf("dedup=false không được gom: %d", len(f.got))
	}
}

func TestTelegramQueueRetriesInOrder(t *testing.T) {
	tg, f, clock := newTgEnv(t, func(chat int64, n int) (int, string) {
		if n <= 2 {
			return 502, `bad gateway`
		}
		return 200, `{"ok":true}`
	})
	tg.Send("một", false)
	tg.Send("hai", false)
	drain(tg, clock)
	want := []string{"một", "một", "hai", "hai"}
	if len(f.got) != 4 {
		t.Fatalf("thiếu tin: %+v (gọi %d)", f.got, len(f.calls))
	}
	for i, w := range want {
		if f.got[i].Text != w {
			t.Fatalf("sai thứ tự: %+v", f.got)
		}
	}
	if len(f.calls) != 6 {
		t.Fatalf("cần 2 lỗi + 4 thành công, gọi %d", len(f.calls))
	}
}

func TestTelegramDropsAfter24h(t *testing.T) {
	down := true
	tg, f, clock := newTgEnv(t, func(chat int64, n int) (int, string) {
		if down {
			return 500, ``
		}
		return 200, `{"ok":true}`
	})
	tg.Send("cũ", false)
	tg.deliver()
	if len(f.calls) != 1 {
		t.Fatalf("lượt đầu phải thử: %d", len(f.calls))
	}
	*clock = clock.Add(24*time.Hour + time.Minute)
	down = false
	drain(tg, clock)
	if len(f.calls) != 1 || len(f.got) != 0 {
		t.Fatalf("tin quá 24 giờ phải bị bỏ: %+v", f.calls)
	}
}

func TestTelegramHonoursRetryAfter(t *testing.T) {
	tg, f, clock := newTgEnv(t, func(chat int64, n int) (int, string) {
		if n == 1 {
			return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 3","parameters":{"retry_after":3}}`
		}
		return 200, `{"ok":true}`
	})
	tg.Send("x", false)
	if w := tg.deliver(); w < 3*time.Second {
		t.Fatalf("phải chờ >= 3 s, được %v", w)
	}
	*clock = clock.Add(2 * time.Second)
	if tg.deliver() == 0 || len(f.calls) != 1 {
		t.Fatalf("chưa đủ 3 s mà đã gọi lại: %d", len(f.calls))
	}
	*clock = clock.Add(time.Second)
	drain(tg, clock)
	if f.calls[1].At.Sub(f.calls[0].At) < 3*time.Second || len(f.got) != 2 {
		t.Fatalf("lần kế phải cách >= 3 s: %+v", f.calls)
	}
}

func TestTelegramBlockedChatDoesNotStallOthers(t *testing.T) {
	tg, f, clock := newTgEnv(t, func(chat int64, n int) (int, string) {
		if chat == 111 {
			return 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
		}
		return 200, `{"ok":true}`
	})
	tg.Send("một", false)
	tg.Send("hai", false)
	drain(tg, clock)
	if len(f.got) != 2 || f.got[0].Chat != 222 || f.got[1].Chat != 222 || f.got[1].Text != "hai" {
		t.Fatalf("B phải nhận cả hai tin: %+v", f.got)
	}
	a := map[string]int{}
	for _, c := range f.calls {
		if c.Chat == 111 {
			a[c.Text]++
		}
	}
	if a["một"] != 1 || a["hai"] != 1 {
		t.Fatalf("A bị 403 không được thử lại cùng một tin: %v", a)
	}
}

func TestTelegramTruncatesLongText(t *testing.T) {
	tg, f, clock := newTgEnv(t, nil)
	tg.Send(strings.Repeat("ộ", 5000), false)
	drain(tg, clock)
	r := []rune(f.got[0].Text)
	if len(r) != 4096 || r[4095] != '…' {
		t.Fatalf("cần 4096 rune kết bằng …, được %d", len(r))
	}
}

func TestTelegramSendDoesNotBlockDuringPost(t *testing.T) {
	in, release := make(chan struct{}, 1), make(chan struct{})
	tg, f, clock := newTgEnv(t, nil)
	f.reply = func(chat int64, n int) (int, string) {
		if n == 1 {
			in <- struct{}{}
			f.mu.Unlock() // nhả khoá của handler để chờ mà không chặn các lần gọi khác
			<-release
			f.mu.Lock()
		}
		return 200, `{"ok":true}`
	}
	tg.Send("một", false)
	done := make(chan struct{})
	go func() { tg.deliver(); close(done) }()
	<-in // deliver đang kẹt giữa lệnh post
	sent := make(chan struct{})
	go func() { tg.Send("hai", false); close(sent) }()
	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("Send bị chặn khi đang post")
	}
	close(release)
	<-done
	drain(tg, clock)
	if len(f.got) != 4 {
		t.Fatalf("cần 4 tin: %d", len(f.got))
	}
}

// tgBot: máy chủ Telegram giả có getMe/getUpdates/sendMessage; token nào ngoài `good` thì trả 401.
type tgBot struct {
	mu      sync.Mutex
	good    map[string]bool
	updates []map[string]any // update_id tăng dần; chỉ trả những cái có update_id >= offset
	paths   []string         // "<token>/<method>" của mọi lần gọi
	offsets []string
	sent    []tgSent
	only    string            // khác rỗng: chỉ token này nhận được update
	gate    func(path string) // gọi trước khi xử lý, để chặn một lệnh gọi giữa chừng
}

func (b *tgBot) calls(method string) (n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.paths {
		if strings.HasSuffix(p, "/"+method) {
			n++
		}
	}
	return
}

func (b *tgBot) start(id int, chat int64, name, text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updates = append(b.updates, map[string]any{"update_id": id, "message": map[string]any{
		"text": text, "chat": map[string]any{"id": chat, "first_name": name}}})
}

func newTgApp(t *testing.T) (*Telegram, *tgBot, func(method, url, body string) (int, string)) {
	b := &tgBot{good: map[string]bool{"TOK": true, "NEW": true}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.gate != nil {
			b.gate(strings.TrimPrefix(r.URL.Path, "/bot"))
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/bot")
		b.paths = append(b.paths, p)
		tok, method, _ := strings.Cut(p, "/")
		if !b.good[tok] {
			w.WriteHeader(401)
			io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
			return
		}
		switch method {
		case "getMe":
			io.WriteString(w, `{"ok":true,"result":{"username":"inix_`+strings.ToLower(tok)+`"}}`)
		case "getUpdates":
			off := r.URL.Query().Get("offset")
			b.offsets = append(b.offsets, off)
			n, _ := strconv.Atoi(off)
			res := []map[string]any{}
			for _, u := range b.updates {
				if u["update_id"].(int) >= n && (b.only == "" || b.only == tok) {
					res = append(res, u)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": res})
		case "sendMessage":
			var m struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			json.NewDecoder(r.Body).Decode(&m)
			b.sent = append(b.sent, tgSent{m.ChatID, m.Text, time.Time{}})
			io.WriteString(w, `{"ok":true}`)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HUBD_TELEGRAM_API", srv.URL)
	h := NewHub(Config{}, newDB(t), func(string, string) {})
	h.db.PutSetting("telegram", map[string]any{"bot_token": "TOK"})
	h.tg = newTelegram(h.db, time.Now)
	a := NewAuth("1234", h.db)
	a.assistant = testTok
	mux := newMux(h, a)
	ck := call(mux, "POST", "/api/login", `{"pin":"1234"}`, "").Result().Cookies()
	return h.tg, b, func(method, url, body string) (int, string) {
		w := call(mux, method, url, body, "", ck...)
		return w.Code, w.Body.String()
	}
}

func tgState(t *testing.T, req func(string, string, string) (int, string)) (st struct {
	TokenSet bool `json:"token_set"`
	Bot      string
	Chats    []struct {
		ID   int64
		Name string
	}
	Pending []struct {
		ID   int64
		Name string
	}
	Error string
}) {
	t.Helper()
	code, body := req("GET", "/api/telegram", "")
	if code != 200 || json.Unmarshal([]byte(body), &st) != nil {
		t.Fatalf("GET /api/telegram: %d %s", code, body)
	}
	return
}

func TestTelegramStartApproveRemove(t *testing.T) {
	tg, bot, req := newTgApp(t)
	bot.start(1, 555, "Lan", "/start")
	bot.start(2, 666, "Hùng", "xin chào") // tin khác /start bị bỏ qua
	tg.poll()
	st := tgState(t, req)
	if st.Bot != "inix_tok" || !st.TokenSet || len(st.Pending) != 1 || st.Pending[0].ID != 555 || st.Pending[0].Name != "Lan" {
		t.Fatalf("cần 1 mục chờ của Lan: %+v", st)
	}
	if len(bot.sent) != 1 || bot.sent[0].Chat != 555 || bot.sent[0].Text != "Đã gửi yêu cầu, chờ chủ nhà duyệt trên màn hình nhà." {
		t.Fatalf("thiếu lời đáp /start: %+v", bot.sent)
	}
	if code, _ := req("POST", "/api/telegram/approve", `{"id":555,"pin":"0000"}`); code != 401 {
		t.Fatalf("PIN sai phải 401, được %d", code)
	}
	if st := tgState(t, req); len(st.Chats) != 0 || len(st.Pending) != 1 {
		t.Fatalf("PIN sai không được thêm: %+v", st)
	}
	if code, _ := req("POST", "/api/telegram/approve", `{"id":999,"pin":"1234"}`); code != 404 {
		t.Fatalf("id không chờ phải 404, được %d", code)
	}
	if code, _ := req("POST", "/api/telegram/approve", `{"id":555,"pin":"1234"}`); code != 204 {
		t.Fatalf("PIN đúng phải 204, được %d", code)
	}
	st = tgState(t, req)
	if len(st.Chats) != 1 || st.Chats[0].ID != 555 || len(st.Pending) != 0 {
		t.Fatalf("cần 555 trong chats: %+v", st)
	}
	if n := len(bot.sent); n != 2 || bot.sent[1].Chat != 555 || bot.sent[1].Text != "Đã được duyệt nhận cảnh báo của iNIX Smart." {
		t.Fatalf("thiếu tin xác nhận: %+v", bot.sent)
	}
	// đã duyệt rồi mà /start lại: báo đã nhận, không thêm chờ
	bot.start(3, 555, "Lan", "/start")
	tg.poll()
	if st := tgState(t, req); len(st.Pending) != 0 || bot.sent[len(bot.sent)-1].Text != "Bạn đã nhận cảnh báo rồi." {
		t.Fatalf("/start của người đã duyệt: %+v %+v", st, bot.sent)
	}
	// kiểm tra tin thử
	if code, _ := req("POST", "/api/telegram/test", ""); code != 204 {
		t.Fatalf("test phải 204, được %d", code)
	}
	tg.deliver()
	if l := bot.sent[len(bot.sent)-1]; l.Chat != 555 || l.Text != "Tin thử từ iNIX Smart." {
		t.Fatalf("thiếu tin thử: %+v", bot.sent)
	}
	for _, c := range []struct {
		url  string
		want int
	}{{"/api/telegram/chats/abc", 400}, {"/api/telegram/chats/777", 404}, {"/api/telegram/chats/555", 204}} {
		if code, _ := req("DELETE", c.url, ""); code != c.want {
			t.Fatalf("DELETE %s: %d, cần %d", c.url, code, c.want)
		}
	}
	if st := tgState(t, req); len(st.Chats) != 0 {
		t.Fatalf("chưa xoá: %+v", st)
	}
	if code, _ := req("POST", "/api/telegram/test", ""); code != 409 {
		t.Fatalf("chưa có người nhận phải 409, được %d", code)
	}
}

func TestTelegramApproveLockout(t *testing.T) {
	tg, bot, req := newTgApp(t)
	bot.start(1, 555, "Lan", "/start")
	tg.poll()
	for i := 0; i < 5; i++ {
		req("POST", "/api/telegram/approve", `{"id":555,"pin":"0000"}`)
	}
	if code, _ := req("POST", "/api/telegram/approve", `{"id":555,"pin":"1234"}`); code != 429 {
		t.Fatalf("đang khoá phải 429, được %d", code)
	}
}

func TestTelegramStartTwiceOnePending(t *testing.T) {
	tg, bot, req := newTgApp(t)
	bot.start(1, 555, "Lan", "/start")
	bot.start(2, 555, "Lan", "/start@inix_bot")
	tg.poll()
	tg.poll() // offset đã qua hết, không xử lý lại
	if st := tgState(t, req); len(st.Pending) != 1 {
		t.Fatalf("hai /start chỉ được một mục chờ: %+v", st.Pending)
	}
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if len(bot.offsets) != 2 || bot.offsets[0] != "0" || bot.offsets[1] != "3" {
		t.Fatalf("offset phải nhảy tới update_id cuối + 1: %v", bot.offsets)
	}
}

func TestTelegramTokenChangeWithoutRestart(t *testing.T) {
	tg, bot, req := newTgApp(t)
	tg.poll()
	if bot.calls("getUpdates") != 1 || tgState(t, req).Bot != "inix_tok" {
		t.Fatalf("lần đầu phải getMe + getUpdates")
	}
	if code, _ := req("PUT", "/api/settings/telegram", `{"bot_token":null}`); code != 204 {
		t.Fatalf("xoá token: %d", code)
	}
	if w := tg.poll(); w != 10*time.Second || bot.calls("getUpdates") != 1 {
		t.Fatalf("không token: nghỉ 10 s và không gọi getUpdates (%v, %d)", w, bot.calls("getUpdates"))
	}
	if st := tgState(t, req); st.TokenSet || st.Bot != "" {
		t.Fatalf("không token thì không có bot: %+v", st)
	}
	// token bị Telegram từ chối: báo lỗi cho trang Cài đặt
	req("PUT", "/api/settings/telegram", `{"bot_token":"SAI"}`)
	tg.poll()
	if st := tgState(t, req); !strings.Contains(st.Error, "token") {
		t.Fatalf("401 phải hiện lỗi: %+v", st)
	}
	req("PUT", "/api/settings/telegram", `{"bot_token":"NEW"}`)
	tg.poll()
	bot.mu.Lock()
	last := bot.paths[len(bot.paths)-1]
	bot.mu.Unlock()
	if last != "NEW/getUpdates" {
		t.Fatalf("phải dùng token mới ngay: %s", last)
	}
	if st := tgState(t, req); st.Error != "" || st.Bot != "inix_new" {
		t.Fatalf("token đúng thì hết lỗi: %+v", st)
	}
	// sendMessage 401 cũng hiện lỗi, gửi được thì xoá
	tg.db.sql.Exec(`INSERT INTO telegram_chats(id,name,added) VALUES(1,'n',0)`)
	req("PUT", "/api/settings/telegram", `{"bot_token":"SAI"}`)
	tg.Send("x", false)
	tg.deliver()
	if st := tgState(t, req); !strings.Contains(st.Error, "token") {
		t.Fatalf("sendMessage 401 phải hiện lỗi: %+v", st)
	}
	req("PUT", "/api/settings/telegram", `{"bot_token":"TOK"}`)
	tg.Send("y", false)
	tg.deliver()
	if st := tgState(t, req); st.Error != "" {
		t.Fatalf("gửi được thì hết lỗi: %+v", st)
	}
}

func TestTelegramChatsNotWritableBySettingsAPI(t *testing.T) {
	_, _, req := newTgApp(t)
	req("PUT", "/api/settings/telegram", `{"chats":[{"id":9,"name":"x"}]}`)
	if st := tgState(t, req); len(st.Chats) != 0 {
		t.Fatalf("Cài đặt không được thêm người nhận: %+v", st.Chats)
	}
	_, body := req("GET", "/api/settings/telegram", "")
	if !strings.Contains(body, `"bot_token_set":true`) || strings.Contains(body, "TOK") {
		t.Fatalf("GET settings phải che token: %s", body)
	}
	if _, body := req("GET", "/api/backup", ""); strings.Contains(body, "TOK") {
		t.Fatalf("sao lưu chứa token: %s", body)
	}
}

func TestTelegramRoutesNeedLogin(t *testing.T) {
	h := NewHub(Config{}, newDB(t), func(string, string) {})
	h.tg = newTelegram(h.db, time.Now)
	a := NewAuth("1234", h.db)
	a.assistant = testTok
	mux := newMux(h, a)
	for _, r := range [][2]string{{"GET", "/api/telegram"}, {"POST", "/api/telegram/approve"}, {"DELETE", "/api/telegram/chats/1"}, {"POST", "/api/telegram/test"}} {
		for _, bearer := range []string{"", testTok} {
			if w := call(mux, r[0], r[1], `{"id":1,"pin":"1234"}`, bearer); w.Code != 401 {
				t.Fatalf("%s %s bearer=%q: %d, cần 401", r[0], r[1], bearer, w.Code)
			}
		}
	}
}

// Tin không chữ, hoặc update không có message, không được làm sập hubd và vẫn phải đẩy offset.
func TestTelegramUpdatesWithoutTextDoNotPanic(t *testing.T) {
	tg, bot, req := newTgApp(t)
	bot.mu.Lock()
	bot.updates = append(bot.updates,
		map[string]any{"update_id": 5, "message": map[string]any{"chat": map[string]any{"id": 1}}},
		map[string]any{"update_id": 6, "edited_message": map[string]any{"text": "/start", "chat": map[string]any{"id": 2}}},
		map[string]any{"update_id": 7, "my_chat_member": map[string]any{}})
	bot.mu.Unlock()
	tg.poll()
	tg.poll()
	if st := tgState(t, req); len(st.Pending) != 0 {
		t.Fatalf("không được có mục chờ: %+v", st.Pending)
	}
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if len(bot.offsets) != 2 || bot.offsets[1] != "8" {
		t.Fatalf("offset phải qua update cuối: %v", bot.offsets)
	}
}

// getUpdates của token cũ trả về sau khi đổi token không được ghi đè trạng thái của token mới.
func TestTelegramStaleTokenResultDiscarded(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		tg, bot, req := newTgApp(t)
		entered, release := make(chan struct{}), make(chan struct{})
		bot.only = "TOK"
		bot.gate = func(p string) {
			if p == "TOK/getUpdates" {
				entered <- struct{}{}
				<-release
			}
		}
		bot.start(7, 9, "Cũ", "/start")
		done := make(chan struct{})
		go func() { tg.poll(); close(done) }()
		<-entered
		req("PUT", "/api/settings/telegram", `{"bot_token":"NEW"}`)
		if revoked {
			bot.mu.Lock()
			bot.good["TOK"] = false
			bot.mu.Unlock()
		}
		tg.poll() // lượt của token mới
		close(release)
		<-done
		st := tgState(t, req)
		tg.pmu.Lock()
		off := tg.offset
		tg.pmu.Unlock()
		if off != 0 || st.Bot != "inix_new" || len(st.Pending) != 0 || st.Error != "" {
			t.Fatalf("revoked=%v: kết quả token cũ lọt vào: offset=%d %+v", revoked, off, st)
		}
		for _, s := range bot.sent {
			if s.Chat == 9 {
				t.Fatalf("revoked=%v: trả lời bằng token cũ: %+v", revoked, bot.sent)
			}
		}
	}
}

// Lưu người nhận lỗi thì yêu cầu vẫn còn trong danh sách chờ để duyệt lại.
func TestTelegramApproveKeepsPendingWhenSaveFails(t *testing.T) {
	tg, bot, req := newTgApp(t)
	bot.start(1, 555, "Lan", "/start")
	tg.poll()
	tg.db.sql.Exec(`DROP TABLE telegram_chats`)
	if code, _ := req("POST", "/api/telegram/approve", `{"id":555,"pin":"1234"}`); code != 500 {
		t.Fatalf("lưu lỗi phải 500, được %d", code)
	}
	tg.pmu.Lock()
	n := len(tg.pending)
	tg.pmu.Unlock()
	if n != 1 {
		t.Fatalf("yêu cầu bị mất khi lưu lỗi: %d", n)
	}
	tg.db.sql.Exec(`CREATE TABLE telegram_chats(id INTEGER PRIMARY KEY, name TEXT NOT NULL, added INTEGER NOT NULL)`)
	if code, _ := req("POST", "/api/telegram/approve", `{"id":555,"pin":"1234"}`); code != 204 {
		t.Fatalf("thử lại phải 204, được %d", code)
	}
}
