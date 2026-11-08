package mpay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"skylogin/internal/profile"
)

// CompleteHostedQRCode 供授权探针使用：标记扫描、确认指定码，再轮询授权码。
// 返回的是 login_info.code，不是二维码 UUID、确认接口的业务码或 AAAA 票。
// 调用方必须提供超时上下文；这里不兑换票据，也不写平台的寄宿槽。
func (c *Client) CompleteHostedQRCode(ctx context.Context, uuid, gameID string, payload map[string]string) (string, error) {
	if uuid == "" || gameID == "" {
		return "", fmt.Errorf("扫码授权缺少 uuid 或 game_id")
	}
	if _, ok := ctx.Deadline(); !ok {
		return "", fmt.Errorf("扫码授权必须设置超时时间")
	}
	scanGame := gameID
	for attempt := 0; attempt < 2; attempt++ {
		q := url.Values{
			"uuid": {uuid}, "game_id": {scanGame},
			"login_channel": {c.channel()}, "app_channel": {c.channel()}, "pay_channel": {c.channel()},
			"gv": {profile.MpayGV}, "gvn": {profile.MpayGvn}, "cv": {profile.MpayCv},
		}
		out, err := c.probeQRCodeRequest(ctx, http.MethodGet, "/mpay/api/qrcode/scan", q)
		if err != nil {
			return "", fmt.Errorf("标记扫描: %w", err)
		}
		code, err := probeBusinessCode(out)
		if err != nil {
			return "", fmt.Errorf("标记扫描: %w", err)
		}
		if code == 1424 && attempt == 0 {
			game, _ := out["game"].(map[string]any)
			redirect, _ := game["id"].(string)
			if redirect == "" || redirect == scanGame {
				return "", fmt.Errorf("标记扫描: 1424 缺少可用的目标游戏")
			}
			scanGame = redirect
			continue
		}
		if code != 0 {
			return "", fmt.Errorf("标记扫描被拒绝: code=%d", code)
		}
		break
	}

	form := url.Values{}
	for k, v := range payload {
		form.Set(k, v)
	}
	// 会话编号以这次建码结果为准，不能沿用登录包中的旧编号。
	form.Set("uuid", uuid)
	form.Set("game_id", scanGame)
	out, err := c.probeQRCodeRequest(ctx, http.MethodPost, "/mpay/api/qrcode/confirm_login", form)
	if err != nil {
		return "", fmt.Errorf("确认授权: %w", err)
	}
	code, err := probeBusinessCode(out)
	if err != nil {
		return "", fmt.Errorf("确认授权: %w", err)
	}
	if code != 0 {
		return "", fmt.Errorf("确认授权被拒绝: code=%d", code)
	}

	for {
		q := url.Values{"uuid": {uuid}, "game_id": {scanGame}, "cv": {profile.HostedCv}}
		out, err = c.probeQRCodeRequest(ctx, http.MethodGet, "/mpay/api/qrcode/query", q)
		if err != nil {
			return "", fmt.Errorf("轮询授权: %w", err)
		}
		code, err = probeBusinessCode(out)
		if err != nil {
			return "", fmt.Errorf("轮询授权: %w", err)
		}
		if code != 0 && code != -1 {
			return "", fmt.Errorf("轮询授权被拒绝: code=%d", code)
		}
		qr, _ := out["qrcode"].(map[string]any)
		status, err := probeNumber(qr["status"])
		if err != nil {
			return "", fmt.Errorf("轮询授权缺少有效的二维码状态")
		}
		if status == 2 {
			info, _ := out["login_info"].(map[string]any)
			authorizationCode, _ := info["code"].(string)
			if authorizationCode == "" {
				return "", fmt.Errorf("二维码已确认，但缺少 login_info.code")
			}
			return authorizationCode, nil
		}
		if status != 0 && status != 1 {
			return "", fmt.Errorf("二维码已结束或状态未知: status=%d", status)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", fmt.Errorf("轮询授权: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// probeQRCodeRequest 只用于实验链路，检查 HTTP 和 JSON，不把凭证或响应全文写进错误。
func (c *Client) probeQRCodeRequest(ctx context.Context, method, path string, values url.Values) (map[string]any, error) {
	u := c.Base + path
	var body io.Reader
	if method == http.MethodGet {
		u += "?" + values.Encode()
	} else {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败")
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("网络请求失败")
	}
	defer resp.Body.Close()
	var out map[string]any
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil || out == nil {
		return nil, fmt.Errorf("HTTP=%d，响应不是有效 JSON 对象", resp.StatusCode)
	}
	if c.ProbeTrace != nil {
		c.ProbeTrace(method, path, values, out)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code, _ := probeBusinessCode(out)
		// 实测 scan 用 HTTP 400 携带 1424 和 game.id；这是上下文切换指令，
		// 交给调用方处理。其它 HTTP 错误仍立即失败，不能忽略确认阶段的拒绝。
		if resp.StatusCode == http.StatusBadRequest && path == "/mpay/api/qrcode/scan" && code == 1424 {
			return out, nil
		}
		return nil, fmt.Errorf("HTTP=%d code=%d", resp.StatusCode, code)
	}
	return out, nil
}

func probeBusinessCode(out map[string]any) (int, error) {
	v, exists := out["code"]
	if !exists {
		return 0, nil // 官方确认成功的响应可以是空对象。
	}
	return probeNumber(v)
}

func probeNumber(v any) (int, error) {
	switch n := v.(type) {
	case json.Number:
		return strconv.Atoi(string(n))
	case string:
		return strconv.Atoi(n)
	default:
		return 0, fmt.Errorf("响应包含无效的数字字段")
	}
}
