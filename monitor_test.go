package main

import (
	"context"
	"testing"
	"time"
)

func TestCountdownTriggersAlertOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alerts := make(chan AlertInfo, 4)
	m := &Monitor{
		cfg:        Config{Carno: "TEST", FreeMinutes: 15},
		running:    true,
		inPark:     true,
		entry:      time.Now().Add(-14*time.Minute - 58*time.Second),
		serial:     "S1",
		parkedBase: 14*time.Minute + 58*time.Second,
		parkedAt:   time.Now(),
		onAlert:    func(a AlertInfo) { alerts <- a },
		onLog:      func(string) {},
		onStatus:   func(MonitorStatus) {},
	}
	go m.ticker(ctx)

	select {
	case a := <-alerts:
		if a.Carno != "TEST" {
			t.Fatalf("提醒车牌错误: %s", a.Carno)
		}
		if !a.Overdue && a.Elapsed < 14*time.Minute {
			t.Fatalf("提醒时已停时长异常: %s", a.Elapsed)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("倒计时结束未触发提醒")
	}

	select {
	case <-alerts:
		t.Fatal("同一停车记录重复提醒")
	case <-time.After(3 * time.Second):
	}
}

func TestMarkPaidResetsState(t *testing.T) {
	m := &Monitor{
		cfg:      Config{Carno: "TEST", FreeMinutes: 15},
		onStatus: func(MonitorStatus) {},
		onLog:    func(string) {},
	}
	m.inPark = true
	m.serial = "S1"
	m.alerted = true
	m.entry = time.Now().Add(-time.Minute)
	m.parkedBase = time.Minute
	m.parkedAt = time.Now()

	m.MarkPaid()

	if m.inPark || m.alerted || m.serial != "" || !m.entry.IsZero() || !m.parkedAt.IsZero() {
		t.Fatalf("MarkPaid 未重置状态: inPark=%v serial=%q", m.inPark, m.serial)
	}
	if m.ignoredSerial != "S1" {
		t.Fatalf("应忽略已缴费的记录 S1，实际 %q", m.ignoredSerial)
	}
}

func TestAdvanceTriggersBeforeZero(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alerts := make(chan AlertInfo, 4)
	m := &Monitor{
		cfg:        Config{Carno: "TEST", FreeMinutes: 15, AdvanceSeconds: 120},
		running:    true,
		inPark:     true,
		entry:      time.Now().Add(-13 * time.Minute),
		serial:     "S1",
		parkedBase: 13 * time.Minute,
		parkedAt:   time.Now(),
		onAlert:    func(a AlertInfo) { alerts <- a },
		onLog:      func(string) {},
		onStatus:   func(MonitorStatus) {},
	}
	go m.ticker(ctx)

	select {
	case a := <-alerts:
		if a.Remaining <= 0 || a.Remaining > 2*time.Minute {
			t.Fatalf("提前提醒的剩余时间异常: %s", a.Remaining)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("提前提醒未在剩余 2 分钟时触发")
	}
}

func TestEntryAlertOncePerSerial(t *testing.T) {
	entries := make(chan EntryInfo, 4)
	m := &Monitor{
		cfg:      Config{Carno: "TEST", FreeMinutes: 15},
		onEntry:  func(e EntryInfo) { entries <- e },
		onStatus: func(MonitorStatus) {},
		onLog:    func(string) {},
	}
	now := time.Now()
	info := ParkingFeeInfo{
		Serialno: "S1",
		Carno:    "TEST",
		Intime:   now.In(cstZone).Add(-time.Minute).Format("2006-01-02 15:04:05"),
		Parktime: 60000,
		Parkname: "测试停车场",
	}

	m.applyParkingInfo(info, now)
	select {
	case <-entries:
	default:
		t.Fatal("首次检测到入场应触发提醒")
	}

	m.applyParkingInfo(info, now.Add(time.Second))
	select {
	case <-entries:
		t.Fatal("同一停车记录不应重复触发入场提醒")
	case <-time.After(300 * time.Millisecond):
	}

	m.MarkPaid()
	m.applyParkingInfo(info, now.Add(2*time.Second))
	if m.inPark {
		t.Fatal("已缴费记录不应恢复为在停")
	}
	select {
	case <-entries:
		t.Fatal("已缴费记录不应再次触发入场提醒")
	case <-time.After(300 * time.Millisecond):
	}

	info2 := info
	info2.Serialno = "S2"
	m.applyParkingInfo(info2, now.Add(3*time.Second))
	select {
	case <-entries:
	case <-time.After(time.Second):
		t.Fatal("新入场记录应触发提醒")
	}
}

func TestAdvanceParse(t *testing.T) {
	if v, err := advanceToSeconds("1", "分钟"); err != nil || v != 60 {
		t.Fatalf("1 分钟应为 60 秒，实际 %d err=%v", v, err)
	}
	if v, err := advanceToSeconds("30", "秒"); err != nil || v != 30 {
		t.Fatalf("30 秒应为 30，实际 %d err=%v", v, err)
	}
	if v, err := advanceToSeconds("0", "分钟"); err != nil || v != 0 {
		t.Fatalf("0 分钟应为 0，实际 %d err=%v", v, err)
	}
	if _, err := advanceToSeconds("-1", "秒"); err == nil {
		t.Fatal("负数应报错")
	}
	if v, unit := secondsToAdvance(60); v != 1 || unit != "分钟" {
		t.Fatalf("60 秒应显示 1 分钟，实际 %d %s", v, unit)
	}
	if v, unit := secondsToAdvance(90); v != 90 || unit != "秒" {
		t.Fatalf("90 秒应显示 90 秒，实际 %d %s", v, unit)
	}
}

func TestLeaveResetsState(t *testing.T) {
	m := &Monitor{
		cfg:      Config{Carno: "TEST", FreeMinutes: 15},
		onStatus: func(MonitorStatus) {},
		onLog:    func(string) {},
	}
	m.inPark = true
	m.serial = "S1"
	m.alerted = true
	m.entry = time.Now().Add(-time.Minute)
	m.parkedBase = time.Minute
	m.parkedAt = time.Now()

	m.applyNoRecord(time.Now(), "当前无在停记录")

	if m.inPark || m.alerted || m.serial != "" || !m.entry.IsZero() || !m.parkedAt.IsZero() {
		t.Fatalf("出场后状态未复位: inPark=%v alerted=%v serial=%q", m.inPark, m.alerted, m.serial)
	}
}
