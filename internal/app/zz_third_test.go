package app

import (
	"os"
	"testing"
)

// 看 示例账号（平台免密登录里用普通 token 成功换 sid 的号）的字段。
func TestZZThird(t *testing.T) {
	if os.Getenv("SELFHOST_GI") == "" {
		t.Skip("跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := findAccount(list, os.Getenv("ZZ_ACCOUNT"))
	if !ok {
		t.Log("本地账号库没有这个号（它是平台自己记住的）")
		return
	}
	m := tokenFields(acc.MpayToken)
	t.Logf("示例账号: sa=%v t=%v g_i=%v fgi=%v di=%v odi有=%v",
		m["sa"], m["t"], m["g_i"], m["fgi"], m["di"], m["odi"] != nil)
}
