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

// daily gọi mỗi phút từ vòng gửi: đúng giờ "daily_at" (HH:MM, giờ Việt Nam) thì lấy bản tóm tắt từ voiced và gửi,
// tối đa một lần mỗi ngày. Rỗng hoặc sai định dạng = tắt. voiced lỗi thì ghi log và bỏ qua hôm đó.
// ponytail: ngày đã gửi chỉ nằm trong RAM, khởi động lại hubd đúng phút đó có thể gửi hai lần.
func (t *Telegram) daily() {
	at, _ := t.db.Setting("telegram")["daily_at"].(string)
	now := t.now().In(tgICT)
	if _, err := time.Parse("15:04", at); err != nil || now.Format("15:04") != at {
		return
	}
	day := now.Format("2006-01-02")
	if t.dailyDay == day {
		return
	}
	t.dailyDay = day // đặt trước khi gọi để voiced lỗi cũng không bị gọi lại trong cùng phút
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
