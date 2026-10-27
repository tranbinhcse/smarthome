// hikbridge: sự kiện của đầu ghi Hikvision (ISAPI alertStream) -> MQTT. Thiết kế: mục 15.1 docs/...-design.md.
//
//	hik/<kênh>/<sự kiện>        ON|OFF (retained). Sự kiện = eventType viết thường: vmd, linedetection, fielddetection...
//	hik/<kênh>/human|vehicle    ON|OFF khi camera nhận ra loại mục tiêu (dòng AcuSense)
//	hik/<kênh>/online           online|offline (retained), hỏi đầu ghi mỗi 30 giây
//	hik/bridge/status           online|offline (LWT)
//
// Biến môi trường: NVR_HOST, NVR_USER, NVR_PASS (tài khoản chỉ xem), MQTT_HOST, MQTT_USER, MQTT_PASS,
// HIK_HOLD (mặc định 10s: không có tin "active" mới trong khoảng này thì về OFF).
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"hubd/isapi"
)

func main() {
	host, user, pass := os.Getenv("NVR_HOST"), os.Getenv("NVR_USER"), os.Getenv("NVR_PASS")
	if host == "" || user == "" {
		log.Fatal("thiếu NVR_HOST hoặc NVR_USER trong .env")
	}
	hold := 10 * time.Second
	if v, err := time.ParseDuration(os.Getenv("HIK_HOLD")); err == nil && v > 0 {
		hold = v
	}
	broker := os.Getenv("MQTT_HOST")
	if broker == "" {
		broker = "mosquitto"
	}
	opts := mqtt.NewClientOptions().AddBroker("tcp://"+broker+":1883").SetClientID("hikbridge").
		SetUsername(os.Getenv("MQTT_USER")).SetPassword(os.Getenv("MQTT_PASS")).
		SetWill("hik/bridge/status", "offline", 1, true).
		SetAutoReconnect(true).SetConnectRetry(true).SetConnectRetryInterval(3 * time.Second).
		SetOnConnectHandler(func(c mqtt.Client) { c.Publish("hik/bridge/status", 1, true, "online") })
	mc := mqtt.NewClient(opts)
	mc.Connect() // SetConnectRetry: tự thử lại nền
	tr := newTracker(hold, func(topic, payload string) { mc.Publish(topic, 1, true, payload) })

	base := "http://" + host
	tp := &isapi.Transport{User: user, Pass: pass} // dùng chung để cùng tạm ngừng khi sai mật khẩu
	go func() {
		for now := range time.Tick(time.Second) {
			tr.expire(now)
		}
	}()
	go func() {
		c, last := &http.Client{Transport: tp, Timeout: 10 * time.Second}, ""
		for ; ; time.Sleep(30 * time.Second) {
			st, err := channels(c, base)
			tr.status(st, err)
			if msg := fmt.Sprint(len(st), err); msg != last { // chỉ ghi log khi đổi, không lặp mỗi 30 giây
				last = msg
				log.Printf("trạng thái kênh: %d kênh, lỗi: %v", len(st), err)
			}
		}
	}()
	wait := 5 * time.Second
	for {
		start := time.Now()
		err := stream(&http.Client{Transport: tp}, base+"/ISAPI/Event/notification/alertStream",
			func(a alert) { tr.alert(a, time.Now()) })
		log.Printf("alertStream ngắt: %v", err)
		if time.Since(start) > time.Minute {
			wait = 5 * time.Second
		}
		time.Sleep(wait)
		wait = min(wait*2, time.Minute)
	}
}

func channels(c *http.Client, base string) (map[string]bool, error) {
	res, err := c.Get(base + "/ISAPI/ContentMgmt/InputProxy/channels/status")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d (tài khoản xem có quyền đọc trạng thái kênh?)", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseStatus(b)
}

// stream giữ một kết nối alertStream; 90 giây không nhận được byte nào (kể cả lúc chờ header: đầu ghi treo,
// mạng đứt không báo) thì cắt để nối lại.
func stream(c *http.Client, url string, fn func(alert)) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idle := time.AfterFunc(90*time.Second, cancel)
	defer idle.Stop()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	log.Print("đã nối alertStream")
	return readAlerts(idleReader{res.Body, func() { idle.Reset(90 * time.Second) }}, fn)
}

type idleReader struct {
	io.Reader
	touch func()
}

func (r idleReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.touch()
	}
	return n, err
}
