package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfigHasOneIndependentSchedule(t *testing.T) {
	cfg := defaultConfig()
	if len(cfg.Monitors) != 1 {
		t.Fatalf("默认监控数量 = %d，应为 1", len(cfg.Monitors))
	}
	mon := cfg.Monitors[0]
	if mon.MonitorFrom != "08:00" || mon.MonitorTo != "18:00" {
		t.Fatalf("默认时段 = %s–%s", mon.MonitorFrom, mon.MonitorTo)
	}
	if mon.IntervalMinutes != 10 || mon.FreeMinutes != 15 {
		t.Fatalf("默认查询或计时参数丢失: %+v", mon)
	}
}

func TestPreviousDualConfigKeepsBothCards(t *testing.T) {
	raw := []byte(`{"monitors":[{"region":"lianhu","token":"test-token","carno":"陕A12345","interval_minutes":15,"free_minutes":30,"advance_seconds":60},{}],"notify_style":"popup"}`)
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	normalizeConfig(&cfg)
	if len(cfg.Monitors) != 2 {
		t.Fatalf("已有配置的两张卡片应保留，实际 %d", len(cfg.Monitors))
	}
	first := cfg.Monitors[0]
	if first.Region != "lianhu" || first.Token != "test-token" || first.Carno != "陕A12345" ||
		first.IntervalMinutes != 15 || first.FreeMinutes != 30 || first.AdvanceSeconds != 60 {
		t.Fatalf("旧配置迁移不完整: %+v", first)
	}
	if first.MonitorFrom != "08:00" || first.MonitorTo != "18:00" {
		t.Fatalf("旧配置未补默认时段: %+v", first)
	}
	if cfg.Region != "" || cfg.Carno != "" || cfg.Token != "" {
		t.Fatal("规范化后不应保留旧顶层配置")
	}
	second := cfg.Monitors[1]
	if second.Carno != "" || second.MonitorFrom != "08:00" || second.MonitorTo != "18:00" {
		t.Fatalf("旧第二张卡片应原样保留: %+v", second)
	}
	if cfg.Monitors[0].ID == "" || cfg.Monitors[1].ID == "" || cfg.Monitors[0].ID == cfg.Monitors[1].ID {
		t.Fatal("旧卡片应迁移出不同的持久化 ID")
	}
}

func TestLoadConfigMigratesLegacyFile(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"region":"gaoxin","token":"test-token","carno":"陕A54321","interval_minutes":10,"free_minutes":15}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig()
	if len(cfg.Monitors) != 1 {
		t.Fatalf("旧单车牌应迁移为一张卡片，实际 %d", len(cfg.Monitors))
	}
	second := cfg.Monitors[0]
	if second.Carno != "陕A54321" || second.MonitorFrom != "08:00" || second.MonitorTo != "18:00" {
		t.Fatalf("旧单车牌配置迁移错误: %+v", second)
	}
}

func TestNormalizePreservesCardCountAndIDs(t *testing.T) {
	cfg := Config{Monitors: []MonitorConfig{
		{ID: "first", Carno: "A"},
		{ID: "second", Carno: "B"},
		{ID: "third", Carno: "C"},
	}}
	normalizeConfig(&cfg)
	if len(cfg.Monitors) != 3 {
		t.Fatalf("已有卡片数量不应被限制，实际 %d", len(cfg.Monitors))
	}
	ids := map[string]bool{}
	for _, mon := range cfg.Monitors {
		if mon.ID == "" || ids[mon.ID] {
			t.Fatalf("卡片 ID 为空或重复: %+v", cfg.Monitors)
		}
		ids[mon.ID] = true
	}
	if cfg.Monitors[1].ID != "second" {
		t.Fatal("已有卡片 ID 不应被重新分配")
	}
	empty := Config{}
	normalizeConfig(&empty)
	if len(empty.Monitors) != 1 {
		t.Fatal("空配置应至少补一张卡片")
	}
}

func TestSaveConfigPersistsIndependentMonitors(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	cfg := twoMonitorTestConfig()
	cfg.Monitors[0].Carno, cfg.Monitors[0].Token = "A", "token-a"
	cfg.Monitors[1].Carno, cfg.Monitors[1].Token = "B", "token-b"
	cfg.Monitors[1].MonitorFrom, cfg.Monitors[1].MonitorTo = "18:00", "08:00"
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	var disk Config
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if len(disk.Monitors) != 2 || disk.Monitors[0].Token != "token-a" || disk.Monitors[1].Token != "token-b" ||
		disk.Monitors[1].MonitorFrom != "18:00" {
		t.Fatalf("双监控配置未独立保存: %s", raw)
	}
}

func TestMonitoringTimeBoundaries(t *testing.T) {
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 11, 27, hour, minute, 0, 0, time.Local)
	}
	cases := []struct {
		from, to string
		hour     int
		minute   int
		want     bool
	}{
		{"08:00", "18:00", 7, 59, false},
		{"08:00", "18:00", 8, 0, true},
		{"08:00", "18:00", 17, 59, true},
		{"08:00", "18:00", 18, 0, false},
		{"18:00", "08:00", 23, 30, true},
		{"18:00", "08:00", 3, 0, true},
		{"18:00", "08:00", 8, 0, false},
		{"18:00", "08:00", 12, 0, false},
		{"00:00", "00:00", 12, 0, true},
		{"00:00", "00:00", 23, 59, true},
	}
	for _, tc := range cases {
		cfg := MonitorConfig{MonitorFrom: tc.from, MonitorTo: tc.to}
		if got := cfg.WithinMonitoringTime(at(tc.hour, tc.minute)); got != tc.want {
			t.Errorf("%s–%s %02d:%02d = %v，应为 %v", tc.from, tc.to, tc.hour, tc.minute, got, tc.want)
		}
	}
}

func TestClockTimeValidation(t *testing.T) {
	for _, value := range []string{"", "8:00", "08:0", "24:00", "08:60", "ab:cd", "08:00:01"} {
		if _, err := parseClockTime(value); err == nil {
			t.Errorf("%q 应被拒绝", value)
		}
	}
	for _, value := range []string{"00:00", "08:00", "23:59"} {
		if _, err := parseClockTime(value); err != nil {
			t.Errorf("%q 应被接受: %v", value, err)
		}
	}
}
