package app

// 手机扫码确认实验：用真实的手机扫码代替 confirm_login。
//
// 动机（2026-11-09）：抓包 sessionC 显示平台自己的扫码流程根本不调 confirm_login——
// 建码后由手机扫码确认，再经加载湾换票。而我们的 confirm 对 sa=3 一律 1306。
// 所以试：声明照旧用 ma75 会话凭证，把"确认"这一步交回给手机。
//
// 步骤：
//  ① 平台登录（a50 账号级）
//  ② 派生 ma75 会话凭证（sa=3）
//  ③ uni_sauth(ma75, platform=ad) 声明
//  ④ 建托管码，把二维码写成 PNG
//  ⑤ 等手机扫码 → query 拿 login_info.code
//  ⑥ 换票；可选启动游戏观察是否弹选区
//
// 开关：SCAN_CONFIRM_DIR=<本地忽略目录>；SCAN_CONFIRM_WAIT 等待秒数（默认 180）；
// SCAN_CONFIRM_LAUNCH=1 才启动游戏。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

func TestZZScanConfirm(t *testing.T) {
	dir := os.Getenv("SCAN_CONFIRM_DIR")
	if dir == "" {
		t.Skip("未开启手机扫码确认实验")
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
	waitSec := 180
	if v, err := strconv.Atoi(os.Getenv("SCAN_CONFIRM_WAIT")); err == nil && v > 0 && v <= 600 {
		waitSec = v
	}
	platform := os.Getenv("SCAN_CONFIRM_PLATFORM")
	if platform == "" {
		platform = "ad"
	}
	summary := map[string]any{}
	defer func() { write("summary", summary) }()

	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatal("读取客户端配置失败")
	}

	// ① 平台登录
	login, err := HeadlessPasswordLogin(os.Getenv("URS_USER"), os.Getenv("URS_PASS"), profile.HostedGameID)
	if err != nil {
		t.Fatal("平台身份登录失败")
	}
	write("a50-login", login)
	summary["a50_sa"] = tokenSaValue(login.Token)
	t.Logf("平台凭证：g_i=%s sa=%.0f", tokenGI(login.Token), tokenSaValue(login.Token))

	// ② 派生 ma75 会话凭证
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

	// ③ 声明平台
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

	// ④ 建码并取二维码图片
	c := mpay.New()
	c.AppChannel = "netease"
	uuid, createOut, err := c.CreateLoginHostedFull(login.DeviceID, profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
	if err != nil {
		summary["create_login"] = errTextSafe(err)
		t.Fatalf("建码失败: %v", err)
	}
	write("create-login", createOut)
	summary["uuid_ok"] = uuid != ""
	png, ierr := c.QRImage(uuid, profile.HostedGameID, profile.HostedCv)
	if ierr != nil || len(png) == 0 {
		summary["qr_image"] = "failed"
		t.Fatalf("取二维码图片失败: %v", ierr)
	}
	qrPath := filepath.Join(dir, "qrcode.png")
	if err := os.WriteFile(qrPath, png, 0o644); err != nil {
		t.Fatal("写二维码失败")
	}
	summary["qr_path"] = qrPath
	t.Logf("二维码已保存：%s（请用网易大神/光遇手游扫）", qrPath)

	// ⑤ 等手机扫码确认
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	var code, channel string
	lastStatus := -1
	for time.Now().Before(deadline) {
		status, cd, ch, raw, qerr := c.ScanStatus(uuid, profile.HostedGameID)
		if qerr == nil {
			if status != lastStatus {
				lastStatus = status
				t.Logf("二维码状态 → %d", status)
			}
			if status == 2 && cd != "" {
				code, channel = cd, ch
				write("query-final", raw)
				break
			}
			if status == 3 || status == 4 {
				summary["scan_status"] = status
				t.Fatalf("二维码已结束（status=%d），未取得授权码", status)
			}
		}
		time.Sleep(2 * time.Second)
	}
	if code == "" {
		summary["scan_result"] = "timeout"
		t.Fatalf("%d 秒内没有收到扫码确认", waitSec)
	}
	summary["scan_result"] = "confirmed"
	summary["login_channel"] = channel
	t.Logf("手机已确认，拿到授权码（%d 字符，channel=%s）", len(code), channel)

	// ⑥ 换票
	out, err := launcher.ExchangeTicket(code, login.DeviceID)
	if err != nil || out == nil || out.Ticket == "" {
		summary["exchange"] = "failed"
		t.Fatalf("换票失败: %v", err)
	}
	write("ticket", out)
	summary["exchange"] = "ok"
	summary["ticket_len"] = len(out.Ticket)
	t.Logf("换票成功（%d 字符，channel=%s）", len(out.Ticket), out.Channel)

	if os.Getenv("SCAN_CONFIRM_LAUNCH") == "1" {
		launchHeadlessProbeGame(t, out.Channel, out.Ticket)
	} else {
		t.Log("未开启启动开关；如需验证免选区，设 SCAN_CONFIRM_LAUNCH=1")
	}
}

// errTextSafe 截断错误文本，避免把凭证写进日志。
func errTextSafe(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
