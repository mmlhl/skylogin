// 临时探针：给 unifiedAccount.loginByServiceTicket 试不同的参数组合。
//
// code=10 是「参数不对/未登录」的笼统答复，所以只能靠穷举参数名来撞。
// 用法：go run ./cmd/ucticket2 <ticket> [accountId] [uid] [passportId]
package main

import (
	"fmt"
	"os"

	"skylogin/internal/ucsdk"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: ucticket2 <ticket> [accountId] [uid] [passportId]")
		os.Exit(1)
	}
	ticket := os.Args[1]
	get := func(i int) string {
		if len(os.Args) > i {
			return os.Args[i]
		}
		return ""
	}
	accountID, uid, passportID := get(2), get(3), get(4)

	c := ucsdk.New(ucsdk.SkyGame())
	c.Verbose = false

	cases := []struct {
		name string
		svc  string
		data any
	}{
		{"只有 ticket", "unifiedAccount.loginByServiceTicket", map[string]any{"serviceTicket": ticket}},
		{"ticket + accountId", "unifiedAccount.loginByServiceTicket", map[string]any{"serviceTicket": ticket, "accountId": accountID}},
		{"ticket + loginType", "unifiedAccount.loginByServiceTicket", map[string]any{"serviceTicket": ticket, "loginType": 1}},
		{"ticket + uid", "unifiedAccount.loginByServiceTicket", map[string]any{"serviceTicket": ticket, "uid": uid}},
		{"ticket + passportId", "unifiedAccount.loginByServiceTicket", map[string]any{"serviceTicket": ticket, "passportId": passportID}},
		{"ticket 字段名换成 ticket", "unifiedAccount.loginByServiceTicket", map[string]any{"ticket": ticket}},
		{"ticket 字段名换成 sid", "unifiedAccount.loginByServiceTicket", map[string]any{"sid": ticket}},
		{"ticket + gameId", "unifiedAccount.loginByServiceTicket", map[string]any{"serviceTicket": ticket, "gameId": 918836}},
		{"ticket + accountId + loginType", "unifiedAccount.loginByServiceTicket", map[string]any{"serviceTicket": ticket, "accountId": accountID, "loginType": 1}},
		{"ticket + mobile", "unifiedAccount.loginByServiceTicket", map[string]any{"serviceTicket": ticket, "mobile": "", "countryCode": "86"}},
	}

	for _, cs := range cases {
		raw, err := c.Call(ucsdk.HostAccount, cs.svc, cs.data)
		code := ucsdk.Code(err)
		body := string(raw)
		if len(body) > 400 {
			body = body[:400] + "…"
		}
		mark := "  "
		if code != 10 && code != 99 {
			mark = "★ "
		}
		fmt.Printf("%s%-28s code=%-4d %s\n", mark, cs.name, code, body)
		if err != nil && code != 10 && code != 99 {
			fmt.Printf("     err=%v\n", err)
		}
	}
}
