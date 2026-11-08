package app

// 会话凭证的滚动续期。
//
// 扫码 / 网页登录拿到的 token 是**会话级**（`sa:3`，带 `lt`/`od`），会过期；
// 但实测：拿它签一张票再走 `login/ticket`，换回来的新 token `lt` 会刷成当前时间
// （2026-11-05 实测：lt 从 1793879111 变成 1793879845，且新 token 立刻可用）。
//
// 也就是说**只要在过期前滚一次，这个号就能一直用下去**，不用重新扫码。
// 所以：启动器每次启动，把"好久没滚过"的会话 token 滚一遍。

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"skylogin/internal/mpay"
)

// tokenLoginUnix 取 token 里的 lt（登录时间，秒）；不是会话级 token 返回 0。
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
//	-9  = 账号级（密码 / 短信登录拿到，永久）
//	 3  = 普通会话级（扫码拿到，会过期，可以滚）
//	-4  = 大神扫码拿到的会话（也会过期，但不知道活多久）
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

// rollable 判断这份凭证要不要滚。
//
// 规则（2026-11-08 晚修订）：
//
//	sa=3  —— 普通会话凭证（扫码 / 网页 / 换票得来）：可滚，滚动只刷新 lt，稳定。
//	sa=-4 —— 大神扫码 / 手机号那种会话：寿命未知。以前不敢碰，怕"本来能活更久、
//	         滚一下就缩短了"。但现在实测 sa=3 的凭证**同样能按渠道签发、同样能启动
//	         游戏**，滚成 3 等于把它纳入可续期轨道，比放着自然过期强。
//	         所以现在允许滚；万一某天发现它其实很长寿，再改回来。
//	sa=-9 —— 账号级，永久有效。滚一遍会被降级成会话级，绝对不碰。
func rollable(token string) bool {
	switch tokenSaValue(token) {
	case 3:
		return tokenLoginUnix(token) > 0
	case -4:
		return true
	}
	return false
}

// rollSessionToken 把一条会话 token 滚到"现在"，返回新 token。
//
// 只处理 sa=3 的普通会话凭证，理由见 rollable 的注释。
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

// rollAfter 超过这个时长没滚过就滚一次。
// 会话凭证实测能活几天到几周，留足余量，避免用户隔两天开一次就失效。
const rollAfter = 12 * time.Hour

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
			continue // 账号级 / 大神扫码那种，先不碰（见 rollable）
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
// 一小时内滚过的就不重复滚了（多开时同一个号可能被连着取好几次票）。
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
			return // 账号级 / 大神扫码那种，先不碰
		}
		lt := tokenLoginUnix(a.MpayToken)
		if time.Now().Unix()-lt < 3600 {
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
