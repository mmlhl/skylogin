package channel

// 九游（UC）：手机号 + 密码、手机号 + 验证码，都是纯 Go 协议直连，不开窗。
//
// 协议细节见 .ai/10。这里只做三件事：
//
//	1. 问用户要手机号（密码方式再要一个密码）
//	2. 调 ucsdk 登录；号下有多个游戏账号时再问一次选哪个
//	3. 把 sid 包成 Cred 交出去
//
// 落库、uni_sauth 校验、续期都由 app 层统一做，这里不碰。
//
// 安全：密码只在函数栈上停留，不进日志、不进 Cred、不落库。
//
// 设备：每个账号第一次登录时随机分配一台真机并落盘（见 device.ucprofile.go），
// 之后一直用它——服务端看到的始终是同一台手机，和真人换机不是一回事。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"skylogin/internal/device"
	"skylogin/internal/ucsdk"
)

// ucPassword 九游手机号 + 密码登录。
type ucPassword struct{}

func (ucPassword) Kind() Kind    { return KindPassword }
func (ucPassword) Label() string { return "手机号 + 密码" }
func (ucPassword) Hint() string  { return "直接用九游账号密码登录，不用开窗" }

func (ucPassword) Available(context.Context) (bool, string) { return true, "" }

func (ucPassword) Login(ctx context.Context, p Progress) (Cred, error) {
	ans, err := p.Ask(ctx, Slice{
		Kind:  "form",
		Title: "九游账号登录",
		Fields: []Field{
			{Name: "mobile", Label: "手机号", Type: "tel", Placeholder: "11 位手机号", MaxLen: 11},
			{Name: "password", Label: "密码", Type: "password", Placeholder: "九游账号密码"},
		},
	})
	if err != nil {
		return Cred{}, err
	}
	mobile := strings.TrimSpace(ans["mobile"])
	password := ans["password"]
	if mobile == "" || password == "" {
		return Cred{}, fmt.Errorf("手机号与密码都不能为空")
	}

	p.Phase("waiting", "正在登录九游…")
	c := ucClientFor(mobile)
	s, accounts, err := c.LoginByPassword(mobile, password, "86", "", "")

	// 214：该手机号下有游戏账号列表，必须带 serviceTicket + accountId 重发
	if errors.Is(err, ucsdk.ErrNeedAccountChoice) {
		ticket := ""
		if s != nil {
			ticket = s.ServiceTicket
		}
		accountID, aerr := ucChooseAccount(ctx, p, accounts)
		if aerr != nil {
			return Cred{}, aerr
		}
		p.Phase("waiting", "正在登录所选账号…")
		s, _, err = c.LoginByPassword(mobile, password, "86", ticket, accountID)
	}
	return ucCredFrom(s, err, KindPassword, mobile)
}

// ucSMS 九游手机号 + 短信验证码登录。
//
// 与密码方式共用同一套收尾（选账号、组 Cred），区别只在拿凭证的两步：
// 先 sendSmsCode，再 loginBySmsCode。
type ucSMS struct{}

func (ucSMS) Kind() Kind    { return KindSMS }
func (ucSMS) Label() string { return "手机号 + 验证码" }
func (ucSMS) Hint() string  { return "发一条短信验证码登录，不用开窗" }

func (ucSMS) Available(context.Context) (bool, string) { return true, "" }

func (ucSMS) Login(ctx context.Context, p Progress) (Cred, error) {
	ans, err := p.Ask(ctx, Slice{
		Kind:  "form",
		Title: "九游验证码登录",
		Fields: []Field{
			{Name: "mobile", Label: "手机号", Type: "tel", Placeholder: "11 位手机号", MaxLen: 11},
		},
	})
	if err != nil {
		return Cred{}, err
	}
	mobile := strings.TrimSpace(ans["mobile"])
	if mobile == "" {
		return Cred{}, fmt.Errorf("手机号不能为空")
	}

	c := ucClientFor(mobile)

	p.Phase("waiting", "正在发送验证码…")
	if err := c.SendSmsCode(mobile, "86"); err != nil {
		return Cred{}, fmt.Errorf("发送验证码失败：%w", err)
	}

	ans, err = p.Ask(ctx, Slice{
		Kind:  "form",
		Title: "输入手机收到的验证码",
		Fields: []Field{
			{Name: "code", Label: "验证码", Type: "text", Placeholder: "短信里的数字", MaxLen: 8},
		},
	})
	if err != nil {
		return Cred{}, err
	}
	code := strings.TrimSpace(ans["code"])
	if code == "" {
		return Cred{}, fmt.Errorf("验证码不能为空")
	}

	p.Phase("waiting", "正在校验验证码…")
	s, accounts, err := c.LoginBySms(mobile, "86", code, "", "")

	if errors.Is(err, ucsdk.ErrNeedAccountChoice) {
		ticket := ""
		if s != nil {
			ticket = s.ServiceTicket
		}
		accountID, aerr := ucChooseAccount(ctx, p, accounts)
		if aerr != nil {
			return Cred{}, aerr
		}
		p.Phase("waiting", "正在登录所选账号…")
		s, _, err = c.LoginBySms(mobile, "86", code, ticket, accountID)
	}
	return ucCredFrom(s, err, KindSMS, mobile)
}

// ucClientFor 取该账号绑定的设备，造一个客户端。
//
// 设备取不到也要能登录（只是这次用随机设备），所以错误不外抛。
func ucClientFor(mobile string) *ucsdk.Client {
	c := ucsdk.New(ucsdk.SkyGame())
	if d, err := device.LoadOrCreateUC(mobile); err == nil && d != nil {
		c.SetDevice(*d)
	}
	return c
}

// ucChooseAccount 让用户从多账号列表里选一个，返回 accountId。
func ucChooseAccount(ctx context.Context, p Progress, accounts []ucsdk.Account) (string, error) {
	if len(accounts) == 0 {
		return "", fmt.Errorf("服务端要求选账号，但没给账号列表")
	}
	opts := make([]Option, 0, len(accounts))
	for _, a := range accounts {
		label := a.Remark
		if label == "" {
			label = a.AccountID
		}
		opts = append(opts, Option{Value: a.AccountID, Label: label})
	}
	pick, err := p.Ask(ctx, Slice{
		Kind:    "choice",
		Title:   "这个号下有多个游戏账号，选一个",
		Options: opts,
	})
	if err != nil {
		return "", err
	}
	accountID := pick["value"]
	if accountID == "" {
		return "", fmt.Errorf("没有选择游戏账号")
	}
	return accountID, nil
}

// ucCredFrom 把登录结果收尾：错误原样抛，成功则包成 Cred。
//
// 密码和验证码两条链路的收尾完全一样，所以合到一处，避免一边改了另一边漏改。
func ucCredFrom(s *ucsdk.Session, err error, method Kind, mobile string) (Cred, error) {
	if err != nil {
		return Cred{}, err
	}
	if s == nil || s.SID == "" {
		return Cred{}, fmt.Errorf("登录成功但没拿到 sid")
	}
	nick := s.Account.Remark
	if nick == "" {
		nick = maskPhone(mobile)
	}
	return Cred{
		// 九游在 uni_sauth 里的 sdkuid 固定是 "0"（见 .ai/10 第 10 节映射表）
		UID:      "0",
		Secret:   s.SID,
		Nickname: nick,
		Method:   method,
		Extra: map[string]string{
			"accountId":    s.AccountID,
			"refreshToken": s.RefreshToken,
		},
	}, nil
}

// maskPhone 把手机号中间四位打码，用作没有昵称时的账号显示名。
func maskPhone(s string) string {
	if len(s) < 7 {
		return s
	}
	return s[:3] + "****" + s[len(s)-4:]
}
