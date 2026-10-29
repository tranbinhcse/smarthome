package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	tg.Send("b", false)
	tg.Send("b", false)
	drain(tg, clock)
	if len(f.got) != 8 {
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
