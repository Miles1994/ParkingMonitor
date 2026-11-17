package main

import (
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

// 走真实操作顺序：点“配置”打开弹窗 → 选远程通知渠道 → 点输入框 → 输入。
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
			cfg.Carno = "陕A12345"

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
