package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

var cstZone = time.FixedZone("CST", 8*3600)

func parseServerTime(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, cstZone)
	if err != nil {
		return time.Time{}
	}
	return t
}

type MonitorStatus struct {
	Running   bool
	InPark    bool
	Phase     string
	Entry     time.Time
	Elapsed   time.Duration
	Remaining time.Duration
	LastQuery time.Time
	Detail    string
	ParkName  string
	Serialno  string
}

type AlertInfo struct {
	Carno       string
	ParkName    string
	Entry       time.Time
	Elapsed     time.Duration
	Remaining   time.Duration
	Overdue     bool
	FreeMinutes int
}

type EntryInfo struct {
	Carno       string
	Serial      string
	Entry       time.Time
	ParkName    string
	FreeMinutes int
}

type Monitor struct {
	mu     sync.Mutex
	cfg    Config
	client *Client
	cancel context.CancelFunc

	running    bool
	inPark     bool
	entry      time.Time
	serial     string
	parkName   string
	alerted    bool
	phase      string
	detail     string
	lastQuery  time.Time
	elapsed    time.Duration
	remaining  time.Duration
	parkedBase time.Duration
	parkedAt   time.Time

	ignoredSerial      string
	entryAlertedSerial string

	firstQuery bool
	failCount  int
	failMsg    string
	failLogged time.Time

	onStatus func(MonitorStatus)
	onAlert  func(AlertInfo)
	onEntry  func(EntryInfo)
	onLog    func(string)
}

func NewMonitor(cfg Config, onStatus func(MonitorStatus), onAlert func(AlertInfo), onEntry func(EntryInfo), onLog func(string)) *Monitor {
	m := &Monitor{
		cfg:      cfg,
		client:   NewClient(cfg.Token, regionByKey(cfg.Region)),
		phase:    "未开始",
		onStatus: onStatus,
		onAlert:  onAlert,
		onEntry:  onEntry,
		onLog:    onLog,
	}
	return m
}

func (m *Monitor) log(msg string) {
	if m.onLog != nil {
		m.onLog(msg)
	}
}

func (m *Monitor) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.inPark = false
	m.entry = time.Time{}
	m.serial = ""
	m.parkName = ""
	m.alerted = false
	m.phase = "查询中"
	m.detail = ""
	m.elapsed = 0
	m.remaining = 0
	m.parkedBase = 0
	m.parkedAt = time.Time{}
	m.ignoredSerial = ""
	m.entryAlertedSerial = ""
	m.firstQuery = true
	m.failCount = 0
	m.failMsg = ""
	m.failLogged = time.Time{}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	cfg := m.cfg
	m.mu.Unlock()

	m.pushStatus()
	m.log(fmt.Sprintf("开始监控：车牌 %s，查询间隔 %d 分钟，计时时长 %d 分钟", cfg.Carno, cfg.IntervalMinutes, cfg.FreeMinutes))

	go m.loop(ctx)
	go m.ticker(ctx)
}

func (m *Monitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	cancel := m.cancel
	m.cancel = nil
	m.phase = "已停止"
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.pushStatus()
	m.log("已停止监控")
}

func (m *Monitor) loop(ctx context.Context) {
	interval := time.Duration(m.cfg.IntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	m.queryOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.queryOnce(ctx)
		}
	}
}

func (m *Monitor) queryOnce(ctx context.Context) {
	carno := m.cfg.Carno
	start := time.Now()

	res, err := m.client.Query(ctx, carno)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		m.recordFailure(start, "查询失败", err.Error())
		return
	}
	if res.Status != 1 {
		m.recordFailure(start, "接口异常", describeResult(res))
		return
	}
	m.recoverQuery()

	if res.Data == nil {
		m.applyNoRecord(start, "接口未返回业务数据")
		return
	}
	info := res.Data.ParkingFeeInfo
	if !info.InPark() {
		m.applyNoRecord(start, "当前无在停记录")
		return
	}
	m.applyParkingInfo(info, start)
}

// recordFailure 只在首次失败、错误内容变化或每 30 分钟时记录一条，
// 避免断网时每个轮询周期都往日志里追加相同错误。
func (m *Monitor) recordFailure(at time.Time, phase, msg string) {
	m.mu.Lock()
	m.phase = phase
	m.detail = msg
	m.lastQuery = at
	m.failCount++
	count := m.failCount
	changed := msg != m.failMsg
	m.failMsg = msg
	logNow := count == 1 || changed || time.Since(m.failLogged) >= 30*time.Minute
	if logNow {
		m.failLogged = time.Now()
	}
	m.mu.Unlock()

	if logNow {
		if count == 1 {
			m.log(phase + "：" + msg)
		} else {
			m.log(fmt.Sprintf("%s（连续 %d 次）：%s", phase, count, msg))
		}
	}
	m.pushStatus()
}

func (m *Monitor) recoverQuery() {
	m.mu.Lock()
	count := m.failCount
	m.failCount = 0
	m.failMsg = ""
	m.failLogged = time.Time{}
	m.mu.Unlock()
	if count > 0 {
		m.log(fmt.Sprintf("查询已恢复正常（此前连续失败 %d 次）", count))
	}
}

func (m *Monitor) applyParkingInfo(info ParkingFeeInfo, start time.Time) {
	m.mu.Lock()
	ignored := m.ignoredSerial
	m.mu.Unlock()
	if info.Serialno != "" && info.Serialno == ignored {
		m.applyNoRecord(start, "该停车记录已标记为已缴费出场")
		return
	}

	entry := parseServerTime(info.Intime)
	if entry.IsZero() && info.Parktime > 0 {
		entry = time.Now().Add(-time.Duration(info.Parktime) * time.Millisecond)
	}
	if entry.IsZero() {
		m.applyNoRecord(start, "无法解析入场时间："+info.Intime)
		return
	}

	parked := time.Duration(info.Parktime) * time.Millisecond
	if parked <= 0 {
		parked = time.Since(entry)
	}

	m.mu.Lock()
	changed := m.serial != info.Serialno
	newEntry := changed && info.Serialno != "" && m.entryAlertedSerial != info.Serialno
	prevPark := m.parkName
	m.inPark = true
	m.entry = entry
	m.serial = info.Serialno
	m.parkName = info.Parkname
	m.parkedBase = parked
	m.parkedAt = time.Now()
	m.firstQuery = false
	if changed {
		m.alerted = false
	}
	if m.ignoredSerial != "" && info.Serialno != m.ignoredSerial {
		m.ignoredSerial = ""
	}
	m.entryAlertedSerial = info.Serialno
	m.phase = "在停"
	m.detail = info.Parkname
	m.lastQuery = start
	m.mu.Unlock()

	if changed && info.Serialno == "" {
		m.log(fmt.Sprintf("在停中：入场 %s，地点 %s", info.Intime, info.Parkname))
	} else if !changed && info.Parkname != prevPark && info.Parkname != "" {
		m.log("停车地点更新：" + info.Parkname)
	}
	m.pushStatus()

	if newEntry && m.onEntry != nil {
		m.onEntry(EntryInfo{
			Carno:       m.cfg.Carno,
			Serial:      info.Serialno,
			Entry:       entry,
			ParkName:    info.Parkname,
			FreeMinutes: m.cfg.FreeMinutes,
		})
	}
}

func (m *Monitor) applyNoRecord(at time.Time, detail string) {
	m.mu.Lock()
	wasInPark := m.inPark
	first := m.firstQuery
	m.firstQuery = false
	m.inPark = false
	m.entry = time.Time{}
	m.serial = ""
	m.parkName = ""
	m.alerted = false
	m.parkedBase = 0
	m.parkedAt = time.Time{}
	m.phase = "未在停"
	m.detail = detail
	m.lastQuery = at
	m.elapsed = 0
	m.remaining = 0
	m.mu.Unlock()

	if wasInPark || first {
		m.log(detail)
	}
	m.pushStatus()
}

// MarkPaid 供“已缴费”按钮调用：视为车辆已出场并重置计时状态，
// 同时忽略当前这笔停车记录，避免下一轮轮询又把状态恢复为在停。
func (m *Monitor) MarkPaid() {
	m.mu.Lock()
	if m.serial != "" {
		m.ignoredSerial = m.serial
	}
	m.inPark = false
	m.entry = time.Time{}
	m.serial = ""
	m.parkName = ""
	m.alerted = false
	m.parkedBase = 0
	m.parkedAt = time.Time{}
	m.elapsed = 0
	m.remaining = 0
	m.phase = "已出场"
	m.detail = "已手动标记缴费，等待下次入场"
	m.mu.Unlock()

	m.log("已出场，监控状态已重置")
	m.pushStatus()
}

func (m *Monitor) ticker(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			m.mu.Lock()
			running := m.running
			inPark := m.inPark
			entry := m.entry
			serial := m.serial
			alerted := m.alerted
			parkName := m.parkName
			parkedBase := m.parkedBase
			parkedAt := m.parkedAt
			free := time.Duration(m.cfg.FreeMinutes) * time.Minute
			advance := time.Duration(m.cfg.AdvanceSeconds) * time.Second
			m.mu.Unlock()

			if !running || !inPark || entry.IsZero() {
				continue
			}
			var elapsed time.Duration
			if parkedAt.IsZero() {
				elapsed = now.Sub(entry)
			} else {
				elapsed = parkedBase + now.Sub(parkedAt)
			}
			if elapsed < 0 {
				elapsed = 0
			}
			remaining := free - elapsed
			m.mu.Lock()
			m.elapsed = elapsed
			m.remaining = remaining
			m.mu.Unlock()
			m.pushStatus()

			if remaining <= advance && !alerted && serial != "" {
				m.mu.Lock()
				if m.serial != serial || m.alerted {
					m.mu.Unlock()
					continue
				}
				m.alerted = true
				m.mu.Unlock()

				alert := AlertInfo{
					Carno:       m.cfg.Carno,
					ParkName:    parkName,
					Entry:       entry,
					Elapsed:     elapsed,
					Remaining:   remaining,
					Overdue:     remaining < 0,
					FreeMinutes: int(free / time.Minute),
				}
				m.log("计时时长倒计时结束，触发提醒")
				if m.onAlert != nil {
					m.onAlert(alert)
				}
			}
		}
	}
}

func (m *Monitor) pushStatus() {
	m.mu.Lock()
	st := MonitorStatus{
		Running:   m.running,
		InPark:    m.inPark,
		Phase:     m.phase,
		Entry:     m.entry,
		Elapsed:   m.elapsed,
		Remaining: m.remaining,
		LastQuery: m.lastQuery,
		Detail:    m.detail,
		ParkName:  m.parkName,
		Serialno:  m.serial,
	}
	m.mu.Unlock()
	if m.onStatus != nil {
		m.onStatus(st)
	}
}
