// 决定性实验：把「扫码拿到的门户会话」直接当 serviceTicket 喂给
// unifiedAccount.loginByServiceTicket，看能不能换出 SDK 的 sid。
//
// 依据（2026-11-14 算法助手在光遇九游包里的算法日志，logId=22053）：
// 游戏自己登录时发的请求明文是
//
//	{"service":"unifiedAccount.loginByServiceTicket",
//	 "data":{"clusterCode":"",
//	         "serviceTicket":"pt109265lh003592156lmfmtfhek75fqux7tdx962v",
//	         "silent":false,
//	         "loginType":3,
//	         "accountId":"2883523570000000000154342121",
//	         "refreshToken":"6151a0ec13886346fe52484e5264a717"}}
//
// 注意 serviceTicket 是 pt109265… 前缀 —— 与扫码得到的 ieu_member_sid 同源。
// 之前 cmd/ucticket2 试这个服务只回 code=10，是因为漏了 loginType=3 与
// clusterCode/silent/accountId/refreshToken 这几个伴生字段。
//
// 用法：
//
//	go run ./cmd/ucqr2sdk            # 弹二维码 → 手机确认 → 自动跑参数矩阵
//	go run ./cmd/ucqr2sdk -sid xxx   # 跳过扫码，直接用给定票据
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"skylogin/internal/ucqrcode"
	"skylogin/internal/ucsdk"
)

const svc = "unifiedAccount.loginByServiceTicket"

func main() {
	dir := flag.String("dir", `E:\skytest\shots`, "二维码保存目录")
	sidIn := flag.String("sid", "", "跳过扫码，直接用它当 serviceTicket")
	accountID := flag.String("account", "", "已知 accountId（可选）")
	refresh := flag.String("refresh", "", "已知 refreshToken（可选）")
	fakeApp := flag.Bool("fakeapp", false, "换票时把 clientDevice 伪装成九游 App（ANDROID/JIUYOU_ANDROID_APP）")
	qrApp := flag.String("qrapp", "", "建码时伪装的 appCode（如 JIUYOU_ANDROID_APP）")
	flag.Parse()

	sid := *sidIn
	rt := *refresh
	if sid == "" {
		sid, rt = scan(*dir, rt, *fakeApp, *qrApp)
		if sid == "" {
			os.Exit(1)
		}
	}
	fmt.Printf("\n门户会话 ieu_member_sid = %s\nrefreshToken = %s\n\n", sid, abbrev(rt, 40))

	// 参数矩阵：从「完全按抓包」到「逐步删字段」，看哪几个是必须的
	type cs struct {
		name string
		data map[string]any
	}
	cases := []cs{
		{"照抓包（含 refreshToken）", map[string]any{
			"clusterCode": "", "serviceTicket": sid, "silent": false,
			"loginType": 3, "accountId": *accountID, "refreshToken": rt,
		}},
		{"loginType=3 全套去空", map[string]any{
			"clusterCode": "", "serviceTicket": sid, "silent": false, "loginType": 3,
		}},
		{"loginType=3 只有 ticket", map[string]any{
			"serviceTicket": sid, "loginType": 3,
		}},
		{"只给 ticket（对照，预期 code=10）", map[string]any{
			"serviceTicket": sid,
		}},
		{"loginType=1", map[string]any{
			"serviceTicket": sid, "loginType": 1, "silent": false,
		}},
		{"loginType=2", map[string]any{
			"serviceTicket": sid, "loginType": 2, "silent": false,
		}},
	}

	for _, c := range cases {
		raw, err := run(c.data)
		code := ucsdk.Code(err)
		mark := "  "
		if code != 10 && code != 99 {
			mark = "★ "
		}
		body := string(raw)
		if len(body) > 600 {
			body = body[:600] + "…"
		}
		fmt.Printf("%s%-34s code=%-4d %s\n", mark, c.name, code, body)
		if err != nil {
			fmt.Printf("     err=%v\n", err)
		}
	}
}

// run 每次用全新的 Client，避免上一次的公钥/会话状态污染。
func run(data map[string]any) ([]byte, error) {
	c := ucsdk.New(ucsdk.SkyGame())
	c.Verbose = false
	raw, err := c.Call(ucsdk.HostAccount, svc, data)
	return raw, err
}

// appDevice 是九游 App 在 mtop 请求里上报的 clientDevice 字段，
// 照抄抓包里 scanQRCode/confirmQRLoginAction 的真实值（PJZ110 那台）。
func appDevice() map[string]any {
	return map[string]any{
		"os":            "Android",
		"osVer":         "16",
		"clientAppCode": "JIUYOU_ANDROID_APP",
		"clientType":    "ANDROID",
		"appVer":        "8.5.7.1",
		"pkgName":       "cn.ninegame.gamemanager",
		"clientBizId":   "jiuyou",
		"appKey":        "23067643",
		"brand":         "OnePlus",
		"model":         "PJZ110",
		"umidToken":     "cQQBS7FLPNoWbwKh1DnKELhoNdCC9Vmf",
		"oaid":          "13DE77E2083543938062A294A49EBC8C1def4d861ce501b55a66979d62f10fcf",
		"utdid":         "au9bvoGKwd8DAMyxFwLYkKWD",
		"clientFlag":    1,
	}
}

// scan 走完整扫码链路，返回 ieu_member_sid 与 refreshToken。
func scan(dir, knownRefresh string, fakeApp bool, qrApp string) (string, string) {
	c := ucqrcode.New()
	if qrApp != "" {
		c.AppCodeOverride = qrApp
		c.ClientTypeOverride = "ANDROID"
		fmt.Printf("   建码身份伪装：appCode=%s clientType=ANDROID\n", qrApp)
	}
	if err := c.StartQRCode(); err != nil {
		fmt.Println("建码失败:", err)
		return "", ""
	}
	name := fmt.Sprintf("%s\\uc-qr-%s.png", dir, c.QRToken[len(c.QRToken)-8:])
	if err := os.WriteFile(name, c.QRImage, 0o644); err != nil {
		fmt.Println("写图片失败:", err)
		return "", ""
	}
	fmt.Printf("\n① 二维码 %s\n", name)
	_ = exec.Command(`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`, name).Start()
	fmt.Println("② 请用手机【九游 App】扫码并确认…")

	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		st, detail, err := c.QueryStatusDetail()
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		switch st {
		case 0:
			vcode, _ := detail["vcode"].(string)
			scene, _ := detail["connectScene"].(string)
			if vcode == "" {
				break
			}
			fmt.Printf("   状态 0：vcode=%s… scene=%s\n", vcode[:min(20, len(vcode))], scene)

			// 第一发：不带 loginToken；-fakeapp 时伪装成 App 身份
			var data map[string]any
			var raw json.RawMessage
			var err error
			if fakeApp {
				data, raw, err = c.AutoLoginByCodeDev(vcode, scene, "", appDevice())
			} else {
				data, raw, err = c.AutoLoginByCodeV2(vcode, scene, "")
			}
			if err != nil {
				fmt.Println("   autoLoginByCode 失败:", err)
				return "", ""
			}
			fmt.Println("   原始响应:", string(raw))
			nextURL, _ := data["nextUrl"].(string)
			tk, _ := data["loginToken"].(string)
			fmt.Printf("   第一发：loginToken=%q nextUrl=%q\n", abbrev(tk, 30), abbrev(nextURL, 90))

			// 带 nextUrl 就抓回来看看它要什么（第一次先当调试信息）
			if nextURL != "" {
				if body, e := c.FetchSecurityIframe(nextURL); e == nil {
					fmt.Printf("   安全页 %d 字节，头部：%s\n", len(body), abbrev(string(body), 400))
				} else {
					fmt.Println("   安全页抓取失败:", e)
				}
			}

			if tk != "" {
				_, raw2, err := c.AutoLoginByCodeV2(vcode, scene, tk)
				if err != nil {
					fmt.Println("   带 loginToken 重发失败:", err)
					return "", ""
				}
				fmt.Printf("   第二发：%s\n", string(raw2))
			}

			sid := c.LoginSessionID()
			if sid != "" {
				fmt.Println("   登录成功 ✔")
				if c.SessionInfo != nil {
					for _, ck := range c.SessionInfo.Cookies {
						if ck.KeyName == "ieu_member_appcode" {
							fmt.Printf("   票的 appcode = %s\n", ck.Value)
						}
					}
				}
				rt := knownRefresh
				if c.SessionInfo != nil && c.SessionInfo.RefreshToken != "" {
					rt = c.SessionInfo.RefreshToken
				}
				return sid, rt
			}
			fmt.Println("   没拿到 ieu_member_sid，cookies:", abbrev(fmt.Sprint(c.Cookies()), 300))
			return "", ""
		case 4:
			fmt.Println("   码失效，重来一次")
			return "", ""
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Println("超时，没扫上")
	return "", ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func abbrev(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
