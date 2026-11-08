package app

// 会话凭证的滚动续期。
//
// 扫码 / 网页登录拿到的 token 是**会话级**（`sa:3`，带 `lt`/`od`），会过期；
// 但实测：拿它签一张票再走 `login/ticket`，换回来的新 token `lt` 会刷成当前时间
// （2026-11-05 实测：lt 从 1793879111 变成 1793879845，且新 token 立刻可用）。
//
// 这能刷新会话的登录时间，但短期验证不能证明无限续期。
// 自动滚动的等级开关和间隔集中在下面，便于根据后续寿命实测调整。

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"skylogin/internal/mpay"
)

// 自动滚动策略：只在这里调整各等级开关，未列出的等级默认关闭。
// 负数等级先保留原凭证：-4 的寿命未知，-9/-99 是账号级，滚动可能降级成 3。
// 这是编译期的代码配置，不从账号或界面动态修改。
var tokenRollEnabledBySA = map[float64]bool{
	3:   true,
	-4:  false,
	-9:  false,
	-99: false,
}

const (
	// 打开登录器时，距凭证登录时间超过此间隔才滚动；不是常驻定时器。
	rollAfter = 12 * time.Hour
	// 单开成功拿票后的滚动间隔，避免短时间重复续期。
	rollAfterLaunch = time.Hour
)

// tokenLoginUnix 取 token 里的 lt（登录时间，秒）；没有该字段时返回 0。
func tokenLoginUnix(token string) int64 {
	raw := strings.TrimPrefix(token, "1-")
	b, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		if b, err = base64.StdEncoding.DecodeString(raw); err != nil {
			return 0
		}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return 0
	}
	switch v := m["lt"].(type) {
	case float64:
		return int64(v)
	case string:
		var n int64
		for _, r := range v {
			if r < '0' || r > '9' {
				return 0
			}
			n = n*10 + int64(r-'0')
		}
		return n
	}
	return 0
}

// tokenSaValue 取凭证里的 sa 值：
//
//	-9/-99 = 账号级（长期有效，仍可能被撤销）
//	 3     = 普通会话级（会过期，可滚动续期）
//	-4     = 扫码或手机号登录可能返回的等级，实际寿命未知
//
// 不认识的返回 0。
func tokenSaValue(token string) float64 {
	raw := strings.TrimPrefix(token, "1-")
	b, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		if b, err = base64.StdEncoding.DecodeString(raw); err != nil {
			return 0
		}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return 0
	}
	switch v := m["sa"].(type) {
	case float64:
		return v
	case string:
		var f float64
		neg := false
		for i, r := range v {
			if i == 0 && r == '-' {
				neg = true
				continue
			}
			if r < '0' || r > '9' {
				return 0
			}
			f = f*10 + float64(r-'0')
		}
		if neg {
			f = -f
		}
		return f
	}
	return 0
}

// rollable 统一判断当前策略是否允许滚动，所有滚动入口都经过这里。
// sa=3 还要求带有效 lt，保留原有检查；其它等级开启后允许没有 lt。
func rollable(token string) bool {
	sa := tokenSaValue(token)
	if !tokenRollEnabledBySA[sa] {
		return false
	}
	return sa != 3 || tokenLoginUnix(token) > 0
}

// rollSessionToken 把一条会话 token 滚到"现在"，返回新 token。
//
// 只处理集中策略允许的凭证，见 tokenRollEnabledBySA。
func rollSessionToken(acc Account, devID string) (string, error) {
	if acc.MpayToken == "" || !rollable(acc.MpayToken) {
		return "", errors.New("不是可续期的会话凭证，跳过")
	}
	longID, shortID := ticketContextForToken(acc.MpayToken)
	c := mpay.New()
	ticket, _, err := c.CreateTicketFor(acc.MpayToken, acc.UserID, devID, longID, shortID)
	if err != nil {
		return "", err
	}
	res, err := c.LoginTicket(ticket, devID, longID, shortID)
	if err != nil {
		return "", err
	}
	user, _ := res["user"].(map[string]any)
	tok, _ := user["token"].(string)
	if tok == "" {
		return "", errors.New("login/ticket 没返回 token")
	}
	return tok, nil
}

// rollStaleTokens 启动时把该滚的会话 token 滚一遍（后台跑，不挡界面）。
func rollStaleTokens() {
	list, err := loadAccounts()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	rolled := 0
	for i := range list {
		a := list[i]
		if a.Channel != "netease" || a.MpayToken == "" {
			continue
		}
		if !rollable(a.MpayToken) {
			continue // 当前等级未开启滚动，或缺少必要的登录时间
		}
		lt := tokenLoginUnix(a.MpayToken)
		if now-lt < int64(rollAfter.Seconds()) {
			continue
		}
		tok, rerr := rollSessionToken(a, a.DeviceID)
		if rerr != nil {
			logf("%s：会话凭证续期失败（%v），等下次启动再试", displayName(a), rerr)
			continue
		}
		a.MpayToken = tok
		a.UpdatedAt = now
		if serr := saveAccount(a); serr != nil {
			logf("%s：续期成功但保存失败: %v", displayName(a), serr)
			continue
		}
		rolled++
		logf("%s：会话凭证已续期（lt 推到现在）", displayName(a))
		// 别把服务端打疼
		time.Sleep(300 * time.Millisecond)
	}
	if rolled > 0 {
		logf("共续期 %d 个账号的会话凭证", rolled)
	}
}

// rollAccountToken 给单个账号滚一次（启动游戏后顺手做，后台）。
//
// rollAfterLaunch 内不重复滚动，避免连续启动时重复取票续期。
func rollAccountToken(userID string) {
	list, err := loadAccounts()
	if err != nil {
		return
	}
	for i := range list {
		a := list[i]
		if a.UserID != userID || a.Channel != "netease" || a.MpayToken == "" {
			continue
		}
		if !rollable(a.MpayToken) {
			return // 当前等级未开启滚动，或缺少必要的登录时间
		}
		lt := tokenLoginUnix(a.MpayToken)
		if time.Now().Unix()-lt < int64(rollAfterLaunch.Seconds()) {
			return
		}
		tok, rerr := rollSessionToken(a, a.DeviceID)
		if rerr != nil {
			logf("%s：本次启动后续期失败（%v）", displayName(a), rerr)
			return
		}
		a.MpayToken = tok
		a.UpdatedAt = time.Now().Unix()
		if err := saveAccount(a); err == nil {
			logf("%s：会话凭证已续期", displayName(a))
		}
		return
	}
}

// tokenFields 解出 token 的全部字段（诊断用，只看值不打日志泄露）。
func tokenFields(token string) map[string]any {
	raw := strings.TrimPrefix(token, "1-")
	b, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		b, _ = base64.StdEncoding.DecodeString(raw)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	return m
}

// tokenGI 取 token 的签发身份（g_i）：aecglf6ee4aaaarz=a50 平台，
// aecfs5n2y4aaaakd=ma75 光遇本体。解不出返回空串。
func tokenGI(token string) string {
	if v, _ := tokenFields(token)["g_i"].(string); v != "" {
		return v
	}
	return ""
}
