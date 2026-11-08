package app

// 确认接口的应用隔离对照。
//
// 已测组合（2026-11-09）：
//
//	ma75 sa=-9 + confirm(game_id=ma75) → 成功
//	ma75 sa=3  + confirm(game_id=ma75) → 403/1306
//	a50  sa=-9 + confirm(game_id=ma75) → 403/1306
//
// 还缺一个：a50 账号级凭证在 a50 自己的上下文里确认，是否通过。
//   - 若通过 → confirm 既要求"账号级"也要求"token 的应用与 game_id 一致"，
//     ma75 派生会话失败就只能归到等级/会话来源。
//   - 若不通过 → a50 应用本身就不能作为确认方，与等级无关。
//
// 本实验刻意不响应 scan 的 1424 重定向，强制在 a50 上下文确认。
// 开关：FORCE_A50_CONFIRM_DIR=<本地忽略目录>

import (
	"encoding/json"
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

func TestZZForceA50Confirm(t *testing.T) {
	dir := os.Getenv("FORCE_A50_CONFIRM_DIR")
	if dir == "" {
		t.Skip("未开启 a50 上下文确认对照")
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
	summary := map[string]any{}
	defer func() { write("summary", summary) }()

	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatal("读取客户端配置失败")
	}

	// 平台账号级凭证
	login, err := HeadlessPasswordLogin(os.Getenv("URS_USER"), os.Getenv("URS_PASS"), profile.HostedGameID)
	if err != nil {
		t.Fatal("平台身份登录失败")
	}
	write("a50-login", login)
	summary["a50_sa"] = tokenSaValue(login.Token)
	t.Logf("平台凭证：g_i=%s sa=%.0f", tokenGI(login.Token), tokenSaValue(login.Token))

	// 用 a50 凭证在 a50 上下文做声明（这一步已知会 501，只做记录，不当作阻断）
	custom := map[string]any{
		"login_channel":      "netease",
		"app_channel":        "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd",
		"platform":           "ad",
		"source_platform":    "ad",
		"sdk_version":        "4.21.0",
		"deviceid":           login.DeviceID,
		"username":           os.Getenv("URS_USER"),
	}
	sauth := netsec.NewSAUTH(login.UserID, login.Token, "a50", custom, dev)
	auth, err := netsec.PostSigned(sauth, "a50")
	if err != nil {
		summary["a50_sauth_error"] = err.Error()
	} else {
		summary["a50_sauth_code"] = auth["code"]
	}
	write("a50-sauth", map[string]any{"request": sauth, "response": auth})
	t.Logf("a50 上下文声明：code=%v（501 属已知签名限制）", auth["code"])

	// 组确认包：token 与 game_id 都用 a50，保持一致
	lp, err := netsec.NewLoginPayloadFromSAUTH(auth, sauth, login.Token, "0",
		strconv.FormatInt(time.Now().Unix(), 10), dev, netsec.NeteaseChannelInfo())
	if err != nil {
		// 声明被拒时无法从响应取 unisdk_login_json，改为直接拼最小确认包
		lp = &netsec.LoginPayload{Body: map[string]string{
			"user_id":       login.UserID,
			"token":         login.Token,
			"login_channel": "netease",
			"app_channel":   "netease.allysdk3rd",
			"sdk_version":   "4.21.0",
			"jf_game_id":    "a50",
			"pay_channel":   "netease",
			"gv":            profile.MpayGV,
			"gvn":           profile.MpayGvn,
			"cv":            profile.MpayCv,
		}}
		summary["payload"] = "minimal"
		t.Log("声明被拒，改用最小确认包（仍保留 a50 账号级 token）")
	} else {
		summary["payload"] = "from_sauth"
	}
	lp.Body["token"] = login.Token
	lp.Body["device_id"] = login.DeviceID

	c := mpay.New()
	c.AppChannel = "netease"

	uuid, _, err := c.CreateLoginHostedFull(login.DeviceID, profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
	if err != nil {
		summary["create_login"] = err.Error()
		t.Fatalf("建码失败: %v", err)
	}

	// 标记扫描：直接用 a50，不看 1424
	scanOut, scanErr := c.Scan(uuid, profile.HostedGameID)
	write("scan", scanOut)
	summary["scan_error"] = shortErr(scanErr)
	t.Logf("scan(a50)：err=%v code=%v", scanErr, scanOut["code"])

	// 强制在 a50 上下文确认
	confOut, confErr := c.Confirm(uuid, profile.HostedGameID, lp.Body)
	write("confirm", confOut)
	summary["confirm_error"] = shortErr(confErr)
	summary["confirm_code"] = confOut["code"]
	summary["confirm_reason"] = confOut["reason"]
	t.Logf("confirm(a50)：err=%v code=%v reason=%v", confErr, confOut["code"], confOut["reason"])
}

// shortErr 把错误转成可安全记录的一小段文本（不含凭证）。
func shortErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
