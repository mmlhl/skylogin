package app

// 本地探针（默认跳过）：验证“游戏身份凭证派生平台会话”的链路。
//
//	DERIVE_KEY=<账号键> go test ./internal/app -run TestZZDeriveSid -v -count=1
//
// 链路：ma75 凭证 → create_ticket(a50 上下文) → login/ticket → a50 会话 token
// → launcher.Login → 平台 sid → 读槽。全通则说明游戏根也能管寄宿槽。

import (
	"os"
	"testing"

	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
)

func TestZZDeriveSid(t *testing.T) {
	key := os.Getenv("DERIVE_KEY")
	if key == "" {
		t.Skip("未设置 DERIVE_KEY")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := findAccount(list, key)
	if !ok || acc.Channel != "netease" || acc.MpayToken == "" {
		t.Fatalf("没找到带凭证的网易号 %s", key)
	}
	longID, shortID := ticketContextForToken(acc.MpayToken)
	t.Logf("凭证 g_i=%s（%s/%s）", longID, shortID, longID)

	// ① 在 a50 上下文签票
	c := mpay.New()
	a50Dev := acc.DeviceID
	tk, _, err := c.CreateTicketFor(acc.MpayToken, acc.UserID, a50Dev, a50Long, a50Short)
	if err != nil {
		t.Fatalf("① a50 上下文签票失败: %v", err)
	}
	// ② login/ticket 换 a50 会话 token
	res, err := c.LoginTicket(tk, a50Dev, a50Long, a50Short)
	if err != nil {
		t.Fatalf("② login/ticket 失败: %v", err)
	}
	user, _ := res["user"].(map[string]any)
	tok, _ := user["token"].(string)
	if tok == "" {
		t.Fatal("② 没返回 token")
	}
	// ③ 用 a50 会话 token 换平台 sid
	sid, err := launcher.Login(acc.UserID, tok, a50Dev)
	if err != nil || sid == "" {
		t.Fatalf("③ 换平台 sid 失败: %v", err)
	}
	// ④ 读槽
	hosted, err := launcher.SavedAccount(sid)
	if err != nil {
		t.Fatalf("④ 读槽失败: %v", err)
	}
	t.Logf("✅ 游戏身份凭证可派生平台会话：sid=%d字符，槽内=%q", len(sid), hosted)
}
