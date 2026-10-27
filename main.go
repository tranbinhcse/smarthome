// hubd — trung tâm điều khiển nhà: MQTT -> trạng thái + lịch sử SQLite + luật -> web (iPad/điện thoại).
// Thiết kế: docs/2026-10-26-trung-tam-dieu-khien-nha-design.md. Vận hành: README.md.
package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

//go:embed web
var webEmbed embed.FS

func main() {
	path := os.Getenv("HUBD_CONFIG")
	if path == "" {
		path = "/data/config.yaml"
	}
	cfg, pin, err := LoadConfig(path)
	if err != nil {
		log.Fatalf("config %s: %v", path, err)
	}
	db, err := OpenDB(cfg.DB)
	if err != nil {
		log.Fatalf("mở SQLite %s: %v", cfg.DB, err)
	}
	if syncCamGen(db) { // camera gán trên trang Cài đặt: ghi file sinh trước khi theo dõi thư mục
		mergeGenerated(&cfg, os.Getenv("HUBD_GEN_DIR"))
	}
	applyDeviceOverrides(&cfg, db.Setting("devices")) // phòng / tên thiết bị sắp xếp trên trang Cài đặt
	if pin == "" {
		log.Println("CẢNH BÁO: HUBD_PIN trống — giao diện và lệnh KHÔNG có mật khẩu")
	}
	// config.yaml (git pull) hay cấu hình sinh (quét QR Tuya trên dashboard) đổi: thoát để Docker chạy lại và nạp mới.
	watch := []string{path}
	if dir := os.Getenv("HUBD_GEN_DIR"); dir != "" {
		watch = append(watch, dir)
	}
	watchFiles(watch, 5*time.Second, func(f string) {
		log.Printf("cấu hình đổi (%s): khởi động lại để nạp", f)
		os.Exit(0)
	})

	var client mqtt.Client
	hub := NewHub(cfg, db, func(topic, payload string) {
		if client != nil {
			client.Publish(topic, 1, false, payload)
		}
	})

	opts := mqtt.NewClientOptions().AddBroker(cfg.MQTT.Broker).SetClientID("hubd")
	if cfg.MQTT.Username != "" {
		opts.SetUsername(cfg.MQTT.Username).SetPassword(cfg.MQTT.Password)
	}
	opts.SetAutoReconnect(true).SetConnectRetry(true).SetConnectRetryInterval(3 * time.Second)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		topics := hub.Topics()
		for _, topic := range topics {
			c.Subscribe(topic, 1, func(_ mqtt.Client, m mqtt.Message) {
				hub.Handle(m.Topic(), string(m.Payload()), time.Now())
			})
		}
		log.Printf("MQTT đã kết nối, đăng ký %d topic", len(topics))
	})
	client = mqtt.NewClient(opts)
	client.Connect() // SetConnectRetry: tự thử lại nền, web vẫn lên được khi broker chưa sẵn sàng

	go func() {
		t := time.NewTicker(5 * time.Second)
		day := time.NewTicker(24 * time.Hour)
		for {
			select {
			case now := <-t.C:
				hub.Tick(now)
			case now := <-day.C:
				db.Prune(180*24*time.Hour, now)
			}
		}
	}()

	auth := NewAuth(pin, db)
	auth.assistant = os.Getenv("HUBD_ASSISTANT_TOKEN")
	mux := newMux(hub, auth)
	if cert := os.Getenv("HUBD_TLS_CERT"); cert != "" {
		// Safari chỉ cho dùng micro (nút nói của trợ lý) trên trang HTTPS.
		go func() {
			log.Printf("HTTPS :8443 dừng: %v", http.ListenAndServeTLS(":8443", cert, os.Getenv("HUBD_TLS_KEY"), mux))
		}()
	}
	log.Printf("hubd nghe tại %s", cfg.HTTP.Listen)
	log.Fatal(http.ListenAndServe(cfg.HTTP.Listen, mux))
}

func newMux(hub *Hub, auth *Auth) *http.ServeMux {
	sub, _ := fs.Sub(webEmbed, "web")
	mux := http.NewServeMux()
	static := http.FileServer(http.FS(sub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache") // iPad luôn lấy giao diện mới sau khi cập nhật hub
		static.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /api/login", auth.Login)
	mux.HandleFunc("GET /api/config", auth.Require(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"zones": hub.cfg.Zones, "entities": hub.cfg.Entities, "devices": hub.cfg.Devices, "scenes": hub.cfg.Scenes})
	}))
	mux.HandleFunc("GET /api/state", auth.Require(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, hub.Snapshot())
	}))
	mux.HandleFunc("GET /api/history", auth.Require(func(w http.ResponseWriter, r *http.Request) {
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		if hours < 1 || hours > 24*180 {
			hours = 24
		}
		now := time.Now()
		pts, err := hub.db.Query(r.URL.Query().Get("id"), now.Add(-time.Duration(hours)*time.Hour), now, 300)
		if err != nil {
			http.Error(w, "lỗi", 500)
			return
		}
		writeJSON(w, pts)
	}))
	mux.HandleFunc("GET /events", auth.Require(func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch := hub.Sub()
		defer hub.Unsub(ch)
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		w.Write([]byte(": ok\n\n"))
		fl.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-ch:
				w.Write([]byte("data: "))
				w.Write(b)
				w.Write([]byte("\n\n"))
				fl.Flush()
			case <-ping.C:
				w.Write([]byte(": ping\n\n"))
				fl.Flush()
			}
		}
	}))
	newCams(hub.cfg.Devices, hub.db).register(mux, auth)
	mux.HandleFunc("POST /api/cmd", auth.Require(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Device, To string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil ||
			!hub.Command(body.Device, body.To) { // to: on|off, hoặc mã nút hồng ngoại (ir.go)
			http.Error(w, "lệnh không hợp lệ", 400)
			return
		}
		w.WriteHeader(204)
	}))
	mountAssistant(mux, hub, auth)
	mountSettings(mux, hub, auth)
	mountTuyaSetup(mux, hub, auth)
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
