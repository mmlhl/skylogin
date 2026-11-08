package app

// 一次性探针：会话级（sa=3）的游戏凭证能不能走完渠道签发（confirm + 出票）。
//
// 背景：sa=3 能过 uni_sauth（200），但用平台凭证换出来的 sa=3 曾在 confirm 阶段
// 报 1306。为排除"平台兑换出来的凭证本身有问题"，这里用**账号级凭证自己滚出来的
// sa=3** 做对照：如果它也 1306，说明限制在等级，不在来源。
//
// 开关：ZZ_SA3CONFIRM=<账号 PlayerID>（建议用 sa=-9 的号，如 aebggifyh5w2dop4）

import (
	"os"
	"testing"

	"skylogin/internal/mpay"
)

func TestZZSa3Confirm(t *testing.T) {
	target := os.Getenv("ZZ_SA3CONFIRM")
	if target == "" {
		t.Skip("未设置 ZZ_SA3CONFIRM")
	}
	acc, ok := findAccountByUserID(target)
	if !ok {
		t.Fatalf("找不到账号 %s", target)
	}
	if acc.Region == "" {
		t.Fatal("没选渠道")
	}
	t.Logf("原始：%s g_i=%s sa=%.0f", displayName(acc), tokenGI(acc.MpayToken), tokenSaValue(acc.MpayToken))

	// 用账号级凭证滚出一份会话级凭证（与平台兑换出来的同一等级）
	longID, shortID := ticketContextForToken(acc.MpayToken)
	c := mpay.New()
	ticket, _, err := c.CreateTicketFor(acc.MpayToken, acc.UserID, acc.DeviceID, longID, shortID)
	if err != nil {
		t.Fatalf("签票失败: %v", err)
	}
	res, err := c.LoginTicket(ticket, acc.DeviceID, longID, shortID)
	if err != nil {
		t.Fatalf("兑换失败: %v", err)
	}
	user, _ := res["user"].(map[string]any)
	tok, _ := user["token"].(string)
	if tok == "" {
		t.Fatal("没换到 token")
	}
	t.Logf("滚出来的会话凭证：g_i=%s sa=%.0f len=%d", tokenGI(tok), tokenSaValue(tok), len(tok))

	sess := acc
	sess.MpayToken = tok
	got, _, err2 := mintTicketByPlatform(sess, mustDevice(t))
	if err2 != nil {
		t.Logf("会话凭证走完整签发：失败（%v）", err2)
		return
	}
	t.Logf("会话凭证走完整签发：成功，票 %d 字符", len(got))
}
