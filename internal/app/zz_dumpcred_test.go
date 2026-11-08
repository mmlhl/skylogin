package app

// 临时：导出某账号凭证给 mpaydb 变体扫描（默认跳过）。
import (
	"os"
	"path/filepath"
	"testing"
)

func TestZZDumpCred(t *testing.T) {
	key := os.Getenv("DUMP_CRED_KEY")
	dir := os.Getenv("DUMP_CRED_DIR")
	if key == "" || dir == "" {
		t.Skip("跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	acc, found := findAccount(list, key)
	if !found {
		t.Fatal("没找到账号")
	}
	os.WriteFile(filepath.Join(dir, "uid.txt"), []byte(acc.UserID), 0o600)
	os.WriteFile(filepath.Join(dir, "token.txt"), []byte(acc.MpayToken), 0o600)
	os.WriteFile(filepath.Join(dir, "device.txt"), []byte(acc.DeviceID), 0o600)
	t.Log("已导出")
}
