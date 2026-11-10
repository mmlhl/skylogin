package app

// 手机号验证码登录。
//
// 为什么这条路比网页登录好：
//   MPay 的手机号登录是一串普通 HTTP 接口，不需要浏览器、不需要可信点击，
//   用户只要输入手机号和收到的 6 位验证码：
//
//     get_sms(mobile)          → 网易发短信
//     verify_sms(mobile, code) → 返回 16 位票据（AAAA…）
//     mobile/finish(票据)      → 返回 user.token（1-eyJ…）和 user id
//
//   票据换登录态走 finish 而不是 login/ticket（2026-10-29 抓包结论）：
//   finish 换出的是账号级 token（s=yd_…，无 lt/od），发烧平台存的就是
//   这种，长期有效；login/ticket 换出的是 MPay 会话（带 lt/od），会过期。
//   finish 失败时退回 login/ticket 老路，保证登录永远可用。
//
// 反过来说，网页登录那条路（fever.163.com + H5 SDK）只有在需要账号密码时才用得上。

import (
	"fmt"
	"os"
	"strings"
	"time"

	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

// SendSMS 发验证码。
func SendSMS(mobile string) error {
	mobile = strings.TrimSpace(mobile)
	if len(mobile) != 11 {
		return fmt.Errorf("手机号要 11 位")
	}
	dev, derr := ensureDeviceID()
	if derr != nil {
		return derr
	}
	if _, err := smsClient().SendSMS(mobile, dev); err != nil {
		return fmt.Errorf("发送验证码失败: %w", err)
	}
	logf("已向 %s 发送登录验证码", maskMobile(mobile))
	return nil
}

// smsClient 返回短信登录用的客户端。
//
// **默认用游戏上下文（ma75）**：2026-11-08 实测，发码、验码、mobile/finish
// 在 ma75 下全部可用，换出的是账号级凭证（sa=-99）且 g_i=ma75——这种凭证
// 才能按渠道签发（见 channelplatform.go）。
//
// 之前默认走 a50（发烧平台上下文），换出的凭证 g_i=a50，走渠道签发会被
// 501 拒。想看老行为时设 SKYLOGIN_SMS_GAME=a50。
func smsClient() *mpay.Client {
	c := mpay.New()
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SKYLOGIN_SMS_GAME")), "a50") {
		logf("[调试] 短信登录改用平台上下文（a50）")
		return c
	}
	c.SMSGameID = profile.LongGameID
	return c
}

// CompleteSMSLogin 校验验证码，换 token 并保存账号。
func CompleteSMSLogin(mobile, code, targetUserID string) error {
	mobile = strings.TrimSpace(mobile)
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return fmt.Errorf("验证码是 6 位数字")
	}

	dev, derr := ensureA50DeviceID()
	if derr != nil {
		return derr
	}
	ticket, _, err := smsClient().VerifySMS(mobile, code, dev)
	if err != nil {
		return fmt.Errorf("验证码校验失败: %w", err)
	}
	logf("验证码校验通过，拿到票据（%s）", maskTicket(ticket))
	c := smsClient()
	// 票据换登录态：优先 mobile/finish（账号级 token，长期有效），
	// 失败退回 login/ticket（会话级，老行为兜底）。见 exchangeSMSLoginTicket。
	out, err := exchangeSMSLoginTicket(c, ticket, dev)
	if err != nil {
		return fmt.Errorf("用票据换登录态失败: %w", err)
	}
	user, _ := out["user"].(map[string]any)
	token, _ := user["token"].(string)
	userID, _ := user["id"].(string)
	// 展示名优先用脱敏的 client_username（finish 的 display_username 是完整手机号）
	display, _ := user["client_username"].(string)
	if display == "" {
		display, _ = user["display_username"].(string)
	}
	if token == "" || userID == "" {
		return fmt.Errorf("登录成功但没拿到凭证，请重试")
	}

	// 防串号：刷新某个账号时，登录的必须是同一个账号
	if targetUserID != "" && targetUserID != userID {
		return fmt.Errorf("这次登录的账号（%s）不是要刷新的那个，已取消保存", userID)
	}

	acc := Account{
		Name:        display,
		Username:    display,
		Channel:     "netease",
		UserID:      userID,
		PlayerID:    userID,
		MpayToken:   token,
		DeviceID:    dev,
		Source:      "sms",
		LoginMethod: "sms",
		CredState:   "ok",
		LastCheck:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	if acc.Name == "" {
		acc.Name = maskMobile(mobile)
	}
	// 已有同 user id 的账号：保留用户起的别名和等级
	if old, ok := findAccountByUserID(userID); ok {
		acc.Name = old.Name
		acc.NickName = old.NickName
		acc.PlayerLevel = old.PlayerLevel
	}

	if err := saveAccount(acc); err != nil {
		return fmt.Errorf("保存账号失败: %w", err)
	}
	_ = setCurrent(acc.PlayerID)
	clearAuthHintForAccount(acc)
	logf("手机号登录成功并已保存: %s 凭证等级: %s", acc.Name, tokenGrade(token))
	return nil
}

// maskMobile 138****1234 这种形式，日志里不留完整号码。
func maskMobile(m string) string {
	if len(m) < 7 {
		return "***"
	}
	return m[:3] + "****" + m[len(m)-4:]
}

// maskTicket 票据只留前 4 位，日志里够定位就行。
func maskTicket(t string) string {
	if len(t) <= 4 {
		return "****"
	}
	return t[:4] + "****"
}

// 说明：验证码换到的票据是一次性的，校验完立刻换 token，
// 所以这里不做票据缓存——失败就让用户重新发码，避免拿旧票重试。
