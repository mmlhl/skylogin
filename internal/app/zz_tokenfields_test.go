package app

import (
	"os"
	"testing"
)

// 细看 sa 和 t（token 类型）的值差异——199 换 sid 成功、js2fv5 失败，
// 差异必然在这几个枚举里。
func TestZZTokenFields(t *testing.T) {
	if os.Getenv("REMEMBER_SELFHOST2") == "" {
		t.Skip("跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"aebgerymcruc6zqx", "aebfx7kv2q5vpldc"} {
		acc, ok := findAccount(list, key)
		if !ok {
			continue
		}
		m := tokenFields(acc.MpayToken)
		t.Logf("%s: sa=%v t=%v prs=%v fgi=%v g_i=%v di=%v", displayName(acc),
			m["sa"], m["t"], m["prs"], m["fgi"], m["g_i"], m["di"])
	}
}
