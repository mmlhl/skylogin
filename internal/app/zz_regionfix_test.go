package app

// 一次性探针：验证"凭证类型不支持选区"的账号能自动修好并成功签发。
//
// 做法：先把账号凭证换成真的平台身份（a50）凭证（模拟早期登录留下的污染），
// 再走 channelMint —— 它应该发现类型不对、自动无头重登换成游戏身份凭证，
// 然后照常完成一次渠道签发。
//
// 开关：ZZ_REGIONFIX=1，SKYLOGIN_EXE 指向构建好的 exe，凭据取 .env。
// 会改写该账号的凭证（最后落成可用的游戏身份凭证）。

import (
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/device"
	"skylogin/internal/profile"
)

func TestZZRegionFix(t *testing.T) {
	if os.Getenv("ZZ_REGIONFIX") == "" {
		t.Skip("未设置 ZZ_REGIONFIX")
	}
	exe := os.Getenv("SKYLOGIN_EXE")
	if exe == "" {
		t.Fatal("需要 SKYLOGIN_EXE")
	}
	os.Setenv("SKYLOGIN_EXE", exe)

	user, pass := os.Getenv("URS_USER"), os.Getenv("URS_PASS")
	if user == "" || pass == "" {
		t.Fatal("缺少 URS_USER / URS_PASS")
	}
	acc, ok := findAccountByUserID("aebggifyh5w2dop4")
	if !ok {
		t.Fatal("找不到测试账号")
	}
	if !hasSavedPassword(acc) {
		t.Fatal("测试账号没存密码，先跑 TestZZHeadlessAccount")
	}

	// ① 污染：换成真的平台身份凭证
	plat, err := HeadlessPasswordLogin(user, pass, profile.HostedGameID)
	if err != nil {
		t.Fatalf("平台身份登录失败: %v", err)
	}
	acc.MpayToken = plat.Token
	acc.DeviceID = plat.DeviceID
	acc.Region = "android"
	if err := saveAccount(acc); err != nil {
		t.Fatalf("写入平台凭证失败: %v", err)
	}
	if reason := regionBlockReason(acc); reason == "" {
		t.Fatal("预期这时应该被拦（凭证类型不对），实际没有")
	} else {
		t.Logf("已污染为平台身份凭证，拦截原因：%s", reason)
	}

	// ② 交给渠道签发：应当自动修好
	ticket, _, err2 := mintTicket(acc, mustDevice(t))
	if err2 != nil {
		t.Fatalf("渠道签发失败: %v", err2)
	}
	if len(ticket) < 8 {
		t.Fatalf("票太短: %q", ticket)
	}
	fresh, ok := findAccountByUserID(acc.UserID)
	if !ok {
		t.Fatal("修复后账号不见了")
	}
	if _, short := ticketContextForToken(fresh.MpayToken); short != profile.ShortGameID {
		t.Fatalf("修复后凭证身份仍不对（short=%q）", short)
	}
	t.Logf("渠道签发成功：票 %d 字符；账号凭证已自动换回游戏身份（%d 字符）", len(ticket), len(fresh.MpayToken))
}

func mustDevice(t *testing.T) *device.ClientProfile {
	t.Helper()
	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatalf("设备标识不可用: %v", err)
	}
	return dev
}
