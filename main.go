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

	intervalScope = tree.Sub(page, tree.Name("interval-select"))
	freeScope     = tree.Sub(page, tree.Name("free-select"))
	unitScope     = tree.Sub(page, tree.Name("unit-select"))

	dialogScope   = tree.Sub(page, tree.Name("dialog"))
	dialogSurface = tree.ID(dialogScope, "surface")
	dialogDismiss = tree.ID(dialogScope, "dismiss")
	dialogOKID    = tree.ID(dialogScope, "ok")
	dialogPaidID  = tree.ID(dialogScope, "paid")

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
)

type alertState struct {
	kind    alertKind
	title   string
	message string
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
	unit        int

	running bool
	status  MonitorStatus
	logs    []string
	alert   *alertState

	tokenValue   string
	pendingToken string

	view        flex.View
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
	msg := fmt.Sprintf("车牌 %s 已入场。\n入场时间：%s\n地点：%s\n免费时长：%d 分钟",
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
		msg = fmt.Sprintf("车牌 %s 已停车 %s（入场时间 %s）。\n预计还剩 %s 达到免费时长上限，请及时挪车或缴费。\n地点：%s",
			a.Carno, formatDuration(a.Elapsed), a.Entry.Format("01-02 15:04"), formatDuration(a.Remaining), a.ParkName)
	} else {
		msg = fmt.Sprintf("车牌 %s 已停车 %s（入场时间 %s）。\n%d 分钟免费时长已结束，请及时挪车或缴费。\n地点：%s",
			a.Carno, formatDuration(a.Elapsed), a.Entry.Format("01-02 15:04"), a.FreeMinutes, a.ParkName)
	}
	m.mu.Lock()
	m.alert = &alertState{kind: alertFree, title: "免费停车即将结束", message: msg}
	m.mu.Unlock()
	m.log("免费时长提醒已触发")
	playAlertSound()
	go func() { _ = sendSystemNotification("免费停车即将结束", msg) }()
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
		return Config{}, fmt.Errorf("免费时长无效：%v", err)
	}
	advance, err := advanceToSeconds(m.advanceField.Text(), unitLabels[m.unit])
	if err != nil {
		return Config{}, fmt.Errorf("提前提醒无效：%v", err)
	}
	if advance > free*60 {
		return Config{}, errors.New("提前提醒时间不能大于免费时长")
	}
	return Config{
		Token:           token,
		Carno:           carno,
		IntervalMinutes: interval,
		FreeMinutes:     free,
		AdvanceSeconds:  advance,
	}, nil
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
	if err := saveConfig(cfg); err != nil {
		m.showError(err)
		return
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	m.log("配置已保存")
}

func (m *model) startMonitor() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	cfg, err := m.collectConfig()
	if err != nil {
		m.showError(err)
		return
	}
	if err := saveConfig(cfg); err != nil {
		m.showError(err)
		return
	}

	mon := NewMonitor(cfg, m.setStatus, m.onFreeAlert, m.onEntryAlert, m.log)

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
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

	m.log("正在准备本地代理...")
	go func() {
		err := m.tokenProxy.Start(
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
			"代理已启动。请打开“西高新智慧停车”小程序，进入“自助缴费”页面点一次“查询缴费”，工具会自动捕获 Token。\n获取成功或 3 分钟后自动关闭代理。")
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
	m.mu.Unlock()

	if pendingToken != "" {
		m.tokenValue = pendingToken
	}

	m.view.Draw(canvas, theme, bounds, func(f *flex.Frame) {
		f.Column().Fill().Pad(20).Gap(12).Children(func() {
			f.Text("停车免费时长监控").Bold().FontSize(20)
			m.paintForm(f, theme, running)
			m.paintStatus(f, theme, st)
			m.paintLog(f, theme, logs)
		})
		m.paintDialog(f, alert)
	})
}

func (m *model) paintForm(f *flex.Frame, theme ui.Theme, running bool) {
	f.Column().FillWidth().Background(theme.Card).Border(1, theme.Border).Radius(12).Pad(16).Gap(10).Children(func() {
		f.Column().Gap(4).Children(func() {
			f.Text("车牌号").FontSize(12).Muted()
			f.Field(&m.carnoField, carnoFieldID, "车牌号", "例如：京A12345")
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
			f.Text("免费时长（分钟）").FontSize(12).Muted()
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				f.Field(&m.freeField, freeFieldID, "免费时长", "默认 15").Grow(1)
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

		f.Row().Gap(8).Children(func() {
			if f.Button(saveBtnID, "保存配置").Outline().Clicked() {
				m.saveConfigFromForm()
			}
			if f.Button(startBtnID, "开始监控").Disabled(running).Clicked() {
				m.startMonitor()
			}
			if f.Button(stopBtnID, "停止监控").Outline().Disabled(!running).Clicked() {
				m.stopMonitor()
			}
		})
	})
}

func (m *model) paintStatus(f *flex.Frame, theme ui.Theme, st MonitorStatus) {
	f.Column().FillWidth().Background(theme.Card).Border(1, theme.Border).Radius(12).Pad(16).Gap(8).Children(func() {
		phase := st.Phase
		if st.Running {
			phase += " · 监控中"
		}
		f.Text("状态：" + phase).Bold().FontSize(14)

		if st.InPark {
			f.Text("距离 " + strconv.Itoa(m.currentFreeMinutes()) + " 分钟免费时长结束").Muted().Self(flex.Center)

			countdown := formatRemaining(st.Remaining)
			txt := f.Text(countdown).FontSize(46).Bold().Self(flex.Center)
			if st.Remaining <= 0 {
				txt.Color(theme.Danger)
			} else {
				txt.Color(theme.Success)
			}

			f.Row().Gap(24).Justify(flex.Center).Children(func() {
				f.Text("入场时间：" + st.Entry.Format("2006-01-02 15:04:05"))
				f.Text("已停时长：" + formatDuration(st.Elapsed))
			})
			if !st.LastQuery.IsZero() {
				f.Text("上次查询：" + st.LastQuery.Format("15:04:05")).Muted()
			}
			if st.Detail != "" {
				f.Text(st.Detail).Muted()
			}
		} else {
			f.Text("--:--").FontSize(46).Bold().Muted().Self(flex.Center)
			f.Text("入场时间：-").Muted()
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
	f.Column().FillWidth().Grow(1).Background(theme.Card).Border(1, theme.Border).Radius(12).Pad(16).Gap(8).Children(func() {
		f.Text("运行日志").Bold().FontSize(14)
		f.ScrollArea(logAreaID).Grow(1).Follow(true).Gap(4).Children(func() {
			for _, line := range logs {
				f.Text(line).FontSize(12).Color(theme.Foreground)
			}
		})
	})
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
		f.Paragraph(alert.message, 0).FontSize(13)
	})
	d.Footer(func() {
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

func main() {
	cfg := loadConfig()
	m := newModel(cfg)
	m.tokenProxy = &TokenProxy{}

	var invalidator app.Invalidator
	m.invalidator = &invalidator
	m.log("就绪：填写车牌与 Token，保存配置后点“开始监控”")

	config := app.Config{
		Title:       "停车免费时长监控",
		Width:       760,
		Height:      900,
		MinWidth:    640,
		MinHeight:   720,
		Invalidator: &invalidator,
		TextInput:   flex.Fields(&m.carnoField, &m.intervalField, &m.freeField, &m.advanceField),
		OnClosed:    m.shutdown,
	}
	config.Paint = m.paint

	var canvas ui.Canvas
	if err := app.RunCanvas(config, &canvas); err != nil {
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
