package app

// 按所选渠道签发游戏票（网易官方账号"选区"的实现）。
//
// 背景：游戏启动后会问"你玩 iOS 还是安卓"。2026-11-08 实测（ad / ios 各一次
// 真机验证）确认：这个选择由 uni_sauth 的 platform 声明决定——声明什么就进
// 哪个区，而且不需要写寄宿槽，换票可以匿名完成。所以只要账号记住"所选渠道"，
// 每次启动按它声明一次就够了，不依赖任何服务端槽位。
//
// 完整链路：
//
//	uni_sauth(platform=渠道) → 自建托管码 → scan/confirm → query 授权码 → 匿名换票
//
// 三个必须照抄的细节（全部是实测踩出来的，改动前先读 05-无头登录真实实验）：
//
//	1. confirm 的外层 device_id 必须是**这张 token 签发时的设备号**，缺失或
//	   换了设备都会 1003（用户请求参数错误）；
//	2. confirm 的 token 必须传原生 "1-eyJ…" 文本，不能做 base64 包装，
//	   包装过的会 1306（用户登录失效）；
//	3. 建码和换票用的是 **a50 平台设备号**（托管码链路），与第 1 条的设备号
//	   不是同一个——三者不能混用。
//
// 仅对网易官方账号、且凭证是账号级（sa=-9，密码/验证码登录）启用：
// 扫码凭证（sa=-4）走这条路尚未验证，继续走原有逻辑。

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

// platformCodeOf 把账号上记的区标注翻成 MPay 的 platform 声明。
//
// 存储值沿用界面上已有的 "android" / "ios"（Region 字段，老数据兼容），
// MPay 那边要的是 ad / ios / hm。空 = 用户还没选过，不启用渠道签发。
func platformCodeOf(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "android":
		return "ad"
	case "ios":
		return "ios"
	case "harmony":
		return "hm"
	}
	return ""
}

// platformLabel 是 platform 声明的中文名（日志用）。
func platformLabel(code string) string {
	switch code {
	case "ad":
		return "安卓"
	case "ios":
		return "iOS"
	case "hm":
		return "鸿蒙"
	}
	return code
}

// channelMintable 判断这个账号能不能走渠道签发。
//
// 条件：
//   - 网易官方账号（渠道服账号的渠道由渠道 SDK 决定，不该由我们声明）；
//   - 用户已经选了渠道；
//   - 凭证是**游戏身份（ma75）签订**的。
//
// 第三条是实测出来的硬限制（2026-11-08）：
//
//	渠道签发走的是 ma75 的 uni_sauth 和 ma75 的签名密钥。用 a50（平台身份）
//	签订的凭证（典型：大神扫码，t=7、sa=-4、g_i=aecglf6ee4aaaarz）去请求，
//	服务端直接回 501（X-Client-Sign error）——身份和签名都对不上。
//	这种号必须先用账号密码/验证码登录一次，换成 ma75 凭证才能选区。
//
// 不再限制 sa 等级：手机号验证码登录也可能给 sa=-4，但只要签发身份是 ma75
// 就能走通。
//
// ⚠ 2026-11-09 更正：上面这条"不再限制 sa 等级"是错的。会话级（sa=3）能过
// uni_sauth（200），但 confirm_login 会 403/1306，签不出票。两次对照证明限制在
// 等级本身，不在来源（平台兑换出来的 sa=3、账号级凭证自己滚出来的 sa=3 都失败）。
// 所以现在要求账号级（sa=-9 / -99），见 isAccountLevelToken。
func channelMintable(acc Account) bool {
	if acc.Channel != "netease" || acc.MpayToken == "" || acc.UserID == "" {
		return false
	}
	if platformCodeOf(acc.Region) == "" {
		return false
	}
	_, short := ticketContextForToken(acc.MpayToken)
	if short != profile.ShortGameID {
		return false
	}
	return isAccountLevelToken(acc.MpayToken)
}

// isAccountLevelToken 判断是不是账号级凭证（长期有效、能走完渠道签发）。
func isAccountLevelToken(token string) bool {
	sa := tokenSaValue(token)
	return sa == -9 || sa == -99
}

// regionBlockReason 返回“选了这个渠道但当前凭证用不了”的原因（空 = 没问题）。
//
// 给界面用：用户在详情页选了渠道后，如果凭证身份不对，要当场告诉他为什么
// 下次启动仍会弹选区，而不是让他反复试。
func regionBlockReason(acc Account) string {
	if platformCodeOf(acc.Region) == "" {
		return ""
	}
	if acc.Channel != "netease" {
		return "只有网易官方账号可以选择渠道"
	}
	if acc.MpayToken == "" {
		return "还没有登录凭证"
	}
	_, short := ticketContextForToken(acc.MpayToken)
	if short != profile.ShortGameID {
		// 用户能看懂的说法：他不需要知道 a50/ma75，只需要知道"点哪"。
		return "这个号的凭证是平台身份，不能直接选区；点『刷新凭证』用密码或验证码重新登录一次即可"
	}
	if !isAccountLevelToken(acc.MpayToken) {
		return "这个号的凭证是会话级（会过期），不能免选区；点『刷新凭证』用密码或验证码重新登录一次即可"
	}
	return ""
}

// mintTicketByPlatform 用账号的长期凭证，按所选渠道签一张游戏票。
//
// 返回 (票, op14 用的渠道名, error)。任何一步失败都会整条放弃并返回错误，
// 由调用方回退到原有签票逻辑——不要在这里"尽力而为"，半截的票交给游戏只会
// 变成"网络错误"。
func mintTicketByPlatform(acc Account, dev *device.ClientProfile) (string, string, error) {
	platform := platformCodeOf(acc.Region)
	if platform == "" {
		return "", "", fmt.Errorf("账号没有记录所选渠道")
	}
	if dev == nil {
		return "", "", fmt.Errorf("缺少设备信息")
	}

	// ① 确认 token 还活着：失效的 token 也能走完一部分接口，等游戏里报错就晚了
	if err := validateNetEaseToken(acc); err != nil {
		// 文案要跟着错误性质走：网络问题说成"凭证已失效"会把人引到错的方向
		if credUnverified(err) {
			return "", "", fmt.Errorf("暂时无法确认凭证（%w）", err)
		}
		return "", "", fmt.Errorf("凭证已失效: %w", err)
	}

	// ② 用 token 自带的上下文声明平台。token 是哪个应用签的，就用哪个 game id，
	//    否则 uni_sauth 会拒（跨应用签发）。
	_, ownShort := ticketContextForToken(acc.MpayToken)

	custom := map[string]any{
		"login_channel":      "netease",
		"app_channel":        "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd",
		"platform":           platform,
		"source_platform":    platform,
		"sdk_version":        "4.21.0",
		"deviceid":           acc.DeviceID,
		"username":           acc.Name,
	}
	sauth := netsec.NewSAUTH(acc.UserID, acc.MpayToken, ownShort, custom, dev)
	authResp, err := netsec.PostSigned(sauth, ownShort)
	if err != nil {
		return "", "", fmt.Errorf("声明%s平台失败: %w", platformLabel(platform), err)
	}
	if code := toStr(authResp["code"]); code != "200" {
		return "", "", fmt.Errorf("声明%s平台被拒（code=%s）", platformLabel(platform), code)
	}
	logf("%s：已按%s平台取得游戏授权", displayName(acc), platformLabel(platform))

	// ③ 组确认包。底层保留原样 SAUTH（会被写进 extra_unisdk_data），
	//    外层两处按实测修正：原生 token + token 配套设备号。
	lp, err := netsec.NewLoginPayloadFromSAUTH(authResp, sauth, acc.MpayToken, "0",
		strconv.FormatInt(time.Now().Unix(), 10), dev, netsec.NeteaseChannelInfo())
	if err != nil {
		return "", "", fmt.Errorf("组装授权包失败: %w", err)
	}
	lp.Body["token"] = acc.MpayToken    // 原生文本，不能 base64
	lp.Body["device_id"] = acc.DeviceID // 必须与 token 配套

	c := mpay.New()
	c.AppChannel = "netease"

	// ④ 建码用 a50 平台设备（托管码链路），换票也用它；与 token 设备无关。
	platformDev := acc.DeviceID
	if d, derr := ensureA50DeviceID(); derr == nil {
		platformDev = d
	} else {
		logf("%s：a50 设备登记失败，沿用账号设备号: %v", displayName(acc), derr)
	}

	uuid, _, err := c.CreateLoginHostedFull(platformDev, profile.HostedGameID,
		profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
	if err != nil {
		return "", "", fmt.Errorf("建码失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	code, err := c.CompleteHostedQRCode(ctx, uuid, profile.HostedGameID, lp.Body)
	if err != nil {
		return "", "", fmt.Errorf("确认授权失败: %w", err)
	}

	// ⑤ 匿名换票：不带 sid，所以不会写/顶掉任何寄宿槽。
	res, err := launcher.ExchangeTicket(code, platformDev)
	if err != nil || res == nil || res.Ticket == "" {
		return "", "", fmt.Errorf("换取游戏票失败: %v", err)
	}
	logf("%s：渠道签发完成（%s，票 %d 字符）", displayName(acc), platformLabel(platform), len(res.Ticket))
	return res.Ticket, "netease", nil
}

// channelMint 是 mintTicket 的渠道分支入口：能走就签，不能走返回 ok=false
// 让调用方继续用原有逻辑。
func channelMint(acc Account) (ticket string, ch string, ok bool) {
	if platformCodeOf(acc.Region) == "" || acc.Channel != "netease" {
		return "", "", false
	}
	if reason := regionBlockReason(acc); reason != "" {
		// 凭证类型不对（典型：早期用平台身份登录留下的 a50 凭证）。
		// 存过密码的号直接自己修：无头重登一次就换成游戏身份凭证，
		// 用户不需要点任何东西。没密码的只能提示他手动刷新。
		if fresh, fixed := refreshGameCred(acc); fixed {
			acc = fresh
		} else {
			// 选了渠道却用不了：必须留痕，并在界面上给提示，
			// 否则用户只会看到“又弹选区了”却不知道为什么。
			logf("%s：所选渠道暂时用不了（%s）", displayName(acc), reason)
			setAuthHint("region_blocked", "所选渠道还没生效",
				fmt.Sprintf("%s：%s", displayName(acc), reason), acc.Username, acc.UserID)
			return "", "", false
		}
	}
	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		logf("%s：渠道签发缺少设备信息（%v），回退原有签票", displayName(acc), err)
		return "", "", false
	}
	t, c, err := mintTicketByPlatform(acc, dev)
	if err != nil {
		logf("%s：渠道签发失败（%v），回退原有签票", displayName(acc), err)
		return "", "", false
	}
	setAccountCredState(acc.UserID, "ok", "")
	return t, c, true
}

// refreshGameCred 把"凭证类型不支持选区"的账号就地修好。
//
// 只有存过账号密码的号能做到（无头重登，不打扰用户）。修完再判一次，
// 还是不行就返回 false，让调用方按原逻辑提示手动刷新。
func refreshGameCred(acc Account) (Account, bool) {
	// 路径一：存过账号密码 —— 无头重登，拿到的直接就是游戏身份凭证（最干净）
	if hasSavedPassword(acc) {
		if _, err := autoRelogin(acc); err != nil {
			logf("%s：自动重登换凭证失败（%v）", displayName(acc), err)
		} else if fresh, ok := findAccountByUserID(acc.UserID); ok && regionBlockReason(fresh) == "" {
			logf("%s：原凭证类型不对，已自动重登换成游戏身份凭证", displayName(acc))
			return fresh, true
		}
	}

	// 没有密码就只能重新登录。
	//
	// 曾经在这里加过一条"平台凭证签票 + 游戏上下文兑换"的路（换出来 g_i=ma75），
	// 当时只验到 uni_sauth 200 就以为通了。2026-11-09 补验 confirm 才发现：
	// 兑换出来的是 sa=3 会话级凭证，confirm_login 一律 403/1306，签不出票。
	// 所以那条路对"免选区"没有价值，已移除；别再重走。
	return acc, false
}

// isPlatformToken 判断这份 token 是不是平台应用（a50）签发的。
func isPlatformToken(token string) bool {
	gi := tokenGI(token)
	return gi != "" && strings.HasPrefix(profile.HostedGameID, gi)
}

// convertPlatformCred 用平台身份凭证换一份游戏身份凭证（**仅诊断，正式流程不再调用**）。
//
// 换得出来（g_i=ma75），但它一定是 sa=3 会话级：uni_sauth 能过，confirm_login 会
// 403/1306，签不出票。2026-11-09 两次对照确认限制在等级、不在来源，所以这条路
// 不能用来免选区，只能用来观察换票链。留着是因为排查"平台账号为什么能进游戏"时有用。
//
//	create_ticket(平台上下文) → login/ticket(游戏上下文) → g_i=游戏身份的 token
func convertPlatformCred(acc Account) (Account, bool) {
	longID, shortID := ticketContextForToken(acc.MpayToken)
	c := mpay.New()
	ticket, _, err := c.CreateTicketFor(acc.MpayToken, acc.UserID, acc.DeviceID, longID, shortID)
	if err != nil {
		logf("%s：平台凭证签票失败（%v），只能重新登录", displayName(acc), err)
		return acc, false
	}
	res, err := c.LoginTicket(ticket, acc.DeviceID, profile.LongGameID, profile.ShortGameID)
	if err != nil {
		logf("%s：平台票换游戏凭证失败（%v），只能重新登录", displayName(acc), err)
		return acc, false
	}
	user, _ := res["user"].(map[string]any)
	tok, _ := user["token"].(string)
	if tok == "" || isPlatformToken(tok) {
		logf("%s：换出来的还不是游戏身份凭证，放弃", displayName(acc))
		return acc, false
	}
	// 原来的平台凭证留着：它能换 sid、当宿主，别丢
	acc.A50Token = acc.MpayToken
	if acc.A50DeviceID == "" {
		acc.A50DeviceID = acc.DeviceID
	}
	acc.MpayToken = tok
	acc.CredState = "ok"
	acc.LastError = ""
	acc.LastCheck = time.Now().Unix()
	acc.UpdatedAt = time.Now().Unix()
	if err := saveAccount(acc); err != nil {
		logf("%s：换到游戏凭证但保存失败（%v）", displayName(acc), err)
		return acc, false
	}
	logf("%s：用平台凭证换出游戏身份凭证（%d 字符），可以选区了", displayName(acc), len(tok))
	return acc, true
}
