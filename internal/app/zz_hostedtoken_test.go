package app

// 一次性探针：验证「被寄宿的号能不能借槽位票换回一条新 token」。
//
// 背景：被寄宿的号自己那份游戏凭证（ma75）过期后，单开照样能靠槽位票启动，
// 但滚动续期（rollSessionToken）要拿自己的 token 去签票，直接 1306 失败。
// 如果槽位票能走 login/ticket 换出新 token，那这个号就不用再依赖宿主，
// 也不用用户重新扫码。
//
// 开关：ZZ_HOSTEDTOKEN=1。只读账号、只打印，不写回任何东西。

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/device"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
)

func TestZZHostedToken(t *testing.T) {
	if os.Getenv("ZZ_HOSTEDTOKEN") == "" {
		t.Skip("未设置 ZZ_HOSTEDTOKEN")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatalf("读账号失败: %v", err)
	}
	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		t.Fatalf("设备指纹不可用: %v", derr)
	}

	// 先看现状：哪些号被寄宿、宿主是谁、各自凭证状态
	for _, a := range list {
		if a.Channel != "netease" {
			continue
		}
		hosted, hostID := isHostedBy(a.UserID)
		t.Logf("账号 %-22s uid=%s sa=%d 有a50=%v cred=%s 被寄宿=%v 宿主=%s",
			displayName(a), a.UserID, int(tokenSaValue(a.MpayToken)), a.A50Token != "",
			a.CredState, hosted, hostID)
	}

	// 挑一个"被寄宿且自己 token 已经不能用"的号来试
	var target Account
	found := false
	for _, a := range list {
		if a.Channel != "netease" || a.MpayToken == "" {
			continue
		}
		if hosted, _ := isHostedBy(a.UserID); !hosted {
			continue
		}
		if os.Getenv("ZZ_UID") != "" && a.UserID != os.Getenv("ZZ_UID") {
			continue
		}
		target = a
		found = true
		break
	}
	if !found {
		t.Skip("没有被寄宿的网易账号可试")
	}

	// 1) 先确认这个号自己的 token 确实不能签票（复现 1306）
	ownLong, ownShort := ticketContextForToken(target.MpayToken)
	if _, _, cerr := mpay.New().CreateTicketFor(target.MpayToken, target.UserID, target.DeviceID, ownLong, ownShort); cerr != nil {
		t.Logf("① 自己 token 签票：失败（%v）——符合预期", cerr)
	} else {
		t.Logf("① 自己 token 签票：居然成功（这个号其实没过期）")
	}

	// 2) 槽位票：先确认槽里真的是它，再出票
	if b, ok := slotBindingOf(target.UserID); ok {
		if host, hok := findAccountByUserID(b.HostUserID); hok {
			sid, serr := hostSid(host)
			if serr != nil {
				t.Logf("② 宿主换 sid 失败：%v", serr)
			} else if got, gerr := launcher.SavedAccount(sid); gerr != nil {
				t.Logf("② 读槽失败：%v", gerr)
			} else {
				t.Logf("② 服务端槽里记的是：%s（目标 %s，一致=%v）", got, target.UserID, got == target.UserID)
			}
		}
	}

	// 3) 关键实验：槽位票 → login/ticket → 新 token
	// 票是一次性的，所以每个上下文都重新出一张
	for _, ctx := range []struct{ long, short, name, dev string }{
		{gameLong, gameShort, "ma75（游戏本体）", target.DeviceID},
		{a50Long, a50Short, "a50（平台）", target.A50DeviceID},
	} {
		tk, terr := rememberedTicket(target)
		if terr != nil || tk == "" {
			t.Logf("③ [%s] 槽位出票失败：%v", ctx.name, terr)
			continue
		}
		devID := ctx.dev
		if devID == "" {
			devID = target.DeviceID
		}
		res, lerr := mpay.New().LoginTicket(tk, devID, ctx.long, ctx.short)
		if lerr != nil {
			t.Logf("③ [%s] login/ticket 失败：%v", ctx.name, lerr)
			continue
		}
		user, _ := res["user"].(map[string]any)
		tok, _ := user["token"].(string)
		if tok == "" {
			t.Logf("③ [%s] 没返回 token：%v", ctx.name, truncate(fmt.Sprint(res), 200))
			continue
		}
		sa := tokenSaValue(tok)
		lt := tokenLoginUnix(tok)
		gi := tokenGI(tok)
		t.Logf("③ [%s] 换到新 token：sa=%v lt=%d g_i=%s 长度=%d", ctx.name, sa, lt, gi, len(tok))
		// 新 token 能不能真的用：用它签一张票
		if _, _, verr := mpay.New().CreateTicketFor(tok, target.UserID, devID, ctx.long, ctx.short); verr != nil {
			t.Logf("   新 token 签票：失败（%v）", verr)
		} else {
			t.Logf("   新 token 签票：成功 ✅")
		}
		if _, uerr := mpay.New().GetUserInfo(tok, target.UserID, devID, ctx.long, ctx.short); uerr != nil {
			t.Logf("   新 token 查用户资料：失败（%v）", uerr)
		} else {
			t.Logf("   新 token 查用户资料：成功 ✅")
		}
		_ = dev
	}
}

// TestZZHealHosted 真机验证新接入的寄宿续期：把一个"凭证已过期"的寄宿号
// 用槽位票换回新 token，并确认多开预检会放行它。
//
// 开关：ZZ_HEALHOSTED=1。会写账号文件（这正是要验证的落盘路径），
// 但会把 current.txt 原样还回去，不改变用户当前选中的账号。
func TestZZHealHosted(t *testing.T) {
	if os.Getenv("ZZ_HEALHOSTED") == "" {
		t.Skip("未设置 ZZ_HEALHOSTED")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatalf("读账号失败: %v", err)
	}
	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		t.Fatalf("设备指纹不可用: %v", derr)
	}
	curBefore, _ := os.ReadFile(currentPath())

	var target Account
	var host Account
	found := false
	for _, a := range list {
		if a.Channel != "netease" {
			continue
		}
		hosted, hostID := isHostedBy(a.UserID)
		if !hosted {
			continue
		}
		target = a
		host, _ = findAccountByUserID(hostID)
		found = true
		break
	}
	if !found {
		t.Skip("没有被寄宿的号")
	}
	t.Logf("目标：%s（sa=%d cred=%s），宿主：%s", displayName(target), int(tokenSaValue(target.MpayToken)), target.CredState, displayName(host))

	before := target.MpayToken
	ok := healHostedCred(target)
	t.Logf("healHostedCred 返回 %v", ok)
	if !ok {
		t.Fatal("寄宿续期失败")
	}

	after, _ := loadAccounts()
	for _, a := range after {
		if a.UserID == target.UserID {
			t.Logf("续期后：sa=%d lt=%d cred=%s token 变了=%v",
				a.TokenSa, a.TokenAt, a.CredState, a.MpayToken != before)
		}
	}

	// 多开预检：宿主 + 这个号，两个都应该放行
	alive, tickets, _, unverified := precheckLaunchable([]Account{host, target}, dev)
	t.Logf("多开预检：放行 %d 个，未确认 %d 个", len(alive), unverified)
	for i, a := range alive {
		t.Logf("  %-22s 票=%d 字符", displayName(a), len(tickets[i]))
	}
	if len(alive) < 2 {
		t.Fatal("多开预检仍然把这个寄宿号踢掉了")
	}

	// 还原"当前账号"，别因为后台探针把用户的选择改掉
	if len(curBefore) > 0 {
		_ = os.WriteFile(currentPath(), curBefore, 0o600)
	}
}
