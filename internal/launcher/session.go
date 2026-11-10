// Package launcher 加载湾（发烧平台自己的服务）的接口封装。
package launcher

// 会话：加载湾的 third_party_account/* 接口在**带平台会话（sid）**时行为不一样
// （平台自己扫码就带着它）。这里用任意一个账号的有效 token 换一个 sid，供扫码换票使用。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"skylogin/internal/netdns"
)

// SessionURL 平台登录接口（用 MPay token 换加载湾会话）。
const SessionURL = "https://loadingbaycn.webapp.163.com/app/v1/login"

// platformUDID 平台客户端的设备指纹（加载湾用 uni-mac 头，值固定成平台那台机器的）。
const platformUDID = "21321A2DD37EFE78212D43A4A02FF362"

// SavedAccount 问加载湾："这个会话对应的槽里记住的是谁"，返回 account_id（空 = 没记住）。
func SavedAccount(sid string) (string, error) {
	if sid == "" {
		return "", fmt.Errorf("缺少会话")
	}
	q := url.Values{}
	q.Set("app_id", SkyAppID)
	req, err := http.NewRequest(http.MethodGet, "https://loadingbaycn.webapp.163.com/app/v1/third_party_account?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/json")
	setPlatformHeaders(req)
	req.Header.Set("sid", sid)
	resp, err := netdns.NewClient(20 * time.Second).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Data struct {
			AccountID string `json:"account_id"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &out) != nil {
		return "", fmt.Errorf("响应解析失败: %s", truncate(string(b), 160))
	}
	return out.Data.AccountID, nil
}

// TicketForAccount 用"服务端记住的授权"直接给某个账号发票——不需要 token、不需要扫码。
//
// 前提：这个账号是服务端记住的那一个（扫码换票时带 sid 就会写进去）。
// 不是的话返回 6008，调用方按普通失败处理。
func TicketForAccount(accountID, deviceID, sid string) (*TicketResult, error) {
	q := url.Values{}
	q.Set("code", "")
	q.Set("app_id", SkyAppID)
	q.Set("account_id", accountID)
	if deviceID != "" {
		q.Set("device_id", deviceID)
	}
	req, err := http.NewRequest(http.MethodGet, TicketURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/json")
	setPlatformHeaders(req)
	if sid != "" {
		req.Header.Set("sid", sid)
	}
	resp, err := netdns.NewClient(20 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Ticket  string `json:"ticket"`
			Channel string `json:"login_channel"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &out) != nil {
		return nil, fmt.Errorf("响应解析失败: %s", truncate(string(body), 160))
	}
	if out.Data.Ticket == "" {
		return nil, fmt.Errorf("免扫发票失败: code=%d %s", out.Code, out.Msg)
	}
	ch := out.Data.Channel
	if ch == "" {
		ch = "netease"
	}
	return &TicketResult{Ticket: out.Data.Ticket, Channel: ch}, nil
}

// setPlatformHeaders 补上平台客户端那套请求头，少一个都可能被判成非平台客户端。
func setPlatformHeaders(req *http.Request) {
	req.Header.Set("channel", "platform")
	req.Header.Set("cv", "w1.18.45.8")
	req.Header.Set("deviceid", "8C:32:23:14:06:2F")
	req.Header.Set("uni-mac", platformUDID)
}

// Login 用一张 MPay token 换加载湾会话，返回 sid。
//
// 参数照抄平台自己的 app/v1/login（sauth_json 内是平台那套字段）。
// 不需要 aim_info / ip —— 实测省略也能通过。
func Login(uid, token, deviceID string) (string, error) {
	return LoginExt(uid, token, deviceID, platformUDID)
}

// LoginExt 和 Login 一样，但可以指定平台 udid。
//
// 用途（诊断）：加载湾的会话可能同时绑 uid / udid / deviceid。如果换 udid 能拿到
// 不同的 sid，那同一个账号就能占多个"记住槽"，缓解"槽位不够、新号顶掉老号"。
func LoginExt(uid, token, deviceID, udid string) (string, error) {
	if udid == "" {
		udid = platformUDID
	}
	if uid == "" || token == "" {
		return "", fmt.Errorf("缺少账号信息")
	}
	sauth := map[string]any{
		"gameid":           "a50",
		"login_channel":    "netease",
		"app_channel":      "netease.allysdk3rd",
		"platform":         "pc",
		"sdkuid":           uid,
		"sessionid":        token,
		"sdk_version":      "4.21.0",
		"udid":             udid,
		"deviceid":         deviceID,
		"client_login_sn":  "2C5C72F7F23D8F698238DD5EC4A5BA3B",
		"gas_token":        "",
		"source_platform":  "pc",
		"get_access_token": "1",
	}
	inner, _ := json.Marshal(sauth)
	body, _ := json.Marshal(map[string]string{"sauth_json": string(inner), "state": ""})

	req, err := http.NewRequest(http.MethodPost, SessionURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	setPlatformHeaders(req)

	resp, err := netdns.NewClient(20 * time.Second).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var out struct {
		Data struct {
			Sid string `json:"sid"`
			ID  string `json:"id"`
		} `json:"data"`
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(b, &out) != nil {
		return "", fmt.Errorf("登录响应解析失败: %s", truncate(string(b), 160))
	}
	if out.Data.Sid == "" {
		return "", fmt.Errorf("换会话失败: code=%d %s", out.Code, out.Msg)
	}
	return out.Data.Sid, nil
}

// TicketForCode 诊断探针：拿一个扫码 code（或任意串）走槽位出票接口。
// 只给 zz_* 测试用。
func TicketForCode(code, sid string) (*TicketResult, error) {
	q := url.Values{}
	q.Set("code", code)
	q.Set("app_id", SkyAppID)
	req, err := http.NewRequest(http.MethodGet, TicketURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/json")
	setPlatformHeaders(req)
	if sid != "" {
		req.Header.Set("sid", sid)
	}
	resp, err := netdns.NewClient(20 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Ticket       string `json:"ticket"`
			LoginChannel string `json:"login_channel"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &out) != nil {
		return nil, fmt.Errorf("响应解析失败: %s", truncate(string(body), 120))
	}
	if out.Data.Ticket == "" {
		return nil, fmt.Errorf("槽位出票失败: code=%d %s", out.Code, out.Msg)
	}
	return &TicketResult{Ticket: out.Data.Ticket, Channel: out.Data.LoginChannel}, nil
}
