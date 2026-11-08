package app

// 看刚保存的 js2fv5 token 的签发身份（决定自寄宿路线成败的关键字段）。
//
//	SELFHOST_GI=1 go test ./internal/app/ -run TestZZTokenGI -v

import (
	"os"
	"testing"
)

func TestZZTokenGI(t *testing.T) {
	if os.Getenv("SELFHOST_GI") == "" {
		t.Skip("跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := findAccount(list, "aebfx7kv2q5vpldc")
	if !ok {
		t.Fatal("找不到 js2fv5")
	}
	m := tokenFields(acc.MpayToken)
	t.Logf("js2fv5 最新保存的 token: sa=%v t=%v g_i=%v 有si=%v 键数=%d",
		m["sa"], m["t"], m["g_i"], m["si"] != nil, len(m))
}
