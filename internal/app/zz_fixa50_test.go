package app

// 一次性探针：验证"只有平台身份凭证、没有密码"的账号也能自己换成游戏凭证。
//
// 走的是官方换票链：平台凭证签票 → 用游戏上下文兑换 → g_i=游戏身份的 token。
// 不需要重新登录，也不需要用户点任何东西。
//
// 开关：ZZ_FIXA50=<账号 PlayerID>（默认 mumu53579 的 aebgg4yc75xyvusm）。

import (
	"os"
	"testing"

	"skylogin/internal/profile"
)

func TestZZFixA50(t *testing.T) {
	target := os.Getenv("ZZ_FIXA50")
	if target == "" {
		t.Skip("未设置 ZZ_FIXA50")
	}
	acc, ok := findAccountByUserID(target)
	if !ok {
		t.Fatalf("找不到账号 %s", target)
	}
	t.Logf("修复前：%s g_i=%s 有密码=%v 拦截=%q",
		displayName(acc), tokenGI(acc.MpayToken), hasSavedPassword(acc), regionBlockReason(acc))

	fresh, fixed := refreshGameCred(acc)
	if !fixed {
		t.Fatalf("没能自己修好（平台凭证可能已过期，需要重新登录一次）")
	}
	if _, short := ticketContextForToken(fresh.MpayToken); short != profile.ShortGameID {
		t.Fatalf("修复后身份仍不对（short=%q）", short)
	}
	if !isPlatformToken(fresh.A50Token) {
		t.Fatal("原来的平台凭证没被保留，以后当不了宿主")
	}
	t.Logf("修复后：%s g_i=%s 拦截=%q（a50 凭证已留存 %d 字符）",
		displayName(fresh), tokenGI(fresh.MpayToken), regionBlockReason(fresh), len(fresh.A50Token))
}
