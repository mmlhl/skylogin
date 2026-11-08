// 端到端探针（默认跳过）：
//
//	REMEMBER_DB_SYNC=<账号键> go test ./internal/app/ -run TestZZDBSync -v
//
// 备份 mpay.db 后，把该网易账号真实写进游戏 ma75 库（走正式的
// syncNetEaseNow），再 list 回读确认。
package app

import (
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/mpaydb"
)

func TestZZDBSync(t *testing.T) {
	key := os.Getenv("REMEMBER_DB_SYNC")
	if key == "" {
		t.Skip("没给 REMEMBER_DB_SYNC，跳过（不碰真实库）")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatalf("读账号库失败: %v", err)
	}
	acc, found := findAccount(list, key)
	if !found || acc.Channel != "netease" || acc.MpayToken == "" {
		t.Fatalf("找不到带凭证的网易号 %s", key)
	}
	dbPath := filepath.Join(mpayDataDir(), "aecfs5n2y4aaaakd-g-ma75-64-mpay.db")
	bak, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("读库失败: %v", err)
	}
	bakPath := dbPath + ".bak"
	if err := os.WriteFile(bakPath, bak, 0o644); err != nil {
		t.Fatalf("备份失败: %v", err)
	}
	t.Logf("已备份到 %s（%d 字节）", bakPath, len(bak))

	syncNetEaseNow(acc)

	// 回读确认
	db, err := mpaydb.New()
	if err != nil {
		t.Fatalf("初始化 wasm 失败: %v", err)
	}
	defer db.Close()
	accounts, err := mpaydb.ListAccounts(db, dbPath)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	for _, a := range accounts {
		t.Logf("库内记录: game=%s channel=%s uid=%s", a.GameID, a.LoginChannel, a.SDKUID)
	}
}
