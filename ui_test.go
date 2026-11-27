package main

import (
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func clickHit(t *testing.T, driver *app.Driver, id ui.ID) bool {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id {
			if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			return true
		}
	}
	return false
}

func scrollConfigToEnd(t *testing.T, driver *app.Driver) {
	t.Helper()
	for range 4 {
		if err := driver.Scroll(ui.ScrollEvent{X: 350, Y: 450, DeltaY: 600}); err != nil {
			t.Fatal(err)
		}
	}
}

func twoMonitorTestConfig() Config {
	cfg := defaultConfig()
	cfg.Monitors = append(cfg.Monitors, defaultMonitorConfig())
	return cfg
}

// 走真实操作顺序：点“配置”打开弹窗 → 滚到共用设置 → 选远程通知渠道 → 输入。
func TestAddAndDeleteMonitorCards(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	m := newModel(twoMonitorTestConfig())
	if err := saveConfig(m.cfg); err != nil {
		t.Fatal(err)
	}
	firstID, secondID := m.monitors[0].id, m.monitors[1].id
	m.monitors[0].form.tokenValue = "token-a"
	m.monitors[1].form.tokenValue = "token-b"
	secondMon := &Monitor{cfg: MonitorConfig{Carno: "B"}}
	m.monitors[1].mon = secondMon
	m.monitors[1].status = MonitorStatus{Running: true, Phase: "在停", InPark: true}

	m.addMonitor()
	if len(m.monitors) != 3 {
		t.Fatalf("新增后应有 3 张卡片，实际 %d", len(m.monitors))
	}
	thirdID := m.monitors[2].id
	if thirdID == "" || thirdID == firstID || thirdID == secondID {
		t.Fatalf("新增卡片 ID 不唯一: %q", thirdID)
	}
	disk := loadConfig()
	if len(disk.Monitors) != 3 || disk.Monitors[2].ID != thirdID {
		t.Fatalf("新增卡片未持久化: %+v", disk.Monitors)
	}
	if !m.deleteMonitor(firstID) {
		t.Fatal("多张卡片时应允许删除")
	}
	if len(m.monitors) != 2 || m.monitors[0].id != secondID || m.monitors[0].mon != secondMon {
		t.Fatal("删除第一张后，第二张的实例或 ID 发生变化")
	}
	if !m.monitors[0].status.InPark || m.monitors[0].form.tokenValue != "token-b" {
		t.Fatal("删除第一张影响了第二张的状态或 Token")
	}
	if !m.deleteMonitor(secondID) {
		t.Fatal("两张卡片时应允许删除另一张")
	}
	if len(m.monitors) != 1 || m.monitors[0].id != thirdID {
		t.Fatal("删除后应保留新增的卡片")
	}
	if m.deleteMonitor(thirdID) {
		t.Fatal("最后一张卡片不应允许删除")
	}
	m.requestDeleteMonitor(thirdID)
	if m.alert == nil || m.alert.kind != alertInfo {
		t.Fatal("删除最后一张时应提示至少保留一张")
	}
	disk = loadConfig()
	if len(disk.Monitors) != 1 || disk.Monitors[0].ID != thirdID {
		t.Fatalf("删除结果未正确持久化: %+v", disk.Monitors)
	}
}

func TestDeleteMonitorKeepsCallbacksBoundToID(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	m := newModel(twoMonitorTestConfig())
	firstID, secondID := m.monitors[0].id, m.monitors[1].id
	m.deleteMonitor(firstID)
	m.setStatus(secondID, MonitorStatus{Running: true, Phase: "查询中"})
	if len(m.monitors) != 1 || !m.monitors[0].status.Running {
		t.Fatal("原第二张卡片未收到按 ID 发送的状态")
	}
	m.setStatus(firstID, MonitorStatus{Running: true, Phase: "旧实例回调"})
	if m.monitors[0].status.Phase != "查询中" {
		t.Fatal("已删除卡片的迟到回调覆盖了其他卡片")
	}
}

func TestMonitorCardAddDeleteButtons(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	m := newModel(twoMonitorTestConfig())
	if err := saveConfig(m.cfg); err != nil {
		t.Fatal(err)
	}
	var invalidator app.Invalidator
	m.invalidator = &invalidator
	var canvas ui.Canvas
	driver, err := app.NewDriver(m.appConfig(), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()

	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == addMonitorBtnID || hit.ID == tree.ID(m.monitors[0].scope, "delete-config") {
			t.Fatal("主界面不应显示新增或删除入口")
		}
	}
	if !clickHit(t, driver, configBtnID) {
		t.Fatal("没有“配置”按钮")
	}
	if !clickHit(t, driver, addMonitorBtnID) {
		t.Fatal("配置弹窗中没有“新增卡片”按钮")
	}
	if len(m.monitors) != 3 || len(loadConfig().Monitors) != 3 {
		t.Fatal("点击新增后界面和配置都应出现第三张卡片")
	}
	deletedID := m.monitors[2].id
	if !clickHit(t, driver, tree.ID(m.monitors[2].scope, "delete-config")) {
		t.Fatal("配置卡片没有删除图标按钮")
	}
	if m.deleteConfirm != deletedID || len(m.monitors) != 3 {
		t.Fatal("删除前应显示行内确认，不能直接移除")
	}
	if !clickHit(t, driver, tree.ID(m.monitors[2].scope, "delete-cancel")) {
		t.Fatal("没有取消删除按钮")
	}
	if m.deleteConfirm != "" || len(m.monitors) != 3 {
		t.Fatal("取消删除后应保留卡片")
	}
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	if !clickHit(t, driver, tree.ID(m.monitors[2].scope, "delete-config")) {
		t.Fatal("取消后应还能再次点击删除图标")
	}
	if !clickHit(t, driver, tree.ID(m.monitors[2].scope, "delete-confirm")) {
		t.Fatal("没有行内确认删除按钮")
	}
	if len(m.monitors) != 2 || m.monitorIndex(deletedID) >= 0 {
		t.Fatal("确认后应删除对应卡片")
	}
	if len(loadConfig().Monitors) != 2 {
		t.Fatal("确认删除后配置应同步保存")
	}
}

func TestLastMonitorHasNoDeleteButton(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	m := newModel(defaultConfig())
	var canvas ui.Canvas
	driver, err := app.NewDriver(m.appConfig(), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if !clickHit(t, driver, configBtnID) {
		t.Fatal("没有“配置”按钮")
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == tree.ID(m.monitors[0].scope, "delete-config") {
			t.Fatal("最后一张卡片不应显示删除按钮")
		}
	}
}

func TestDeleteFailureKeepsMonitorCard(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	m := newModel(twoMonitorTestConfig())
	if err := saveConfig(m.cfg); err != nil {
		t.Fatal(err)
	}
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if m.deleteMonitor(m.monitors[0].id) {
		t.Fatal("配置不可读取时不应删除界面卡片")
	}
	if len(m.monitors) != 2 || len(m.cfg.Monitors) != 2 {
		t.Fatal("删除失败后应保留全部卡片")
	}
}

func TestMonitorCardsAndIndependentScheduleFields(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	m := newModel(twoMonitorTestConfig())
	var canvas ui.Canvas
	driver, err := app.NewDriver(m.appConfig(), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()

	texts := driver.Snapshot().Texts
	count := 0
	for _, text := range texts {
		if text.Value == "监控 1" || text.Value == "监控 2" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("主界面应有 2 张监控卡片，找到 %d 个标题", count)
	}
	if !clickHit(t, driver, configBtnID) {
		t.Fatal("没有“配置”按钮")
	}
	if err := driver.Resize(640, 720); err != nil {
		t.Fatal(err)
	}
	for i, mon := range m.monitors {
		scope := mon.scope
		if i == 1 {
			scrollConfigToEnd(t, driver)
		}
		if m.monitors[i].form.fromField.Text() != "08:00" || m.monitors[i].form.toField.Text() != "18:00" {
			t.Fatalf("监控 %d 默认时段错误", i+1)
		}
		before := m.monitors[1-i].form.fromField.Text()
		if !clickHit(t, driver, tree.ID(scope, "from")) {
			t.Fatalf("监控 %d 没有开始时间输入框", i+1)
		}
		m.monitors[i].form.fromField.SetText("")
		if err := driver.Type("19:00"); err != nil {
			t.Fatal(err)
		}
		if m.monitors[i].form.fromField.Text() != "19:00" {
			t.Fatalf("监控 %d 输入未写入自己的字段，实际 %q", i+1, m.monitors[i].form.fromField.Text())
		}
		if got := m.monitors[1-i].form.fromField.Text(); got != before {
			t.Fatalf("输入监控 %d 的时间影响了另一张卡片：%q → %q", i+1, before, got)
		}
	}
	if m.monitors[0].form.fromField.Text() != "19:00" || m.monitors[1].form.fromField.Text() != "19:00" {
		t.Fatal("两个时间段输入应分别写入各自的字段")
	}
	if !clickHit(t, driver, saveBtnID) {
		t.Fatal("最小窗口下保存按钮应始终可点击")
	}
}

func TestIndependentMonitorCollection(t *testing.T) {
	m := newModel(twoMonitorTestConfig())
	m.monitors[0].form.carnoField.SetText("A")
	m.monitors[0].form.tokenValue = "token-a"
	m.monitors[1].form.carnoField.SetText("B")
	m.monitors[1].form.tokenValue = "token-b"
	m.monitors[1].form.fromField.SetText("invalid")

	first, err := m.collectMonitor(m.monitors[0].id)
	if err != nil || first.Carno != "A" || first.Token != "token-a" {
		t.Fatalf("监控 1 不应被监控 2 的无效时间阻塞: %+v err=%v", first, err)
	}
	if _, err := m.collectMonitor(m.monitors[1].id); err == nil {
		t.Fatal("监控 2 的无效时间应被校验")
	}
	m.monitors[1].form.fromField.SetText("20:00")
	cfg, err := m.collectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Monitors[0].Token != "token-a" || cfg.Monitors[1].Token != "token-b" ||
		cfg.Monitors[0].MonitorFrom != "08:00" || cfg.Monitors[1].MonitorFrom != "20:00" {
		t.Fatalf("两个监控的配置混合了: %+v", cfg.Monitors)
	}
}

func TestRemoteFieldsTypeAfterSwitchingChannel(t *testing.T) {
	cases := []struct {
		name    string
		choice  ui.ID
		fieldID ui.ID
		typed   string
		text    func(m *model) string
	}{
		{"pushplus", remoteScope.At(1), pushTokenFieldID, "pp-abc123", func(m *model) string { return m.pushTokenField.Text() }},
		{"ntfy", remoteScope.At(2), ntfyTopicFieldID, "pm-xyz789", func(m *model) string { return m.ntfyTopicField.Text() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APPDATA", t.TempDir())
			cfg := defaultConfig()
			cfg.Region = regionList[0].Key
			cfg.Monitors[0].Carno = "陕A12345"

			m := newModel(cfg)

			var canvas ui.Canvas
			driver, err := app.NewDriver(m.appConfig(), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()

			if !clickHit(t, driver, configBtnID) {
				t.Fatal("没有“配置”按钮")
			}
			scrollConfigToEnd(t, driver)
			if !clickHit(t, driver, tc.choice) {
				t.Fatal("没有远程通知渠道选项")
			}
			if !clickHit(t, driver, tc.fieldID) {
				t.Fatalf("切到该渠道后输入框 %v 不存在", tc.fieldID)
			}
			if err := driver.Type(tc.typed); err != nil {
				t.Fatal(err)
			}
			if got := tc.text(m); got != tc.typed {
				t.Fatalf("输入后内容为 %q，期望 %q", got, tc.typed)
			}
		})
	}
}

// 选择远程渠道后出现“发送测试”按钮。
func TestRemoteTestButtonAppears(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	cfg := defaultConfig()
	cfg.Region = regionList[0].Key

	m := newModel(cfg)

	var canvas ui.Canvas
	driver, err := app.NewDriver(m.appConfig(), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()

	hasTestBtn := func() bool {
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == testRemoteBtnID {
				return true
			}
		}
		return false
	}

	if !clickHit(t, driver, configBtnID) {
		t.Fatal("没有“配置”按钮")
	}
	if hasTestBtn() {
		t.Fatal("未选择渠道时不应出现发送测试按钮")
	}
	scrollConfigToEnd(t, driver)
	if !clickHit(t, driver, remoteScope.At(2)) {
		t.Fatal("没有 ntfy 选项")
	}
	if !hasTestBtn() {
		t.Fatal("切到 ntfy 后没有发送测试按钮")
	}
	if !clickHit(t, driver, remoteScope.At(1)) {
		t.Fatal("没有 PushPlus 选项")
	}
	if !hasTestBtn() {
		t.Fatal("切到 PushPlus 后没有发送测试按钮")
	}
}

// 下载中状态卡显示进度行。
func TestUpdateProgressShownInStatusCard(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	cfg := defaultConfig()
	cfg.Region = regionList[0].Key

	m := newModel(cfg)
	m.updating = true
	m.updatePct = 45
	m.updateVersion = "9.9.9"

	var canvas ui.Canvas
	driver, err := app.NewDriver(m.appConfig(), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()

	found := false
	for _, text := range driver.Snapshot().Texts {
		if strings.Contains(text.Value, "下载中 v9.9.9 · 45%") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("状态卡中没有下载进度行")
	}
}
