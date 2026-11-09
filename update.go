//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	appVersion = "1.2.1"
	githubRepo = "Miles1994/ParkingMonitor"
)

var updateClient = &http.Client{Timeout: 30 * time.Second}

type updateInfo struct {
	Version string
	URL     string
	Page    string
}

func (m *model) startUpdateChecker() {
	go func() {
		time.Sleep(8 * time.Second)
		m.checkUpdate()
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			m.checkUpdate()
		}
	}()
}

func (m *model) checkUpdate() {
	info, err := checkLatestRelease()
	if err != nil {
		m.log("检查更新失败：" + err.Error())
		return
	}
	if !versionNewer(info.Version, appVersion) {
		return
	}

	m.mu.Lock()
	if m.updatePrompted {
		m.mu.Unlock()
		return
	}
	m.updatePrompted = true
	if m.alert == nil {
		m.alert = &alertState{
			kind:    alertUpdate,
			title:   "发现新版本 v" + info.Version,
			message: updateMessage(info),
			update:  info,
		}
		m.mu.Unlock()
		m.log("发现新版本 v" + info.Version)
		m.requestPaint()
		return
	}
	m.pendingUpdate = info
	m.mu.Unlock()
	m.log("发现新版本 v" + info.Version)
	m.requestPaint()
}

func updateMessage(info *updateInfo) string {
	return fmt.Sprintf("当前版本 v%s，最新版本 v%s。\n是否下载并自动更新？\n更新完成后程序会自动重启。", appVersion, info.Version)
}

func (m *model) downloadAndUpdate(info *updateInfo) {
	m.mu.Lock()
	if m.updating {
		m.mu.Unlock()
		return
	}
	m.updating = true
	m.mu.Unlock()

	m.log("正在下载新版本 v" + info.Version + "...")
	go func() {
		path, err := downloadUpdate(info)
		if err != nil {
			m.mu.Lock()
			m.updating = false
			m.mu.Unlock()
			m.log("下载更新失败：" + err.Error())
			m.showError(err)
			return
		}
		m.log("更新已下载，正在重启完成安装")
		if err := applyUpdate(path); err != nil {
			m.mu.Lock()
			m.updating = false
			m.mu.Unlock()
			m.log("启动更新失败：" + err.Error())
			m.showError(err)
			return
		}
		m.shutdown()
		os.Exit(0)
	}()
}

func checkLatestRelease() (*updateInfo, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+githubRepo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ParkingMonitor/"+appVersion)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("检查更新失败（HTTP %d）", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return nil, err
	}

	info := &updateInfo{Version: strings.TrimPrefix(release.TagName, "v"), Page: release.HTMLURL}
	for _, asset := range release.Assets {
		if strings.EqualFold(asset.Name, "ParkingMonitor.exe") {
			info.URL = asset.BrowserDownloadURL
			break
		}
	}
	if info.Version == "" || info.URL == "" {
		return nil, errors.New("最新版本缺少 ParkingMonitor.exe 附件")
	}
	return info, nil
}

func downloadUpdate(info *updateInfo) (string, error) {
	if !strings.HasPrefix(info.URL, "https://github.com/"+githubRepo+"/releases/download/") {
		return "", errors.New("更新地址异常")
	}
	resp, err := updateClient.Get(info.URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载更新失败（HTTP %d）", resp.StatusCode)
	}

	path := filepath.Join(os.TempDir(), "ParkingMonitor-"+info.Version+".exe")
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 100<<20)); err != nil {
		out.Close()
		os.Remove(path)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(path)
		return "", err
	}

	stat, err := os.Stat(path)
	if err != nil || stat.Size() < 1<<20 {
		os.Remove(path)
		return "", errors.New("下载的文件大小异常")
	}
	file, err := os.Open(path)
	if err != nil {
		os.Remove(path)
		return "", err
	}
	header := make([]byte, 2)
	_, readErr := io.ReadFull(file, header)
	file.Close()
	if readErr != nil || string(header) != "MZ" {
		os.Remove(path)
		return "", errors.New("下载的文件不是有效的程序")
	}
	return path, nil
}

func applyUpdate(newPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.Abs(exe)

	script := filepath.Join(os.TempDir(), "ParkingMonitor-update.cmd")
	content := fmt.Sprintf(`@echo off
:wait
tasklist /FI "PID eq %d" 2>nul | findstr /C:"%d" >nul
if not errorlevel 1 (
  timeout /t 1 /nobreak >nul
  goto wait
)
move /Y "%s" "%s" >nul
start "" "%s"
del "%%~f0"
`, os.Getpid(), os.Getpid(), newPath, exe, exe)
	if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
		return err
	}

	cmd := exec.Command("cmd", "/c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func versionNewer(latest, current string) bool {
	l, c := parseVersion(latest), parseVersion(current)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out [3]int
	parts := strings.SplitN(v, ".", 3)
	for i := 0; i < len(parts) && i < 3; i++ {
		part := strings.TrimFunc(parts[i], func(r rune) bool { return r < '0' || r > '9' })
		n, _ := strconv.Atoi(part)
		out[i] = n
	}
	return out
}
