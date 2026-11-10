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

	carnoFieldID    = tree.ID(page, "carno")
	tokenFieldID    = tree.ID(page, "token")
	intervalFieldID = tree.ID(page, "interval")
	freeFieldID     = tree.ID(page, "free")
	advanceFieldID  = tree.ID(page, "advance")
	saveBtnID       = tree.ID(page, "save")
	startBtnID      = tree.ID(page, "start")
	stopBtnID       = tree.ID(page, "stop")
	autoBtnID       = tree.ID(page, "auto")
	advMinusID      = tree.ID(page, "adv-minus")
	advPlusID       = tree.ID(page, "adv-plus")
	logAreaID       = tree.ID(page, "log")
	logScrollID     = tree.ID(page, "log-scroll")
	configBtnID     = tree.ID(page, "config")
	configCloseID   = tree.ID(page, "config-close")

	intervalScope = tree.Sub(page, tree.Name("interval-select"))
	freeScope     = tree.Sub(page, tree.Name("free-select"))
	regionScope   = tree.Sub(page, tree.Name("region-select"))
	unitScope     = tree.Sub(page, tree.Name("unit-select"))

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

	historyScope       = tree.Sub(page, tree.Name("history"))
	historyDeleteScope = tree.Sub(page, tree.Name("history-delete"))
	historyToggleID    = tree.ID(page, "history-toggle")
	toastUndoID        = tree.ID(page, "toast-undo")
	toastCloseID       = tree.ID(page, "toast-close")

	intervalPresets = []string{"5", "10", "15", "30"}
	freePresets     = []string{"10", "15", "20", "30", "60"}
	unitLabels      = []string{"分钟", "秒"}
)

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
}

type toastState struct {
	message string
	undo    func()
	id      int
	expires time.Time
	total   time.Duration
}

type model struct {
	mu sync.Mutex

	cfg Config

	carnoField    flex.Field
	intervalField flex.Field
	freeField     flex.Field
	advanceField  flex.Field

	intervalSel flex.SelectState
	freeSel     flex.SelectState
	unitSel     flex.SelectState
	regionSel   flex.SelectState
	unit        int
	region      int

	running        bool
	status         MonitorStatus
	logs           []string
	alert          *alertState
	configOpen     bool
	logScroll      flex.SliderState
	pendingUpdate  *updateInfo
	updatePrompted bool
	updating       bool

	closeConfirm    bool
	closeDontAsk    bool
	closeAllow      func()
	historyExpanded bool
	toast           *toastState
	toastID         int
	window          *app.Window
	application     *app.Application
	tray            *app.StatusItem

	tokenValue   string
	pendingToken string

	view        flex.View
	toastView   flex.View
	invalidator *app.Invalidator
	monitor     *Monitor
	tokenProxy  *TokenProxy
}

func newModel(cfg Config) *model {
	m := &model{cfg: cfg}
	m.carnoField.SetText(cfg.Carno)
	m.tokenValue = cfg.Token
	m.intervalField.SetText(strconv.Itoa(cfg.IntervalMinutes))
	m.freeField.SetText(strconv.Itoa(cfg.FreeMinutes))

	advValue, advUnit := secondsToAdvance(cfg.AdvanceSeconds)
	m.advanceField.SetText(strconv.Itoa(advValue))
	if advUnit == "秒" {
		m.unit = 1
	}
	m.region = regionIndex(cfg.Region)
	m.status.Phase = "未开始"
	return m
}

func (m *model) requestPaint() {
	if m.invalidator != nil {
		m.invalidator.Invalidate()
	}
}

func (m *model) log(msg string) {
	m.mu.Lock()
	m.logs = append(m.logs, time.Now().Format("15:04:05")+"  "+msg)
	if len(m.logs) > 150 {
		m.logs = m.logs[len(m.logs)-150:]
	}
	m.mu.Unlock()
	m.requestPaint()
}

func (m *model) setStatus(st MonitorStatus) {
	m.mu.Lock()
	m.status = st
	m.mu.Unlock()
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
	m.mu.Unlock()
	m.requestPaint()
}

func (m *model) onEntryAlert(info EntryInfo) {
	msg := fmt.Sprintf("车牌 %s 已入场。\n入场时间：%s\n地点：%s\n计时时长：%d 分钟",
		info.Carno, info.Entry.Format("2006-01-02 15:04:05"), info.ParkName, info.FreeMinutes)
	m.mu.Lock()
	m.alert = &alertState{kind: alertEntry, title: "车辆已入场", message: msg}
	m.mu.Unlock()
	m.log("检测到车辆入场：" + info.Carno)
	playAlertSound()
	go func() { _ = sendSystemNotification("车辆已入场", msg) }()
	m.requestPaint()
}

func (m *model) onFreeAlert(a AlertInfo) {
	var msg string
	if a.Remaining > 0 {
		msg = fmt.Sprintf("车牌 %s 已停车 %s（入场时间 %s）。\n预计还剩 %s 达到计时时长上限，请及时挪车或缴费。\n地点：%s",
			a.Carno, formatDuration(a.Elapsed), a.Entry.Format("01-02 15:04"), formatDuration(a.Remaining), a.ParkName)
	} else {
		msg = fmt.Sprintf("车牌 %s 已停车 %s（入场时间 %s）。\n%d 分钟计时时长已结束，请及时挪车或缴费。\n地点：%s",
			a.Carno, formatDuration(a.Elapsed), a.Entry.Format("01-02 15:04"), a.FreeMinutes, a.ParkName)
	}
	m.mu.Lock()
	m.alert = &alertState{kind: alertFree, title: "停车计时即将结束", message: msg}
	m.mu.Unlock()
	m.log("计时提醒已触发")
	playAlertSound()
	go func() { _ = sendSystemNotification("停车计时即将结束", msg) }()
	m.requestPaint()
}

func (m *model) collectConfig() (Config, error) {
	carno := strings.TrimSpace(m.carnoField.Text())
	token := strings.TrimSpace(m.tokenValue)
	if carno == "" {
		return Config{}, errors.New("请填写车牌号")
	}
	if strings.ContainsAny(carno, " \t\r\n") {
		return Config{}, errors.New("车牌号不能包含空格")
	}
	if token == "" {
		return Config{}, errors.New("请填写 Token")
	}
	interval, err := parseIntervalMinutes(m.intervalField.Text())
	if err != nil {
		return Config{}, fmt.Errorf("查询间隔无效：%v", err)
	}
	free, err := parseFreeMinutes(m.freeField.Text())
	if err != nil {
		return Config{}, fmt.Errorf("计时时长无效：%v", err)
	}
	advance, err := advanceToSeconds(m.advanceField.Text(), unitLabels[m.unit])
	if err != nil {
		return Config{}, fmt.Errorf("提前提醒无效：%v", err)
	}
	if advance > free*60 {
		return Config{}, errors.New("提前提醒时间不能大于计时时长")
	}
	m.mu.Lock()
	closeAction := m.cfg.CloseAction
	history := append([]string(nil), m.cfg.HistoryCarnos...)
	m.mu.Unlock()
	return Config{
		Region:          regionList[m.region].Key,
		Token:           token,
		Carno:           carno,
		IntervalMinutes: interval,
		FreeMinutes:     free,
		AdvanceSeconds:  advance,
		CloseAction:     closeAction,
		HistoryCarnos:   history,
	}, nil
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

func (m *model) stepAdvance(delta int) {
	v, err := strconv.Atoi(strings.TrimSpace(m.advanceField.Text()))
	if err != nil {
		v = 0
	}
	v += delta
	if v < 0 {
		v = 0
	}
	m.advanceField.SetText(strconv.Itoa(v))
	m.requestPaint()
}

func (m *model) saveConfigFromForm() {
	cfg, err := m.collectConfig()
	if err != nil {
		m.showError(err)
		return
	}
	cfg.HistoryCarnos = addHistory(cfg.HistoryCarnos, cfg.Carno)
	if err := saveConfig(cfg); err != nil {
		m.showError(err)
		return
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	m.log("配置已保存")
}

func (m *model) startMonitor() bool {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return false
	}
	m.mu.Unlock()

	cfg, err := m.collectConfig()
	if err != nil {
		m.showError(err)
		return false
	}
	cfg.HistoryCarnos = addHistory(cfg.HistoryCarnos, cfg.Carno)
	if err := saveConfig(cfg); err != nil {
		m.showError(err)
		return false
	}

	mon := NewMonitor(cfg, m.setStatus, m.onFreeAlert, m.onEntryAlert, m.log)

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return false
	}
	m.cfg = cfg
	old := m.monitor
	m.monitor = mon
	m.running = true
	m.mu.Unlock()

	if old != nil {
		old.Stop()
	}
	mon.Start()
	keepAwake(true)
	m.requestPaint()
	return true
}

func (m *model) stopMonitor() {
	m.mu.Lock()
	mon := m.monitor
	m.running = false
	m.mu.Unlock()
	if mon != nil {
		mon.Stop()
	}
	keepAwake(false)
	m.requestPaint()
}

func (m *model) markPaid() {
	m.mu.Lock()
	mon := m.monitor
	m.mu.Unlock()
	if mon != nil {
		mon.MarkPaid()
	}
}

func (m *model) captureToken() {
	if m.tokenProxy.IsRunning() {
		m.showInfo("提示", "代理已在运行，请先在微信中完成一次查询。")
		return
	}

	region := regionList[m.region]
	m.log("正在准备本地代理（" + region.Name + "）...")
	go func() {
		err := m.tokenProxy.Start(
			region.Hostname,
			func(token string) {
				m.mu.Lock()
				m.pendingToken = token
				m.cfg.Token = token
				cfg := m.cfg
				m.mu.Unlock()
				if saveErr := saveConfig(cfg); saveErr != nil {
					m.log("Token 已捕获，但保存失败：" + saveErr.Error())
				} else {
					m.log("Token 已自动获取并保存")
				}
				m.showInfo("获取成功", "Token 已填入并保存。")
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
			"代理已启动。请打开“"+region.MiniName+"”小程序，进入“自助缴费”页面点一次“查询缴费”，工具会自动捕获 Token。\n获取成功或 3 分钟后自动关闭代理。")
	}()
}

func (m *model) shutdown() {
	m.mu.Lock()
	mon := m.monitor
	proxy := m.tokenProxy
	m.mu.Unlock()
	if mon != nil {
		mon.Stop()
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
	st := m.status
	running := m.running
	logs := append([]string(nil), m.logs...)
	alert := m.alert
	pendingToken := m.pendingToken
	m.pendingToken = ""
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

	if pendingToken != "" {
		m.tokenValue = pendingToken
	}

	f := m.view.Begin(canvas, theme, bounds)
	f.PopupLayer(10)
	f.Column().Fill().Pad(20).Gap(12).Children(func() {
		f.Row().FillWidth().Items(flex.Center).Gap(8).Children(func() {
			f.Text("停车时长监控").Bold().FontSize(20)
			f.Text("v" + appVersion).Muted().FontSize(12).Self(flex.Center)
			f.Spacer()
			if f.Button(startBtnID, "开始监控").Disabled(running).Clicked() {
				m.startMonitor()
			}
			if f.Button(stopBtnID, "停止监控").Outline().Disabled(!running).Clicked() {
				m.stopMonitor()
			}
			if f.Button(configBtnID, "配置").Outline().Clicked() {
				m.configOpen = true
				m.requestPaint()
			}
		})
		m.paintStatus(f, theme, st)
		m.paintLog(f, theme, logs)
	})
	m.paintConfigDialog(f, theme)
	m.paintDialog(f, alert)
	m.paintCloseDialog(f, theme)
	f.End()

	if toast != nil {
		canvas.PushLayer(20)
		scope := ui.ID{}
		switch {
		case m.closeConfirm:
			scope = closeSurface
		case m.configOpen:
			scope = configSurface
		case alert != nil:
			scope = dialogSurface
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

func (m *model) configSummary() string {
	region := regionList[m.region].Name
	carno := strings.TrimSpace(m.carnoField.Text())
	if carno == "" {
		carno = "未填写车牌"
	}
	interval := strings.TrimSpace(m.intervalField.Text())
	free := strings.TrimSpace(m.freeField.Text())
	return fmt.Sprintf("%s · %s · 每 %s 分钟 · 计时 %s 分钟", region, carno, interval, free)
}

func (m *model) paintConfigDialog(f *flex.Frame, theme ui.Theme) {
	if !m.configOpen {
		return
	}
	d := f.Dialog(flex.DialogSpec{
		Title:     "监控配置",
		Width:     520,
		Scope:     configSurface,
		DismissID: configDismiss,
		CloseID:   configDismiss,
	})
	d.Body(func() {
		m.paintForm(f, theme)
	})
	d.Footer(func() {
		if f.Button(saveBtnID, "保存配置").Clicked() {
			m.saveConfigFromForm()
		}
		if f.Button(configCloseID, "关闭").Outline().Clicked() {
			m.configOpen = false
		}
	})
	if d.Dismissed() {
		m.configOpen = false
	}
}

func (m *model) paintForm(f *flex.Frame, theme ui.Theme) {
	f.Column().FillWidth().Gap(10).Children(func() {
		f.Column().Gap(4).Children(func() {
			f.Text("地区").FontSize(12).Muted()
			if i, ok := f.Select(regionScope, &m.regionSel, regionNames, m.region).Picked(); ok {
				if i != m.region {
					m.region = i
					m.tokenValue = ""
					if m.running {
						m.stopMonitor()
					}
					m.log("已切换到" + regionList[i].Name + "，请点击“点击获取”重新获取 Token")
				}
			}
		})

		f.Column().Gap(4).Children(func() {
			f.Text("车牌号").FontSize(12).Muted()
			f.Field(&m.carnoField, carnoFieldID, "车牌号", "例如：京A12345")
			m.paintHistoryTags(f, theme)
		})

		f.Column().Gap(4).Children(func() {
			f.Text("Token").FontSize(12).Muted()
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				f.Input(components.InputSnapshot{
					ID:          tokenFieldID,
					Label:       "Token",
					Value:       m.tokenValue,
					Placeholder: "点击右侧按钮自动获取",
					ReadOnly:    true,
					Secure:      true,
				}).Grow(1)
				if f.Button(autoBtnID, "点击获取").Outline().Clicked() {
					m.captureToken()
				}
			})
		})

		f.Column().Gap(4).Children(func() {
			f.Text("查询间隔（分钟，最低 5）").FontSize(12).Muted()
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				f.Field(&m.intervalField, intervalFieldID, "查询间隔", "最低 5").Grow(1)
				if i, ok := f.Select(intervalScope, &m.intervalSel, intervalPresets, -1).Placeholder("常用值").Width(110).Picked(); ok {
					m.intervalField.SetText(intervalPresets[i])
				}
			})
		})

		f.Column().Gap(4).Children(func() {
			f.Text("计时时长（分钟）").FontSize(12).Muted()
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				f.Field(&m.freeField, freeFieldID, "计时时长", "默认 15").Grow(1)
				if i, ok := f.Select(freeScope, &m.freeSel, freePresets, -1).Placeholder("常用值").Width(110).Picked(); ok {
					m.freeField.SetText(freePresets[i])
				}
			})
		})

		f.Column().Gap(4).Children(func() {
			f.Text("提前提醒").FontSize(12).Muted()
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				f.Field(&m.advanceField, advanceFieldID, "提前提醒", "0").Grow(1)
				if i, ok := f.Select(unitScope, &m.unitSel, unitLabels, m.unit).Width(100).Picked(); ok {
					m.unit = i
				}
				if f.Button(advMinusID, "-").Outline().Width(36).Clicked() {
					m.stepAdvance(-1)
				}
				if f.Button(advPlusID, "+").Outline().Width(36).Clicked() {
					m.stepAdvance(1)
				}
			})
		})

	})
}

func (m *model) paintHistoryTags(f *flex.Frame, theme ui.Theme) {
	m.mu.Lock()
	history := append([]string(nil), m.cfg.HistoryCarnos...)
	m.mu.Unlock()
	if len(history) == 0 {
		return
	}

	const perRow, rowLimit = 4, 8
	shown := history
	if !m.historyExpanded && len(shown) > rowLimit {
		shown = shown[:rowLimit]
	}
	for start := 0; start < len(shown); start += perRow {
		end := min(start+perRow, len(shown))
		f.Row().Gap(6).Children(func() {
			for i := start; i < end; i++ {
				m.paintHistoryTag(f, theme, shown[i])
			}
		})
	}
	if len(history) > rowLimit {
		label := fmt.Sprintf("展开（%d）", len(history))
		if m.historyExpanded {
			label = "收起"
		}
		if f.Button(historyToggleID, label).Ghost().Clicked() {
			m.historyExpanded = !m.historyExpanded
			m.requestPaint()
		}
	}
}

func (m *model) paintHistoryTag(f *flex.Frame, theme ui.Theme, carno string) {
	useID := tree.ID(historyScope, carno)
	deleteID := tree.ID(historyDeleteScope, carno)
	f.Row().Height(26).Background(theme.Muted).Radius(13).PadSides(0, 6, 0, 10).Gap(6).Items(flex.Center).Children(func() {
		if f.Text(carno).FontSize(12).Hit(useID, "使用车牌 "+carno).Clicked() {
			m.carnoField.SetText(carno)
			m.requestPaint()
		}
		if f.Text("×").FontSize(14).Color(theme.MutedForeground).Hit(deleteID, "删除历史车牌 "+carno).HitOutset(6, 6, 6, 6).Clicked() {
			m.deleteHistory(carno)
		}
	})
}

func (m *model) deleteHistory(carno string) {
	m.mu.Lock()
	list := append([]string(nil), m.cfg.HistoryCarnos...)
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
	cfg := m.cfg
	m.mu.Unlock()

	if err := saveConfig(cfg); err != nil {
		m.log("保存历史车牌失败：" + err.Error())
	}
	m.showToast("已删除历史车牌 "+carno, func() { m.restoreHistory(carno, idx) })
}

func (m *model) restoreHistory(carno string, idx int) {
	m.mu.Lock()
	list := append([]string(nil), m.cfg.HistoryCarnos...)
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
	cfg := m.cfg
	m.mu.Unlock()

	if err := saveConfig(cfg); err != nil {
		m.log("恢复历史车牌失败：" + err.Error())
	}
	m.requestPaint()
}

func (m *model) showToast(message string, undo func()) {
	const lifetime = 10 * time.Second
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

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			m.mu.Lock()
			t := m.toast
			expired := t != nil && t.id == id && !time.Now().Before(t.expires)
			if expired {
				m.toast = nil
			}
			m.mu.Unlock()
			if t == nil || t.id != id {
				return
			}
			m.requestPaint()
			if expired {
				return
			}
		}
	}()
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
				if f.Button(toastUndoID, "撤销").Outline().Clicked() {
					m.undoToast()
				}
			})
			f.Slot().FillWidth().Height(3).Background(theme.Muted).Radius(2).DrawOver(func(d *flex.DrawContext) {
				progress := d.Rect.Width * fraction
				if progress <= 0 {
					return
				}
				d.Canvas.Rounded(ui.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: progress, Height: d.Rect.Height}, 2, theme.Primary)
			})
		})
	})
}

func (m *model) paintStatus(f *flex.Frame, theme ui.Theme, st MonitorStatus) {
	f.Column().FillWidth().Background(theme.Card).Border(1, theme.Border).Radius(12).Pad(16).Gap(8).Children(func() {
		phase := st.Phase
		if st.Running {
			phase += " · 监控中"
		}
		f.Row().FillWidth().Items(flex.Center).Gap(12).Children(func() {
			f.Text("状态：" + phase).Bold().FontSize(14)
			f.Spacer()
			f.Text(m.configSummary()).Muted().FontSize(12)
		})

		if st.InPark {
			f.Text("距离 " + strconv.Itoa(m.currentFreeMinutes()) + " 分钟倒计时结束").Muted().Self(flex.Center)

			countdown := formatRemaining(st.Remaining)
			txt := f.Text(countdown).FontSize(46).Bold().Self(flex.Center)
			if st.Remaining <= 0 {
				txt.Color(theme.Danger)
			} else {
				txt.Color(theme.Success)
			}

			f.Row().Gap(48).Children(func() {
				f.Text("入场时间：" + st.Entry.Format("2006-01-02 15:04:05")).Width(230)
				f.Text("已停时长：" + formatDuration(st.Elapsed)).Width(180)
			})
			if !st.LastQuery.IsZero() {
				f.Text("上次查询：" + st.LastQuery.Format("15:04:05")).Muted()
			}
			if st.Detail != "" {
				f.Text(st.Detail).Muted()
			}
		} else {
			f.Text("--:--").FontSize(46).Bold().Muted().Self(flex.Center)
			f.Row().Gap(48).Children(func() {
				f.Text("入场时间：-").Muted().Width(230)
				f.Text("已停时长：-").Muted().Width(180)
			})
			if !st.LastQuery.IsZero() {
				f.Text("上次查询：" + st.LastQuery.Format("15:04:05")).Muted()
			}
			if st.Detail != "" {
				f.Text(st.Detail).Muted()
			}
		}
	})
}

func (m *model) currentFreeMinutes() int {
	if v, err := strconv.Atoi(strings.TrimSpace(m.freeField.Text())); err == nil && v > 0 {
		return v
	}
	return 15
}

func (m *model) paintLog(f *flex.Frame, theme ui.Theme, logs []string) {
	f.Column().FillWidth().Grow(1).Shrink(1).Background(theme.Card).Border(1, theme.Border).Radius(12).Pad(16).Gap(8).Children(func() {
		f.Text("运行日志").Bold().FontSize(14)
		f.Row().FillWidth().Grow(1).Shrink(1).Gap(8).Children(func() {
			f.ScrollArea(logAreaID).Grow(1).Shrink(1).FillHeight().Follow(true).Gap(4).Children(func() {
				for _, line := range logs {
					f.Text(line).FontSize(12).Color(theme.Foreground)
				}
			})
			m.paintLogScrollbar(f, theme)
		})
	})
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
			if f.Button(dialogPaidID, "已缴费").Outline().Clicked() {
				m.markPaid()
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
	cfg := m.cfg
	m.mu.Unlock()
	if err := saveConfig(cfg); err != nil {
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

	config := app.Config{
		Title:          "停车时长监控",
		Width:          760,
		Height:         900,
		MinWidth:       640,
		MinHeight:      720,
		Invalidator:    &invalidator,
		TextInput:      flex.Fields(&m.carnoField, &m.intervalField, &m.freeField, &m.advanceField),
		OnScroll:       m.view.Wheel,
		OnCloseRequest: m.requestClose,
		OnClosed:       m.shutdown,
	}
	config.Paint = m.paint

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
