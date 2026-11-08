package mpaydb

import (
	"os"
	"testing"
)

// 各操作码在真实库上的行为对照。
func TestZZOps(t *testing.T) {
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

	for _, op := range []int{4, 5, 6, 7} {
		dbCopy := make([]byte, len(original))
		copy(dbCopy, original)
		args := map[string]any{
			"game_id": "aecglf6ee4aaaarz-g-a50", "login_channel": "netease", "sdkuid": uid,
			"name": "测试", "machine_identifier": MachineIdentifier(),
		}
		changed, newDB, result, err := db.Invoke(op, dbCopy, args)
		status := "ok"
		if err != nil {
			status = err.Error()
		}
		t.Logf("op=%d -> changed=%v newDB=%d result=%d字符 %s", op, changed, len(newDB), len(result), status)
	}
}
