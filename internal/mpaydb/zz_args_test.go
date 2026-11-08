package mpaydb

import (
	"os"
	"testing"
)

// 探测参数 JSON 结构：list 在什么参数形状下能成功。
func TestZZArgShapes(t *testing.T) {
	src := os.Getenv("MPAYDB_PUT_DB")
	uid := os.Getenv("MPAYDB_PUT_UID")
	if src == "" || uid == "" {
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

	shapes := []struct {
		name string
		args string // 已编码的 JSON，nil = 空对象 {}
	}{
		{"空对象", `{}`},
		{"带game_id", `{"game_id":"aecglf6ee4aaaarz-g-a50"}`},
		{"带三个键", `{"game_id":"aecglf6ee4aaaarz-g-a50","login_channel":"netease","sdkuid":"` + uid + `"}`},
		{"空串", ``},
		{"数组", `[{}]`},
		{"null", `null`},
	}
	for _, s := range shapes {
		dbCopy := make([]byte, len(original))
		copy(dbCopy, original)
		_, _, res, err := db.InvokeJSON(7, dbCopy, []byte(s.args))
		if err != nil {
			t.Logf("%-10s -> %v", s.name, err)
		} else {
			t.Logf("%-10s -> 成功: %s", s.name, truncateStr(string(res), 120))
		}
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
