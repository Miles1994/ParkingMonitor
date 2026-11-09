package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCryptoRoundTrip(t *testing.T) {
	key, err := newAESKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 16 {
		t.Fatalf("aes key 长度应为 16，实际 %d", len(key))
	}
	plain := []byte(`{"carno":"京A12345","version":"1.4.6"}`)
	body, err := encryptRequestBody(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	var combined string
	if err := json.Unmarshal([]byte(body), &combined); err != nil {
		t.Fatalf("body 应为 JSON 字符串: %v", err)
	}
	parts := strings.SplitN(combined, "-", 2)
	if len(parts) != 2 {
		t.Fatalf("body 结构异常: %s", combined)
	}
	rsaRaw, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil || len(rsaRaw) != 256 {
		t.Fatalf("RSA 段长度异常: %d, err=%v", len(rsaRaw), err)
	}
	aesRaw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	dec, err := aesECBDecrypt([]byte(key), aesRaw)
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != string(plain) {
		t.Fatalf("AES 往返不一致: %s", dec)
	}
}

func TestInParkDetection(t *testing.T) {
	now := time.Date(2026, 11, 9, 11, 21, 29, 0, cstZone)

	inPark := ParkingFeeInfo{
		Intime:   "2026-11-09 10:57:59",
		Outtime:  "2026-11-09 11:21:53",
		Parktime: 1434707,
	}
	if !inPark.InParkAt(now) {
		t.Fatal("outtime 为动态结算时间时应判定为在停")
	}

	left := ParkingFeeInfo{
		Intime:   "2026-11-09 09:32:26",
		Outtime:  "2026-11-09 09:44:08",
		Parktime: 702451,
	}
	if left.InParkAt(now) {
		t.Fatal("已出场记录应判定为不在停")
	}

	empty := ParkingFeeInfo{}
	if empty.InParkAt(now) {
		t.Fatal("无记录应判定为不在停")
	}
}

func TestQueryLive(t *testing.T) {
	token := os.Getenv("PARKING_TOKEN")
	if token == "" {
		t.Skip("未设置 PARKING_TOKEN")
	}
	c := NewClient(token)
	res, err := c.Query(context.Background(), "京A12345")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 1 {
		t.Fatalf("接口返回 status=%d msg=%s", res.Status, res.Msg)
	}
	if res.Data == nil {
		t.Fatal("业务数据为空")
	}
	info := res.Data.ParkingFeeInfo
	t.Logf("carno=%s inPark=%v intime=%s outtime=%s parktime=%dms unpaid=%.2f",
		res.Data.Carno, info.InPark(), info.Intime, info.Outtime, info.Parktime, info.Unpaidfee)
}
