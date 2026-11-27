package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

var (
	tree = ui.Tree{}
	page = tree.Feature("main")

	pushTokenFieldID = tree.ID(page, "pushplus-token")
	ntfyTopicFieldID = tree.ID(page, "ntfy-topic")
	testRemoteBtnID  = tree.ID(page, "test-remote")
	saveBtnID        = tree.ID(page, "save")
	startBtnID       = tree.ID(page, "start-all")
	stopBtnID        = tree.ID(page, "stop-all")
	addMonitorBtnID  = tree.ID(page, "add-monitor")
	logAreaID        = tree.ID(page, "log")
	logScrollID      = tree.ID(page, "log-scroll")
	logViewID        = tree.ID(page, "log-view")
	logClearID       = tree.ID(page, "log-clear")
	configBtnID      = tree.ID(page, "config")
	configCloseID    = tree.ID(page, "config-close")

	notifyScope  = tree.Sub(page, tree.Name("notify-style"))
	remoteScope  = tree.Sub(page, tree.Name("remote-channel"))
	configAreaID = tree.ID(page, "config-scroll")

	logDialogScope   = tree.Sub(page, tree.Name("log-dialog"))
	logDialogSurface = tree.ID(logDialogScope, "surface")
	logDialogDismiss = tree.ID(logDialogScope, "dismiss")
	logDialogCloseID = tree.ID(logDialogScope, "close")

	dialogScope    = tree.Sub(page, tree.Name("dialog"))
	dialogSurface  = tree.ID(dialogScope, "surface")
	dialogDismiss  = tree.ID(dialogScope, "dismiss")
	dialogOKID     = tree.ID(dialogScope, "ok")
	dialogPaidID   = tree.ID(dialogScope, "paid")
	dialogUpdateID = tree.ID(dialogScope, "update")

	configScope   = tree.Sub(page, tree.Name("config-dialog"))
	configSurface = tree.ID(configScope, "surface")
	configDismiss = tree.ID(configScope, "dismiss")

	closeScope     = tree.Sub(page, tree.Name("close-dialog"))
	closeSurface   = tree.ID(closeScope, "surface")
	closeDismiss   = tree.ID(closeScope, "dismiss")
	closeCancelID  = tree.ID(closeScope, "cancel")
	closeExitID    = tree.ID(closeScope, "exit")
	closeTrayID    = tree.ID(closeScope, "tray")
	closeDontAskID = tree.ID(closeScope, "dont-ask")

	toastUndoID  = tree.ID(page, "toast-undo")
	toastCloseID = tree.ID(page, "toast-close")

	intervalPresets = []string{"5", "10", "15", "30"}
	freePresets     = []string{"10", "15", "20", "30", "60"}
	unitLabels      = []string{"分钟", "秒"}
	notifyLabels    = []string{"侧边通知", "弹出框"}
	notifyKeys      = []string{"side", "popup"}
	remoteLabels    = []string{"不启用", "PushPlus", "ntfy"}
	remoteKeys      = []string{"", "pushplus", "ntfy"}
	statsLabels     = []string{"当日", "本周", "本月"}
)

var trashIcon = ui.Icon{
	ViewBox: 24,
	Stroke:  2,
	Cap:     ui.CapRound,
	Join:    ui.JoinRound,
	Commands: []ui.PathCommand{
		ui.MoveTo(3, 6), ui.LineTo(21, 6),
		ui.MoveTo(19, 6), ui.LineTo(19, 20),
		ui.CubicTo(19, 21, 18, 22, 17, 22),
		ui.LineTo(7, 22),
		ui.CubicTo(6, 22, 5, 21, 5, 20),
		ui.LineTo(5, 6),
		ui.MoveTo(8, 6), ui.LineTo(8, 4),
		ui.CubicTo(8, 3, 9, 2, 10, 2),
		ui.LineTo(14, 2),
		ui.CubicTo(15, 2, 16, 3, 16, 4),
		ui.LineTo(16, 6),
		ui.MoveTo(10, 11), ui.LineTo(10, 17),
		ui.MoveTo(14, 11), ui.LineTo(14, 17),
	},
}

type alertKind int

const (
	alertNone alertKind = iota
	alertInfo
	alertEntry
	alertFree
	alertUpdate
)

type alertState struct {
	kind    alertKind
	title   string
	message string
	update  *updateInfo
	monitor string
}

type toastState struct {
	message string
	undo    func()
	id      int
	expires time.Time
	total   time.Duration
}

type monitorForm struct {
	carnoField    flex.Field
	intervalField flex.Field
	freeField     flex.Field
	advanceField  flex.Field
	fromField     flex.Field
	toField       flex.Field

	intervalSel flex.SelectState
	freeSel     flex.SelectState
	unitSel     flex.SelectState
	regionSel   flex.SelectState
	unit        int
	region      int

	tokenValue   string
	pendingToken string
}

func newMonitorForm(cfg MonitorConfig) monitorForm {
	form := monitorForm{region: regionIndex(cfg.Region), tokenValue: cfg.Token}
	form.carnoField.SetText(cfg.Carno)
	form.intervalField.SetText(strconv.Itoa(cfg.IntervalMinutes))
	form.freeField.SetText(strconv.Itoa(cfg.FreeMinutes))
	form.fromField.SetText(cfg.MonitorFrom)
	form.toField.SetText(cfg.MonitorTo)
	advance, unit := secondsToAdvance(cfg.AdvanceSeconds)
	form.advanceField.SetText(strconv.Itoa(advance))
	if unit == "秒" {
		form.unit = 1
	}
	return form
}

func (f *monitorForm) collect(index int) (MonitorConfig, error) {
	label := fmt.Sprintf("监控 %d：", index+1)
	carno := strings.TrimSpace(f.carnoField.Text())
	if strings.ContainsAny(carno, " \t\r\n") {
		return MonitorConfig{}, errors.New(label + "车牌号不能包含空格")
	}
	if carno == "" {
		return MonitorConfig{Region: regionList[f.region].Key, Token: strings.TrimSpace(f.tokenValue)}, nil
	}
	if strings.TrimSpace(f.tokenValue) == "" {
		return MonitorConfig{}, errors.New(label + "请填写 Token")
	}
	interval, err := parseIntervalMinutes(f.intervalField.Text())
	if err != nil {
		return MonitorConfig{}, fmt.Errorf("%s查询间隔无效：%v", label, err)
	}
	free, err := parseFreeMinutes(f.freeField.Text())
	if err != nil {
		return MonitorConfig{}, fmt.Errorf("%s计时时长无效：%v", label, err)
	}
	advance, err := advanceToSeconds(f.advanceField.Text(), unitLabels[f.unit])
	if err != nil {
		return MonitorConfig{}, fmt.Errorf("%s提前提醒无效：%v", label, err)
	}
	if advance > free*60 {
		return MonitorConfig{}, errors.New(label + "提前提醒时间不能大于计时时长")
	}
	from := strings.TrimSpace(f.fromField.Text())
	to := strings.TrimSpace(f.toField.Text())
	if _, err := parseClockTime(from); err != nil {
		return MonitorConfig{}, errors.New(label + "开始时间无效，请使用 24 小时制 HH:mm")
	}
	if _, err := parseClockTime(to); err != nil {
		return MonitorConfig{}, errors.New(label + "结束时间无效，请使用 24 小时制 HH:mm")
	}
	return MonitorConfig{
		Region: regionList[f.region].Key, Token: strings.TrimSpace(f.tokenValue), Carno: carno,
		IntervalMinutes: interval, FreeMinutes: free, AdvanceSeconds: advance,
		MonitorFrom: from, MonitorTo: to,
	}, nil
}

func (f *monitorForm) setConfig(cfg MonitorConfig) {
	f.region = regionIndex(cfg.Region)
	f.tokenValue = cfg.Token
	f.pendingToken = ""
	f.carnoField.SetText(cfg.Carno)
	f.intervalField.SetText(strconv.Itoa(cfg.IntervalMinutes))
	f.freeField.SetText(strconv.Itoa(cfg.FreeMinutes))
	f.fromField.SetText(cfg.MonitorFrom)
	f.toField.SetText(cfg.MonitorTo)
	advance, unit := secondsToAdvance(cfg.AdvanceSeconds)
	f.advanceField.SetText(strconv.Itoa(advance))
	f.unit = 0
	if unit == "秒" {
		f.unit = 1
	}
}

type monitorUI struct {
	id              string
	scope           ui.Scope
	form            monitorForm
	status          MonitorStatus
	mon             *Monitor
	statsRange      int
	historyExpanded bool
}

func newMonitorUI(cfg MonitorConfig) *monitorUI {
	mon := &monitorUI{id: cfg.ID, form: newMonitorForm(cfg)}
	mon.scope = tree.Sub(page, tree.Name("monitor-"+cfg.ID))
	mon.status.Phase = "未开始"
	return mon
}

func (m *model) monitorIndex(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.monitorIndexLocked(id)
}

func (m *model) monitorExistsLocked(id string) bool {
	return m.monitorIndexLocked(id) >= 0
}

func (m *model) monitorIndexLocked(id string) int {
	for i, mon := range m.monitors {
		if mon.id == id {
			return i
		}
	}
	return -1
}

type model struct {
	mu       sync.Mutex
	configMu sync.Mutex

	cfg Config

	monitors []*monitorUI

	pushTokenField flex.Field
	ntfyTopicField flex.Field

	remote        int
	testingRemote bool

	logs           []string
	logsOpen       bool
	alert          *alertState
	configOpen     bool
	deleteConfirm  string
	pendingDelete  string
	logScroll      flex.SliderState
	pendingUpdate  *updateInfo
	updatePrompted bool
	updating       bool
	updatePct      int
	updateVersion  string
	alertTopmost   bool

	notify  int
	entries *entryStore
	logFile *logFile

	closeConfirm bool
	closeDontAsk bool
	closeAllow   func()
	toast        *toastState
	toastID      int
	window       *app.Window
	application  *app.Application
	tray         *app.StatusItem

	view        flex.View
	toastView   flex.View
	invalidator *app.Invalidator
	tokenProxy  *TokenProxy
}

func newModel(cfg Config) *model {
	normalizeConfig(&cfg)
	m := &model{cfg: cfg}
	for i, cfg := range cfg.Monitors {
		if cfg.ID == "" {
			cfg.ID = fmt.Sprintf("monitor-%d", i+1)
		}
		m.monitors = append(m.monitors, newMonitorUI(cfg))
	}
	m.pushTokenField.SetText(cfg.PushPlusToken)
	m.ntfyTopicField.SetText(cfg.NtfyTopic)
	m.remote = remoteIndex(cfg.RemoteChannel)
	if cfg.NotifyStyle == "popup" {
		m.notify = 1
	}
	m.entries = loadEntryStore()
	m.logFile = newLogFile()
	return m
}

func remoteIndex(key string) int {
	for i, k := range remoteKeys {
		if k == key {
			return i
		}
	}
	return 0
}

func (m *model) requestPaint() {
	if m.invalidator != nil {
		m.invalidator.Invalidate()
	}
}

func (m *model) log(msg string) {
	now := time.Now()
	m.mu.Lock()
	m.logs = append(m.logs, now.Format("15:04:05")+"  "+msg)
	if len(m.logs) > 150 {
		m.logs = m.logs[len(m.logs)-150:]
	}
	m.mu.Unlock()
	m.logFile.write(now, msg)
	m.requestPaint()
}

func (m *model) showInfo(title, message string) {
	m.mu.Lock()
	m.alert = &alertState{kind: alertInfo, title: title, message: message}
	m.mu.Unlock()
	m.requestPaint()
}

func (m *model) showError(err error) {
	if err != nil {
		m.showInfo("提示", err.Error())
	}
}

func (m *model) clearAlert() {
	m.mu.Lock()
	m.alert = nil
	top := m.alertTopmost
	m.alertTopmost = false
	m.mu.Unlock()
	if top && m.window != nil {
		_ = m.window.SetAlwaysOnTop(false)
	}
	m.requestPaint()
}

func (m *model) onEntryAlert(info EntryInfo) {
	msg := fmt.Sprintf("车牌 %s 已入场。\n入场时间：%s\n地点：%s\n计时时长：%d 分钟",
		info.Carno, info.Entry.Format("2006-01-02 15:04:05"), info.ParkName, info.FreeMinutes)
	m.mu.Lock()
	m.alert = &alertState{kind: alertEntry, title: "车辆已入场", message: msg, monitor: info.MonitorID}
	m.mu.Unlock()
	m.log("检测到车辆入场：" + info.Carno)
	if m.entries.add(info) {
		m.requestPaint()
	}
	playAlertSound()
	go func() { _ = sendSystemNotification("车辆已入场", msg) }()
	m.sendRemote("车辆已入场", msg)
	m.requestPaint()
}

func (m *model) onFreeAlert(a AlertInfo) {
	var msg string
	if a.Remaining > 0 {
		msg = fmt.Sprintf("车牌 %s 已停车 %s（入场时间 %s）。\n预计还剩 %s 达到计时时长上限，请及时挪车。\n地点：%s",
			a.Carno, formatDuration(a.Elapsed), a.Entry.Format("01-02 15:04"), formatDuration(a.Remaining), a.ParkName)
	} else {
		msg = fmt.Sprintf("车牌 %s 已停车 %s（入场时间 %s）。\n%d 分钟计时时长已结束，请及时挪车。\n地点：%s",
			a.Carno, formatDuration(a.Elapsed), a.Entry.Format("01-02 15:04"), a.FreeMinutes, a.ParkName)
	}
	m.mu.Lock()
	popup := m.cfg.NotifyStyle == "popup"
	if popup {
		m.alert = &alertState{kind: alertFree, title: "停车计时即将结束", message: msg, monitor: a.MonitorID}
	}
	m.mu.Unlock()

	playAlertSound()
	if popup {
		m.log("计时提醒已触发（弹出框）")
		m.showAlertWindow("停车计时即将结束", msg)
	} else {
		m.log("计时提醒已触发（侧边通知）")
		go func() { _ = sendSystemNotification("停车计时即将结束", msg) }()
	}
	m.sendRemote("停车计时即将结束", msg)
	m.requestPaint()
}

func (m *model) sendRemote(title, body string) {
	m.mu.Lock()
	notifier := newRemoteNotifier(m.cfg)
	m.mu.Unlock()
	if notifier == nil {
		return
	}
	go func() {
		if err := notifier.send(title, body); err != nil {
			m.log("远程通知发送失败（" + notifier.label() + "）：" + err.Error())
			return
		}
		m.log("已发送远程通知（" + notifier.label() + "）")
	}()
}

// showAlertWindow 把主窗口从托盘或最小化状态恢复到前台并临时置顶，
// 确保窗口内的居中提醒能被看到；置顶状态在关闭提醒时复位。
func (m *model) showAlertWindow(title, msg string) {
	window := m.window
	if window == nil {
		go func() { _ = sendSystemNotification(title, msg) }()
		return
	}
	window.Show()
	if err := window.SetAlwaysOnTop(true); err == nil {
		m.mu.Lock()
		m.alertTopmost = true
		m.mu.Unlock()
	}
}

func (m *model) collectMonitor(id string) (MonitorConfig, error) {
	index := m.monitorIndex(id)
	if index < 0 {
		return MonitorConfig{}, errors.New("监控配置不存在或已删除")
	}
	return m.monitors[index].form.collect(index)
}

func (m *model) collectConfig() (Config, error) {
	monitors := make([]MonitorConfig, len(m.monitors))
	seen := make(map[string]int, len(m.monitors))
	for i := range monitors {
		cfg, err := m.collectMonitor(m.monitors[i].id)
		if err != nil {
			return Config{}, err
		}
		cfg.ID = m.monitors[i].id
		monitors[i] = cfg
		if cfg.Carno == "" {
			continue
		}
		if previous, exists := seen[cfg.Carno]; exists {
			return Config{}, fmt.Errorf("监控 %d 和监控 %d 的车牌不能相同", previous+1, i+1)
		}
		seen[cfg.Carno] = i
	}
	remote := remoteKeys[m.remote]
	pushToken := strings.TrimSpace(m.pushTokenField.Text())
	ntfyTopic := strings.TrimSpace(m.ntfyTopicField.Text())
	switch remote {
	case "pushplus":
		if pushToken == "" {
			return Config{}, errors.New("请填写 PushPlus Token")
		}
	case "ntfy":
		if !ntfyTopicPattern.MatchString(ntfyTopic) {
			return Config{}, errors.New("ntfy 主题只能包含字母、数字、下划线和连字符，长度 1-64")
		}
	}
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	cfg.Monitors = monitors
	cfg.NotifyStyle = notifyKeys[m.notify]
	cfg.RemoteChannel = remote
	cfg.PushPlusToken = pushToken
	cfg.NtfyTopic = ntfyTopic
	for _, mon := range monitors {
		cfg.HistoryCarnos = addHistory(cfg.HistoryCarnos, mon.Carno)
	}
	return cfg, nil
}

func addHistory(list []string, carno string) []string {
	carno = strings.TrimSpace(carno)
	if carno == "" {
		return list
	}
	out := make([]string, 0, len(list)+1)
	out = append(out, carno)
	for _, v := range list {
		if v != carno {
			out = append(out, v)
		}
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func (m *model) stepAdvance(id string, delta int) {
	index := m.monitorIndex(id)
	if index < 0 {
		return
	}
	field := &m.monitors[index].form.advanceField
	v, err := strconv.Atoi(strings.TrimSpace(field.Text()))
	if err != nil {
		v = 0
	}
	v += delta
	if v < 0 {
		v = 0
	}
	field.SetText(strconv.Itoa(v))
	m.requestPaint()
}

func (m *model) persistConfig(update func(*Config)) (Config, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	return m.persistConfigLocked(update)
}

// persistConfigLocked 的调用方必须已持有 configMu；保存期间不持有 UI 锁。
func (m *model) persistConfigLocked(update func(*Config)) (Config, error) {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	update(&cfg)
	return cfg, saveConfig(cfg)
}

func (m *model) saveConfigFromForm() {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	formCfg, err := m.collectConfig()
	if err != nil {
		m.showError(err)
		return
	}
	forms := make(map[string]MonitorConfig, len(formCfg.Monitors))
	for _, mon := range formCfg.Monitors {
		forms[mon.ID] = mon
	}
	cfg, err := m.persistConfigLocked(func(cfg *Config) {
		for i := range cfg.Monitors {
			if form, ok := forms[cfg.Monitors[i].ID]; ok {
				cfg.Monitors[i] = form
				delete(forms, cfg.Monitors[i].ID)
			}
		}
		for _, mon := range formCfg.Monitors {
			if form, ok := forms[mon.ID]; ok {
				cfg.Monitors = append(cfg.Monitors, form)
				cfg.HistoryCarnos = addHistory(cfg.HistoryCarnos, form.Carno)
			}
		}
		cfg.NotifyStyle = formCfg.NotifyStyle
		cfg.RemoteChannel = formCfg.RemoteChannel
		cfg.PushPlusToken = formCfg.PushPlusToken
		cfg.NtfyTopic = formCfg.NtfyTopic
	})
	if err != nil {
		m.showError(err)
		return
	}
	m.mu.Lock()
	for _, cfg := range cfg.Monitors {
		if index := m.monitorIndexLocked(cfg.ID); index >= 0 {
			m.monitors[index].form.setConfig(cfg)
		}
	}
	m.cfg = cfg
	m.mu.Unlock()
	m.showNotice("配置已保存；运行中的监控需重启后生效")
}

func (m *model) isRunning(id string) bool {
	index := m.monitorIndex(id)
	return index >= 0 && m.monitors[index].status.Running
}

func (m *model) anyRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.monitors {
		if m.monitors[i].status.Running {
			return true
		}
	}
	return false
}

func (m *model) setStatus(id string, st MonitorStatus) {
	m.mu.Lock()
	if index := m.monitorIndexLocked(id); index >= 0 {
		m.monitors[index].status = st
	}
	m.mu.Unlock()
	m.requestPaint()
}

func (m *model) startMonitor(id string) bool {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	index := m.monitorIndex(id)
	if index < 0 || m.isRunning(id) {
		return false
	}
	monCfg, err := m.collectMonitor(id)
	if err != nil {
		m.showError(err)
		return false
	}
	if monCfg.Carno == "" {
		m.showError(fmt.Errorf("监控 %d：请填写车牌号", index+1))
		return false
	}

	cfg, err := m.persistConfigLocked(func(cfg *Config) {
		found := false
		for i := range cfg.Monitors {
			if cfg.Monitors[i].ID == id {
				cfg.Monitors[i] = monCfg
				found = true
				break
			}
		}
		if !found {
			cfg.Monitors = append(cfg.Monitors, monCfg)
		}
		cfg.HistoryCarnos = addHistory(cfg.HistoryCarnos, monCfg.Carno)
	})
	if err != nil {
		m.showError(err)
		return false
	}

	mon := NewMonitor(id, monCfg,
		func(st MonitorStatus) { m.setStatus(id, st) },
		m.onFreeAlert, m.onEntryAlert, m.log)

	m.mu.Lock()
	index = m.monitorIndexLocked(id)
	if index < 0 || m.monitors[index].status.Running {
		m.mu.Unlock()
		return false
	}
	m.cfg = cfg
	for i := range m.monitors {
		if pending := m.monitors[i].form.pendingToken; pending != "" {
			m.cfg.Monitors[i].Token = pending
		}
	}
	old := m.monitors[index].mon
	m.monitors[index].mon = mon
	m.monitors[index].status.Running = true
	m.mu.Unlock()

	if old != nil {
		old.Stop()
	}
	mon.Start()
	if m.anyRunning() {
		keepAwake(true)
	}
	m.requestPaint()
	return true
}

func (m *model) stopMonitor(id string) {
	index := m.monitorIndex(id)
	if index < 0 {
		return
	}
	mon := m.monitors[index].mon
	if mon != nil {
		mon.Stop()
	}
	if !m.anyRunning() {
		keepAwake(false)
	}
	m.requestPaint()
}

func (m *model) startAllMonitors() {
	configured := false
	seen := make(map[string]int)
	for i := range m.monitors {
		if m.isRunning(m.monitors[i].id) || strings.TrimSpace(m.monitors[i].form.carnoField.Text()) == "" {
			continue
		}
		monCfg, err := m.collectMonitor(m.monitors[i].id)
		if err != nil {
			m.showError(err)
			continue
		}
		if previous, exists := seen[monCfg.Carno]; exists {
			m.showError(fmt.Errorf("监控 %d 和监控 %d 的车牌相同，请修改后再全部开始", previous+1, i+1))
			continue
		}
		seen[monCfg.Carno] = i
		configured = true
		m.startMonitor(m.monitors[i].id)
	}
	if !configured && !m.anyRunning() {
		m.showInfo("提示", "请先在“配置”中填写至少一个监控车牌。")
	}
}

func (m *model) stopAllMonitors() {
	for i := range m.monitors {
		m.stopMonitor(m.monitors[i].id)
	}
}

func (m *model) markPaid(id string) {
	index := m.monitorIndex(id)
	if index < 0 {
		return
	}
	if mon := m.monitors[index].mon; mon != nil {
		mon.MarkPaid()
	}
}

func (m *model) addMonitor() {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	cfg := defaultMonitorConfig()
	cfg.ID = fmt.Sprintf("monitor-%d", time.Now().UnixNano())
	updated, err := m.persistConfigLocked(func(saved *Config) {
		saved.Monitors = append(saved.Monitors, cfg)
	})
	if err != nil {
		m.showError(err)
		return
	}
	m.mu.Lock()
	m.cfg = updated
	m.monitors = append(m.monitors, newMonitorUI(cfg))
	if m.configOpen {
		m.view.ScrollState(configAreaID).ScrollToEnd()
	}
	m.mu.Unlock()
	m.log("已新增监控卡片")
	m.showNotice("已新增监控，请配置车牌和 Token")
	m.requestPaint()
}

func (m *model) requestDeleteMonitor(id string) {
	index := m.monitorIndex(id)
	if index < 0 {
		return
	}
	if len(m.monitors) <= 1 {
		m.showInfo("提示", "至少需要保留一个监控卡片。")
		return
	}
	m.deleteConfirm = id
	m.requestPaint()
}

func (m *model) cancelDeleteMonitor() {
	m.deleteConfirm = ""
	m.requestPaint()
}

func (m *model) deleteMonitor(id string) bool {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	index := m.monitorIndex(id)
	if index < 0 || len(m.monitors) <= 1 {
		return false
	}
	mon := m.monitors[index].mon
	if mon != nil {
		mon.Stop()
	}
	cfg, err := m.persistConfigLocked(func(cfg *Config) {
		for i := range cfg.Monitors {
			if cfg.Monitors[i].ID == id {
				cfg.Monitors = append(cfg.Monitors[:i], cfg.Monitors[i+1:]...)
				break
			}
		}
	})
	if err != nil {
		m.showError(err)
		return false
	}
	m.mu.Lock()
	index = m.monitorIndexLocked(id)
	if index < 0 || len(m.monitors) <= 1 {
		m.mu.Unlock()
		return false
	}
	m.monitors = append(m.monitors[:index], m.monitors[index+1:]...)
	m.cfg = cfg
	m.deleteConfirm = ""
	if m.alert != nil && m.alert.monitor == id {
		m.alert = nil
	}
	m.mu.Unlock()
	if mon != nil {
		mon.Stop()
	}
	if !m.anyRunning() {
		keepAwake(false)
	}
	m.log("已删除监控卡片")
	m.showNotice("监控卡片已删除")
	m.requestPaint()
	return true
}

func (m *model) captureToken(id string) {
	if m.tokenProxy.IsRunning() {
		m.showInfo("提示", "代理已在运行，请先在微信中完成一次查询。")
		return
	}

	index := m.monitorIndex(id)
	if index < 0 {
		return
	}
	region := regionList[m.monitors[index].form.region]
	m.log(fmt.Sprintf("正在准备监控 %d 的本地代理（%s）...", index+1, region.Name))
	go func() {
		err := m.tokenProxy.Start(
			region.Hostname,
			func(token string) {
				m.configMu.Lock()
				defer m.configMu.Unlock()
				m.mu.Lock()
				index := m.monitorIndexLocked(id)
				exists := index >= 0
				m.mu.Unlock()
				if !exists {
					m.log("Token 获取完成，但对应卡片已删除")
					m.tokenProxy.Stop()
					return
				}
				cfg, saveErr := m.persistConfigLocked(func(cfg *Config) {
					for i := range cfg.Monitors {
						if cfg.Monitors[i].ID == id {
							cfg.Monitors[i].Token = token
							break
						}
					}
				})
				m.mu.Lock()
				index = m.monitorIndexLocked(id)
				if index >= 0 {
					m.monitors[index].form.pendingToken = token
					m.cfg.Monitors[index].Token = token
				}
				exists = index >= 0
				if saveErr == nil {
					m.cfg = cfg
				}
				m.mu.Unlock()
				if !exists {
					m.log("Token 获取完成，但对应卡片已删除")
				} else if saveErr != nil {
					m.log("Token 已捕获，但保存失败：" + saveErr.Error())
					m.showInfo("保存失败", "Token 已填入当前卡片，但写入配置失败。请稍后点“保存全部配置”重试。")
				} else {
					m.log("Token 已自动获取并保存")
					m.showInfo("获取成功", "Token 已填入并保存。")
				}
				m.tokenProxy.Stop()
			},
			m.log,
			func() { m.requestPaint() },
		)
		if err != nil {
			m.log("启动自动获取失败：" + err.Error())
			m.showError(err)
			return
		}
		m.showInfo("请在微信中操作一次",
			"代理已启动。请打开“"+region.MiniName+"”小程序，进入停车查询页面点一次“查询”，工具会自动捕获 Token。\n获取成功或 3 分钟后自动关闭代理。")
	}()
}

func (m *model) shutdown() {
	m.mu.Lock()
	monitors := make([]*Monitor, len(m.monitors))
	for i := range m.monitors {
		monitors[i] = m.monitors[i].mon
	}
	proxy := m.tokenProxy
	m.mu.Unlock()
	for _, mon := range monitors {
		if mon != nil {
			mon.Stop()
		}
	}
	if proxy != nil {
		proxy.Stop()
	}
	keepAwake(false)
}

func (m *model) paint(canvas *ui.Canvas, size ui.Size) {
	theme := ui.LightTheme()
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, theme.Background)

	m.mu.Lock()
	statuses := make([]MonitorStatus, len(m.monitors))
	for i := range m.monitors {
		statuses[i] = m.monitors[i].status
		pending := m.monitors[i].form.pendingToken
		m.monitors[i].form.pendingToken = ""
		if pending != "" {
			m.monitors[i].form.tokenValue = pending
		}
	}
	running := false
	allRunning := true
	for i, st := range statuses {
		running = running || st.Running
		allRunning = allRunning && (st.Running || strings.TrimSpace(m.monitors[i].form.carnoField.Text()) == "")
	}
	logs := append([]string(nil), m.logs...)
	alert := m.alert
	pendingUpdate := m.pendingUpdate
	m.pendingUpdate = nil
	toast := m.toast
	if pendingUpdate != nil && alert == nil {
		alert = &alertState{
			kind:    alertUpdate,
			title:   "发现新版本 v" + pendingUpdate.Version,
			message: updateMessage(pendingUpdate),
			update:  pendingUpdate,
		}
		m.alert = alert
	}
	m.mu.Unlock()

	f := m.view.Begin(canvas, theme, bounds)
	f.PopupLayer(10)
	f.Column().Fill().Pad(20).Gap(12).Children(func() {
		f.Row().FillWidth().Items(flex.Center).Gap(8).Children(func() {
			f.Text("停车时长监控").Bold().FontSize(20)
			f.Text("v" + appVersion).Muted().FontSize(12).Self(flex.Center)
			f.Spacer()
			if f.Button(startBtnID, "全部开始").Disabled(allRunning).Clicked() {
				m.startAllMonitors()
			}
			if f.Button(stopBtnID, "全部停止").Outline().Disabled(!running).Clicked() {
				m.stopAllMonitors()
			}
			if f.Button(configBtnID, "配置").Outline().Clicked() {
				m.configOpen = true
				m.requestPaint()
			}
		})
		f.ScrollArea(tree.ID(page, "monitor-list")).Grow(1).Shrink(1).Gap(12).Children(func() {
			for i := range m.monitors {
				var st MonitorStatus
				if i < len(statuses) {
					st = statuses[i]
				} else {
					st = m.monitors[i].status
				}
				m.paintMonitorCard(f, theme, i, st)
			}
		})
		m.paintLogBar(f)
		f.Text("本工具仅供交流学习使用，禁止商用，请勿用于任何非法用途。").FontSize(11).Muted()
	})
	m.paintConfigDialog(f, theme)
	m.paintLogDialog(f, theme, logs)
	m.paintDialog(f, alert)
	m.paintCloseDialog(f, theme)
	f.End()

	if m.pendingDelete != "" {
		id := m.pendingDelete
		m.pendingDelete = ""
		m.deleteMonitor(id)
	}

	if toast != nil {
		canvas.PushLayer(20)
		scope := ui.ID{}
		switch {
		case m.closeConfirm:
			scope = closeSurface
		case alert != nil:
			scope = dialogSurface
		case m.logsOpen:
			scope = logDialogSurface
		case m.configOpen:
			scope = configSurface
		}
		if !scope.IsZero() {
			canvas.PushFocusScope(scope)
		}
		m.toastView.Draw(canvas, theme, bounds, func(tf *flex.Frame) {
			m.paintToast(tf, bounds, theme, toast)
		})
		if !scope.IsZero() {
			canvas.PopFocusScope()
		}
		canvas.PopLayer()
	}
}

func (m *model) configSummary(index int) string {
	form := &m.monitors[index].form
	carno := strings.TrimSpace(form.carnoField.Text())
	if carno == "" {
		carno = "未配置"
	}
	return fmt.Sprintf("%s · %s · 每 %s 分钟 · 计时 %s 分钟",
		regionList[form.region].Name, carno, strings.TrimSpace(form.intervalField.Text()), strings.TrimSpace(form.freeField.Text()))
}

func (m *model) paintConfigDialog(f *flex.Frame, theme ui.Theme) {
	if !m.configOpen {
		return
	}
	d := f.Dialog(flex.DialogSpec{
		Title:       "监控配置",
		Description: "每张卡片独立查询和计时；车牌留空则不启用。",
		Width:       680,
		Height:      max(360, min(float32(760), f.Bounds().Height-32)),
		Scope:       configSurface,
		DismissID:   configDismiss,
		CloseID:     configDismiss,
	})
	d.Body(func() {
		f.ScrollArea(configAreaID).Grow(1).Shrink(1).FillWidth().Children(func() {
			m.paintForm(f, theme)
		})
	})
	d.Footer(func() {
		if f.Button(addMonitorBtnID, "新增卡片").Outline().Clicked() {
			m.addMonitor()
		}
		if f.Button(saveBtnID, "保存全部配置").Clicked() {
			m.saveConfigFromForm()
		}
		if f.Button(configCloseID, "关闭").Outline().Clicked() {
			m.configOpen = false
			m.deleteConfirm = ""
		}
	})
	if d.Dismissed() {
		m.configOpen = false
		m.deleteConfirm = ""
	}
}

func (m *model) paintForm(f *flex.Frame, theme ui.Theme) {
	f.Column().FillWidth().Gap(12).PadSides(2, 6, 2, 2).Children(func() {
		for i := range m.monitors {
			m.paintMonitorForm(f, theme, i)
		}
		m.paintSharedSettings(f, theme)
	})
}

func (m *model) paintMonitorForm(f *flex.Frame, theme ui.Theme, index int) {
	form := &m.monitors[index].form
	scope := m.monitors[index].scope
	regionScope := tree.Sub(scope, tree.Name("region-select"))
	intervalScope := tree.Sub(scope, tree.Name("interval-select"))
	freeScope := tree.Sub(scope, tree.Name("free-select"))
	unitScope := tree.Sub(scope, tree.Name("unit-select"))
	carnoFieldID := tree.ID(scope, "carno")
	tokenFieldID := tree.ID(scope, "token")
	intervalFieldID := tree.ID(scope, "interval")
	freeFieldID := tree.ID(scope, "free")
	advanceFieldID := tree.ID(scope, "advance")
	fromFieldID := tree.ID(scope, "from")
	toFieldID := tree.ID(scope, "to")
	autoBtnID := tree.ID(scope, "auto")
	advMinusID := tree.ID(scope, "adv-minus")
	advPlusID := tree.ID(scope, "adv-plus")

	f.Column().FillWidth().Background(theme.Card).Border(1, theme.Border).Radius(12).Pad(14).Gap(9).Children(func() {
		f.Row().FillWidth().Items(flex.Center).Gap(8).Children(func() {
			f.Text(fmt.Sprintf("监控 %d", index+1)).Bold().FontSize(15)
			carno := strings.TrimSpace(form.carnoField.Text())
			if carno != "" {
				f.Text(carno).Muted().FontSize(12)
			} else {
				f.Text("可选，留空不启用").Muted().FontSize(12)
			}
			f.Spacer()
			f.Text(m.configSummary(index)).Muted().FontSize(11).Ellipsis()
			m.paintDeleteMonitorButton(f, theme, scope, m.monitors[index].id, len(m.monitors) <= 1)
		})
		if m.deleteConfirm == m.monitors[index].id {
			f.Row().FillWidth().Background(theme.Muted).Radius(8).PadSides(10, 8, 10, 12).Gap(8).Items(flex.Center).Children(func() {
				f.Text("删除该监控卡片？").FontSize(12).Color(theme.Danger)
				f.Text("配置将一并删除，其他监控不受影响").FontSize(11).Muted()
				f.Spacer()
				if f.Button(tree.ID(scope, "delete-cancel"), "取消").Outline().Small().Clicked() {
					m.cancelDeleteMonitor()
				}
				if f.Button(tree.ID(scope, "delete-confirm"), "确认删除").Destructive().Small().Clicked() {
					m.pendingDelete = m.monitors[index].id
					m.requestPaint()
				}
			})
		}
		f.Row().FillWidth().Gap(10).Items(flex.End).Children(func() {
			f.Column().Width(180).Gap(4).Children(func() {
				f.Text("地区").FontSize(12).Muted()
				if i, ok := f.Select(regionScope, &form.regionSel, regionNames, form.region).Picked(); ok && i != form.region {
					form.region = i
					form.tokenValue = ""
					if m.isRunning(m.monitors[index].id) {
						m.stopMonitor(m.monitors[index].id)
					}
					m.log(fmt.Sprintf("监控 %d 已切换到%s，请重新获取 Token", index+1, regionList[i].Name))
				}
			})
			f.Column().Grow(1).Shrink(1).Gap(4).Children(func() {
				f.Text("车牌号").FontSize(12).Muted()
				f.Field(&form.carnoField, carnoFieldID, "车牌号", "例如：京A12345")
			})
		})
		m.paintHistoryTags(f, theme, index)
		f.Column().Gap(4).Children(func() {
			f.Text("Token（每个地区独立获取）").FontSize(12).Muted()
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				f.Input(components.InputSnapshot{
					ID: tokenFieldID, Label: "Token", Value: form.tokenValue,
					Placeholder: "点击右侧按钮自动获取", ReadOnly: true, Secure: true,
				}).Grow(1)
				if f.Button(autoBtnID, "点击获取").Outline().Clicked() {
					m.captureToken(m.monitors[index].id)
				}
			})
		})
		f.Column().Gap(4).Children(func() {
			f.Text("监控时间段（24 小时制，按本机时间）").FontSize(12).Muted()
			f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
				f.Field(&form.fromField, fromFieldID, "监控开始时间", defaultMonitorFrom).Grow(1)
				f.Text("至").Muted().Self(flex.Center)
				f.Field(&form.toField, toFieldID, "监控结束时间", defaultMonitorTo).Grow(1)
				f.Text("支持跨午夜；起止相同为全天").FontSize(11).Muted()
			})
		})
		f.Row().FillWidth().Gap(10).Items(flex.End).Children(func() {
			f.Column().Grow(1).Shrink(1).Gap(4).Children(func() {
				f.Text("查询间隔（分钟，最低 5）").FontSize(12).Muted()
				f.Row().Gap(6).Items(flex.Center).Children(func() {
					f.Field(&form.intervalField, intervalFieldID, "查询间隔", "最低 5").Grow(1)
					if i, ok := f.Select(intervalScope, &form.intervalSel, intervalPresets, -1).Placeholder("常用值").Width(100).Picked(); ok {
						form.intervalField.SetText(intervalPresets[i])
					}
				})
			})
			f.Column().Grow(1).Shrink(1).Gap(4).Children(func() {
				f.Text("计时时长（分钟）").FontSize(12).Muted()
				f.Row().Gap(6).Items(flex.Center).Children(func() {
					f.Field(&form.freeField, freeFieldID, "计时时长", "默认 15").Grow(1)
					if i, ok := f.Select(freeScope, &form.freeSel, freePresets, -1).Placeholder("常用值").Width(100).Picked(); ok {
						form.freeField.SetText(freePresets[i])
					}
				})
			})
		})
		f.Column().Gap(4).Children(func() {
			f.Text("提前提醒").FontSize(12).Muted()
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				f.Field(&form.advanceField, advanceFieldID, "提前提醒", "0").Grow(1)
				if i, ok := f.Select(unitScope, &form.unitSel, unitLabels, form.unit).Width(100).Picked(); ok {
					form.unit = i
				}
				if f.Button(advMinusID, "-").Outline().Width(36).Clicked() {
					m.stepAdvance(m.monitors[index].id, -1)
				}
				if f.Button(advPlusID, "+").Outline().Width(36).Clicked() {
					m.stepAdvance(m.monitors[index].id, 1)
				}
			})
		})
	})
}

func (m *model) paintDeleteMonitorButton(f *flex.Frame, theme ui.Theme, scope ui.Scope, id string, disabled bool) {
	if disabled {
		return
	}
	color := theme.Danger
	button := f.ButtonBase(tree.ID(scope, "delete-config"), "删除监控").
		Size(30, 30).Radius(7).Background(theme.Card).
		Hover(flex.Look{}.Background(theme.Muted))
	button.Children(func() {
		f.Icon(trashIcon).Size(15, 15).Color(color).Self(flex.Center)
	})
	if button.Clicked() {
		m.requestDeleteMonitor(id)
	}
}

func (m *model) paintSharedSettings(f *flex.Frame, theme ui.Theme) {
	f.Column().FillWidth().Background(theme.Card).Border(1, theme.Border).Radius(12).Pad(14).Gap(10).Children(func() {
		f.Text("共用通知设置").Bold().FontSize(15)

		f.Column().Gap(4).Children(func() {
			f.Text("倒计时提醒方式").FontSize(12).Muted()
			if i, ok := f.Choice(notifyScope, notifyLabels, m.notify).ChosenIndex(); ok {
				m.notify = i
				m.requestPaint()
			}
			f.Text("侧边：系统通知，不打断当前操作；弹出框：窗口居中并置顶").FontSize(11).Muted()
		})

		f.Column().Gap(4).Children(func() {
			f.Text("远程通知（同步到手机）").FontSize(12).Muted()
			if i, ok := f.Choice(remoteScope, remoteLabels, m.remote).ChosenIndex(); ok {
				m.remote = i
				m.requestPaint()
			}
			switch remoteKeys[m.remote] {
			case "pushplus":
				pushToken := strings.TrimSpace(m.pushTokenField.Text())
				f.Row().Gap(8).Items(flex.Center).Children(func() {
					f.Field(&m.pushTokenField, pushTokenFieldID, "PushPlus Token", "pushplus.plus → 一对一推送 → 用户 Token").Grow(1)
					if f.Button(testRemoteBtnID, "发送测试").Outline().
						Disabled(pushToken == "" || m.testingRemoteNow()).
						Clicked() {
						m.sendTestRemote()
					}
				})
				f.Text("关注「pushplus 推送加」公众号即可收到，实名后 200 条/天").FontSize(11).Muted()
			case "ntfy":
				ntfyTopic := strings.TrimSpace(m.ntfyTopicField.Text())
				f.Row().Gap(8).Items(flex.Center).Children(func() {
					f.Field(&m.ntfyTopicField, ntfyTopicFieldID, "ntfy 主题", "例如：parking-zx-9281").Grow(1)
					if f.Button(testRemoteBtnID, "发送测试").Outline().
						Disabled(!ntfyTopicPattern.MatchString(ntfyTopic) || m.testingRemoteNow()).
						Clicked() {
						m.sendTestRemote()
					}
				})
				f.Text("手机 ntfy App 订阅同一主题即可收到，公共服务器 ntfy.sh，250 条/天").FontSize(11).Muted()
			}
		})

	})
}

func (m *model) testingRemoteNow() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.testingRemote
}

// sendTestRemote pushes one message with what is typed right now, so the
// channel can be verified before the configuration is saved.
func (m *model) sendTestRemote() {
	var cfg Config
	switch remoteKeys[m.remote] {
	case "pushplus":
		cfg.RemoteChannel = "pushplus"
		cfg.PushPlusToken = strings.TrimSpace(m.pushTokenField.Text())
	case "ntfy":
		cfg.RemoteChannel = "ntfy"
		cfg.NtfyTopic = strings.TrimSpace(m.ntfyTopicField.Text())
	default:
		return
	}
	notifier := newRemoteNotifier(cfg)
	if notifier == nil {
		return
	}

	m.mu.Lock()
	if m.testingRemote {
		m.mu.Unlock()
		return
	}
	m.testingRemote = true
	m.mu.Unlock()
	m.requestPaint()

	m.log("正在发送测试消息（" + notifier.label() + "）...")
	go func() {
		err := notifier.send("停车时长监控测试消息", "这是一条测试消息，收到说明远程通知配置成功。")
		m.mu.Lock()
		m.testingRemote = false
		m.mu.Unlock()
		m.requestPaint()
		if err != nil {
			m.log("测试消息发送失败（" + notifier.label() + "）：" + err.Error())
			m.showError(err)
			return
		}
		m.log("测试消息已发送（" + notifier.label() + "），请检查手机是否收到")
		m.showNotice("测试消息已发送，请检查手机是否收到")
	}()
}

func (m *model) paintHistoryTags(f *flex.Frame, theme ui.Theme, index int) {
	m.mu.Lock()
	history := append([]string(nil), m.cfg.HistoryCarnos...)
	m.mu.Unlock()
	if len(history) == 0 {
		return
	}

	const perRow, rowLimit = 4, 8
	shown := history
	if !m.monitors[index].historyExpanded && len(shown) > rowLimit {
		shown = shown[:rowLimit]
	}
	for start := 0; start < len(shown); start += perRow {
		end := min(start+perRow, len(shown))
		f.Row().Gap(6).Children(func() {
			for i := start; i < end; i++ {
				m.paintHistoryTag(f, theme, index, shown[i])
			}
		})
	}
	if len(history) > rowLimit {
		label := fmt.Sprintf("展开（%d）", len(history))
		if m.monitors[index].historyExpanded {
			label = "收起"
		}
		toggleID := tree.ID(m.monitors[index].scope, "history-toggle")
		if f.Button(toggleID, label).Ghost().Clicked() {
			m.monitors[index].historyExpanded = !m.monitors[index].historyExpanded
			m.requestPaint()
		}
	}
}

func (m *model) paintHistoryTag(f *flex.Frame, theme ui.Theme, index int, carno string) {
	scope := m.monitors[index].scope
	useID := tree.ID(tree.Sub(scope, tree.Name("history")), carno)
	deleteID := tree.ID(tree.Sub(scope, tree.Name("history-delete")), carno)
	f.Row().Height(26).Background(theme.Muted).Radius(13).PadSides(0, 6, 0, 10).Gap(6).Items(flex.Center).Children(func() {
		if f.Text(carno).FontSize(12).Hit(useID, "使用车牌 "+carno).Clicked() {
			m.monitors[index].form.carnoField.SetText(carno)
			m.requestPaint()
		}
		if f.Text("×").FontSize(14).Color(theme.MutedForeground).Hit(deleteID, "删除历史车牌 "+carno).HitOutset(6, 6, 6, 6).Clicked() {
			m.deleteHistory(carno)
		}
	})
}

func (m *model) saveConfigFields(update func(*Config)) error {
	_, err := m.persistConfig(update)
	return err
}

func (m *model) deleteHistory(carno string) {
	m.mu.Lock()
	previous := append([]string(nil), m.cfg.HistoryCarnos...)
	list := append([]string(nil), previous...)
	idx := -1
	for i, v := range list {
		if v == carno {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.mu.Unlock()
		return
	}
	list = append(list[:idx], list[idx+1:]...)
	m.cfg.HistoryCarnos = list
	m.mu.Unlock()

	if err := m.saveConfigFields(func(cfg *Config) {
		cfg.HistoryCarnos = list
	}); err != nil {
		m.mu.Lock()
		m.cfg.HistoryCarnos = previous
		m.mu.Unlock()
		m.log("保存历史车牌失败：" + err.Error())
	}
	m.showToast("已删除历史车牌 "+carno, func() { m.restoreHistory(carno, idx) })
}

func (m *model) restoreHistory(carno string, idx int) {
	m.mu.Lock()
	previous := append([]string(nil), m.cfg.HistoryCarnos...)
	list := append([]string(nil), previous...)
	exists := false
	for _, v := range list {
		if v == carno {
			exists = true
			break
		}
	}
	if !exists {
		if idx < 0 || idx > len(list) {
			idx = 0
		}
		list = append(list[:idx], append([]string{carno}, list[idx:]...)...)
	}
	m.cfg.HistoryCarnos = list
	m.mu.Unlock()

	if err := m.saveConfigFields(func(cfg *Config) {
		cfg.HistoryCarnos = list
	}); err != nil {
		m.log("恢复历史车牌失败：" + err.Error())
	}
	m.requestPaint()
}

func (m *model) showToast(message string, undo func()) {
	m.startToast(message, 10*time.Second, undo)
}

func (m *model) showNotice(message string) {
	m.startToast(message, 4*time.Second, nil)
}

func (m *model) startToast(message string, lifetime time.Duration, undo func()) {
	m.mu.Lock()
	m.toastID++
	id := m.toastID
	m.toast = &toastState{
		message: message,
		undo:    undo,
		id:      id,
		expires: time.Now().Add(lifetime),
		total:   lifetime,
	}
	m.mu.Unlock()
	m.requestPaint()

	time.AfterFunc(lifetime, func() {
		m.mu.Lock()
		t := m.toast
		expired := t != nil && t.id == id && !time.Now().Before(t.expires)
		if expired {
			m.toast = nil
		}
		m.mu.Unlock()
		if expired {
			m.requestPaint()
		}
	})
}

func (m *model) dismissToast() {
	m.mu.Lock()
	m.toast = nil
	m.mu.Unlock()
	m.requestPaint()
}

func (m *model) undoToast() {
	m.mu.Lock()
	t := m.toast
	m.toast = nil
	m.mu.Unlock()
	if t != nil && t.undo != nil {
		t.undo()
	}
	m.requestPaint()
}

func (m *model) paintToast(f *flex.Frame, bounds ui.Rect, theme ui.Theme, toast *toastState) {
	if toast == nil {
		return
	}
	const width, height = float32(360), float32(96)
	fraction := float32(0)
	if toast.total > 0 {
		remaining := time.Until(toast.expires)
		if remaining > 0 {
			fraction = float32(remaining) / float32(toast.total)
		}
	}
	f.Popup().At(bounds.Width-width-20, bounds.Height-height-20).Width(width).Children(func() {
		f.Column().FillWidth().Height(height).Background(theme.CardElevated).Border(1, theme.Border).Radius(12).Pad(12).Gap(8).Children(func() {
			f.Text(toast.message).FontSize(13)
			f.Row().FillWidth().Gap(8).Justify(flex.End).Children(func() {
				if f.Button(toastCloseID, "关闭").Ghost().Clicked() {
					m.dismissToast()
				}
				if toast.undo != nil {
					if f.Button(toastUndoID, "撤销").Outline().Clicked() {
						m.undoToast()
					}
				}
			})
			f.Slot().FillWidth().Height(3).Background(theme.Muted).Radius(2).DrawOver(func(d *flex.DrawContext) {
				if fraction > 0 {
					d.Canvas.RequestFrame()
				}
				progress := d.Rect.Width * fraction
				if progress <= 0 {
					return
				}
				d.Canvas.Rounded(ui.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: progress, Height: d.Rect.Height}, 2, theme.Primary)
			})
		})
	})
}

func (m *model) paintStatus(f *flex.Frame, theme ui.Theme, index int, st MonitorStatus) {
	form := &m.monitors[index].form
	scope := m.monitors[index].scope
	m.mu.Lock()
	updating := m.updating
	updatePct := m.updatePct
	updateVersion := m.updateVersion
	m.mu.Unlock()

	f.Column().FillWidth().Background(theme.Card).Border(1, theme.Border).Radius(12).Pad(14).Gap(7).Children(func() {
		phase := st.Phase
		if st.Running {
			phase += " · 监控中"
		}
		f.Row().FillWidth().Items(flex.Center).Gap(8).Children(func() {
			f.Text(fmt.Sprintf("监控 %d", index+1)).Bold().FontSize(14)
			f.Text(phase).FontSize(12).Muted()
			f.Spacer()
			f.Text(m.configSummary(index)).Muted().FontSize(11).Ellipsis()
		})
		f.Text(fmt.Sprintf("监控时段 %s–%s", form.fromField.Text(), form.toField.Text())).FontSize(11).Muted()

		if updating && index == 0 {
			f.Row().FillWidth().Items(flex.Center).Gap(12).Children(func() {
				f.Text("下载中 v" + updateVersion + " · " + strconv.Itoa(updatePct) + "%").Muted().FontSize(12).Width(150)
				f.Slot().FillWidth().Height(6).Background(theme.Muted).Radius(3).DrawOver(func(d *flex.DrawContext) {
					progress := d.Rect.Width * float32(updatePct) / 100
					if progress <= 0 {
						return
					}
					d.Canvas.Rounded(ui.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: progress, Height: d.Rect.Height}, 3, theme.Primary)
				})
			})
		}

		if st.InPark {
			label := "距离 " + strconv.Itoa(m.currentFreeMinutes(index)) + " 分钟倒计时结束"
			if st.Paused {
				label = "倒计时已随查询暂停"
			}
			f.Text(label).Muted().Self(flex.Center)
			countdown := formatRemaining(st.Remaining)
			txt := f.Text(countdown).FontSize(38).Bold().Self(flex.Center)
			if st.Remaining <= 0 {
				txt.Color(theme.Danger)
			} else {
				txt.Color(theme.Success)
			}
			f.Row().Gap(28).Children(func() {
				f.Text("入场时间：" + st.Entry.Format("2006-01-02 15:04:05")).Width(245)
				f.Text("已停时长：" + formatDuration(st.Elapsed))
			})
		} else {
			f.Text("--:--").FontSize(38).Bold().Muted().Self(flex.Center)
			f.Row().Gap(28).Children(func() {
				f.Text("入场时间：-").Muted().Width(245)
				f.Text("已停时长：-").Muted()
			})
		}
		if !st.LastQuery.IsZero() {
			f.Text("上次查询：" + st.LastQuery.Format("15:04:05")).Muted().FontSize(12)
		}
		if st.Detail != "" {
			f.Text(st.Detail).Muted().FontSize(12)
		}
		f.Row().FillWidth().Items(flex.Center).Gap(8).Children(func() {
			if f.Button(tree.ID(scope, "start"), "开始").Disabled(st.Running).Clicked() {
				m.startMonitor(m.monitors[index].id)
			}
			if f.Button(tree.ID(scope, "stop"), "停止").Outline().Disabled(!st.Running).Clicked() {
				m.stopMonitor(m.monitors[index].id)
			}
			if f.Button(tree.ID(scope, "paid"), "已离场").Outline().Disabled(!st.InPark).Clicked() {
				m.markPaid(m.monitors[index].id)
				m.showNotice(fmt.Sprintf("监控 %d 已标记离场", index+1))
			}
			f.Spacer()
			f.Text("每张卡片独立运行").Muted().FontSize(11)
		})
	})
}

func (m *model) currentFreeMinutes(index int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(m.monitors[index].form.freeField.Text())); err == nil && v > 0 {
		return v
	}
	return 15
}

func (m *model) paintStats(f *flex.Frame, theme ui.Theme, index int) {
	carno := strings.TrimSpace(m.monitors[index].form.carnoField.Text())
	buckets := buildChartBuckets(m.entries.snapshot(), carno, m.monitors[index].statsRange, time.Now())
	statsScope := tree.Sub(m.monitors[index].scope, tree.Name("stats-range"))
	f.Column().FillWidth().Gap(6).Children(func() {
		f.Row().FillWidth().Items(flex.Center).Gap(8).Children(func() {
			f.Text("入场统计").Bold().FontSize(13)
			label := carno
			if label == "" {
				label = "未配置"
			}
			f.Text(label).Muted().FontSize(12)
			f.Spacer()
			if i, ok := f.Choice(statsScope, statsLabels, m.monitors[index].statsRange).ChosenIndex(); ok {
				m.monitors[index].statsRange = i
				m.requestPaint()
			}
		})
		f.Slot().FillWidth().Height(138).DrawOver(func(d *flex.DrawContext) {
			paintEntryChart(d, theme, buckets)
		})
	})
}

func (m *model) paintMonitorCard(f *flex.Frame, theme ui.Theme, index int, st MonitorStatus) {
	f.Column().FillWidth().Background(theme.Card).Border(1, theme.Border).Radius(12).Pad(2).Gap(0).Children(func() {
		m.paintStatus(f, theme, index, st)
		f.Slot().FillWidth().Height(1).Background(theme.Border).MarginX(14)
		f.Column().FillWidth().Pad(12).Children(func() {
			m.paintStats(f, theme, index)
		})
	})
}
func paintEntryChart(d *flex.DrawContext, theme ui.Theme, buckets []chartBucket) {
	rect := d.Rect
	if rect.Width < 40 || rect.Height < 40 {
		return
	}
	total, maxCount := 0, 0
	for _, b := range buckets {
		total += b.Count
		if b.Count > maxCount {
			maxCount = b.Count
		}
	}
	if total == 0 {
		d.Canvas.LabelCenter("暂无入场记录",
			ui.Rect{X: rect.X, Y: rect.Y + rect.Height/2 - 8, Width: rect.Width, Height: 16},
			ui.TextStyle{Size: 12, Color: theme.MutedForeground})
		return
	}

	const labelH, valueH = float32(16), float32(14)
	plot := ui.Rect{X: rect.X + 4, Y: rect.Y + valueH, Width: rect.Width - 8, Height: rect.Height - valueH - labelH}
	if plot.Width < 20 || plot.Height < 20 {
		return
	}

	gap := float32(4)
	if len(buckets) > 14 {
		gap = 2
	}
	barW := (plot.Width - gap*float32(len(buckets)-1)) / float32(len(buckets))
	if barW <= 0 {
		return
	}
	scale := float32(maxCount + 1)

	for i := 0; i <= 2; i++ {
		y := plot.Y + plot.Height*float32(i)/2
		d.Canvas.Fill(ui.Rect{X: plot.X, Y: y, Width: plot.Width, Height: 1}, theme.Border)
	}

	for i, b := range buckets {
		x := plot.X + float32(i)*(barW+gap)
		if b.Count > 0 {
			h := float32(b.Count) / scale * plot.Height
			if h < 2 {
				h = 2
			}
			y := plot.Y + plot.Height - h
			color := theme.Primary
			if b.Current {
				color = theme.Success
			}
			d.Canvas.Rounded(ui.Rect{X: x, Y: y, Width: barW, Height: h}, min(float32(3), h/2), color)
			d.Canvas.LabelCenter(strconv.Itoa(b.Count),
				ui.Rect{X: x - 4, Y: y - valueH, Width: barW + 8, Height: valueH},
				ui.TextStyle{Size: 10, Color: theme.MutedForeground})
		}
		if b.ShowLabel {
			d.Canvas.LabelCenter(b.Label,
				ui.Rect{X: x - 10, Y: plot.Y + plot.Height + 2, Width: barW + 20, Height: labelH},
				ui.TextStyle{Size: 10, Color: theme.MutedForeground})
		}
	}
}

func (m *model) paintLogBar(f *flex.Frame) {
	f.Row().FillWidth().Items(flex.Center).Gap(8).Children(func() {
		f.Text("监控日志").Bold().FontSize(14)
		if f.Button(logViewID, "查看").Outline().Clicked() {
			m.logsOpen = true
			m.requestPaint()
		}
	})
}

func (m *model) paintLogDialog(f *flex.Frame, theme ui.Theme, logs []string) {
	if !m.logsOpen {
		return
	}
	d := f.Dialog(flex.DialogSpec{
		Title:     "监控日志",
		Width:     680,
		Height:    520,
		Scope:     logDialogSurface,
		DismissID: logDialogDismiss,
		CloseID:   logDialogDismiss,
	})
	d.Body(func() {
		f.Row().FillWidth().Grow(1).Shrink(1).Gap(8).Children(func() {
			f.ScrollArea(logAreaID).Grow(1).Shrink(1).FillHeight().Follow(true).Gap(4).Children(func() {
				if len(logs) == 0 {
					f.Text("暂无日志").Muted().FontSize(12)
				}
				for _, line := range logs {
					f.Text(line).FontSize(12).Color(theme.Foreground)
				}
			})
			m.paintLogScrollbar(f, theme)
		})
		if dir := m.logFile.Dir(); dir != "" {
			f.Text("日志文件：" + dir + "（保留 " + strconv.Itoa(logRetentionDays) + " 天）").Muted().FontSize(11)
		}
	})
	d.Footer(func() {
		if f.Button(logClearID, "清空列表").Outline().Clicked() {
			m.mu.Lock()
			m.logs = nil
			m.mu.Unlock()
			m.requestPaint()
		}
		if f.Button(logDialogCloseID, "关闭").Clicked() {
			m.logsOpen = false
			m.requestPaint()
		}
	})
	if d.Dismissed() {
		m.logsOpen = false
	}
}

func (m *model) paintLogScrollbar(f *flex.Frame, theme ui.Theme) {
	state := m.view.ScrollState(logAreaID)
	limit := state.Max()
	if limit <= 0 {
		return
	}
	m.logScroll.Value = limit - state.Offset
	bar := f.SliderBase(logScrollID, &m.logScroll).Vertical().Width(16).Shrink(1).FillHeight().Range(0, limit).Step(1)
	bar.Draw(func(d *flex.DrawContext) {
		track := ui.Rect{X: d.Rect.X + d.Rect.Width/2 - 2, Y: d.Rect.Y, Width: 4, Height: d.Rect.Height}
		d.Canvas.Rounded(track, 2, theme.Border)
		thumbHeight := d.Rect.Height
		if state.Content > state.Viewport && state.Content > 0 {
			thumbHeight *= state.Viewport / state.Content
		}
		thumbHeight = min(max(thumbHeight, 24), d.Rect.Height)
		y := d.Rect.Y + (1-m.logScroll.Fraction())*(d.Rect.Height-thumbHeight)
		d.Canvas.Rounded(ui.Rect{X: d.Rect.X + d.Rect.Width/2 - 4, Y: y, Width: 8, Height: thumbHeight}, 4, theme.MutedForeground)
	})
	if bar.Changed() {
		state.ScrollTo(limit - m.logScroll.Value)
	}
}

func (m *model) paintDialog(f *flex.Frame, alert *alertState) {
	if alert == nil {
		return
	}
	d := f.Dialog(flex.DialogSpec{
		Title:     alert.title,
		Width:     460,
		Scope:     dialogSurface,
		DismissID: dialogDismiss,
		CloseID:   dialogDismiss,
	})
	d.Body(func() {
		f.Paragraph(alert.message, 6).FontSize(13)
	})
	d.Footer(func() {
		if alert.kind == alertUpdate {
			if f.Button(dialogUpdateID, "下载并更新").Clicked() {
				if alert.update != nil {
					m.downloadAndUpdate(alert.update)
				}
				m.clearAlert()
			}
			if f.Button(dialogOKID, "稍后").Outline().Clicked() {
				m.clearAlert()
			}
			return
		}
		if alert.kind == alertFree {
			if f.Button(dialogPaidID, "已离场").Outline().Clicked() {
				m.markPaid(alert.monitor)
				m.clearAlert()
			}
		}
		if f.Button(dialogOKID, "好").Clicked() {
			m.clearAlert()
		}
	})
	if d.Dismissed() {
		m.clearAlert()
	}
}

func (m *model) requestClose(allow func()) {
	m.mu.Lock()
	action := m.cfg.CloseAction
	m.closeAllow = allow
	if action != "" {
		m.mu.Unlock()
		if action == "tray" {
			m.minimizeToTray()
		} else {
			m.exitApp()
		}
		return
	}
	m.closeDontAsk = false
	m.closeConfirm = true
	m.mu.Unlock()
	m.requestPaint()
}

func (m *model) paintCloseDialog(f *flex.Frame, theme ui.Theme) {
	if !m.closeConfirm {
		return
	}
	d := f.Dialog(flex.DialogSpec{
		Title:     "关闭停车时长监控？",
		Width:     440,
		Scope:     closeSurface,
		DismissID: closeDismiss,
		CloseID:   closeDismiss,
	})
	d.Body(func() {
		f.Column().Gap(12).Children(func() {
			f.Paragraph("最小化到托盘后，监控和后台任务会继续运行；退出应用会停止正在进行的监控。", 3).FontSize(13)
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				if f.Checkbox(closeDontAskID, m.closeDontAsk).Clicked() {
					m.closeDontAsk = !m.closeDontAsk
					m.requestPaint()
				}
				f.Text("不再询问").FontSize(13)
			})
		})
	})
	d.Footer(func() {
		if f.Button(closeCancelID, "取消").Outline().Clicked() {
			m.cancelClose()
		}
		if f.Button(closeExitID, "退出应用").Destructive().Clicked() {
			m.rememberCloseAction("exit")
			m.exitApp()
		}
		if f.Button(closeTrayID, "最小化到托盘").Clicked() {
			m.rememberCloseAction("tray")
			m.minimizeToTray()
		}
	})
	if d.Dismissed() {
		m.cancelClose()
	}
}

func (m *model) cancelClose() {
	m.mu.Lock()
	m.closeConfirm = false
	m.closeDontAsk = false
	m.closeAllow = nil
	m.mu.Unlock()
	m.requestPaint()
}

func (m *model) rememberCloseAction(action string) {
	if !m.closeDontAsk {
		return
	}
	m.mu.Lock()
	m.cfg.CloseAction = action
	m.closeDontAsk = false
	m.mu.Unlock()
	if err := m.saveConfigFields(func(cfg *Config) {
		cfg.CloseAction = action
	}); err != nil {
		m.log("保存关闭行为失败：" + err.Error())
	}
}

func (m *model) minimizeToTray() {
	m.mu.Lock()
	m.closeConfirm = false
	m.closeAllow = nil
	m.mu.Unlock()
	if m.window != nil {
		m.window.Hide()
	}
}

func (m *model) showWindow() {
	if m.window != nil {
		m.window.Show()
	}
}

func (m *model) exitApp() {
	m.mu.Lock()
	allow := m.closeAllow
	m.closeAllow = nil
	m.closeConfirm = false
	m.mu.Unlock()
	m.shutdown()
	if m.tray != nil {
		m.tray.Remove()
	}
	if allow != nil {
		allow()
	} else if m.application != nil {
		m.application.Quit()
	}
}

func (m *model) quitFromTray() {
	m.shutdown()
	if m.tray != nil {
		m.tray.Remove()
	}
	if m.application != nil {
		m.application.Quit()
	}
}

func (m *model) textInputs(context app.TextInputContext) *app.TextInput {
	fields := []*flex.Field{&m.pushTokenField, &m.ntfyTopicField}
	for i := range m.monitors {
		form := &m.monitors[i].form
		fields = append(fields, &form.carnoField, &form.intervalField, &form.freeField,
			&form.advanceField, &form.fromField, &form.toField)
	}
	return flex.Fields(fields...)(context)
}

func (m *model) appConfig() app.Config {
	config := app.Config{
		Title:          "停车时长监控",
		Width:          760,
		Height:         900,
		MinWidth:       640,
		MinHeight:      720,
		Invalidator:    m.invalidator,
		TextInput:      m.textInputs,
		OnScroll:       m.view.Wheel,
		OnCloseRequest: m.requestClose,
		OnClosed:       m.shutdown,
	}
	config.Paint = m.paint
	return config
}

func main() {
	if !ensureSingleInstance() {
		return
	}

	cfg := loadConfig()
	m := newModel(cfg)
	m.tokenProxy = &TokenProxy{}

	var invalidator app.Invalidator
	m.invalidator = &invalidator
	m.log("就绪：点击右上角“配置”填写车牌与 Token，然后点“开始监控”")
	m.startUpdateChecker()

	config := m.appConfig()

	if err := app.RunApplication(func(application *app.Application) error {
		var canvas ui.Canvas
		window, err := application.OpenWindow(config, &canvas)
		if err != nil {
			return err
		}
		m.window = window
		m.application = application
		if item, err := application.AddStatusItem(app.StatusItemConfig{
			Tooltip: "停车时长监控 v" + appVersion,
			Icon:    trayIcon(),
			OnClick: func(app.StatusClick) { m.showWindow() },
			Menu: func() []app.MenuItem {
				return []app.MenuItem{
					{Title: "打开主界面", OnSelect: m.showWindow},
					{Separator: true},
					{Title: "退出", OnSelect: m.quitFromTray},
				}
			},
		}); err == nil {
			m.tray = item
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func parseIntervalMinutes(s string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 5 || v > 1440 {
		return 0, errors.New("请输入 5-1440 之间的整数分钟")
	}
	return v, nil
}

func parseFreeMinutes(s string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 1 || v > 1440 {
		return 0, errors.New("请输入 1-1440 之间的整数分钟")
	}
	return v, nil
}

func advanceToSeconds(value, unit string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || v < 0 || v > 1440*60 {
		return 0, errors.New("请输入 0 或正整数")
	}
	if strings.TrimSpace(unit) == "分钟" {
		v *= 60
	}
	return v, nil
}

func secondsToAdvance(sec int) (int, string) {
	if sec > 0 && sec%60 != 0 {
		return sec, "秒"
	}
	return sec / 60, "分钟"
}

func describeAdvance(sec int) string {
	if sec <= 0 {
		return "不提前"
	}
	value, unit := secondsToAdvance(sec)
	return fmt.Sprintf("提前 %d %s", value, unit)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d小时%02d分%02d秒", h, m, s)
	}
	return fmt.Sprintf("%d分%02d秒", m, s)
}

func formatRemaining(d time.Duration) string {
	sign := ""
	if d < 0 {
		sign = "-"
		d = -d
	}
	total := int(d / time.Second)
	return fmt.Sprintf("%s%02d:%02d", sign, total/60, total%60)
}
