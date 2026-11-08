// 冒烟测试（默认跳过）：
//
//	MPAYDB_TEST_DB=<mpay.db 路径> go test ./internal/mpaydb/ -run TestZZList -v
//
// 只读：用 wasm 的 list_accounts 列出真实库里的账号，验证 wasm 能跑、
// 能解出记录。不做任何写入。
package mpaydb

import (
	"os"
	"testing"
)

func TestZZList(t *testing.T) {
	path := os.Getenv("MPAYDB_TEST_DB")
	if path == "" {
		t.Skip("没给 MPAYDB_TEST_DB，跳过")
	}
	db, err := New()
	if err != nil {
		t.Fatalf("初始化 wasm 失败: %v", err)
	}
	defer db.Close()
	accounts, err := ListAccounts(db, path)
	if err != nil {
		t.Fatalf("list_accounts 失败: %v", err)
	}
	t.Logf("共 %d 条记录:", len(accounts))
	for _, a := range accounts {
		t.Logf("  game=%s channel=%s uid=%s", a.GameID, a.LoginChannel, a.SDKUID)
	}
	t.Logf("machine_identifier=%s", MachineIdentifier())
}
