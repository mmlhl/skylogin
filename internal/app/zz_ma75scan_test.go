package app

// 用**游戏本体（ma75）**建码，让渠道跟着扫码设备走。
//
// 依据（2026-11-09 官方抓包 launcher-20261024-143522，14:36:37）：
//
//	官方游戏内扫码建码：game_id=…-ma75, jf_game_id=ma75, cv=c4.20.0,
//	                    app_channel=a50_sdk_cn, qrcode_channel_type=2, is_remember=2
//	手机扫/确认时：      app_channel=huawei（变成扫码设备自己的渠道）
//
// 也就是说渠道由"扫码设备"写进 scan/confirm，而不是建码方决定。
// 我们现在的扫码走 a50 托管码，服务端按 a50_sdk_cn（PC/安卓）处理，所以永远是安卓。
//
// 本实验要回答两件事：
//  1. ma75 建码 + 手机扫 + 用**加载湾**换票，能不能走通（不必复刻 exchange_token）
//  2. 用 iOS 设备扫，渠道会不会变成 iOS
//
// 开关：MA75_SCAN_DIR=<本地忽略目录>；MA75_SCAN_WAIT 等待秒数（默认 300）；
// MA75_SCAN_LAUNCH=1 才启动游戏。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"skylogin/internal/fever"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

func TestZZMa75Scan(t *testing.T) {
	dir := os.Getenv("MA75_SCAN_DIR")
	if dir == "" {
		t.Skip("未开启 ma75 建码扫码实验")
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
	if v, err := strconv.Atoi(os.Getenv("MA75_SCAN_WAIT")); err == nil && v > 0 && v <= 600 {
		waitSec = v
	}
	summary := map[string]any{}
	defer func() { write("summary", summary) }()

	// 用游戏自己的设备号：官方那次建码用的就是 ma75 的设备
	devID, derr := ensureDeviceID()
	if derr != nil || devID == "" {
		t.Fatalf("游戏设备号不可用: %v", derr)
	}

	c := mpay.New()
	// 官方参数：game_id=ma75、jf=ma75、cv=c4.20.0、gv=0.0.0.0、arch=win_x64
	uuid, createOut, err := c.CreateScanSession(devID, profile.LongGameID, profile.ShortGameID,
		proto().AppChannel, "c4.20.0", "0.0.0.0", fever.SystemVersion(), "win_x64")
	if err != nil {
		summary["create_login"] = "failed"
		t.Fatalf("ma75 建码失败: %v", err)
	}
	write("create-login", createOut)
	t.Logf("ma75 建码成功（game_id=%s）", profile.LongGameID)

	png, ierr := c.QRImage(uuid, profile.LongGameID, "c4.20.0")
	if ierr != nil || len(png) == 0 {
		t.Fatalf("取二维码失败: %v", ierr)
	}
	qrPath := filepath.Join(dir, "qrcode.png")
	if err := os.WriteFile(qrPath, png, 0o644); err != nil {
		t.Fatal("写二维码失败")
	}
	summary["qr_path"] = qrPath
	t.Logf("二维码已保存：%s（请用光遇手游扫）", qrPath)

	// 等扫码；这次不自己 scan/confirm，全部交给手机
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	var code, channel string
	lastStatus := -1
	for time.Now().Before(deadline) {
		status, cd, ch, raw, qerr := c.ScanStatus(uuid, profile.LongGameID)
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
				summary["scan"] = "ended"
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
	t.Logf("手机已确认（channel=%s），code 长度=%d", channel, len(code))

	// 关键：用加载湾换票，看它认不认 ma75 的 code
	out, xerr := launcher.ExchangeTicket(code, devID)
	if xerr != nil || out == nil || out.Ticket == "" {
		summary["exchange"] = "failed"
		summary["exchange_err"] = truncText(xerr.Error(), 120)
		t.Logf("加载湾换票失败：%v → ma75 的 code 走不通这条", xerr)
		return
	}
	write("ticket", out)
	summary["exchange"] = "ok"
	summary["ticket_len"] = len(out.Ticket)
	summary["ticket_channel"] = out.Channel
	t.Logf("加载湾换票成功（%d 字符，channel=%s）", len(out.Ticket), out.Channel)

	if os.Getenv("MA75_SCAN_LAUNCH") == "1" {
		launchHeadlessProbeGame(t, out.Channel, out.Ticket)
	}
}

// trunc 截断文本，避免把凭证写进日志。
func truncText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
