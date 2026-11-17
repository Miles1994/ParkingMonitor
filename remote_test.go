package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestRemoteNtfy(t *testing.T) {
	topic := fmt.Sprintf("pm-e2e-%d", rand.Intn(1_000_000))
	n := newRemoteNotifier(Config{RemoteChannel: "ntfy", NtfyTopic: topic})
	if n == nil {
		t.Fatal("notifier nil")
	}
	if err := n.send("车辆已入场", "车牌 陕A12345 已入场。\n入场时间：2026-11-17 11:00:00\n地点：测试车位\n计时时长：15 分钟"); err != nil {
		t.Fatalf("ntfy send: %v", err)
	}
	t.Logf("topic=%s", topic)
}

func TestRemotePushPlusError(t *testing.T) {
	n := newRemoteNotifier(Config{RemoteChannel: "pushplus", PushPlusToken: "invalid-token"})
	if n == nil {
		t.Fatal("notifier nil")
	}
	err := n.send("测试", "测试内容")
	if err == nil {
		t.Fatal("expect error for invalid token")
	}
	if !strings.Contains(err.Error(), "PushPlus") {
		t.Fatalf("unexpected err: %v", err)
	}
	t.Logf("err=%v", err)
}

func TestRemoteValidation(t *testing.T) {
	if newRemoteNotifier(Config{RemoteChannel: "ntfy", NtfyTopic: "带中文"}) != nil {
		t.Fatal("should reject non-ascii topic")
	}
	if newRemoteNotifier(Config{RemoteChannel: "pushplus", PushPlusToken: " "}) != nil {
		t.Fatal("should reject empty token")
	}
	if newRemoteNotifier(Config{}) != nil {
		t.Fatal("should be nil when disabled")
	}
}
