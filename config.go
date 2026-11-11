package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Region          string   `json:"region"`
	Token           string   `json:"token"`
	Carno           string   `json:"carno"`
	IntervalMinutes int      `json:"interval_minutes"`
	FreeMinutes     int      `json:"free_minutes"`
	AdvanceSeconds  int      `json:"advance_seconds"`
	NotifyStyle     string   `json:"notify_style"`
	CloseAction     string   `json:"close_action"`
	HistoryCarnos   []string `json:"history_carnos"`
}

func defaultConfig() Config {
	return Config{IntervalMinutes: 10, FreeMinutes: 15}
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
	cfg := defaultConfig()
	p, err := configPath()
	if err != nil {
		return cfg
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(raw, &cfg)
	normalizeConfig(&cfg)
	return cfg
}

func normalizeConfig(cfg *Config) {
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
	if cfg.NotifyStyle != "popup" {
		cfg.NotifyStyle = "side"
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
