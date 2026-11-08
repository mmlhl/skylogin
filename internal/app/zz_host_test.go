package app

// 一次性探针：确认"谁能当寄宿宿主"。
//
// 待验证的三个问题：
//  1. 游戏身份（ma75）凭证能不能换加载湾会话 sid —— 换不了就当不了宿主
//  2. 平台身份（a50）凭证能不能换 sid
//  3. 换到的 sid 现在的寄宿槽里是谁（空 / 某个 account_id）
//
// 开关：ZZ_HOST=1，SKYLOGIN_EXE 指向构建好的 skylogin.exe，凭据取 .env。

import (
	"os"
	"testing"

	"skylogin/internal/launcher"
	"skylogin/internal/profile"
)

func TestZZHostQualification(t *testing.T) {
	if os.Getenv("ZZ_HOST") == "" {
		t.Skip("未设置 ZZ_HOST")
	}
	user, pass := os.Getenv("URS_USER"), os.Getenv("URS_PASS")
	if user == "" || pass == "" {
		t.Fatal("缺少 URS_USER / URS_PASS")
	}

	// ① 先拿一份游戏身份凭证，看它能不能换 sid
	game, err := HeadlessPasswordLogin(user, pass, profile.LongGameID)
	if err != nil {
		t.Fatalf("游戏身份登录失败: %v", err)
	}
	sidGame, err := launcher.Login(game.UserID, game.Token, game.DeviceID)
	if err != nil || sidGame == "" {
		t.Logf("① 游戏身份凭证换 sid：失败（%v）→ 不能当宿主", err)
	} else {
		t.Logf("① 游戏身份凭证换 sid：成功 sid=%d 字符", len(sidGame))
	}

	// ② 再拿一份平台身份凭证
	platform, err := HeadlessPasswordLogin(user, pass, profile.HostedGameID)
	if err != nil {
		t.Fatalf("平台身份登录失败: %v", err)
	}
	if !platform.Platform {
		t.Fatalf("期望平台身份凭证，实际不是")
	}
	sidPlat, err := launcher.Login(platform.UserID, platform.Token, platform.DeviceID)
	if err != nil || sidPlat == "" {
		t.Fatalf("② 平台身份凭证换 sid：失败（%v）", err)
	}
	t.Logf("② 平台身份凭证换 sid：成功 sid=%d 字符", len(sidPlat))

	// ③ 看这个 sid 的寄宿槽
	hosted, herr := launcher.SavedAccount(sidPlat)
	if herr != nil {
		t.Logf("③ 读槽失败: %v", herr)
	} else if hosted == "" {
		t.Logf("③ 槽是空的（可以当宿主，扫码时不会顶掉别人）")
	} else {
		t.Logf("③ 槽里已经是 %s（同一 sid 再写会替换掉它）", hosted)
	}

	// ④ 用这个 sid 给槽里的号出票，看能不能出（验证宿主是否真的在生效）
	if hosted != "" {
		r, terr := launcher.TicketForAccount(hosted, platform.DeviceID, sidPlat)
		if terr != nil {
			t.Logf("④ 用槽出票：失败（%v）", terr)
		} else {
			t.Logf("④ 用槽出票：成功（%d 字符票）", len(r.Ticket))
		}
	}
}
