package app

// 一次性探针：确认 sid 稳不稳定——这决定宿主的 sid 能不能复用。
//
// 为什么要测：寄宿是按 sid 绑的，如果同一账号每次登录都拿到**不同的 sid**，
// 那"上次记住的号"很可能就跟着旧 sid 一起作废；如果 sid 稳定（或服务端
// 复用同一会话），那把 sid 存下来长期用就能保住寄宿。
//
// 开关：ZZ_SID=1，SKYLOGIN_EXE 指向构建好的 exe，凭据取 .env。

import (
	"os"
	"testing"

	"skylogin/internal/launcher"
	"skylogin/internal/profile"
)

func TestZZSidStable(t *testing.T) {
	if os.Getenv("ZZ_SID") == "" {
		t.Skip("未设置 ZZ_SID")
	}
	user, pass := os.Getenv("URS_USER"), os.Getenv("URS_PASS")
	if user == "" || pass == "" {
		t.Fatal("缺少 URS_USER / URS_PASS")
	}
	plat, err := HeadlessPasswordLogin(user, pass, profile.HostedGameID)
	if err != nil {
		t.Fatalf("平台身份登录失败: %v", err)
	}

	// 同一个 udid（默认平台设备）连换两次
	sid1, err := launcher.Login(plat.UserID, plat.Token, plat.DeviceID)
	if err != nil {
		t.Fatalf("第一次换 sid 失败: %v", err)
	}
	sid2, err := launcher.Login(plat.UserID, plat.Token, plat.DeviceID)
	if err != nil {
		t.Fatalf("第二次换 sid 失败: %v", err)
	}
	same := sid1 == sid2
	t.Logf("同一设备连换两次：sid1=%s… sid2=%s… → %s",
		sid1[:8], sid2[:8], map[bool]string{true: "稳定（可复用）", false: "每次都变（寄宿会跟着旧 sid 走）"}[same])

	// 两次读到的槽是否一致
	h1, e1 := launcher.SavedAccount(sid1)
	h2, e2 := launcher.SavedAccount(sid2)
	t.Logf("槽：sid1→%q(%v)  sid2→%q(%v)", h1, e1, h2, e2)

	// 再换一个设备，看是不是另一个会话
	sid3, err := launcher.LoginExt(plat.UserID, plat.Token, plat.DeviceID, "SKYLOGIN0000000000000000000000009")
	if err != nil {
		t.Fatalf("换设备换 sid 失败: %v", err)
	}
	h3, e3 := launcher.SavedAccount(sid3)
	t.Logf("换设备：sid3=%s…（与首两次%s）槽=%q(%v)", sid3[:8],
		map[bool]string{true: "相同", false: "不同"}[sid3 == sid1], h3, e3)
}
