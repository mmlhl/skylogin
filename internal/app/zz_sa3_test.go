package app

// 一次性探针：验证「平台登录在游戏侧换出来的 ma75 会话来凭证」能否直接声明渠道。
//
// 背景（2026-11-08 抓包 sessionA 14:47:18）：官方平台启动游戏时，游戏拿平台给的
// 票调 /mpay/api/users/login/ticket，服务端回的 user.token 里 g_i=ma75、sa=3。
// 如果这种 sa=3 的 ma75 token 也能过 uni_sauth，那「平台登录 → 换票 → 拿 ma75
// token → 声明渠道」就是完整的免选区链路，完全不需要 a50 的签名密钥。
//
// 开关：ZZ_SA3=1，ZZ_ACCOUNT=<账号名>，ZZ_PLATFORM=ad|ios（默认 ad）

import (
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/device"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
)

func TestZZSa3Sauth(t *testing.T) {
	if os.Getenv("ZZ_SA3") == "" {
		t.Skip("未设置 ZZ_SA3")
	}
	name := os.Getenv("ZZ_ACCOUNT")
	if name == "" {
		t.Fatal("缺少 ZZ_ACCOUNT")
	}
	platform := os.Getenv("ZZ_PLATFORM")
	if platform == "" {
		platform = "ad"
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatalf("读账号失败: %v", err)
	}
	acc, ok := findAccount(list, name)
	if !ok {
		t.Fatalf("没找到账号 %s", name)
	}
	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatalf("设备失败: %v", err)
	}
	longID, shortID := ticketContextForToken(acc.MpayToken)
	t.Logf("原凭证: g_i=%s sa=%v t=%v len=%d", tokenGI(acc.MpayToken),
		tokenSaValue(acc.MpayToken), tokenFields(acc.MpayToken)["t"], len(acc.MpayToken))

	c := mpay.New()
	ticket, _, err := c.CreateTicketFor(acc.MpayToken, acc.UserID, acc.DeviceID, longID, shortID)
	if err != nil {
		t.Fatalf("签票失败: %v", err)
	}
	t.Logf("票: %d 字符", len(ticket))

	res, err := c.LoginTicket(ticket, acc.DeviceID, longID, shortID)
	if err != nil {
		t.Fatalf("login/ticket 失败: %v", err)
	}
	user, _ := res["user"].(map[string]any)
	tok, _ := user["token"].(string)
	if tok == "" {
		t.Fatalf("login/ticket 没返回 token，响应=%v", res)
	}
	f := tokenFields(tok)
	t.Logf("换出的 token: g_i=%s sa=%v t=%v lt=%v odi=%v len=%d",
		tokenGI(tok), tokenSaValue(tok), f["t"], f["lt"], f["odi"], len(tok))

	// 用这份「会话级」token 直接声明渠道，看服务端是否认
	custom := map[string]any{
		"login_channel":      "netease",
		"app_channel":        "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd",
		"platform":           platform,
		"source_platform":    platform,
		"sdk_version":        "4.21.0",
		"deviceid":           acc.DeviceID,
		"username":           acc.Name,
	}
	sauth := netsec.NewSAUTH(acc.UserID, tok, shortID, custom, dev)
	r, err := netsec.PostSigned(sauth, shortID)
	if err != nil {
		t.Fatalf("uni_sauth 请求失败: %v", err)
	}
	t.Logf("uni_sauth(platform=%s) → code=%v（%v）", platform, r["code"], r["msg"])
}
