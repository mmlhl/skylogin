package app

// 手机号验证码登录。
//
// 为什么这条路比网页登录好：
//   MPay 的手机号登录是一串普通 HTTP 接口，不需要浏览器、不需要可信点击，
//   用户只要输入手机号和收到的 6 位验证码：
//
//     get_sms(mobile)          → 网易发短信
//     verify_sms(mobile, code) → 返回 16 位票据（AAAA…）
//     login/ticket(票据)       → 返回 user.token（1-eyJ…）和 user id
//
//   拿到的 token 和扫码登录同源，存下来就能长期免登录。
//
// 反过来说，网页登录那条路（fever.163.com + H5 SDK）只有在需要账号密码时才用得上。

import (
	"fmt"
	"strings"
	"time"

	"skylogin/internal/mpay"
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
	if _, err := mpay.New().SendSMS(mobile, dev); err != nil {
		return fmt.Errorf("发送验证码失败: %w", err)
	}
	logf("已向 %s 发送登录验证码", maskMobile(mobile))
	return nil
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
	ticket, _, err := mpay.New().VerifySMS(mobile, code, dev)
	if err != nil {
		return fmt.Errorf("验证码校验失败: %w", err)
	}
	logf("验证码校验通过，拿到票据（%s）", maskTicket(ticket))
	c := mpay.New()
	// a50（发烧平台）上下文兑换：和验证码 / 建码保持同一上下文，
	// 换出来的 token 是 a50 原生的，签票、设备绑定都与密码登录一致。
	out, err := loginTicketAuto(c, ticket, dev)
	if err != nil {
		return fmt.Errorf("用票据换登录态失败: %w", err)
	}
	user, _ := out["user"].(map[string]any)
	token, _ := user["token"].(string)
	userID, _ := user["id"].(string)
	display, _ := user["display_username"].(string)
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
	clearAuthHint()
	logf("手机号登录成功并已保存: %s", acc.Name)
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
