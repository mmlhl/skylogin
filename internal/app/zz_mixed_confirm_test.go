package app

// 混合确认实验：声明用会话凭证，确认换账号级凭证。
//
// 动机（2026-11-09）：
//   - sa=3 能过 uni_sauth（platform 声明 200），但 confirm_login 报 1306「用户登录失效」。
//   - 1306 在同一份实现里也出现在"token 被 base64 包装"时，说明它校验的是
//     "这份 token 是不是一个可用的登录会话"，而不是直接读 sa 数值。
//   - 账号级凭证（sa=-9）的 s 是 yd_ 开头的账号会话；会话级（sa=3）的 s 是 [mp]- 开头的
//     MPay 应用会话。两者形态不同。
//
// 于是试一个此前没做过的组合：
//
//	uni_sauth 的 sessionid / extra_unisdk_data ← ma75 会话凭证（必须，签名应用要匹配）
//	confirm_login 的 token                      ← 平台账号级凭证（账号会话更"可信"）
//
// 之前所有实验都是同一个 token 既做声明又做确认，从未拆开过。
//
// 开关：MIXED_CONFIRM_DIR=<本地忽略目录>；MIXED_CONFIRM_TOKEN=platform|derived 选确认用哪份。

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

func TestZZMixedConfirm(t *testing.T) {
	dir := os.Getenv("MIXED_CONFIRM_DIR")
	if dir == "" {
		t.Skip("未开启混合确认实验")
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
	platform := os.Getenv("MIXED_CONFIRM_PLATFORM")
	if platform == "" {
		platform = "ad"
	}
	summary := map[string]any{}
	defer func() { write("summary", summary) }()

	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatal("读取客户端配置失败")
	}

	// ① 平台登录，拿账号级 a50 凭证
	login, err := HeadlessPasswordLogin(os.Getenv("URS_USER"), os.Getenv("URS_PASS"), profile.HostedGameID)
	if err != nil {
		t.Fatal("平台身份登录失败")
	}
	write("a50-login", login)
	summary["a50_sa"] = tokenSaValue(login.Token)
	t.Logf("平台凭证：g_i=%s sa=%.0f", tokenGI(login.Token), tokenSaValue(login.Token))

	// ② 派生 ma75 会话凭证（声明必须用它：签名应用要匹配 ma75）
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
	summary["derived_sa"] = tokenSaValue(dToken)
	t.Logf("派生凭证：g_i=%s sa=%.0f", tokenGI(dToken), tokenSaValue(dToken))

	// ③ 用派生凭证做声明
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
	t.Logf("声明 platform=%s：code=%v", platform, auth["code"])
	if toStr(auth["code"]) != "200" {
		t.Fatalf("声明未通过，前提不成立：code=%v", auth["code"])
	}

	// ④ 组确认包：授权上下文来自派生凭证，但 token 按开关替换
	lp, err := netsec.NewLoginPayloadFromSAUTH(auth, sauth, dToken, "0",
		strconv.FormatInt(time.Now().Unix(), 10), dev, netsec.NeteaseChannelInfo())
	if err != nil {
		t.Fatal("组装授权包失败")
	}
	which := os.Getenv("MIXED_CONFIRM_TOKEN")
	confirmToken, confirmDevice := dToken, login.DeviceID
	if which == "platform" {
		confirmToken, confirmDevice = login.Token, login.DeviceID
	}
	lp.Body["token"] = confirmToken
	lp.Body["device_id"] = confirmDevice
	summary["confirm_token_kind"] = map[string]string{"": "derived", "platform": "platform", "derived": "derived"}[which]
	summary["confirm_sa"] = tokenSaValue(confirmToken)
	t.Logf("确认用 token：sa=%.0f（声明仍用 sa=%.0f）", tokenSaValue(confirmToken), tokenSaValue(dToken))

	// ⑤ 建码 → 标记扫描 → 确认 → 轮询授权码
	c := mpay.New()
	c.AppChannel = "netease"
	traceIndex := 0
	c.ProbeTrace = func(method, path string, request url.Values, response map[string]any) {
		traceIndex++
		write("trace-"+strconv.Itoa(traceIndex), map[string]any{"method": method, "path": path, "request": request, "response": response})
		t.Logf("%s → code=%v", path, response["code"])
	}
	uuid, _, err := c.CreateLoginHostedFull(login.DeviceID, profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
	if err != nil {
		summary["create_login"] = err.Error()
		t.Fatalf("建码失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	code, err := c.CompleteHostedQRCode(ctx, uuid, profile.HostedGameID, lp.Body)
	if err != nil {
		summary["authorize_error"] = err.Error()
		t.Logf("授权失败：%v", err)
		return
	}
	summary["authorize"] = "ok"
	summary["code_len"] = len(code)
	t.Log("确认与轮询均通过，已拿到授权码")
}
