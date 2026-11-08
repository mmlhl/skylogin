package app

// 一次性探针：验证「无头账号密码登录 → 落库 → 保存密码」整条链。
//
// 会**改写**该账号的凭证与密码字段（凭证会换成这次登录新拿的），所以默认跳过。
// 开关：ZZ_HEADLESS_ACCT=1，凭证取 .env 的 URS_USER / URS_PASS，
// 并要求 SKYLOGIN_EXE 指向构建好的 skylogin.exe（测试宿主不认 --headless-login）。

import (
	"os"
	"testing"

	"skylogin/internal/profile"
)

func TestZZHeadlessAccount(t *testing.T) {
	if os.Getenv("ZZ_HEADLESS_ACCT") == "" {
		t.Skip("未设置 ZZ_HEADLESS_ACCT")
	}
	user, pass := os.Getenv("URS_USER"), os.Getenv("URS_PASS")
	if user == "" || pass == "" {
		t.Fatal("缺少 URS_USER / URS_PASS")
	}
	if os.Getenv("SKYLOGIN_EXE") == "" {
		t.Fatal("需要 SKYLOGIN_EXE 指向构建好的 skylogin.exe")
	}

	res, err := HeadlessLoginAccount(user, pass, "android", "")
	if err != nil {
		t.Fatalf("无头添加账号失败: %v", err)
	}
	uid, _ := res["userId"].(string)
	t.Logf("登录结果: uid=%s name=%v existed=%v region=%v", uid, res["name"], res["existed"], res["region"])

	acc, ok := findAccountByUserID(uid)
	if !ok {
		t.Fatalf("落库后找不到账号 %s", uid)
	}
	if _, short := ticketContextForToken(acc.MpayToken); short != profile.ShortGameID {
		t.Fatalf("凭证身份不是游戏（short=%q）", short)
	}
	if !hasSavedPassword(acc) {
		t.Fatal("没有保存密码，自动重登用不了")
	}
	u2, p2, err := accountPassword(acc)
	if err != nil {
		t.Fatalf("取回密码失败: %v", err)
	}
	if u2 != user || p2 != pass {
		t.Fatalf("密码往返不一致（user=%q, pass 长度 %d）", u2, len(p2))
	}
	t.Logf("账号 %s：凭证可签发 + 密码往返正常（设备 %s）", displayName(acc), acc.DeviceID)
}
