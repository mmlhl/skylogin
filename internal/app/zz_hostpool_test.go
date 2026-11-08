package app

// 一次性探针：补齐平台凭证后（可当宿主），看寄宿池的真实状态。
//
// 开关：ZZ_HOSTPOOL=1，SKYLOGIN_EXE 指向构建好的 exe，凭据取 .env。
// 会给测试账号补一份平台身份凭证（A50Token），这是当宿主的前提。

import (
	"os"
	"testing"

	"skylogin/internal/profile"
)

func TestZZHostPool(t *testing.T) {
	if os.Getenv("ZZ_HOSTPOOL") == "" {
		t.Skip("未设置 ZZ_HOSTPOOL")
	}
	user, pass := os.Getenv("URS_USER"), os.Getenv("URS_PASS")
	if user == "" || pass == "" {
		t.Fatal("缺少 URS_USER / URS_PASS")
	}
	plat, err := HeadlessPasswordLogin(user, pass, profile.HostedGameID)
	if err != nil {
		t.Fatalf("平台身份登录失败: %v", err)
	}
	if !plat.Platform {
		t.Fatal("期望平台身份凭证")
	}
	acc, ok := findAccountByUserID(plat.UserID)
	if !ok {
		t.Fatalf("账号库里没有 %s", plat.UserID)
	}
	if err := saveHeadlessCred(acc.PlayerID, plat); err != nil {
		t.Fatalf("保存平台凭证失败: %v", err)
	}
	fromHeadless := hostCache
	_ = fromHeadless
	invalidateHostSlot(plat.UserID)

	pool := HostPoolStatus()
	t.Logf("能当宿主 %d 个，空槽 %d 个，提示=%q", pool.CanHost, pool.FreeSlots, pool.Note)
	for _, h := range pool.Hosts {
		slot := "空"
		if h.HostedID != "" {
			slot = h.HostedName + "(" + h.HostedID + ")"
		}
		t.Logf("  %-28s 可宿主=%-5v 槽=%s %s", h.Name, h.CanHost, slot, h.Reason)
	}
	if pool.CanHost == 0 {
		t.Fatal("补齐平台凭证后仍然没有可用宿主")
	}
	if sel, sid, err := PickHostSlot(); err != nil {
		t.Fatalf("选宿主失败: %v", err)
	} else {
		t.Logf("选中的宿主：%s（槽里 %q，sid %d 字符）", sel.Name, sel.HostedID, len(sid))
	}
}
