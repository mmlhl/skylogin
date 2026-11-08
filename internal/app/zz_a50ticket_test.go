package app

import (
	"os"
	"testing"

	"skylogin/internal/mpay"
)

// a50 token 在 a50 上下文直接签票（平台的常规路径）。
func TestZZA50Ticket(t *testing.T) {
	if os.Getenv("SELFHOST_GI") == "" {
		t.Skip("跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := findAccount(list, "aebggifyh5w2dop4")
	if !ok || acc.A50Token == "" {
		t.Fatal("没有 a50 凭证")
	}
	c := mpay.New()
	tk, _, err := c.CreateTicketFor(acc.A50Token, acc.UserID, acc.A50DeviceID, a50Long, a50Short)
	if err != nil {
		t.Fatalf("a50 上下文签票失败: %v", err)
	}
	t.Logf("✅ a50 token 签票成功（%d 字符）——这就是平台的常规票源", len(tk))
}
