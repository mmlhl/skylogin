package app

// 记住槽能不能替代"扫码确认"，让选区长期生效。
//
// 背景（2026-11-09）：
//   - 已证明：只发 platform 声明、不做确认 → 启动仍弹选区。
//   - 已证明：声明 + 手机扫码确认 → 免选区。
//   - 未验证：声明 + 把号写进记住槽（带 sid 换票），之后只靠槽出票能不能免选区。
//
// 如果槽能替代确认，产品就能做到"扫一次，之后每次启动只靠槽"；否则每次都要扫。
//
// 流程：
//  ① 平台登录 → 派生 ma75 会话凭证（sa=3）
//  ② uni_sauth(platform=ad) 声明
//  ③ 建码 → 手机扫码 → 拿 login_info.code
//  ④ 带 sid 换票（这一步才会写槽）
//  ⑤ 回读槽，确认里面是这个号
//  ⑥ 用槽出票（不再声明、不再扫码、不再确认）
//  ⑦ 用这张槽票启动游戏，观察是否弹选区
//
// 开关：SLOT_REGION_DIR=<本地忽略目录>；SLOT_REGION_WAIT 等待秒数（默认 300）。

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

func TestZZSlotRegion(t *testing.T) {
	dir := os.Getenv("SLOT_REGION_DIR")
	if dir == "" {
		t.Skip("未开启槽位选区实验")
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
	waitSec := 300
	if v, err := strconv.Atoi(os.Getenv("SLOT_REGION_WAIT")); err == nil && v > 0 && v <= 600 {
		waitSec = v
	}
	platform := os.Getenv("SLOT_REGION_PLATFORM")
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

	// ② 派生 ma75 会话凭证（声明要用它，签名应用必须是 ma75）
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
	summary["declare_first_platform"] = auth["first_login_platform"]
	t.Logf("声明 platform=%s：code=%v first_login_platform=%v", platform, auth["code"], auth["first_login_platform"])
	if toStr(auth["code"]) != "200" {
		t.Fatalf("声明未通过：code=%v", auth["code"])
	}

	// ④ 建码并展示二维码
	//
	// remember 是建码参数 is_remember。默认 2（官方扫码登录用的值）；
	// 设 SLOT_REGION_REMEMBER=1 做对照——抓包显示手机确认时带的是 is_remember=1，
	// 写槽可能取决于这个值，而不是换票时带没带 sid。
	remember := os.Getenv("SLOT_REGION_REMEMBER")
	if remember == "" {
		remember = "2"
	}
	summary["is_remember"] = remember
	c := mpay.New()
	c.AppChannel = "netease"
	uuid, createOut, err := c.CreateLoginHostedFull(login.DeviceID, profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, remember)
	if err != nil {
		t.Fatalf("建码失败: %v", err)
	}
	write("create-login", createOut)
	png, ierr := c.QRImage(uuid, profile.HostedGameID, profile.HostedCv)
	if ierr != nil || len(png) == 0 {
		t.Fatalf("取二维码失败: %v", ierr)
	}
	qrPath := filepath.Join(dir, "qrcode.png")
	if err := os.WriteFile(qrPath, png, 0o644); err != nil {
		t.Fatal("写二维码失败")
	}
	summary["qr_path"] = qrPath
	t.Logf("二维码已保存：%s（请用光遇手游扫）", qrPath)

	// ⑤ 等手机扫码
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	var code, channel string
	for time.Now().Before(deadline) {
		status, cd, ch, raw, qerr := c.ScanStatus(uuid, profile.HostedGameID)
		if qerr == nil {
			if status == 2 && cd != "" {
				code, channel = cd, ch
				write("query-final", raw)
				break
			}
			if status == 3 || status == 4 {
				t.Fatalf("二维码已结束（status=%d）", status)
			}
		}
		time.Sleep(2 * time.Second)
	}
	if code == "" {
		summary["scan"] = "timeout"
		t.Fatalf("%d 秒内没有扫码确认", waitSec)
	}
	summary["scan"] = "confirmed"
	summary["login_channel"] = channel
	t.Logf("手机已确认（channel=%s）", channel)

	// ⑥ 换平台会话 sid（用平台账号级凭证）
	sid, err := launcher.Login(login.UserID, login.Token, login.DeviceID)
	if err != nil || sid == "" {
		summary["sid"] = "failed"
		t.Fatalf("换 sid 失败: %v", err)
	}
	summary["sid_ok"] = true

	// ⑦ 带 sid 换票 —— 这一步才会把号写进记住槽
	hosted, err := launcher.ExchangeTicketWithSid(code, login.DeviceID, sid)
	if err != nil || hosted == nil || hosted.Ticket == "" {
		summary["exchange_with_sid"] = "failed"
		t.Fatalf("带 sid 换票失败: %v", err)
	}
	write("ticket-with-sid", hosted)
	summary["exchange_with_sid"] = "ok"
	t.Logf("带 sid 换票成功（%d 字符）", len(hosted.Ticket))

	// ⑧ 回读槽：里面应该是**被扫的那个号**（手机侧账号 B），不是提供 sid 的平台号 A
	saved, serr := launcher.SavedAccount(sid)
	summary["slot_read_ok"] = serr == nil
	summary["slot_empty"] = serr == nil && saved == ""
	t.Logf("槽回读：err=%v 空槽=%v 长度=%d", serr, saved == "", len(saved))

	// ⑨ 用换到的票换出被扫账号的身份（B）
	//
	// 这一步是之前失败的关键：槽里记的是 B，出票必须用 B 的 UserID。
	// 用平台号 A 的 UserID 去要票，服务端只会回 6008。
	out, err := loginTicketAuto(c, hosted.Ticket, login.DeviceID)
	if err != nil {
		summary["login_ticket"] = "failed"
		t.Fatalf("用换到的票取身份失败: %v", err)
	}
	bUser, _ := out["user"].(map[string]any)
	bID, _ := bUser["id"].(string)
	bToken, _ := bUser["token"].(string)
	if bID == "" || bToken == "" {
		t.Fatal("没拿到被扫账号的身份")
	}
	write("scanned-account", out)
	summary["scanned_is_platform_account"] = bID == login.UserID
	summary["scanned_sa"] = tokenSaValue(bToken)
	t.Logf("被扫账号：与平台账号相同=%v，凭证 sa=%.0f", bID == login.UserID, tokenSaValue(bToken))

	// ⑩ 用槽给被扫账号出票：不再声明、不再扫码、不再确认
	slotTicket, terr := launcher.TicketForAccount(bID, login.DeviceID, sid)
	if terr != nil || slotTicket == nil || slotTicket.Ticket == "" {
		summary["slot_ticket"] = "failed"
		t.Fatalf("用槽出票失败: %v", terr)
	}
	write("slot-ticket", slotTicket)
	summary["slot_ticket"] = "ok"
	summary["slot_ticket_len"] = len(slotTicket.Ticket)
	t.Logf("用槽出票成功（%d 字符，channel=%s）", len(slotTicket.Ticket), slotTicket.Channel)

	// ⑪ 启动游戏：槽位票的渠道由扫码设备决定，预期不弹选区
	summary["launch"] = "started"
	launchHeadlessProbeGame(t, slotTicket.Channel, slotTicket.Ticket)
}
