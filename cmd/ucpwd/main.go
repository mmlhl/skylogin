package main

// 临时探针：验证「手机号 + 密码」登录能否走通。
//
// 账号密码从环境变量读（UC_MOBILE / UC_PASSWORD），不写在代码里。
// 跑通后即可删除，正式入口在渠道层。

import (
	"errors"
	"fmt"
	"os"

	"skylogin/internal/app"
	"skylogin/internal/channel"
	"skylogin/internal/ucsdk"
)

func main() {
	mobile := os.Getenv("UC_MOBILE")
	password := os.Getenv("UC_PASSWORD")
	if mobile == "" || password == "" {
		fmt.Println("需要环境变量 UC_MOBILE 与 UC_PASSWORD")
		return
	}

	c := ucsdk.New(ucsdk.SkyGame())
	c.Verbose = true

	s, accounts, err := c.LoginByPassword(mobile, password, "86", "", "")

	// 214：该手机号下有游戏账号列表，必须带 serviceTicket + accountId 重发
	if errors.Is(err, ucsdk.ErrNeedAccountChoice) && len(accounts) > 0 {
		fmt.Println("该号下的游戏账号：")
		for _, a := range accounts {
			fmt.Printf("  - accountId=%s  备注=%s\n", a.AccountID, a.Remark)
		}
		ticket := s.ServiceTicket
		pick := accounts[0]
		fmt.Printf("用 %s（%s）重登…\n", pick.Remark, pick.AccountID)
		s, accounts, err = c.LoginByPassword(mobile, password, "86", ticket, pick.AccountID)
	}

	if err != nil {
		if errors.Is(err, ucsdk.ErrNeedAccountChoice) {
			fmt.Println("需要选择游戏账号：")
			for _, a := range accounts {
				fmt.Printf("  - accountId=%s  备注=%s\n", a.AccountID, a.Remark)
			}
			fmt.Println("  serviceTicket =", s.ServiceTicket)
			return
		}
		fmt.Println("登录失败:", err)
		if code := ucsdk.Code(err); code != 0 {
			fmt.Printf("业务码 = %d\n", code)
		}
		return
	}
	if s == nil {
		fmt.Println("登录返回为空")
		return
	}
	fmt.Println("登录成功")
	fmt.Println("  sid        =", s.SID)
	fmt.Println("  accountId  =", s.AccountID)
	fmt.Println("  备注       =", s.Account.Remark)
	fmt.Println("  refresh    =", s.RefreshToken)

	// 立即用这个 sid 调一个需要登录态的接口：
	// 能拿到正常业务响应就说明 sid 本身有效，问题在 uni_sauth 的参数上。
	fmt.Println()
	raw, err := c.Call(ucsdk.HostAccount, ucsdk.ServiceUserInfo, map[string]any{"sid": s.SID})
	if err != nil {
		fmt.Println("getUserSimpleInfo(sid) 失败:", err)
		if code := ucsdk.Code(err); code != 0 {
			fmt.Printf("业务码 = %d\n", code)
		}
	} else {
		s2 := string(raw)
		if len(s2) > 300 {
			s2 = s2[:300] + "…"
		}
		fmt.Println("getUserSimpleInfo(sid) 返回:", s2)
	}

	// 再验一步：这个 sid 能不能换到光遇登录态（uni_sauth）。
	// 这一步失败就说明账号存了也启动不了，之前就是这里静默通过了。
	fmt.Println()
	if err := app.VerifyChannelCredential(channel.KeyUC, "0", s.SID); err != nil {
		fmt.Println("uni_sauth 校验失败:", err)
	} else {
		fmt.Println("uni_sauth 校验通过")
	}
}
