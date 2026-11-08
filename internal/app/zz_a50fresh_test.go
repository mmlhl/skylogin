package app

// 只读验证：新登录拿到的 A50Token 能不能换 sid。
//
// 目的：确认 "1000 sauth error" 是"同设备重新登录把旧 token 顶掉"，
// 而不是账号或平台能力问题。若新 token 能换到 sid，说明只要登录后
// 立刻把新凭证存回账号库，宿主就不会因为顶号而失效。
//
// 开关：A50_FRESH=1；只输出布尔与长度，不输出 token/sid。

import (
	"os"
	"testing"

	"skylogin/internal/launcher"
	"skylogin/internal/profile"
)

func TestZZA50Fresh(t *testing.T) {
	if os.Getenv("A50_FRESH") == "" {
		t.Skip("未开启 A50 新凭证验证")
	}
	if os.Getenv("URS_USER") == "" || os.Getenv("URS_PASS") == "" {
		t.Fatal("缺少本机登录凭据")
	}
	login, err := HeadlessPasswordLogin(os.Getenv("URS_USER"), os.Getenv("URS_PASS"), profile.HostedGameID)
	if err != nil {
		t.Fatal("平台身份登录失败")
	}
	t.Logf("新登录：g_i=%s sa=%.0f 长度=%d", tokenGI(login.Token), tokenSaValue(login.Token), len(login.Token))

	sid, serr := launcher.Login(login.UserID, login.Token, login.DeviceID)
	if serr != nil {
		t.Logf("新 A50Token 换 sid 失败：%v", serr)
		return
	}
	t.Logf("新 A50Token 换 sid 成功（%d 字符）→ 旧 token 只是被顶号，不是账号问题", len(sid))
}
