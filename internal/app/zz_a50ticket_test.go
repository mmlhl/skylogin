package app

// 一次性探针：验证「平台身份（a50）的票，用游戏身份（ma75）的上下文兑换」
// 能不能换出一份可用于声明渠道的游戏凭证。
//
// 依据：抓包 sessionA 14:47:18，官方平台给游戏的票，游戏用 game_id=…-ma75
// 调 users/login/ticket，服务端回的就是 g_i=ma75 / sa=3 的 token。
//
// 开关：ZZ_A50_SLOTS=<a50 无头登录落盘的槽位 json>

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/device"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

func TestZZA50ToGame(t *testing.T) {
	slotsPath := os.Getenv("ZZ_A50_SLOTS")
	if slotsPath == "" {
		t.Skip("未设置 ZZ_A50_SLOTS")
	}
	b, err := os.ReadFile(slotsPath)
	if err != nil {
		t.Fatalf("读槽位失败: %v", err)
	}
	var slots []string
	if err := json.Unmarshal(b, &slots); err != nil || len(slots) < 3 {
		t.Fatalf("槽位解析失败: %v", err)
	}
	uid, dev, token := slots[0], slots[1], slots[2]
	t.Logf("平台凭证: g_i=%s sa=%v t=%v uid=%s dev=%s",
		tokenGI(token), tokenSaValue(token), tokenFields(token)["t"], uid, dev)

	c := mpay.New()

	// ① 用平台凭证在平台上下文签一张票（平台自己启动游戏就是这么做的）
	ticket, out, err := c.CreateTicketFor(token, uid, dev, profile.HostedGameID, "a50")
	if err != nil {
		t.Fatalf("平台凭证签票失败: %v  响应=%v", err, out)
	}
	t.Logf("① 平台上下文签票成功：%d 字符", len(ticket))

	// ② 用这张票在**游戏上下文**兑换 —— 身份是否变成 ma75？
	res, err := c.LoginTicket(ticket, dev, profile.LongGameID, profile.ShortGameID)
	if err != nil {
		t.Fatalf("游戏上下文兑换失败: %v", err)
	}
	user, _ := res["user"].(map[string]any)
	tok, _ := user["token"].(string)
	if tok == "" {
		t.Fatalf("兑换没有返回 token，响应=%v", res)
	}
	f := tokenFields(tok)
	t.Logf("② 兑换出的 token：g_i=%s sa=%v t=%v lt=%v odi=%v len=%d",
		tokenGI(tok), tokenSaValue(tok), f["t"], f["lt"], f["odi"], len(tok))
	if _, short := ticketContextForToken(tok); short != profile.ShortGameID {
		t.Fatalf("兑换出的不是游戏身份（g_i=%s），这条路不成立", tokenGI(tok))
	}

	// ③ 拿它声明渠道
	devInfo, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatalf("设备失败: %v", err)
	}
	platform := os.Getenv("ZZ_PLATFORM")
	if platform == "" {
		platform = "ad"
	}
	custom := map[string]any{
		"login_channel":      "netease",
		"app_channel":        "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd",
		"platform":           platform,
		"source_platform":    platform,
		"sdk_version":        "4.21.0",
		"deviceid":           dev,
		"username":           uid,
	}
	sauth := netsec.NewSAUTH(uid, tok, profile.ShortGameID, custom, devInfo)
	r, err := netsec.PostSigned(sauth, profile.ShortGameID)
	if err != nil {
		t.Fatalf("uni_sauth 请求失败: %v", err)
	}
	t.Logf("③ uni_sauth(platform=%s) → code=%v", platform, r["code"])
	if toStr(r["code"]) != "200" {
		t.Fatalf("声明渠道被拒：code=%v", r["code"])
	}
}
