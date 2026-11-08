package mpaydb

import (
	"os"
	"testing"
)

// 单键探测：找出 wasm 参数解析器认识的键名。
func TestZZKeys(t *testing.T) {
	src := os.Getenv("MPAYDB_PUT_DB")
	if src == "" {
		t.Skip("跳过")
	}
	original, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	db, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	keys := []string{"game_id", "login_channel", "sdkuid", "uuid", "uid", "account",
		"name", "position", "machine_identifier", "credentials", "create", "token",
		"user_id", "op", "operation", "filter", "limit"}
	for _, k := range keys {
		dbCopy := make([]byte, len(original))
		copy(dbCopy, original)
		_, _, _, err := db.InvokeJSON(7, dbCopy, []byte(`{"`+k+`":"x"}`))
		if err == nil {
			t.Logf("%-20s -> ✅ 接受", k)
		} else {
			t.Logf("%-20s -> %v", k, err)
		}
	}
}
