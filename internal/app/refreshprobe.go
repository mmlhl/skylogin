package app

// refresh_token 探针（开发用）：验证"渠道服扫码的账号能不能用 oauth2 的
// refresh_token 续期"，也就是过期后能不能不重扫。
//
//	skylogin.exe --probe-refresh
//
// 步骤：
//  1. 从账号里挑一条渠道服扫码的（有整包的），真发一次 uni_sauth
//  2. 把响应里 unisdk_login_json.oauth2 的 access_token / refresh_token 解出来，
//     看它们的 exp（JWT 明文段）
//  3. 拿 refresh_token 去试几种换新 token 的口子，把每次的回答都记下来
//
// 结果写 %APPDATA%\SkyLogin\refresh_probe.log。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
)

// ProbeRefreshToken 试 refresh_token 续期。
func ProbeRefreshToken() {
	logPath := filepath.Join(dataDir(), "refresh_probe.log")
	log := func(format string, args ...any) { qrProbeLog(logPath, format, args...) }
	log("=== refresh_token 探针 ===")

	// 挑一条还能用的：渠道凭证本身有寿命（vivo 实测约 1 小时），
	// 过期的那条连 uni_sauth 都过不了，得换下一条试。
	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		log("设备画像不可用: %v", derr)
		return
	}
	var acc Account
	var parts sauthParts
	var resp map[string]any
	found := false
	for _, a := range packageAccounts() {
		p, perr := buildSauth(a, dev)
		if perr != nil {
			continue
		}
		r, rerr := netsec.PostSigned(p.SAUTH, profile.ShortGameID)
		if rerr != nil || decodeUnisdkLoginJSON(r) == nil {
			log("%s 的凭证已失效: %s", a.Name, cut(redactJSON(r), 160))
			continue
		}
		acc, parts, resp, found = a, p, r, true
		break
	}
	if !found {
		log("所有渠道服扫码账号的凭证都失效了，需要重新扫码")
		return
	}
	log("用账号: %s（channel=%s）", acc.Name, acc.Channel)

	login := decodeUnisdkLoginJSON(resp)
	if login == nil {
		log("响应里没有 unisdk_login_json: %s", cut(redactJSON(resp), 400))
		return
	}
	oauth, _ := login["oauth2"].(map[string]any)
	access, _ := oauth["access_token"].(string)
	refresh, _ := oauth["refresh_token"].(string)
	log("access_token: %d 字符 exp=%s", len(access), jwtExp(access))
	log("refresh_token: %d 字符 exp=%s", len(refresh), jwtExp(refresh))
	if refresh == "" {
		log("没有 refresh_token，这条路走不了")
		return
	}

	// 试各种口子。每个都只发一次，看服务端怎么答。
	try := func(name string, f func() (int, string, error)) {
		code, body, err := f()
		if err != nil {
			log("[%s] 请求失败: %v", name, err)
			return
		}
		log("[%s] HTTP %d -> %s", name, code, cut(redactBody(body), 320))
	}

	base := profile.UniSdkBaseURL + "/" + profile.ShortGameID + "/sdk/"

	// 关键对照：把原来的 sessionid 拿掉，只给 refresh_token。
	// 不拿掉的话请求会靠"还没过期的 sessionid"通过，测不出 refresh 到底行不行。
	withRefresh := func(session string) map[string]any {
		sauth := map[string]any{}
		for k, v := range parts.SAUTH {
			sauth[k] = v
		}
		sauth["sessionid"] = session
		sauth["refresh_token"] = refresh
		sauth["grant_type"] = "refresh_token"
		return sauth
	}

	// 1. 只带 refresh_token（sessionid 置空）—— 这是"过期后能不能续"的真测试
	try("unia_sauth(空 sessionid + refresh_token)", func() (int, string, error) {
		return postSignedPath(base+"uni_sauth", withRefresh(""))
	})

	// 2. sessionid 乱填 + refresh_token
	try("uni_sauth(假 sessionid + refresh_token)", func() (int, string, error) {
		return postSignedPath(base+"uni_sauth", withRefresh("expired_session_for_test"))
	})

	// 3. 对照：两个都拿掉，应该失败
	try("uni_sauth(都不要，对照)", func() (int, string, error) {
		sauth := map[string]any{}
		for k, v := range parts.SAUTH {
			sauth[k] = v
		}
		sauth["sessionid"] = ""
		return postSignedPath(base+"uni_sauth", sauth)
	})

	// 4. 把 refresh_token 当 sessionid 用
	try("uni_sauth(sessionid=refresh)", func() (int, string, error) {
		sauth := map[string]any{}
		for k, v := range parts.SAUTH {
			sauth[k] = v
		}
		sauth["sessionid"] = refresh
		return postSignedPath(base+"uni_sauth", sauth)
	})

	// 3. 猜一个专用口
	try("POST sdk/uni_refresh_token", func() (int, string, error) {
		return postSignedPath(base+"uni_refresh_token", map[string]any{
			"gameid":        profile.ShortGameID,
			"refresh_token": refresh,
			"udid":          acc.DeviceID,
		})
	})

	// 4. 猜 oauth2 的标准口
	try("POST sdk/oauth2/token", func() (int, string, error) {
		return postSignedPath(base+"oauth2/token", map[string]any{
			"grant_type":    "refresh_token",
			"refresh_token": refresh,
			"gameid":        profile.ShortGameID,
		})
	})
}

// packageAccounts 返回所有带整包的渠道服扫码账号。
func packageAccounts() []Account {
	list, err := loadAccounts()
	if err != nil {
		return nil
	}
	out := make([]Account, 0, len(list))
	for _, a := range list {
		if a.UnisdkData != "" {
			out = append(out, a)
		}
	}
	return out
}

// postSignedPath 给任意 sdk 路径发带签名的 JSON 请求。
//
// 签名规则和 uni_sauth 一致：HMAC-SHA256(方法 + 完整 URL + body, log_key)。
func postSignedPath(endpoint string, data map[string]any) (int, string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("X-Client-Sign", netsec.HMACSign(http.MethodPost, endpoint, string(payload), protocol.Get().LogKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Dalvik/2.1.0 (Linux; U; Android 12; M2102K1AC Build/V417IR)")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	return resp.StatusCode, string(b), nil
}

// decodeUnisdkLoginJSON 解出响应里的 unisdk_login_json（base64 的 JSON）。
func decodeUnisdkLoginJSON(resp map[string]any) map[string]any {
	s, _ := resp["unisdk_login_json"].(string)
	if s == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// jwtExp 读 JWT 明文段里的 exp，转成可读时间；不是 JWT 就返回 "?"。
func jwtExp(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return "?"
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		if raw, err = base64.StdEncoding.DecodeString(parts[1]); err != nil {
			return "?"
		}
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return "?"
	}
	exp, _ := m["exp"].(float64)
	if exp == 0 {
		return "?"
	}
	t := time.Unix(int64(exp), 0)
	return fmt.Sprintf("%s（还有 %d 小时）", t.Format("2006-01-02 15:04"), int(time.Until(t).Hours()))
}

// redactBody 把响应里像凭证的长串打码，日志只留结构。
func redactBody(s string) string {
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil {
		return s
	}
	return redactJSON(m)
}
