// 临时探针：验证「网页扫码拿到的 ieu_member_sid（pt109265…）能不能换出 SDK 的 sid」。
//
// 背景：扫码登录后 .9game.cn 下出现
//
//	ieu_member_sid = pt109265…
//	ng_session_id  = pt109265…（同一个值）
//
// 而短信登录第一次响应里的 serviceTicket 也是 pt109265… 开头 —— 两者同源。
// 所以先试「拿它当 serviceTicket 走登录」，再试几个可能的服务名。
//
// 用法：go run ./cmd/ucticket <pt109265…>
package main

import (
	"fmt"
	"os"

	"skylogin/internal/ucsdk"
)

type smsLogin struct {
	Mobile        string `json:"mobile"`
	CountryCode   string `json:"countryCode"`
	SmsCode       string `json:"smsCode"`
	LoginType     int    `json:"loginType"`
	ServiceTicket string `json:"serviceTicket,omitempty"`
	AccountID     string `json:"accountId,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: ucticket <pt109265…>")
		os.Exit(1)
	}
	ticket := os.Args[1]
	c := ucsdk.New(ucsdk.SkyGame())

	show := func(title, svc string, data any) {
		raw, err := c.Call(ucsdk.HostAccount, svc, data)
		fmt.Printf("\n--- %s (%s)\n", title, svc)
		fmt.Printf("    err  : %v\n", err)
		fmt.Printf("    code : %d\n", ucsdk.Code(err))
		if len(raw) > 0 {
			s := string(raw)
			if len(s) > 900 {
				s = s[:900] + "…"
			}
			fmt.Printf("    data : %s\n", s)
		}
	}

	// 1) 拿 ticket 当 serviceTicket，走验证码登录（不带验证码）
	show("验证码登录 + serviceTicket", ucsdk.ServiceLoginBySms, smsLogin{
		CountryCode: "86", LoginType: 1, ServiceTicket: ticket,
	})

	// 2) 候选：直接用 serviceTicket 登录
	for _, svc := range []string{
		"unifiedAccount.loginByServiceTicket",
		"unifiedAccount.loginByTicket",
		"unifiedAccount.loginByIeuSid",
		"unifiedAccount.loginByNgSession",
		"unifiedAccount.loginBySessionTicket",
	} {
		show("候选服务名", svc, map[string]any{"serviceTicket": ticket})
	}
}
