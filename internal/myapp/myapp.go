// Package myapp 实现腾讯应用宝（YSDK）的微信扫码登录。
//
// 为什么要它
//
//	光遇 PC 客户端只内置了网易自己的渠道，应用宝服的光遇是**腾讯渠道包**
//	（com.tencent.tmgp.eyou.eygy，game_id 同样是 ma75）。想加应用宝账号，
//	就得自己完成"微信扫码 → 应用宝 openid"这一步。
//
// 协议（纯 HTTP，不涉及 mmtls）
//
//  1. GET  ysdk.qq.com/auth/wx_scan_code_login   取签名参数
//  2. GET  open.weixin.qq.com/connect/sdk/qrconnect 换二维码图 + uuid
//  3. GET  long.open.weixin.qq.com/connect/l/qrconnect  轮询扫码状态
//  4. GET  ysdk.qq.com/auth/wx_verify_code       用 wx_code 换应用宝登录态
//
// 参数取值：wx_appid / offerid 是光遇应用宝版（com.tencent.tmgp.eyou.eygy）
// 渠道包自带的常量；签名盐来自腾讯官方 YSDK 库本身（见 sigMagic 的注释）。
package myapp

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	// WxAppID 是光遇应用宝版在微信开放平台的 appid。
	WxAppID = "wx365db603f56bf4da"
	// OfferID 是应用宝给光遇的 offerid（YSDK 里的 appid）。
	OfferID = "1109425751"
	// Version 是应用宝 SDK 版本号，服务端按它校验。
	Version = "2.2.2"

	// sigMagic 是签名用的固定盐：sig = md5(timestamp + sigMagic)。
	//
	// 出处：腾讯官方 YSDK 的 native 库 libYSDK.so（光·遇 0.16.7 APK 内
	// lib/arm64-v8a/libYSDK.so，.rodata 偏移 0x195d0）。那里是一段连着的
	// 字符串池：
	//	"0123456789abcdef0123456789ABCDEF445B2EB80C3737BE5C2F3668B46D3E96"
	//	"5C2F##3[6$^(68#%#D3E96;]35q#FB46"
	// 也就是说它是 SDK 自带的互操作性常量，不是任何第三方工具的产物。
	sigMagic = "5C2F##3[6$^(68#%#D3E96;]35q#FB46"
	// nonceBase 是 noncestr 的前缀，后面拼时间戳。
	//
	// 服务端**不校验** noncestr 内容：实测空前缀、任意串都能拿到 ret=0，
	// 所以这里用自己的前缀，跟任何第三方实现无关。
	nonceBase = "skylogin-unisdk-bridge"
	// scope 是微信授权范围，和应用宝客户端一致。
	scope = "snsapi_userinfo,snsapi_friend,snsapi_message"

	ysdkBase   = "https://ysdk.qq.com"
	weixinQR   = "https://open.weixin.qq.com/connect/sdk/qrconnect"
	weixinPoll = "https://long.open.weixin.qq.com/connect/l/qrconnect"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// Session 是一次微信扫码会话。
type Session struct {
	UUID      string
	QRCodePNG []byte // 微信返回的二维码（base64 解出来的 PNG）
	sign      map[string]string
}

// CreateSession 申请一张应用宝的微信登录二维码。
func CreateSession() (*Session, error) {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)

	// 第一步：让 ysdk 给一组带签名的参数
	q := url.Values{}
	q.Set("noncestr", nonceBase+ts)
	q.Set("scope", scope)
	q.Set("wx_appid", WxAppID)
	q.Set("loginplatform", "2")
	q.Set("sig", signature(ts))
	q.Set("appid", WxAppID)
	q.Set("timestamp", ts)

	var signed struct {
		AppID     string `json:"appid"`
		NonceStr  string `json:"noncestr"`
		Signature string `json:"signature"`
		Timestamp string `json:"timestamp"`
		Scope     string `json:"scope"`
		Ret       int    `json:"ret"`
		Msg       string `json:"msg"`
	}
	if err := getJSON(ysdkBase+"/auth/wx_scan_code_login?"+q.Encode(), &signed); err != nil {
		return nil, err
	}
	if signed.Ret != 0 {
		return nil, fmt.Errorf("申请扫码失败: ret=%d %s", signed.Ret, signed.Msg)
	}

	// 第二步：拿这组参数去微信换二维码
	wq := url.Values{}
	wq.Set("appid", signed.AppID)
	wq.Set("noncestr", signed.NonceStr)
	wq.Set("timestamp", signed.Timestamp)
	wq.Set("scope", signed.Scope)
	wq.Set("signature", signed.Signature)

	var qr struct {
		UUID   string `json:"uuid"`
		QRCode struct {
			Base64 string `json:"qrcodebase64"`
		} `json:"qrcode"`
		ErrCode int `json:"errcode"`
	}
	if err := getJSON(weixinQR+"?"+wq.Encode(), &qr); err != nil {
		return nil, err
	}
	if qr.UUID == "" {
		return nil, fmt.Errorf("微信没有返回 uuid（errcode=%d）", qr.ErrCode)
	}

	png, err := decodeBase64Image(qr.QRCode.Base64)
	if err != nil {
		return nil, err
	}
	return &Session{
		UUID:      qr.UUID,
		QRCodePNG: png,
		sign: map[string]string{
			"appid": signed.AppID, "noncestr": signed.NonceStr,
			"timestamp": signed.Timestamp, "scope": signed.Scope, "signature": signed.Signature,
		},
	}, nil
}

// ScanState 是轮询结果。
type ScanState int

const (
	Waiting  ScanState = iota // 还没扫
	Scanned                   // 扫了没确认
	Canceled                  // 用户在手机上取消
	Expired                   // 二维码过期
)

// Poll 查一次扫码状态；confirmed 时返回 wx_code。
func (s *Session) Poll() (state ScanState, code string, err error) {
	state, code, _, err = s.PollRaw()
	return
}

// PollRaw 和 Poll 一样，但把原始响应也带出来（排查用）。
func (s *Session) PollRaw() (state ScanState, code, raw string, err error) {
	var out struct {
		WxErrCode int    `json:"wx_errcode"`
		WxCode    string `json:"wx_code"`
	}
	b, gerr := getRaw(weixinPoll + "?f=json&uuid=" + url.QueryEscape(s.UUID))
	if gerr != nil {
		return Waiting, "", "", gerr
	}
	raw = string(b)
	if json.Unmarshal(b, &out) != nil {
		return Waiting, "", raw, fmt.Errorf("轮询响应不是 JSON: %s", truncate(raw, 200))
	}
	// 注意：微信确认后返回的是 **wx_errcode=405 + wx_code**，不是 0。
	// 所以先看有没有 code，再看状态码，否则会把"确认成功"当成"扫了没确认"。
	if out.WxCode != "" {
		return Scanned, out.WxCode, raw, nil
	}
	switch out.WxErrCode {
	case 405:
		return Scanned, "", raw, nil
	case 403:
		return Canceled, "", raw, nil
	case 402:
		return Expired, "", raw, nil
	}
	return Waiting, "", raw, nil
}

// Verify 用 wx_code 换取应用宝的登录态（openid / access_token 等）。
//
// channel 字段服务端要的是固定串，先按抓到的那份传 "00000000"；
// 不行时调用方可以换 "myapp" 再试（见 CLI 探针）。
func (s *Session) Verify(code, channel string) (map[string]any, error) {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	q := url.Values{}
	q.Set("channel", channel)
	q.Set("code", code)
	q.Set("offerid", OfferID)
	q.Set("platform", "desktop_m_wechat")
	q.Set("client_hope_switch", "1")
	q.Set("wx_appid", WxAppID)
	q.Set("version", Version)
	q.Set("sig", signature(ts))
	q.Set("anti_hope_switch", "1")
	q.Set("appid", WxAppID)
	q.Set("timestamp", ts)

	var out map[string]any
	if err := getJSON(ysdkBase+"/auth/wx_verify_code?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	if ret, _ := out["ret"].(float64); ret != 0 {
		return out, fmt.Errorf("校验 wx_code 失败: ret=%v msg=%v", out["ret"], out["msg"])
	}
	return out, nil
}

// signature 是 ysdk 那套签名：md5(timestamp + 固定盐)。
func signature(ts string) string {
	sum := md5.Sum([]byte(ts + sigMagic))
	return hex.EncodeToString(sum[:])
}

func getJSON(u string, out any) error {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("响应不是 JSON（HTTP %d）: %s", resp.StatusCode, truncate(string(b), 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// decodeBase64Image 把微信返回的 qrcodebase64 解成 PNG 字节。
func decodeBase64Image(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("二维码内容为空")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("二维码 base64 解码失败: %w", err)
	}
	return b, nil
}

// getRaw 只取响应原文（排查轮询返回值用）。
func getRaw(u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<18))
}

// WxNickname 拉微信昵称（拉不到返回空串，不影响登录）。
//
// 单独导出是因为 channel 层要把昵称填进 Cred.Nickname，
// 而 myappNickname 是包内私有实现。
// WxNickname 拉微信昵称（拉不到返回空串，不影响登录）。
//
// 单独导出是因为 channel 层要把昵称填进 Cred.Nickname。
// 微信的 userinfo 接口偶尔会回 gzip 或 errcode，失败一律当"没昵称"处理。
func WxNickname(atk, openID string) string {
	var out struct {
		Nickname string `json:"nickname"`
		ErrCode  int    `json:"errcode"`
	}
	u := "https://api.weixin.qq.com/sns/userinfo?access_token=" +
		url.QueryEscape(atk) + "&openid=" + url.QueryEscape(openID)
	if err := getJSON(u, &out); err != nil {
		return ""
	}
	if out.ErrCode != 0 {
		return ""
	}
	return out.Nickname
}

// PollCtx 带 ctx 的轮询：用户取消时能立刻断开长轮询，不用干等 15 秒。
//
// 微信的轮询端点是长轮询（没人扫时挂 15 秒才回 408），
// 不带 ctx 的话"取消"要等这次请求自己超时才生效，体验很差。
func (s *Session) PollCtx(ctx context.Context) (ScanState, string, error) {
	var out struct {
		WxErrCode int    `json:"wx_errcode"`
		WxCode    string `json:"wx_code"`
	}
	u := weixinPoll + "?f=json&uuid=" + url.QueryEscape(s.UUID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Waiting, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Waiting, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	if err != nil {
		return Waiting, "", err
	}
	if json.Unmarshal(b, &out) != nil {
		return Waiting, "", fmt.Errorf("轮询响应不是 JSON: %s", truncate(string(b), 200))
	}
	if out.WxCode != "" {
		return Scanned, out.WxCode, nil
	}
	switch out.WxErrCode {
	case 405:
		return Scanned, "", nil
	case 403:
		return Canceled, "", nil
	case 402:
		return Expired, "", nil
	}
	return Waiting, "", nil
}

