package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// Giờ Việt Nam cố định UTC+7, không có giờ mùa hè; không dựa vào tzdata trong container.
var tgICT = time.FixedZone("ICT", 7*3600)

// briefHTTP tách khỏi client Telegram: voiced tổng hợp giọng nói trước khi trả lời nên chậm.
var briefHTTP = &http.Client{Timeout: 30 * time.Second}

// tgCatchUp: trễ nhịp (429, chờ lùi, hub bận) thì vẫn gửi nếu chưa quá chừng này sau giờ hẹn; quá nữa thì bỏ hôm đó,
// khỏi báo bất ngờ khi hubd khởi động lại giữa ngày.
const tgCatchUp = 5 * time.Minute

// daily gọi mỗi nhịp từ vòng gửi: tới giờ "daily_at" (H:MM hoặc HH:MM, giờ Việt Nam) mà hôm nay chưa gửi thì lấy bản
// tóm tắt từ voiced và gửi. Rỗng hoặc sai định dạng = tắt. voiced chậm (tới 30 s) nên chạy nền, không chặn việc gửi cảnh báo.
// voiced lỗi thì ghi log và bỏ qua hôm đó.
// ponytail: ngày đã gửi chỉ nằm trong RAM, khởi động lại hubd trong khoảng bù nhịp có thể gửi hai lần.
func (t *Telegram) daily() {
	at, _ := t.db.Setting("telegram")["daily_at"].(string)
	hm, err := time.Parse("15:04", at)
	if err != nil {
		return
	}
	now := t.now().In(tgICT)
	due := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, tgICT)
	day := due.Format("2006-01-02")
	if now.Before(due) || now.Sub(due) > tgCatchUp || t.dailyDay == day {
		return
	}
	t.dailyDay = day // đặt trước khi gọi để voiced lỗi cũng không bị gọi lại
	t.dailyWG.Add(1)
	go func() {
		defer t.dailyWG.Done()
		t.brief()
	}()
}

func (t *Telegram) brief() {
	base := strings.TrimRight(os.Getenv("HUBD_VOICED_URL"), "/")
	if base == "" {
		return
	}
	resp, err := briefHTTP.Post(base+"/voice/brief", "application/json", strings.NewReader("{}"))
	if err != nil {
		log.Printf("telegram: báo cáo hằng ngày: %v", err)
		return
	}
	defer resp.Body.Close()
	var out struct {
		Reply string `json:"reply"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&out) != nil || out.Reply == "" {
		log.Printf("telegram: báo cáo hằng ngày: voiced trả %d", resp.StatusCode)
		return
	}
	t.Send(out.Reply, false)
}
