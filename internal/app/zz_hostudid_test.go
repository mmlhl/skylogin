package app

// 一次性探针：验证"寄宿槽到底是按什么分的"——账号，还是平台设备（udid）。
//
// 背景：loadinbay 的 LoginExt 允许指定 udid。如果换 udid 能拿到不同 sid、
// 且各自的寄宿槽互相独立，那"槽位不够、新号顶掉老号"就有解了：
// 每个扫码号分配一个 udid，各自记住。
//
// 开关：ZZ_HOST_UDID=1，SKYLOGIN_EXE 指向构建好的 exe，凭据取 .env。

import (
	"os"
	"testing"

	"skylogin/internal/launcher"
	"skylogin/internal/profile"
)

func TestZZHostUdid(t *testing.T) {
	if os.Getenv("ZZ_HOST_UDID") == "" {
		t.Skip("未设置 ZZ_HOST_UDID")
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
	t.Logf("平台凭证 uid=%s dev=%s", plat.UserID, plat.DeviceID)

	// 三台"平台设备"：默认那台 + 两个自造的
	udids := []string{"", "SKYLOGIN0000000000000000000000001", "SKYLOGIN0000000000000000000000002"}
	for i, u := range udids {
		sid, err := launcher.LoginExt(plat.UserID, plat.Token, plat.DeviceID, u)
		if err != nil || sid == "" {
			t.Logf("udid[%d]=%q → 换 sid 失败: %v", i, u, err)
			continue
		}
		hosted, herr := launcher.SavedAccount(sid)
		if herr != nil {
			t.Logf("udid[%d]=%q → sid=%d 字符，读槽失败: %v", i, u, len(sid), herr)
			continue
		}
		slot := hosted
		if slot == "" {
			slot = "（空）"
		}
		t.Logf("udid[%d]=%q → sid=%s… 槽=%s", i, u, sid[:8], slot)
	}
}
