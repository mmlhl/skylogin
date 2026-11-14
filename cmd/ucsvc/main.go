// 临时探针：枚举九游 SDK 的候选服务名，判断哪些真实存在。
//
// 用法：
//
//	go run ./cmd/ucsvc                # 跑内置候选清单
//	go run ./cmd/ucsvc -svc a.b.c     # 只试一个，并打印完整明文响应
//
// 判据（实测口径）：sdk-account.9game.cn 上「服务名不存在」统一回 code=99
// 「系统繁忙，请稍后再试」；存在的服务会给 code=10/11 等有意义的业务码。
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"skylogin/internal/ucsdk"
)

var candidates = []string{
	// 登录类：换 sid 的可能入口
	"unifiedAccount.loginByToken",
	"unifiedAccount.loginByAccessToken",
	"unifiedAccount.loginByIeuToken",
	"unifiedAccount.loginByPassportToken",
	"unifiedAccount.loginByWebToken",
	"unifiedAccount.loginBySsoCode",
	"unifiedAccount.loginBySsoTicket",
	"unifiedAccount.loginByAuthCode",
	"unifiedAccount.loginByUcToken",
	"unifiedAccount.loginBySessionId",
	"unifiedAccount.loginBySession",
	"unifiedAccount.loginBySilentCodeService",
	// 扫码类
	"unifiedAccount.getQrCode",
	"unifiedAccount.createQrCode",
	"unifiedAccount.qrCodeLogin",
	"unifiedAccount.loginByQrCode",
	"unifiedAccount.loginByScanCode",
	"unifiedAccount.getLoginQrCode",
	"unifiedAccount.queryQrCodeStatus",
	"unifiedAccount.scanQrCode",
	"unifiedAccount.confirmQrCode",
	"unifiedAccount.checkQrCode",
	// sid / token 互换
	"unifiedAccount.getSidByToken",
	"unifiedAccount.getSid",
	"unifiedAccount.exchangeSid",
	"unifiedAccount.exchangeToken",
	"unifiedAccount.getTokenBySid",
	"unifiedAccount.getSessionInfo",
	// 第二批：门户会话 / cookie 形态的换票口
	"unifiedAccount.loginByWebCookie",
	"unifiedAccount.loginByCookie",
	"unifiedAccount.loginByIeuSession",
	"unifiedAccount.loginByIeuCookie",
	"unifiedAccount.loginByPortalToken",
	"unifiedAccount.loginByPassportSession",
	"unifiedAccount.loginBySsoSession",
	"unifiedAccount.loginByThirdSession",
	"unifiedAccount.getSidByIeuSid",
	"unifiedAccount.exchangeIeuSid",
	"unifiedAccount.getUcSession",
	"unifiedAccount.loginByUcSession",
	// 其它可能有用的
	"unifiedAccount.getUserSimpleInfo",
	"unifiedAccount.getAccountList",
	"unifiedAccount.logout",
	"ucid.user.getUserInfo",
	"ucid.user.refreshLogin",
}

func main() {
	one := flag.String("svc", "", "只试这一个服务名")
	verbose := flag.Bool("v", false, "打印完整响应")
	flag.Parse()

	c := ucsdk.New(ucsdk.SkyGame())
	c.Verbose = *verbose

	list := candidates
	if *one != "" {
		list = []string{*one}
	}

	fmt.Printf("host=%s game=%d channel=%s\n\n", ucsdk.HostAccount, c.Game.GameID, c.Game.ChannelID)
	for _, svc := range list {
		raw, err := c.Call(ucsdk.HostAccount, svc, struct{}{})
		body := string(raw)
		if len(body) > 220 {
			body = body[:220] + "…"
		}
		code := ucsdk.Code(err)
		verdict := "?"
		switch {
		case code == 99:
			verdict = "不存在"
		case code == 0 && err == nil:
			verdict = "★ 存在且接受空参数"
		case code != 0:
			verdict = "存在（有业务码）"
		case err != nil:
			verdict = "错误: " + err.Error()
		}
		fmt.Printf("%-46s code=%-4d %-22s %s\n", svc, code, verdict, body)
		time.Sleep(400 * time.Millisecond)
	}
	os.Exit(0)
}
