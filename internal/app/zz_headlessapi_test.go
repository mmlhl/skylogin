package app

// 一次性探针：验证「主进程 → --headless-login 子进程」这条无头调用链真的能跑通。
//
// 开关：ZZ_HEADLESS=1，凭证取 .env 的 URS_USER / URS_PASS。
// 只验证链路和身份判定，不写账号库。

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"skylogin/internal/profile"
)

func TestZZHeadlessAPI(t *testing.T) {
	if os.Getenv("ZZ_HEADLESS") == "" {
		t.Skip("未设置 ZZ_HEADLESS")
	}
	user, pass := os.Getenv("URS_USER"), os.Getenv("URS_PASS")
	if user == "" || pass == "" {
		t.Fatal("缺少 URS_USER / URS_PASS")
	}
	sky := os.Getenv("ZZ_SKY")
	if sky == "" {
		sky = `D:\FeverGames\1.18.45.8`
	}
	skin := filepath.Join(sky, "skin.zip")

	r, err := runHeadless(headlessRequest{
		Action:   "urs",
		SkyDir:   sky,
		Skin:     skin,
		GameID:   profile.LongGameID,
		LogPath:  filepath.Join(dataDir(), "zz_headless.log"),
		UID:      user,
		Password: pass,
		WaitSec:  45,
	}, 120*time.Second)
	if err != nil {
		t.Fatalf("无头调用失败: %v（结果: ok=%v token=%d 字符）", err, r.OK, len(r.Token))
	}
	t.Logf("子进程返回: ok=%v uid=%s dev=%s token=%d 字符 平台身份=%v 票=%d 字符",
		r.OK, r.UserID, r.DeviceID, len(r.Token), r.Platform, len(r.Ticket))
	if r.Platform {
		t.Fatalf("应该是游戏身份凭证，却是平台身份")
	}
	if _, short := ticketContextForToken(r.Token); short != profile.ShortGameID {
		t.Fatalf("凭证身份不是游戏（short=%q）", short)
	}
	t.Logf("token 的 g_i=%s（应为游戏身份）", tokenGI(r.Token))
}
