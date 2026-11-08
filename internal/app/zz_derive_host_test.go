package app

// 验证：用 ma75 账号级凭证派生一份 a50 凭证，能不能换到 sid（当宿主用）。
//
// 背景：手机号验证码登录拿到的 ma75 凭证是 sa=-99，能免选区，但短信码一次性，
// 没法再登一次 a50 上下文去拿平台凭证。如果 ma75 能派生 a50，手机号账号也能当宿主。
//
// 开关：DERIVE_HOST=<账号名或 PlayerID>；只输出布尔与长度，不输出凭证。

import (
	"os"
	"testing"

	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

func TestZZDeriveHostCred(t *testing.T) {
	target := os.Getenv("DERIVE_HOST")
	if target == "" {
		t.Skip("未指定 DERIVE_HOST")
	}
	acc, ok := findAccountByUserID(target)
	if !ok {
		list, _ := loadAccounts()
		for _, a := range list {
			if displayName(a) == target || a.Username == target {
				acc, ok = a, true
				break
			}
		}
	}
	if !ok {
		t.Fatalf("找不到账号 %s", target)
	}
	t.Logf("源账号：%s  ma75 凭证 sa=%.0f 长度=%d", displayName(acc), tokenSaValue(acc.MpayToken), len(acc.MpayToken))
	if !isAccountLevelToken(acc.MpayToken) {
		t.Log("源凭证不是账号级，派生的 a50 大概也换不出 sid，仍照测")
	}

	c := mpay.New()
	// ① 用 ma75 上下文签票
	ticket, _, err := c.CreateTicketFor(acc.MpayToken, acc.UserID, acc.DeviceID, profile.LongGameID, profile.ShortGameID)
	if err != nil {
		t.Fatalf("ma75 签票失败: %v", err)
	}
	// ② 在 a50 上下文兑换
	//
	// 设备号要跟着上下文走：a50 只认自己登记过的设备号，拿 ma75 的设备号
	// 去兑换会回 1003（用户请求参数错误）。两个都试一遍，把结果记清楚。
	devs := []struct {
		name string
		id   string
	}{{"ma75设备", acc.DeviceID}}
	if a50Dev, derr := ensureA50DeviceID(); derr == nil && a50Dev != acc.DeviceID {
		devs = append(devs, struct {
			name string
			id   string
		}{"a50设备", a50Dev})
	}
	var a50Token string
	for _, d := range devs {
		out, err := c.LoginTicket(ticket, d.id, profile.HostedGameID, "a50")
		if err != nil {
			t.Logf("用%s兑换 a50 失败：%v", d.name, err)
			continue
		}
		user, _ := out["user"].(map[string]any)
		if tok, _ := user["token"].(string); tok != "" {
			a50Token = tok
			t.Logf("用%s兑换成功", d.name)
			break
		}
	}
	if a50Token == "" {
		t.Log("两个设备都没换出 a50 凭证")
		return
	}
	t.Logf("派生的 a50 凭证：g_i=%s sa=%.0f 长度=%d", tokenGI(a50Token), tokenSaValue(a50Token), len(a50Token))

	// ③ 关键：能不能换 sid（宿主资格的唯一标准）
	sid, serr := launcher.Login(acc.UserID, a50Token, acc.DeviceID)
	if serr != nil {
		t.Logf("派生凭证换 sid 失败：%v → 这条不能当宿主", serr)
		return
	}
	t.Logf("派生凭证换 sid 成功（%d 字符）→ 手机号账号也能当宿主", len(sid))
}
