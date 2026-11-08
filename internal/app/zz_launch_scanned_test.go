package app

// 用手机扫码换到的票启动游戏，观察是否弹选区。
//
// 背景：2026-11-09 手机扫码确认实验成功——
// 平台登录 → 派生 ma75 sa=3 → 声明 ad 200 → 建码 → 手机扫码确认 → 换票成功。
// 这一步绕过了 confirm_login 的 1306，所以要看这张票启动时游戏还弹不弹选区。
//
// 开关：SCAN_CONFIRM_DIR=<含 ticket.json 的本地忽略目录>；只发一次票，不重复。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/launcher"
)

func TestZZLaunchScannedTicket(t *testing.T) {
	dir := os.Getenv("SCAN_CONFIRM_DIR")
	if dir == "" {
		t.Skip("未指定扫码实验目录")
	}
	b, err := os.ReadFile(filepath.Join(dir, "ticket.json"))
	if err != nil {
		t.Fatal("没有找到换票结果")
	}
	var res launcher.TicketResult
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal("换票结果解析失败")
	}
	if res.Ticket == "" {
		t.Fatal("票为空")
	}
	t.Logf("使用扫码换到的票（%d 字符，channel=%s）启动游戏", len(res.Ticket), res.Channel)
	launchHeadlessProbeGame(t, res.Channel, res.Ticket)
}
