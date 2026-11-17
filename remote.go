package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	pushPlusEndpoint = "https://www.pushplus.plus/send"
	ntfyEndpoint     = "https://ntfy.sh"
)

var (
	ntfyTopicPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	remoteHTTP       = &http.Client{Timeout: 10 * time.Second}
)

// remoteNotifier 把提醒转发到手机：PushPlus 落到微信，ntfy 落到 ntfy App。
type remoteNotifier struct {
	channel   string
	pushToken string
	ntfyTopic string
}

func newRemoteNotifier(cfg Config) *remoteNotifier {
	switch cfg.RemoteChannel {
	case "pushplus":
		token := strings.TrimSpace(cfg.PushPlusToken)
		if token == "" {
			return nil
		}
		return &remoteNotifier{channel: "pushplus", pushToken: token}
	case "ntfy":
		topic := strings.TrimSpace(cfg.NtfyTopic)
		if !ntfyTopicPattern.MatchString(topic) {
			return nil
		}
		return &remoteNotifier{channel: "ntfy", ntfyTopic: topic}
	}
	return nil
}

func (n *remoteNotifier) label() string {
	if n.channel == "pushplus" {
		return "PushPlus"
	}
	return "ntfy"
}

func (n *remoteNotifier) send(title, body string) error {
	switch n.channel {
	case "pushplus":
		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		payload := map[string]string{
			"token":    n.pushToken,
			"title":    title,
			"content":  body,
			"template": "txt",
		}
		if err := postJSON(pushPlusEndpoint, payload, &resp); err != nil {
			return err
		}
		if resp.Code != 200 {
			return fmt.Errorf("PushPlus 返回 %d：%s", resp.Code, strings.TrimSpace(resp.Msg))
		}
		return nil
	case "ntfy":
		payload := map[string]any{
			"topic":    n.ntfyTopic,
			"title":    title,
			"message":  body,
			"priority": 4,
			"tags":     []string{"car"},
		}
		return postJSON(ntfyEndpoint, payload, nil)
	}
	return errors.New("未配置远程通知渠道")
}

func postJSON(url string, payload any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := remoteHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d：%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("响应解析失败：%w", err)
		}
	}
	return nil
}
