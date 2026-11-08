package app

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 对比 199（能换 sid）和 js2fv5 滚出来的 token 的字段差异。
func TestZZTokenCompare(t *testing.T) {
	if os.Getenv("REMEMBER_SELFHOST2") == "" {
		t.Skip("跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	dump := func(label, token string) {
		raw := strings.TrimPrefix(token, "1-")
		b, err := base64.RawStdEncoding.DecodeString(raw)
		if err != nil {
			b, _ = base64.StdEncoding.DecodeString(raw)
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		// 只打键和几个安全值，不泄露 token 本体
		keys := []string{}
		for k := range m {
			keys = append(keys, k)
		}
		si, _ := m["si"].(string)
		lt, _ := m["lt"]
		odi, _ := m["odi"]
		t.Logf("%s: 键=%v si有=%v lt=%v odi有=%v", label, keys, si != "", lt, odi != "")
	}
	for _, key := range []string{"aebgerymcruc6zqx", "aebfx7kv2q5vpldc"} {
		acc, ok := findAccount(list, key)
		if !ok {
			continue
		}
		dump(displayName(acc), acc.MpayToken)
	}
}
