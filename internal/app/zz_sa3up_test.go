package app

// 一次性探针：会话级（sa=3）的游戏凭证，能不能借 DLL 升级成账号级（sa=-9）。
//
// 为什么需要：平台凭证换出来的游戏凭证是 sa=3，uni_sauth 能过（200），
// 但 confirm_login 阶段会 1306（用户登录失效），签不出票。
// 如果 DLL 的 UILessTokenLogin 认了这份 token 之后回调的是 sa=-9，
// 那"平台账号也能真正选区"就闭环了。
//
// 开关：ZZ_SA3UP=<账号 PlayerID>

import (
	"os"
	"testing"

	"skylogin/internal/profile"
)

func TestZZSa3Upgrade(t *testing.T) {
	target := os.Getenv("ZZ_SA3UP")
	if target == "" {
		t.Skip("未设置 ZZ_SA3UP")
	}
	acc, ok := findAccountByUserID(target)
	if !ok {
		t.Fatalf("找不到账号 %s", target)
	}
	t.Logf("原始凭证：g_i=%s sa=%.0f len=%d dev=%s",
		tokenGI(acc.MpayToken), tokenSaValue(acc.MpayToken), len(acc.MpayToken), acc.DeviceID)

	res, err := HeadlessTokenLogin(acc.UserID, acc.MpayToken, acc.DeviceID, profile.LongGameID)
	if err != nil {
		t.Fatalf("DLL 认凭证失败: %v", err)
	}
	t.Logf("DLL 回调：g_i=%s sa=%.0f len=%d dev=%s 票=%d 字符",
		tokenGI(res.Token), tokenSaValue(res.Token), len(res.Token), res.DeviceID, len(res.Ticket))
	if tokenSaValue(res.Token) == -9 {
		t.Logf("升级成功：会话凭证被 DLL 换成了账号级凭证")
	} else {
		t.Logf("仍然是 sa=%.0f，没升级", tokenSaValue(res.Token))
	}
}
