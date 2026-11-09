package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	apiUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36 MicroMessenger/7.0.20.1781 NetType/WIFI MiniProgramEnv/Windows"
)

type Client struct {
	token  string
	region Region
	http   *http.Client
}

func NewClient(token string, region Region) *Client {
	return &Client{
		token:  token,
		region: region,
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

type ParkingFeeInfo struct {
	ID          json.RawMessage `json:"id"`
	Serialno    string          `json:"serialno"`
	Carno       string          `json:"carno"`
	Parkname    string          `json:"parkname"`
	Address     string          `json:"address"`
	Carport     string          `json:"carport"`
	Intime      string          `json:"intime"`
	Outtime     string          `json:"outtime"`
	Parktime    int64           `json:"parktime"`
	ParktimeStr string          `json:"parktimestr"`
	Totalfee    float64         `json:"totalfee"`
	Unpaidfee   float64         `json:"unpaidfee"`
	Paidfee     float64         `json:"paidfee"`
}

func (f ParkingFeeInfo) HasRecord() bool {
	return f.Serialno != "" || f.Intime != ""
}

const inParkTolerance = 2 * time.Minute

// InPark 判断车辆是否仍在停。服务端对在停车辆会把 outtime 动态刷新为
// “当前时刻 + 少量缓冲”，并持续增加 parktime，因此不能只看 outtime 是否为空。
func (f ParkingFeeInfo) InPark() bool {
	return f.InParkAt(time.Now())
}

func (f ParkingFeeInfo) InParkAt(now time.Time) bool {
	if f.Intime == "" {
		return false
	}
	if f.Outtime == "" || f.Outtime == "null" {
		return true
	}
	if out := parseServerTime(f.Outtime); !out.IsZero() {
		if absDuration(now.Sub(out)) <= inParkTolerance {
			return true
		}
	}
	if entry := parseServerTime(f.Intime); !entry.IsZero() && f.Parktime > 0 {
		parked := time.Duration(f.Parktime) * time.Millisecond
		if absDuration(now.Sub(entry)-parked) <= inParkTolerance {
			return true
		}
	}
	return false
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

type NoPayInfo struct {
	Nonpaysum           float64           `json:"nonpaysum"`
	Nonpaynum           int               `json:"nonpaynum"`
	UnPaymentDtoOutList []json.RawMessage `json:"unPaymentDtoOutList"`
}

type FeeData struct {
	Carno               string         `json:"carno"`
	ParkingFeeInfo      ParkingFeeInfo `json:"parkingFeeInfo"`
	NoPayInfo           NoPayInfo      `json:"noPayInfo"`
	MustPayAnNoPayOrder bool           `json:"mustPayAnNoPayOrder"`
}

type QueryResult struct {
	Status int
	Msg    string
	Data   *FeeData
}

type apiResponse struct {
	Status int
	Msg    string
	Data   json.RawMessage
}

func (c *Client) call(ctx context.Context, path string, payload map[string]any, withToken bool) (*apiResponse, error) {
	body, err := json.Marshal(mergeCommonPayload(c.region, payload))
	if err != nil {
		return nil, err
	}
	aesKey, err := newAESKey()
	if err != nil {
		return nil, err
	}
	encrypted, err := encryptRequestBody(body, aesKey)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.region.Host+path, bytes.NewBufferString(encrypted))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Encrypt", "V2")
	req.Header.Set("Referer", c.region.Referer())
	req.Header.Set("User-Agent", apiUserAgent)
	if withToken && c.token != "" {
		req.Header.Set("Token", c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("Token 已失效（HTTP 401）")
	}

	var envelope struct {
		Status int             `json:"status"`
		Msg    string          `json:"msg"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("响应格式异常: %w", err)
	}

	out := &apiResponse{Status: envelope.Status, Msg: envelope.Msg}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return out, nil
	}

	decoded := envelope.Data
	if envelope.Data[0] == '"' {
		var enc string
		if err := json.Unmarshal(envelope.Data, &enc); err != nil {
			return nil, err
		}
		if enc == "" {
			return out, nil
		}
		decoded, err = decryptResponseData(enc, aesKey)
		if err != nil {
			return nil, fmt.Errorf("响应解密失败: %w", err)
		}
	}
	out.Data = decoded
	return out, nil
}

func mergeCommonPayload(region Region, payload map[string]any) map[string]any {
	merged := map[string]any{
		"userid":     "",
		"openId":     "",
		"aliUserId":  "",
		"clienttype": 4,
		"storeid":    "",
		"paymode":    "WX_MP",
		"subAppId":   region.AppID,
		"paytype":    "2",
		"mchid":      region.Mchid,
		"version":    region.Version,
	}
	for k, v := range payload {
		merged[k] = v
	}
	return merged
}

func (c *Client) Query(ctx context.Context, carno string) (*QueryResult, error) {
	resp, err := c.call(ctx, "app/getParkingFeeAndNoPayList", map[string]any{"carno": carno}, true)
	if err != nil {
		return nil, err
	}
	result := &QueryResult{Status: resp.Status, Msg: resp.Msg}
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return result, nil
	}
	var data FeeData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("业务数据解析失败: %w", err)
	}
	result.Data = &data
	return result, nil
}

func (c *Client) SendSms(ctx context.Context, phone string) error {
	resp, err := c.call(ctx, "app/sendMsg", map[string]any{"phonenum": phone, "msgtype": "L"}, false)
	if err != nil {
		return err
	}
	if resp.Status != 1 {
		return fmt.Errorf("发送验证码失败：%s", strings.TrimSpace(resp.Msg))
	}
	return nil
}

type LoginResult struct {
	Token       string
	ID          string
	PhoneNumber string
	Raw         map[string]any
}

func (c *Client) LoginBySms(ctx context.Context, phone, code string) (*LoginResult, error) {
	resp, err := c.call(ctx, "app/appletsBindPhoneNumber", map[string]any{
		"phoneNumber": phone,
		"checkCode":   code,
		"id":          "",
		"bindType":    2,
		"mpType":      2,
	}, false)
	if err != nil {
		return nil, err
	}
	if resp.Status != 1 {
		return nil, fmt.Errorf("登录失败：%s", strings.TrimSpace(resp.Msg))
	}
	return parseLogin(resp.Data)
}

func (c *Client) LoginByPassword(ctx context.Context, phone, password string) (*LoginResult, error) {
	resp, err := c.call(ctx, "app/login", map[string]any{
		"phoneNumber": phone,
		"password":    password,
		"loginType":   1,
	}, false)
	if err != nil {
		return nil, err
	}
	if resp.Status != 1 {
		return nil, fmt.Errorf("登录失败：%s", strings.TrimSpace(resp.Msg))
	}
	return parseLogin(resp.Data)
}

func parseLogin(raw json.RawMessage) (*LoginResult, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("登录响应为空")
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("登录响应解析失败: %w", err)
	}
	result := &LoginResult{Raw: m}
	if v, ok := m["token"].(string); ok {
		result.Token = v
	}
	if v, ok := m["id"]; ok && v != nil {
		result.ID = fmt.Sprint(v)
	}
	if v, ok := m["phoneNumber"].(string); ok {
		result.PhoneNumber = v
	}
	if result.Token == "" {
		return result, errors.New("登录响应中没有 token 字段，账号可能还未注册微信")
	}
	return result, nil
}

func describeResult(r *QueryResult) string {
	if r == nil {
		return ""
	}
	if r.Status != 1 {
		return strings.TrimSpace(fmt.Sprintf("接口返回 %d: %s", r.Status, r.Msg))
	}
	if r.Data == nil {
		return "接口返回空数据"
	}
	return ""
}
