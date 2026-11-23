//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	appVersion = "1.2.9"
	githubRepo = "Miles1994/ParkingMonitor"
)

var updateClient = &http.Client{Timeout: 30 * time.Second}

// downloadClient fetches the new executable. Files are tens of megabytes and
// slow links need minutes, so only the connection stages are bounded; a total
// timeout would abort a transfer that is still making progress.
var downloadClient = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	},
}

type updateInfo struct {
	Version string
	URL     string
	Page    string
}

func (m *model) startUpdateChecker() {
	go func() {
		time.Sleep(5 * time.Second)
		m.checkUpdate()
		ticker := time.NewTicker(time.Hour)
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
	m.updatePct = 0
	m.updateVersion = info.Version
	m.mu.Unlock()
	m.requestPaint()

	m.log("正在下载新版本 v" + info.Version + "...")
	go func() {
		path, err := downloadUpdate(info, func(percent int) {
			m.mu.Lock()
			m.updatePct = percent
			m.mu.Unlock()
			if percent%10 == 0 {
				m.log(fmt.Sprintf("正在下载新版本 v%s... %d%%", info.Version, percent))
				return
			}
			m.requestPaint()
		})
		if err != nil {
			m.mu.Lock()
			m.updating = false
			m.updatePrompted = false
			m.mu.Unlock()
			m.log("下载更新失败：" + err.Error())
			m.showError(err)
			m.retryUpdateCheck()
			return
		}
		m.log("更新已下载，正在重启完成安装")
		if err := applyUpdate(path); err != nil {
			m.mu.Lock()
			m.updating = false
			m.updatePrompted = false
			m.mu.Unlock()
			m.log("启动更新失败：" + err.Error())
			m.showError(err)
			return
		}
		m.shutdown()
		os.Exit(0)
	}()
}

// retryUpdateCheck asks again shortly after a failed download, so a user who
// chose to update is not left waiting for the six-hour tick. The prompt flag
// was cleared, so a still-relevant update pops up again.
func (m *model) retryUpdateCheck() {
	time.AfterFunc(2*time.Minute, m.checkUpdate)
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

func downloadUpdate(info *updateInfo, progress func(percent int)) (string, error) {
	if !strings.HasPrefix(info.URL, "https://github.com/"+githubRepo+"/releases/download/") {
		return "", errors.New("更新地址异常")
	}
	resp, err := downloadClient.Get(info.URL)
	if err != nil {
		return "", errors.New("下载更新失败：网络连接超时或中断，请稍后重试")
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
	body := io.LimitReader(resp.Body, 100<<20)
	var src io.Reader = body
	if progress != nil && resp.ContentLength > 0 {
		src = &progressReader{inner: body, total: resp.ContentLength, last: -1, report: progress}
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(path)
		return "", errors.New("下载更新中断：网络连接异常，请稍后重试")
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

// progressReader reports download progress as whole percents, so the status
// card's bar moves smoothly on a slow link.
type progressReader struct {
	inner  io.Reader
	total  int64
	done   int64
	last   int
	report func(percent int)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	r.done += int64(n)
	percent := int(r.done * 100 / r.total)
	if percent > 100 {
		percent = 100
	}
	if percent != r.last {
		r.last = percent
		r.report(percent)
	}
	return n, err
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
