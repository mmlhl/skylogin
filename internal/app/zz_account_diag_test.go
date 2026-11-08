package app

// 诊断某个网易账号的选区条件：凭证等级、有没有平台凭证、槽里是不是它自己。
//
// 只输出判断结果与长度，不输出 token、UserID 原文。
// 开关：ACCOUNT_DIAG=<账号名或 PlayerID>

import (
	"os"
	"testing"
	"time"

	"skylogin/internal/launcher"
)

func TestZZAccountDiag(t *testing.T) {
	target := os.Getenv("ACCOUNT_DIAG")
	if target == "" {
		t.Skip("未指定 ACCOUNT_DIAG")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal("读账号失败")
	}
	// ACCOUNT_DIAG=* 列出全部账号摘要（不含 token 原文）
	if target == "*" {
		for _, a := range list {
			t.Logf("%-28s 来源=%-10s sa=%-3.0f 平台凭证=%-5v 区=%-8q 更新时间=%s",
				displayName(a), a.Source, tokenSaValue(a.MpayToken), a.A50Token != "",
				a.Region, time.Unix(a.UpdatedAt, 0).Format("01-02 15:04:05"))
		}
		return
	}
	var acc Account
	found := false
	for _, a := range list {
		if a.Username == target || a.PlayerID == target || a.Name == target || displayName(a) == target {
			acc = a
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("账号库里没有 %s", target)
	}

	sa := tokenSaValue(acc.MpayToken)
	t.Logf("账号：%s", displayName(acc))
	t.Logf("  渠道=%s 来源=%s 登录方式=%s 所选区=%q", acc.Channel, acc.Source, acc.LoginMethod, acc.Region)
	t.Logf("  游戏凭证：长度 %d，g_i=%s，sa=%.0f", len(acc.MpayToken), tokenGI(acc.MpayToken), sa)
	t.Logf("  账号级=%v（只有 sa=-9/-99 能自己 confirm）", isAccountLevelToken(acc.MpayToken))
	t.Logf("  平台凭证 A50Token：%v", acc.A50Token != "")
	t.Logf("  保存过密码：%v", hasSavedPassword(acc))
	t.Logf("  渠道签发可用=%v", channelMintable(acc))
	t.Logf("  住在别人槽里(hosted)=%v", isHosted(acc.UserID))
	// 直接看界面拿到的字段，确认 hosted 真的输出到 /api/state
	view := accountView(acc)
	t.Logf("  accountView.hosted=%v  accountView.region=%q", view["hosted"], view["region"])
	if reason := regionBlockReason(acc); reason != "" {
		t.Logf("  选区阻断原因：%s", reason)
	}

	if acc.A50Token == "" {
		t.Log("  没有平台凭证，不能当宿主，也没有可读的寄宿槽")
		return
	}
	sid, serr := launcher.Login(acc.UserID, acc.A50Token, acc.A50DeviceID)
	if serr != nil || sid == "" {
		t.Logf("  换 sid 失败：%v", serr)
		return
	}
	hosted, herr := launcher.SavedAccount(sid)
	if herr != nil {
		t.Logf("  读槽失败：%v", herr)
		return
	}
	t.Logf("  自己的槽：空=%v，槽内是本账号=%v", hosted == "", hosted == acc.UserID)
}
