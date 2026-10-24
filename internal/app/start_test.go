package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/fever"
	"skylogin/internal/netsec"
)

type fakeTicket struct{}

func (fakeTicket) Login(payload map[string]string) (string, map[string]any, error) {
	return "TICKET-TEST-42", map[string]any{"ok": true}, nil
}

// TestLaunchGameEndToEnd 验证整条启动链路（除真实服务器外）：
// 账号 → 取票 → 冒充平台 → "启动游戏" → 游戏发 op13 → 收到 op14 票据。
func TestLaunchGameEndToEnd(t *testing.T) {
	dir := t.TempDir()
	oldDataDir := dataDirFn
	dataDirFn = func() string { return dir }
	defer func() { dataDirFn = oldDataDir }()

	exe := filepath.Join(dir, "sky.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveAccount(Account{
		PlayerID: "1001", NickName: "tester", PlayerLevel: "1",
		GameAuthSign: "SIGN", Ts: "1700000000000",
		UpdatedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	oldRefresh, oldPID, oldFind, oldStart := refreshFn, launcherPIDFn, findSkyFn, startSkyFn
	oldTicket, oldPayload, oldClass := newTicketSource, buildPayloadFn, hostClassFn
	defer func() {
		refreshFn, launcherPIDFn, findSkyFn, startSkyFn = oldRefresh, oldPID, oldFind, oldStart
		newTicketSource, buildPayloadFn, hostClassFn = oldTicket, oldPayload, oldClass
	}()

	refreshFn = func(a Account) (Account, error) { return a, nil }
	launcherPIDFn = func() (uint32, bool) { return 0, false }
	findSkyFn = func() (string, error) { return exe, nil }
	startSkyFn = func(string) (bool, error) { return false, nil }
	newTicketSource = func() ticketSource { return fakeTicket{} }
	hostClassFn = func() string { return "LHMW_FG_Main_TEST" }
	buildPayloadFn = func(acc Account, dev *device.ClientProfile) (*netsec.LoginPayload, map[string]any, error) {
		return &netsec.LoginPayload{Body: map[string]string{"user_id": acc.PlayerID}}, map[string]any{}, nil
	}

	msg, err := launchGame()
	if err != nil {
		t.Fatalf("launchGame 失败: %v", err)
	}
	if msg == "" {
		t.Fatal("没有返回提示信息")
	}

	// 模拟游戏侧：建客户端窗口 → 发 op13 → 等 op14
	client := fever.NewClient("LHMW_FG_clientFGpma75_i0")
	if err := client.Start(); err != nil {
		t.Fatalf("客户端启动失败: %v", err)
	}
	defer client.Stop()
	time.Sleep(200 * time.Millisecond)

	if err := client.SendTicketRequest("LHMW_FG_Main_TEST", 0); err != nil {
		t.Fatalf("发送 op13 失败: %v", err)
	}
	reply, err := client.WaitTicket(5 * time.Second)
	if err != nil {
		t.Fatalf("没收到票据: %v", err)
	}
	if reply.Ticket != "TICKET-TEST-42" {
		t.Fatalf("票据不一致: %q", reply.Ticket)
	}
	if reply.LoginChannel != "huawei" {
		t.Fatalf("渠道不一致: %q", reply.LoginChannel)
	}
}
