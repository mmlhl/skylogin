package app

// 只声明、不确认的实验：验证 uni_sauth 的 platform 声明是否独立生效。
//
// 背景（2026-11-09 抓包复核）：
//   - 平台扫码流程 create_login(a50) → query → 手机确认 → third_party_account/ticket
//     → login/ticket(ma75) → sa=3，全程没有 scan / confirm_login。
//   - sa=3 的 uni_sauth(platform=ad) 响应与 sa=-9 完全一致：
//     code=200 / status=ok / subcode=0 / first_login_platform=ad。
//     也就是说声明这一步对 sa=3 同样成功，只有把授权交给二维码的 confirm 被 1306 挡住。
//
// 于是要回答的问题是：如果声明本身已经把"这个账号玩 ad"落到服务端，
// 那么不经过 confirm、直接用平台票启动，游戏还会不会弹选区？
//
// 本实验刻意让"声明"和"出票"互相独立：
//   ① 平台登录 → 派生 ma75 sa=3 → 用 sa=3 做 uni_sauth(platform=ad)（只声明）
//   ② 完全不用这份授权，改拿平台原生凭证在 a50 上下文签票
//   ③ IPC 把这张平台票交给游戏
//
// 如果第 ③ 步不弹选区，说明声明独立生效、confirm 不是免选区的必要条件，
// 平台登录也就有了免选区的可能路径；如果仍然弹，说明完整授权链不可省。
//
// 开关：DECLARE_ONLY_DIR=<本地忽略目录>；DECLARE_ONLY_LAUNCH=1 才真的启动游戏。

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

func TestZZDeclareOnlyLaunch(t *testing.T) {
	dir := os.Getenv("DECLARE_ONLY_DIR")
	if dir == "" {
		t.Skip("未开启只声明不确认实验")
	}
	if os.Getenv("URS_USER") == "" || os.Getenv("URS_PASS") == "" {
		t.Fatal("缺少本机登录凭据")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal("建立实验目录失败")
	}
	write := func(name string, v any) {
		t.Helper()
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal("编码记录失败")
		}
		if os.WriteFile(filepath.Join(dir, name+".json"), b, 0o600) != nil {
			t.Fatal("保存记录失败")
		}
	}
	platform := os.Getenv("DECLARE_ONLY_PLATFORM")
	if platform == "" {
		platform = "ad"
	}
	summary := map[string]any{}
	defer func() { write("summary", summary) }()

	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatal("读取客户端配置失败")
	}

	// ① 平台身份登录（拿到账号级 a50 凭证）
	login, err := HeadlessPasswordLogin(os.Getenv("URS_USER"), os.Getenv("URS_PASS"), profile.HostedGameID)
	if err != nil {
		t.Fatal("平台身份登录失败（详情仅留本地）")
	}
	write("a50-login", login)
	summary["a50_gi"] = tokenGI(login.Token)
	summary["a50_sa"] = tokenSaValue(login.Token)
	t.Logf("平台登录：g_i=%s sa=%.0f", tokenGI(login.Token), tokenSaValue(login.Token))

	// ② 派生一份 ma75 会话凭证（与平台扫码登录得到的等级相同）
	derivedTicket, _, err := mpay.New().CreateTicketFor(login.Token, login.UserID, login.DeviceID, profile.HostedGameID, "a50")
	if err != nil {
		t.Fatal("平台凭证签票失败")
	}
	derived, err := mpay.New().LoginTicket(derivedTicket, login.DeviceID, profile.LongGameID, profile.ShortGameID)
	if err != nil {
		t.Fatal("ma75 上下文兑换失败")
	}
	write("derived", derived)
	dUser, _ := derived["user"].(map[string]any)
	dToken, _ := dUser["token"].(string)
	if dToken == "" {
		t.Fatal("兑换没有返回 token")
	}
	summary["derived_gi"] = tokenGI(dToken)
	summary["derived_sa"] = tokenSaValue(dToken)
	t.Logf("派生凭证：g_i=%s sa=%.0f", tokenGI(dToken), tokenSaValue(dToken))

	// ③ 只做声明：uni_sauth(platform=ad)，不建码、不确认
	custom := map[string]any{
		"login_channel":      "netease",
		"app_channel":        "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd",
		"platform":           platform,
		"source_platform":    platform,
		"sdk_version":        "4.21.0",
		"deviceid":           login.DeviceID,
		"username":           os.Getenv("URS_USER"),
	}
	sauth := netsec.NewSAUTH(login.UserID, dToken, profile.ShortGameID, custom, dev)
	auth, err := netsec.PostSigned(sauth, profile.ShortGameID)
	if err != nil {
		t.Fatal("声明请求失败")
	}
	write("declare", map[string]any{"request": sauth, "response": auth})
	summary["declare_code"] = auth["code"]
	summary["declare_status"] = auth["status"]
	summary["declare_first_login_platform"] = auth["first_login_platform"]
	t.Logf("声明 platform=%s：code=%v status=%v first_login_platform=%v",
		platform, auth["code"], auth["status"], auth["first_login_platform"])
	if toStr(auth["code"]) != "200" {
		t.Fatalf("声明被拒，实验前提不成立：code=%v", auth["code"])
	}

	// ④ 出票：完全不用上面的授权，拿平台原生凭证在 a50 上下文签票
	finalTicket, _, err := mpay.New().CreateTicketFor(login.Token, login.UserID, login.DeviceID, profile.HostedGameID, "a50")
	if err != nil {
		t.Fatal("平台上下文签票失败")
	}
	summary["final_ticket_len"] = len(finalTicket)
	t.Logf("平台票已就绪（%d 字符），这张票不携带任何声明结果", len(finalTicket))

	// ⑤ 交给游戏；是否免选区由使用者观察
	if os.Getenv("DECLARE_ONLY_LAUNCH") == "1" {
		launchHeadlessProbeGame(t, "netease", finalTicket)
	} else {
		t.Log("未开启启动开关，实验到此为止")
	}
}
