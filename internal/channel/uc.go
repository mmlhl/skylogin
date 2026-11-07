package channel

// 九游（UC）：手机号 + 密码，纯 Go 协议直连，不开窗。
//
// 协议细节见 .ai/10。这里只做三件事：
//
//	1. 问用户要手机号和密码
//	2. 调 ucsdk 登录（号下有多个游戏账号时再问一次选哪个）
//	3. 把 sid 包成 Cred 交出去
//
// 落库、uni_sauth 校验、续期都由 app 层统一做，这里不碰。
//
// 安全：密码只在本函数栈上停留，不写日志、不进 Cred、不落库。
// ucsdk.Client 默认 Verbose=false，也不会打印明文。

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	c := ucsdk.New(ucsdk.SkyGame())
	s, accounts, err := c.LoginByPassword(mobile, password, "86", "", "")

	// 214：这个手机号下有游戏账号列表，必须带 serviceTicket + accountId 重发
	if errors.Is(err, ucsdk.ErrNeedAccountChoice) {
		ticket := ""
		if s != nil {
			ticket = s.ServiceTicket
		}
		opts := make([]Option, 0, len(accounts))
		for _, a := range accounts {
			label := a.Remark
			if label == "" {
				label = a.AccountID
			}
			opts = append(opts, Option{Value: a.AccountID, Label: label})
		}
		pick, aerr := p.Ask(ctx, Slice{
			Kind:    "choice",
			Title:   "这个号下有多个游戏账号，选一个",
			Options: opts,
		})
		if aerr != nil {
			return Cred{}, aerr
		}
		accountID := pick["value"]
		if accountID == "" {
			return Cred{}, fmt.Errorf("没有选择游戏账号")
		}
		p.Phase("waiting", "正在登录所选账号…")
		s, _, err = c.LoginByPassword(mobile, password, "86", ticket, accountID)
	}
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
		Method:   KindPassword,
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
