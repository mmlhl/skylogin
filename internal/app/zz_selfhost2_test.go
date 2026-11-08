package app

// 自寄宿探测第二阶段（默认跳过）：
//
//	REMEMBER_SELFHOST2=1 go test ./internal/app/ -run TestZZSelfHost2 -v
//
// 目标：让密码号 js2fv5 自己完成"寄宿"——不需要手机扫码。
//
// 链路（对照 sessionC 平台扫码登录的抓包）：
//  1. 用 19943133859 的会话级凭证换平台 sid（js2fv5 自己的换不了，实测）
//  2. 用平台形态建一张游戏码（qrcode_channel_type=2 + is_remember=2）
//  3. 用 js2fv5 自己的长期凭证走完整网易 uni_sauth（声明 platform=ad——
//     这就是"选择渠道"的服务端落点），组标准登录包
//  4. ConfirmForm 确认这张码（自己扫自己）
//  5. code + sid 调 third_party_account/ticket → js2fv5 寄宿进 199 的槽
//
// 成功后 js2fv5 的启动将走槽位票 → 不弹渠道选择（dd 已实测）。

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

func TestZZSelfHost2(t *testing.T) {
	if os.Getenv("REMEMBER_SELFHOST2") == "" {
		t.Skip("没给 REMEMBER_SELFHOST2，跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	acc, found := findAccount(list, "aebfx7kv2q5vpldc") // js2fv5：要寄宿的号
	if !found || acc.MpayToken == "" {
		t.Fatal("找不到 js2fv5 或凭证为空")
	}
	hostAcc, found := findAccount(list, "aebgerymcruc6zqx") // 199：用它的会话换 sid
	if !found || hostAcc.MpayToken == "" {
		t.Fatal("找不到 19943133859 或凭证为空")
	}

	// 第 1 步：sid —— 密码号（sa=-9，ma75 签发）直接换会 1000。
	// 服务端校验 token 的签发身份（g_i）必须是 a50（199 实测：g_i=a50、t=7 能换）。
	// 所以先用 a50 上下文签票 + login/ticket，换出一张 a50 签发的会话 token。
	c0 := mpay.New()
	sessToken := acc.MpayToken
	{
		a50Dev := acc.DeviceID
		if d, derr := ensureA50DeviceID(); derr == nil {
			a50Dev = d
		}
		tk, _, terr := c0.CreateTicketFor(acc.MpayToken, acc.UserID, a50Dev, a50Long, a50Short)
		if terr != nil {
			t.Fatalf("a50 上下文签票失败: %v", terr)
		}
		res, lerr2 := c0.LoginTicket(tk, a50Dev, a50Long, a50Short)
		if lerr2 != nil {
			t.Fatalf("a50 票换 token 失败: %v", lerr2)
		}
		user, _ := res["user"].(map[string]any)
		tok, _ := user["token"].(string)
		if tok == "" {
			t.Fatal("login/ticket 没返回 token")
		}
		sessToken = tok
		fm := tokenFields(sessToken)
		t.Logf("①a a50 会话 token 已取得: sa=%v t=%v g_i=%v", fm["sa"], fm["t"], fm["g_i"])
	}
	sid, lerr := launcher.Login(acc.UserID, sessToken, acc.DeviceID)
	if lerr != nil || sid == "" {
		t.Fatalf("①b 换 sid 失败: %v", lerr)
	}
	t.Logf("① sid 已取得（%d 字符，自寄宿）", len(sid))

	// 第 2 步：平台形态建码
	uuid, raw, err := c0.CreateLoginHostedFull(acc.DeviceID, profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
	if err != nil {
		t.Fatalf("平台形态建码失败: %v", err)
	}
	qt, _, _ := mpay.ExtractString(raw, "qrcode_login_type")
	t.Logf("② 平台形态建码成功 uuid=%s qrcode_login_type=%q", uuid, qt)

	// 第 3 步：完整网易 uni_sauth，声明 platform=ad
	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		t.Fatalf("设备标识不可用: %v", derr)
	}
	custom := map[string]any{
		"login_channel":      "netease",
		"app_channel":        "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd",
		"platform":           "ad",
		"source_platform":    "ad",
		"sdk_version":        "4.21.0",
	}
	if and, aerr := device.LoadOrCreateAndroid(filepath.Join(dataDir(), "android.json")); aerr == nil {
		custom["deviceid"] = and.DeviceID
		custom["username"] = acc.Name
	}
	sauth := netsec.NewSAUTH(acc.UserID, acc.MpayToken, profile.ShortGameID, custom, dev)
	authResp, aerr := netsec.PostSigned(sauth, profile.ShortGameID)
	if aerr != nil {
		t.Fatalf("③ uni_sauth 失败: %v", aerr)
	}
	code, status, msg := toStr(authResp["code"]), toStr(authResp["status"]), toStr(authResp["msg"])
	t.Logf("③ uni_sauth: code=%s status=%s msg=%s", code, status, msg)
	if code != "200" {
		return
	}

	// 第 4 步：组标准登录包并确认
	lp, perr := netsec.NewLoginPayloadChan(authResp, acc.MpayToken, acc.MpayToken, acc.UserID, "0", fmt.Sprint(time.Now().Unix()), dev, netsec.NeteaseChannelInfo())
	if perr != nil {
		t.Fatalf("④ 组登录包失败: %v", perr)
	}
	out, cerr := c0.ConfirmForm(lp.Body)
	if cerr != nil {
		t.Fatalf("④ confirm 失败: %v", cerr)
	}
	t.Logf("④ confirm 结果: %.300s", fmt.Sprint(out))

	// 第 5 步：槽位出票（寄宿验证）
	r2, terr2 := launcher.TicketForCode(uuid, sid)
	if terr2 != nil {
		t.Logf("⑤ 槽位出票失败: %v", terr2)
		return
	}
	t.Logf("⑤ ✅ 自寄宿成功！ticket=%d字符 channel=%s（js2fv5 已进 199 的槽，渠道=安卓）", len(r2.Ticket), r2.Channel)
}
