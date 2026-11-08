package app

// 自寄宿完整链一次性验证（默认跳过）：
//
//	REMEMBER_FULL=1 URS_USER=… URS_PASS=… go test ./internal/app/ -run TestZZFullChain -v
//
// 假设：DLL 无头登录推 devices/users 时，服务端把账号写进了"那次会话的槽"；
// 用回调 token 换的 sid 就是那次会话 → 立刻槽位出票应该命中。

import (
	"os"
	"testing"

	"skylogin/internal/launcher"
	"skylogin/internal/mpayhost"
)

func TestZZFullChain(t *testing.T) {
	if os.Getenv("REMEMBER_FULL") == "" {
		t.Skip("跳过")
	}
	user, pass := os.Getenv("URS_USER"), os.Getenv("URS_PASS")
	if user == "" || pass == "" {
		t.Fatal("需要 URS_USER / URS_PASS")
	}

	// 第 1 步：DLL 无头登录（RSA 加密），拿回调 a50 token + 设备号
	res := mpayhost.ProbeUILess(mpayhost.UILessOptions{
		Options: mpayhost.Options{
			SkyDir:  "D:\\FeverApps\\sky",
			SkinZip: "D:\\FeverGames\\1.18.45.8\\skin.zip",
			GameID:  "aecglf6ee4aaaarz-g-a50",
			Channel: "a50_sdk_cn",
			LogPath: "E:\\skytest\\tmp\\fullchain-urs.txt",
		},
		Action: mpayhost.ActionUrs,
		UID:    user,
		Token:  pass, // Token 字段复用为密码（探针内部 RSA 加密）
		WaitSec: 20,
	})
	if !res.OK || len(res.Slots) < 3 {
		t.Fatalf("无头登录失败: %s", res.Error)
	}
	a50Token, a50Dev := res.Slots[2], res.Slots[1]
	t.Logf("① 无头登录成功 uid=%s dev=%s token g_i=%s", res.Slots[0], a50Dev, tokenGI(a50Token))

	// 第 2 步：立刻用同一份凭证换 sid
	sid, lerr := launcher.Login(res.Slots[0], a50Token, a50Dev)
	if lerr != nil || sid == "" {
		t.Fatalf("② 换 sid 失败: %v", lerr)
	}
	t.Logf("② sid 已取得（%d 字符）", len(sid))

	// 第 3 步：立刻读槽 + 槽位出票
	hosted, herr := launcher.SavedAccount(sid)
	t.Logf("③ 槽里是: %q (err=%v)", hosted, herr)
	r, terr := launcher.TicketForAccount(res.Slots[0], a50Dev, sid)
	if terr != nil {
		t.Fatalf("④ 槽位出票失败: %v", terr)
	}
	t.Logf("④ ✅ 槽位出票成功 ticket=%d字符 channel=%s —— 全链打通", len(r.Ticket), r.Channel)
}
