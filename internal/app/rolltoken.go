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

// ── 寄宿续期 ──────────────────────────────────────────────────────
//
// 被寄宿的号（住在别人平台槽里的扫码号）自己那份 token 有两种坏法：
//
//	① 彻底过期：签票、查资料都 1306，rollSessionToken 直接失败；
//	② "半死"：create_ticket 还能签（服务端对会话校验很松），签出来是废票，
//	   只有 login/ticket 才 1306 —— 2026-11-11 实测就是这个状态。
//
// 两种情况下 rollSessionToken 都救不回来，但这个号其实照样能启动：票是从宿主的
// 平台槽出的，根本不需要它自己的 token。既然槽位票不需要 token，那它也能反过来
// 喂回一条新 token：
//
//	槽位票（GET third_party_account/ticket）→ login/ticket → 新 token（lt=现在）
//
// 2026-11-11 探针实测：换回来的 token sa=3、lt=现在，立刻能签票、能查用户资料。
// 所以"寄宿"不只是免扫出票，还能把这个号的凭证重新养回来。

// rollHostedToken 用寄宿槽给这个号换一条新会话凭证。
//
// 上下文只能用游戏本体（ma75）：槽位票是平台按扫码设备签的**游戏票**，
// 拿 a50 上下文去换实测 1003 用户请求参数错误。
func rollHostedToken(acc Account) (string, error) {
	ticket, err := rememberedTicket(acc)
	if err != nil {
		return "", err
	}
	if ticket == "" {
		return "", errors.New("寄宿槽没有给出票")
	}
	res, lerr := mpay.New().LoginTicket(ticket, acc.DeviceID, gameLong, gameShort)
	if lerr != nil {
		return "", lerr
	}
	user, _ := res["user"].(map[string]any)
	tok, _ := user["token"].(string)
	if tok == "" {
		return "", errors.New("login/ticket 没返回 token")
	}
	return tok, nil
}

// healHostedCred 用寄宿槽把这个号自己的凭证重新养回来。
//
// 只对"被寄宿"的号有意义：槽里没有它的号，rememberedTicket 只会 6008。
// 这里先用本地账本做一次廉价预筛——账本没记录就直接放弃，否则每个普通账号
// 都要挨个宿主去换 sid 问一遍（很贵）。账本不准也不影响启动：出票走的是
// mintTicketOnce，它不依赖账本。
// 成功 = token 换成新的、状态改回 ok，界面上那个"凭证过期"自己就消失了。
func healHostedCred(acc Account) bool {
	if acc.Channel != "netease" || acc.UserID == "" {
		return false
	}
	// 长期凭证（sa=-9/-99）和寿命未知的 -4 不能主动换：槽位票换回来的是 sa=3
	// 的会话凭证，等于把"永久"换成"会过期"（.ai/凭证研究/02 实测过）。
	// 只有它已经确认失效时才值得换——那时候换到什么都不会更差。
	if sa := tokenSaValue(acc.MpayToken); sa < 0 && acc.CredState != "expired" {
		return false
	}
	if _, ok := slotBindingOf(acc.UserID); !ok {
		return false
	}
	tok, err := rollHostedToken(acc)
	if err != nil {
		logf("%s：寄宿续期没成功（%v）", displayName(acc), err)
		return false
	}
	acc.MpayToken = tok
	acc.TokenSa = int(tokenSaValue(tok))
	acc.TokenAt = tokenLoginUnix(tok)
	acc.UpdatedAt = time.Now().Unix()
	acc.CredState = "ok"
	acc.LastError = ""
	if serr := saveAccount(acc); serr != nil {
		logf("%s：寄宿续期成功但保存失败（%v）", displayName(acc), serr)
		return false
	}
	setAccountCredState(acc.UserID, "ok", "")
	logf("%s：寄宿槽换回了新凭证（lt 推到现在），这个号不用再重扫", displayName(acc))
	return true
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
		// 已经确认失效的号：被寄宿的话，槽位票能直接换回一条能用的新凭证。
		// 这一步比等用户重新扫码省事，也顺手把界面上的"凭证过期"清掉。
		if a.CredState == "expired" {
			if healHostedCred(a) {
				rolled++
				time.Sleep(300 * time.Millisecond)
				continue
			}
			// 不是寄宿号（或槽也用不了）：交给下面的正常滚动再试一次，
			// 免得"过期"是旧状态时白放弃一次机会。
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
			// 自己那份 token 救不回来时，被寄宿的号还有一条路：槽位票不需要
			// 它自己的 token（见 rollHostedToken）。
			if healHostedCred(a) {
				rolled++
				time.Sleep(300 * time.Millisecond)
				continue
			}
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
		// 已经确认失效的号：先走寄宿续期（被寄宿的号靠槽位票就能换回新凭证）。
		if a.CredState == "expired" {
			if healHostedCred(a) {
				return
			}
			// 不是寄宿号 / 槽也用不了：继续往下试正常滚动，别把机会堵死
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
			// 自己那份 token 救不回来（过期 / 半死）时，被寄宿的号还能靠槽位票
			// 换一条新的（见 rollHostedToken）。
			if healHostedCred(a) {
				return
			}
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
