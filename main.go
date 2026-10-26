// hubd MVP — trung tâm điều khiển nhà, bản đầu.
// Chỉ nói MQTT: nhận trạng thái từ các topic, phục vụ web, gửi lệnh bật/tắt.
// Chưa có: SQLite lịch sử, luật, PIN (sẽ thêm theo plan docs/superpowers/plans/2026-10-26-hubd.md).
package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"gopkg.in/yaml.v3"
)

//go:embed web
var webEmbed embed.FS

type Config struct {
	MQTT struct {
		Broker   string `yaml:"broker"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"mqtt"`
	HTTP struct {
		Listen string `yaml:"listen"`
	} `yaml:"http"`
	Entities []Entity `yaml:"entities"`
	Devices  []Device `yaml:"devices"`
}

type Entity struct {
	ID    string `yaml:"id" json:"id"`
	Name  string `yaml:"name" json:"name"`
	Unit  string `yaml:"unit" json:"unit"`
	Topic string `yaml:"topic" json:"-"`
}

type Device struct {
	ID           string `yaml:"id" json:"id"`
	Name         string `yaml:"name" json:"name"`
	CommandTopic string `yaml:"command_topic" json:"-"`
	StateTopic   string `yaml:"state_topic" json:"-"`
	On           string `yaml:"on" json:"-"`
	Off          string `yaml:"off" json:"-"`
}

func (d Device) on() string {
	if d.On == "" {
		return "ON"
	}
	return d.On
}
func (d Device) off() string {
	if d.Off == "" {
		return "OFF"
	}
	return d.Off
}

// --- trạng thái trong bộ nhớ, bảo vệ bằng mutex (MVP) ---
type Store struct {
	mu   sync.RWMutex
	val  map[string]string
	at   map[string]time.Time
	subs map[chan []byte]struct{}
}

func NewStore() *Store {
	return &Store{val: map[string]string{}, at: map[string]time.Time{}, subs: map[chan []byte]struct{}{}}
}
func (s *Store) set(id, v string) {
	s.mu.Lock()
	s.val[id] = v
	s.at[id] = time.Now()
	s.mu.Unlock()
	b, _ := json.Marshal(map[string]string{"id": id, "value": v})
	s.pub(b)
}
func (s *Store) snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.val))
	for k, v := range s.val {
		out[k] = v
	}
	return out
}
func (s *Store) sub() chan []byte {
	ch := make(chan []byte, 64)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}
func (s *Store) unsub(ch chan []byte) {
	s.mu.Lock()
	delete(s.subs, ch)
	s.mu.Unlock()
}
func (s *Store) pub(b []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.subs {
		select {
		case ch <- b:
		default:
		}
	}
}

func main() {
	path := os.Getenv("HUBD_CONFIG")
	if path == "" {
		path = "/data/config.yaml"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("doc config %s: %v", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}
	if cfg.HTTP.Listen == "" {
		cfg.HTTP.Listen = ":8080"
	}

	store := NewStore()

	// map topic -> entity id (và device state topic)
	topicEntity := map[string]string{}
	for _, e := range cfg.Entities {
		if e.Topic != "" {
			topicEntity[e.Topic] = e.ID
		}
	}
	for _, d := range cfg.Devices {
		if d.StateTopic != "" {
			topicEntity[d.StateTopic] = "dev:" + d.ID
		}
	}

	opts := mqtt.NewClientOptions().AddBroker(cfg.MQTT.Broker).SetClientID("hubd")
	if cfg.MQTT.Username != "" {
		opts.SetUsername(cfg.MQTT.Username).SetPassword(cfg.MQTT.Password)
	}
	opts.SetAutoReconnect(true).SetConnectRetry(true)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		for topic, id := range topicEntity {
			t, eid := topic, id
			c.Subscribe(t, 1, func(_ mqtt.Client, m mqtt.Message) {
				store.set(eid, string(m.Payload()))
			})
		}
		log.Printf("MQTT connected, subscribed %d topics", len(topicEntity))
	})
	client := mqtt.NewClient(opts)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		log.Fatalf("mqtt connect: %v", tok.Error())
	}

	devByID := map[string]Device{}
	for _, d := range cfg.Devices {
		devByID[d.ID] = d
	}

	sub, _ := fs.Sub(webEmbed, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		ents := append([]Entity(nil), cfg.Entities...)
		sort.Slice(ents, func(i, j int) bool { return ents[i].Name < ents[j].Name })
		writeJSON(w, map[string]any{"entities": ents, "devices": cfg.Devices})
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, store.snapshot())
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch := store.sub()
		defer store.unsub(ch)
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
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
	})
	mux.HandleFunc("POST /api/cmd", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Device, To string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, "bad json", 400)
			return
		}
		d, ok := devByID[body.Device]
		if !ok || d.CommandTopic == "" {
			http.Error(w, "unknown device", 400)
			return
		}
		payload := d.on()
		if body.To == "off" {
			payload = d.off()
		}
		client.Publish(d.CommandTopic, 1, false, payload)
		w.WriteHeader(204)
	})

	log.Printf("hubd nghe tai %s", cfg.HTTP.Listen)
	log.Fatal(http.ListenAndServe(cfg.HTTP.Listen, mux))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
