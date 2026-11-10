package app

// 多开预检与寄宿续期的回归测试。
//
// 2026-11-11 的用户日志：同一个号单开正常（票是宿主的寄宿槽出的），多开却报
// "凭证有效的账号不足两个"。原因是预检只验账号**自己**的 token，而寄宿号的票
// 根本不是它自己的 token 出的——于是"单开能起、多开说过期"。

import (
	"fmt"
	"testing"
	"time"

	"skylogin/internal/device"
)

// TestPrecheckKeepsHostedAccount 预检必须按"能不能出票"判断，而不是"自己 token 活不活"。
func TestPrecheckKeepsHostedAccount(t *testing.T) {
	dir := t.TempDir()
	oldDir := dataDirFn
	dataDirFn = func() string { return dir }
	defer func() { dataDirFn = oldDir }()

	now := time.Now().Unix()
	host := Account{
		Channel: "netease", PlayerID: "1001", UserID: "1001", Username: "host@x",
		MpayToken: "1-" + b64(t, `{"g_i":"aecglf6ee4aaaarz","sa":-9,"t":1}`),
		CredState: "ok", UpdatedAt: now,
	}
	// 被寄宿的号：自己的 token 已经"半死"（查资料 1306），但槽位票照样能出
	hosted := Account{
		Channel: "netease", PlayerID: "2002", UserID: "2002", Username: "hosted@x",
		MpayToken: "1-" + b64(t, `{"g_i":"aecfs5n2y4aaaakd","sa":3,"t":1,"lt":1790000000}`),
		CredState: "expired", UpdatedAt: now,
	}
	// 既没寄宿、自己的 token 也确实不能用
	broken := Account{
		Channel: "netease", PlayerID: "3003", UserID: "3003", Username: "broken@x",
		MpayToken: "1-" + b64(t, `{"g_i":"aecfs5n2y4aaaakd","sa":3,"t":1,"lt":1790000000}`),
		UpdatedAt: now,
	}
	if err := saveList([]Account{host, hosted, broken}); err != nil {
		t.Fatal(err)
	}
	recordSlotBinding("1001", "2002")

	oldMint := mintOnceFn
	defer func() { mintOnceFn = oldMint }()
	mintOnceFn = func(a Account, dev *device.ClientProfile) (string, string, error) {
		if a.UserID == "2002" {
			return "SLOT-TICKET", "netease", nil
		}
		return "", "", fmt.Errorf("%w（MPay 拒绝: 1306 用户登录失效）", errCredExpired)
	}

	alive, tickets, channels, unverified := precheckLaunchable([]Account{hosted, broken}, nil)
	if unverified != 0 {
		t.Fatalf("这不该被算成「没问到服务端」，unverified=%d", unverified)
	}
	if len(alive) != 1 || alive[0].UserID != "2002" {
		t.Fatalf("被寄宿的号被预检踢掉了：alive=%v", alive)
	}
	if tickets[0] != "SLOT-TICKET" || channels[0] != "netease" {
		t.Fatalf("预检没把票带回来：ticket=%q channel=%q", tickets[0], channels[0])
	}

	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		switch a.UserID {
		case "2002":
			if a.CredState != "ok" {
				t.Fatalf("出票成功的号状态应改回 ok，实际 %q", a.CredState)
			}
		case "3003":
			if a.CredState != "expired" {
				t.Fatalf("真出不了票的号应标 expired，实际 %q", a.CredState)
			}
		}
	}
}

// TestHealHostedCredRefusesLongTermToken 长期凭证不能被槽位票换成会话凭证。
//
// 槽位票换回来的是 sa=3 的会话凭证；拿 sa=-9 的长期凭证去换等于把"永久"
// 换成"会过期"（.ai/凭证研究/02 实测过）。
func TestHealHostedCredRefusesLongTermToken(t *testing.T) {
	dir := t.TempDir()
	oldDir := dataDirFn
	dataDirFn = func() string { return dir }
	defer func() { dataDirFn = oldDir }()

	recordSlotBinding("1001", "2002")
	acc := Account{
		Channel: "netease", PlayerID: "2002", UserID: "2002",
		MpayToken: "1-" + b64(t, `{"g_i":"aecfs5n2y4aaaakd","sa":-9,"t":1}`),
		CredState: "ok",
	}
	if healHostedCred(acc) {
		t.Fatal("sa=-9 的长期凭证不该拿槽位票去换（会降级成会话凭证）")
	}
}
