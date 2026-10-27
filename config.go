package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MQTT struct {
		Broker   string `yaml:"broker"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"mqtt"`
	HTTP struct {
		Listen string `yaml:"listen"`
	} `yaml:"http"`
	DB       string   `yaml:"db"`
	Zones    []Zone   `yaml:"zones"`
	Entities []Entity `yaml:"entities"`
	Devices  []Device `yaml:"devices"`
	Scenes   []Scene  `yaml:"scenes"`
	Rules    []Rule   `yaml:"rules"`
}

// Zone là một phòng/khu trên một tầng. Type "panel" = tủ điện của tầng (hiện như dải CB, không phải phòng).
type Zone struct {
	ID    string `yaml:"id" json:"id"`
	Name  string `yaml:"name" json:"name"`
	Floor int    `yaml:"floor" json:"floor"`
	Type  string `yaml:"type" json:"type,omitempty"`
	Desc  string `yaml:"desc" json:"desc,omitempty"` // mô tả ngắn, sửa trên trang Cài đặt
}

// Entity là một số đo. Field: đọc một khoá của payload JSON (vd dps Tuya), nhân Scale.
// Device: gắn số đo này lên thẻ của thiết bị đó trên dashboard.
type Entity struct {
	ID     string  `yaml:"id" json:"id"`
	Name   string  `yaml:"name" json:"name"`
	Unit   string  `yaml:"unit" json:"unit"`
	Zone   string  `yaml:"zone" json:"zone"`
	Device string  `yaml:"device" json:"device,omitempty"`
	Topic  string  `yaml:"topic" json:"-"`
	Field  string  `yaml:"field" json:"-"`
	Scale  float64 `yaml:"scale" json:"-"`
}

// Kind: loại thiết bị để dashboard chọn biểu tượng (category Tuya: kg công tắc, dlq CB, pc ổ cắm...).
type Device struct {
	ID           string `yaml:"id" json:"id"`
	Name         string `yaml:"name" json:"name"`
	Zone         string `yaml:"zone" json:"zone"`
	Kind         string `yaml:"kind" json:"kind"`
	Protected    bool   `yaml:"protected" json:"protected"`
	Pin          bool   `yaml:"pin" json:"pin,omitempty"`     // lệnh của người dùng cần PIN (trợ lý không bao giờ có)
	Group        string `yaml:"group" json:"group,omitempty"` // gom nhiều lỗ của một ổ cắm thành một thẻ
	CommandTopic string `yaml:"command_topic" json:"-"`
	StateTopic   string `yaml:"state_topic" json:"-"`
	Field        string `yaml:"field" json:"-"` // khoá trong JSON của state_topic (ổ nhiều lỗ đọc chung dps)
	OnlineTopic  string `yaml:"online_topic" json:"-"`
	Channel      int    `yaml:"channel" json:"-"` // camera: số kênh trên đầu ghi Hikvision (camera.go)
	On           string `yaml:"on" json:"-"`
	Off          string `yaml:"off" json:"-"`
	// Codes: mã hồng ngoại học được (Pronto) theo mã nút dashboard (power, vol_up...). Xem firmware/README.md.
	Codes map[string]string `yaml:"codes" json:"-"`
}

// Scene: kịch bản một chạm, cùng dạng dashboard đọc (steps: [{d, to}]). Pin: cần PIN để chạy.
// Kịch bản có bước đụng thiết bị pin/protected cũng tính là cần PIN.
type Scene struct {
	ID    string `yaml:"id" json:"id"`
	Name  string `yaml:"name" json:"name"`
	Icon  string `yaml:"icon" json:"icon,omitempty"`
	Note  string `yaml:"note" json:"note,omitempty"`
	Pin   bool   `yaml:"pin" json:"pin,omitempty"`
	Steps []Step `yaml:"steps" json:"steps,omitempty"`
}

type Step struct {
	D  string `yaml:"d" json:"d"`
	To string `yaml:"to" json:"to"` // on | off
}

func (d Device) payload(to string) string {
	if to == "off" {
		if d.Off == "" {
			return "OFF"
		}
		return d.Off
	}
	if d.On == "" {
		return "ON"
	}
	return d.On
}

// Bí mật (mật khẩu MQTT, PIN) lấy từ biến môi trường, không nằm trong config.yaml đã commit.
func LoadConfig(path string) (Config, string, error) {
	var cfg Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, "", err
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, "", err
	}
	if dir := os.Getenv("HUBD_GEN_DIR"); dir != "" {
		mergeGenerated(&cfg, dir)
	}
	// Mục nào thiếu trong YAML thì slice là nil → JSON null làm dashboard vỡ; luôn trả [].
	if cfg.Zones == nil {
		cfg.Zones = []Zone{}
	}
	if cfg.Entities == nil {
		cfg.Entities = []Entity{}
	}
	if cfg.Devices == nil {
		cfg.Devices = []Device{}
	}
	if cfg.Scenes == nil {
		cfg.Scenes = []Scene{}
	}
	if cfg.Rules == nil {
		cfg.Rules = []Rule{}
	}
	if cfg.HTTP.Listen == "" {
		cfg.HTTP.Listen = ":8080"
	}
	if cfg.DB == "" {
		cfg.DB = "/db/hubd.db"
	}
	if v := os.Getenv("HUBD_DB"); v != "" {
		cfg.DB = v
	}
	if v := os.Getenv("HUBD_MQTT_USER"); v != "" {
		cfg.MQTT.Username = v
	}
	if v := os.Getenv("HUBD_MQTT_PASS"); v != "" {
		cfg.MQTT.Password = v
	}
	return cfg, os.Getenv("HUBD_PIN"), nil
}
