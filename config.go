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
	Rules    []Rule   `yaml:"rules"`
}

// Zone là một phòng/khu trên một tầng. Type "panel" = tủ điện của tầng (hiện như dải CB, không phải phòng).
type Zone struct {
	ID    string `yaml:"id" json:"id"`
	Name  string `yaml:"name" json:"name"`
	Floor int    `yaml:"floor" json:"floor"`
	Type  string `yaml:"type" json:"type,omitempty"`
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
	Group        string `yaml:"group" json:"group,omitempty"` // gom nhiều lỗ của một ổ cắm thành một thẻ
	CommandTopic string `yaml:"command_topic" json:"-"`
	StateTopic   string `yaml:"state_topic" json:"-"`
	Field        string `yaml:"field" json:"-"` // khoá trong JSON của state_topic (ổ nhiều lỗ đọc chung dps)
	OnlineTopic  string `yaml:"online_topic" json:"-"`
	On           string `yaml:"on" json:"-"`
	Off          string `yaml:"off" json:"-"`
	// Codes: mã hồng ngoại học được (Pronto) theo mã nút dashboard (power, vol_up...). Xem firmware/README.md.
	Codes map[string]string `yaml:"codes" json:"-"`
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
