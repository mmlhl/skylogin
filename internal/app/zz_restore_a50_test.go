package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

func TestZZRestoreA50(t *testing.T) {
	dir, key := os.Getenv("A50_RESTORE_DIR"), os.Getenv("A50_RESTORE_KEY")
	if dir == "" || key == "" {
		t.Skip("未设置 A50_RESTORE_DIR / A50_RESTORE_KEY")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("账号数=%d，逐条键：", len(list))
	for i := range list {
		t.Logf("  key=%q userID=%q playerID=%q", accountKey(list[i]), list[i].UserID, list[i].PlayerID)
	}
	b, err := os.ReadFile(filepath.Join(dir, "platform-slots.json"))
	if err != nil {
		t.Fatal("读不到平台槽位文件")
	}
	var slots []string
	if json.Unmarshal(b, &slots) != nil || len(slots) < 3 {
		t.Fatal("平台槽位文件不完整")
	}
	uid, dev, token := slots[0], slots[1], slots[2]

	// 有效性验证：a50 上下文查用户资料
	if _, err := mpay.New().GetUserInfo(token, uid, dev, profile.HostedGameID, "a50"); err != nil {
		t.Fatalf("a50 token 已失效: %v", err)
	}
	t.Log("a50 token 仍然有效")

	for i := range list {
		if accountKey(list[i]) != key && list[i].UserID != key && list[i].PlayerID != key {
			continue
		}
		list[i].A50Token = token
		list[i].A50DeviceID = dev
		if err := saveList(list); err != nil {
			t.Fatal(err)
		}
		t.Logf("已恢复 %s 的 A50Token（uid=%s）", displayName(list[i]), uid)
		return
	}
	t.Fatal("没找到目标账号")
}
