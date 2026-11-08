package app

// 一次性探针：验证「凭证失效 → 自动用保存的密码重登 → 恢复可用」整条链。
//
// 做法：先把账号凭证换成一个语法正确但服务端不认的假 token（模拟过期），
// 再调 autoReloginAndVerify。成功即证明自动重登真的能救回来。
//
// 开关：ZZ_AUTORELOGIN=1，要求该账号已经存过密码（先跑 TestZZHeadlessAccount）。

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/device"
	"skylogin/internal/profile"
)

func TestZZAutoRelogin(t *testing.T) {
	if os.Getenv("ZZ_AUTORELOGIN") == "" {
		t.Skip("未设置 ZZ_AUTORELOGIN")
	}
	if os.Getenv("SKYLOGIN_EXE") == "" {
		t.Fatal("需要 SKYLOGIN_EXE 指向构建好的 skylogin.exe")
	}
	uid := os.Getenv("ZZ_UID")
	if uid == "" {
		uid = "aebggifyh5w2dop4"
	}
	acc, ok := findAccountByUserID(uid)
	if !ok {
		t.Fatalf("找不到账号 %s", uid)
	}
	if !hasSavedPassword(acc) {
		t.Fatal("这个账号没存过密码，先跑 TestZZHeadlessAccount")
	}

	// 造一个"看着像游戏身份、服务端不认"的假凭证
	fake := "1-" + base64.RawURLEncoding.EncodeToString([]byte(`{"g_i":"aecfs5n2y4aaaakd","sa":-9,"t":1,"lt":1}`))
	acc.MpayToken = fake
	if err := saveAccount(acc); err != nil {
		t.Fatalf("写入假凭证失败: %v", err)
	}
	t.Logf("已把 %s 的凭证换成假 token，模拟过期", displayName(acc))

	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatalf("设备标识不可用: %v", err)
	}
	fresh, err := autoReloginAndVerify(acc, dev)
	if err != nil {
		t.Fatalf("自动重登失败: %v", err)
	}
	if _, short := ticketContextForToken(fresh.MpayToken); short != profile.ShortGameID {
		t.Fatalf("重登后的凭证身份不对（short=%q）", short)
	}
	if fresh.MpayToken == fake {
		t.Fatal("凭证没被替换")
	}
	t.Logf("自动重登成功：%s 恢复可用（凭证 %d 字符，设备 %s）",
		displayName(fresh), len(fresh.MpayToken), fresh.DeviceID)
}
