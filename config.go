package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMonitorFrom = "08:00"
	defaultMonitorTo   = "18:00"
)

type MonitorConfig struct {
	ID              string `json:"id"`
	Region          string `json:"region"`
	Token           string `json:"token"`
	Carno           string `json:"carno"`
	IntervalMinutes int    `json:"interval_minutes"`
	FreeMinutes     int    `json:"free_minutes"`
	AdvanceSeconds  int    `json:"advance_seconds"`
	MonitorFrom     string `json:"monitor_from"`
	MonitorTo       string `json:"monitor_to"`
}

func defaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		IntervalMinutes: 10,
		FreeMinutes:     15,
		MonitorFrom:     defaultMonitorFrom,
		MonitorTo:       defaultMonitorTo,
	}
}

func (c MonitorConfig) WithinMonitoringTime(now time.Time) bool {
	from, errFrom := parseClockTime(c.MonitorFrom)
	to, errTo := parseClockTime(c.MonitorTo)
	if errFrom != nil || errTo != nil {
		from, to = 8*60, 18*60
	}
	minute := now.Hour()*60 + now.Minute()
	if from == to {
		return true // 起止时间相同表示全天监控。
	}
	if from < to {
		return minute >= from && minute < to
	}
	return minute >= from || minute < to // 例如 18:00–08:00，跨午夜监控。
}

func parseClockTime(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, errors.New("时间格式应为 HH:mm")
	}
	hour, errHour := strconv.Atoi(parts[0])
	minute, errMinute := strconv.Atoi(parts[1])
	if errHour != nil || errMinute != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, errors.New("请输入有效的 24 小时制时间，例如 08:00")
	}
	return hour*60 + minute, nil
}

type Config struct {
	Monitors       []MonitorConfig `json:"monitors"`
	monitorsStored bool

	// 以下字段用于读取旧版配置；保存时统一写入 Monitors。
	Region          string `json:"region,omitempty"`
	Token           string `json:"token,omitempty"`
	Carno           string `json:"carno,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	FreeMinutes     int    `json:"free_minutes,omitempty"`
	AdvanceSeconds  int    `json:"advance_seconds,omitempty"`

	NotifyStyle   string   `json:"notify_style"`
	RemoteChannel string   `json:"remote_channel"`
	PushPlusToken string   `json:"pushplus_token"`
	NtfyTopic     string   `json:"ntfy_topic"`
	CloseAction   string   `json:"close_action"`
	HistoryCarnos []string `json:"history_carnos"`
}

func defaultConfig() Config {
	return Config{Monitors: []MonitorConfig{defaultMonitorConfig()}}
}

func (cfg *Config) UnmarshalJSON(raw []byte) error {
	type plainConfig Config
	var decoded plainConfig
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*cfg = Config(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	_, cfg.monitorsStored = fields["monitors"]
	return nil
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "ParkingMonitor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func loadConfig() Config {
	cfg := Config{}
	p, err := configPath()
	if err != nil {
		return defaultConfig()
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return defaultConfig()
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return defaultConfig()
	}
	normalizeConfig(&cfg)
	return cfg
}

func normalizeMonitorConfig(cfg *MonitorConfig) {
	if !validRegion(cfg.Region) {
		cfg.Region = regionList[0].Key
	}
	if cfg.IntervalMinutes < 5 || cfg.IntervalMinutes > 1440 {
		cfg.IntervalMinutes = 10
	}
	if cfg.FreeMinutes < 1 || cfg.FreeMinutes > 1440 {
		cfg.FreeMinutes = 15
	}
	if cfg.AdvanceSeconds < 0 {
		cfg.AdvanceSeconds = 0
	}
	if limit := cfg.FreeMinutes * 60; cfg.AdvanceSeconds > limit {
		cfg.AdvanceSeconds = limit
	}
	cfg.Carno = strings.TrimSpace(cfg.Carno)
	if _, err := parseClockTime(cfg.MonitorFrom); err != nil {
		cfg.MonitorFrom = defaultMonitorFrom
	}
	if _, err := parseClockTime(cfg.MonitorTo); err != nil {
		cfg.MonitorTo = defaultMonitorTo
	}
}

func normalizeConfig(cfg *Config) {
	defaults := defaultMonitorConfig()
	legacy := MonitorConfig{
		Region: cfg.Region, Token: cfg.Token, Carno: cfg.Carno,
		IntervalMinutes: cfg.IntervalMinutes, FreeMinutes: cfg.FreeMinutes,
		AdvanceSeconds: cfg.AdvanceSeconds, MonitorFrom: defaults.MonitorFrom, MonitorTo: defaults.MonitorTo,
	}
	hasLegacy := legacy.Region != "" || legacy.Token != "" || legacy.Carno != "" ||
		legacy.IntervalMinutes != 0 || legacy.FreeMinutes != 0 || legacy.AdvanceSeconds != 0
	if len(cfg.Monitors) == 0 && !cfg.monitorsStored && hasLegacy {
		cfg.Monitors = []MonitorConfig{legacy}
	}
	if len(cfg.Monitors) == 0 {
		cfg.Monitors = []MonitorConfig{defaultMonitorConfig()}
	}
	seenIDs := make(map[string]bool, len(cfg.Monitors))
	for i := range cfg.Monitors {
		normalizeMonitorConfig(&cfg.Monitors[i])
		if cfg.Monitors[i].ID == "" || seenIDs[cfg.Monitors[i].ID] {
			cfg.Monitors[i].ID = fmt.Sprintf("monitor-%d-%d", time.Now().UnixNano(), i)
		}
		seenIDs[cfg.Monitors[i].ID] = true
	}
	// 清除旧字段，避免再次保存重复配置。
	cfg.Region, cfg.Token, cfg.Carno = "", "", ""
	cfg.IntervalMinutes, cfg.FreeMinutes, cfg.AdvanceSeconds = 0, 0, 0

	if cfg.NotifyStyle != "popup" {
		cfg.NotifyStyle = "side"
	}
	cfg.PushPlusToken = strings.TrimSpace(cfg.PushPlusToken)
	cfg.NtfyTopic = strings.TrimSpace(cfg.NtfyTopic)
	switch cfg.RemoteChannel {
	case "pushplus":
		if cfg.PushPlusToken == "" {
			cfg.RemoteChannel = ""
		}
	case "ntfy":
		if !ntfyTopicPattern.MatchString(cfg.NtfyTopic) {
			cfg.RemoteChannel = ""
		}
	default:
		cfg.RemoteChannel = ""
	}
	if cfg.CloseAction != "tray" && cfg.CloseAction != "exit" {
		cfg.CloseAction = ""
	}
	seen := make(map[string]bool, len(cfg.HistoryCarnos))
	history := make([]string, 0, len(cfg.HistoryCarnos))
	for _, carno := range cfg.HistoryCarnos {
		carno = strings.TrimSpace(carno)
		if carno == "" || seen[carno] {
			continue
		}
		seen[carno] = true
		history = append(history, carno)
		if len(history) >= 20 {
			break
		}
	}
	cfg.HistoryCarnos = history
}

var configSaveMu sync.Mutex

func updateConfig(update func(*Config)) (Config, error) {
	configSaveMu.Lock()
	defer configSaveMu.Unlock()
	cfg := loadConfig()
	update(&cfg)
	return cfg, saveConfig(cfg)
}

func saveConfig(cfg Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, raw, 0o600)
}
