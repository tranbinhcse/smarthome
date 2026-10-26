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

type Zone struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
}

type Entity struct {
	ID    string `yaml:"id" json:"id"`
	Name  string `yaml:"name" json:"name"`
	Unit  string `yaml:"unit" json:"unit"`
	Zone  string `yaml:"zone" json:"zone"`
	Topic string `yaml:"topic" json:"-"`
}

type Device struct {
	ID           string `yaml:"id" json:"id"`
	Name         string `yaml:"name" json:"name"`
	Zone         string `yaml:"zone" json:"zone"`
	Protected    bool   `yaml:"protected" json:"protected"`
	CommandTopic string `yaml:"command_topic" json:"-"`
	StateTopic   string `yaml:"state_topic" json:"-"`
	OnlineTopic  string `yaml:"online_topic" json:"-"`
	On           string `yaml:"on" json:"-"`
	Off          string `yaml:"off" json:"-"`
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
