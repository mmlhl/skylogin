package app

import (
	"os"
	"testing"
)

func TestZZA50(t *testing.T) {
	if os.Getenv("SELFHOST_GI") == "" {
		t.Skip("跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := findAccount(list, "aebggifyh5w2dop4")
	if !ok {
		t.Fatal("没找到")
	}
	t.Logf("mmgyyx: A50Token有=%v len=%d g_i=%s", acc.A50Token != "", len(acc.A50Token), tokenGI(acc.A50Token))
}
