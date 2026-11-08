package app

// 本地探针（默认跳过）：把实验目录里的新鲜 ma75 凭证写回账号库，
// 用于真机验证渠道签发。只读实验目录、写本机账号记录，不发额外请求。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

func TestZZApplyFreshToken(t *testing.T) {
	dir, key := os.Getenv("FRESH_TOKEN_DIR"), os.Getenv("FRESH_TOKEN_KEY")
	if dir == "" || key == "" {
		t.Skip("未设置 FRESH_TOKEN_DIR / FRESH_TOKEN_KEY")
	}
	b, err := os.ReadFile(filepath.Join(dir, "game-slots.json"))
	if err != nil {
		t.Fatal("读不到实验槽位文件")
	}
	var slots []string
	if json.Unmarshal(b, &slots) != nil || len(slots) < 3 {
		t.Fatal("实验槽位文件不完整")
	}
	uid, dev, token := slots[0], slots[1], slots[2]

	// 先确认这张 token 还活着（失效的 token 也能走一部分接口，不能只看长度）
	if _, err := mpay.New().GetUserInfo(token, uid, dev, profile.LongGameID, profile.ShortGameID); err != nil {
		t.Fatalf("实验 token 已失效: %v", err)
	}
	t.Log("实验 token 仍然有效")

	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	for i := range list {
		if accountKey(list[i]) != key && list[i].UserID != key {
			continue
		}
		list[i].MpayToken = token
		list[i].DeviceID = dev
		list[i].UserID = uid
		list[i].PlayerID = uid
		list[i].CredState = "ok"
		list[i].LastError = ""
		if err := saveList(list); err != nil {
			t.Fatal(err)
		}
		t.Logf("已更新 %s 的凭证（uid=%s，渠道=%q）", displayName(list[i]), uid, list[i].Region)
		return
	}
	t.Fatal("没找到目标账号")
}
