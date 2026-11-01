package app

import (
	"encoding/json"
	"os"
	"testing"
)

// 副本键体系（"PlayerID-2" 后缀键）的单元测试。

func TestNextCopyKey(t *testing.T) {
	t.Run("无副本时从 2 起", func(t *testing.T) {
		list := []Account{{PlayerID: "abc"}}
		if got := nextCopyKey(list, "abc"); got != "abc-2" {
			t.Fatalf("got %q want abc-2", got)
		}
	})
	t.Run("已有 -2 时顺延到 -3", func(t *testing.T) {
		list := []Account{
			{PlayerID: "abc"},
			{PlayerID: "abc", CopyKey: "abc-2"},
		}
		if got := nextCopyKey(list, "abc"); got != "abc-3" {
			t.Fatalf("got %q want abc-3", got)
		}
	})
	t.Run("副本键与别的账号互不干扰", func(t *testing.T) {
		list := []Account{
			{PlayerID: "abc"},
			{PlayerID: "other", CopyKey: "other-2"},
		}
		if got := nextCopyKey(list, "abc"); got != "abc-2" {
			t.Fatalf("got %q want abc-2", got)
		}
	})
}

func TestResolveAccountKeyWithCopies(t *testing.T) {
	list := []Account{
		{PlayerID: "abc"},
		{PlayerID: "abc", CopyKey: "abc-2"},
	}
	t.Run("副本键直取", func(t *testing.T) {
		if got := resolveAccountKey(list, "abc-2"); got != "abc-2" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("PlayerID 解析到原始行", func(t *testing.T) {
		if got := resolveAccountKey(list, "abc"); got != "abc" {
			t.Fatalf("got %q want abc（原始行）", got)
		}
	})
}

func TestSaveAccountCopyIsolation(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	orig := Account{Channel: "netease", PlayerID: "abc", UserID: "abc", Name: "原始", MpayToken: "1-t0"}
	if err := saveAccount(orig); err != nil {
		t.Fatal(err)
	}
	dup := orig
	dup.CopyKey = "abc-2"
	dup.Name = "原始 · 副本2"
	if err := saveAccount(dup); err != nil {
		t.Fatal(err)
	}

	// 原始行重新登录（换新 token）：只更新原始行，副本的键不被顶掉
	relogin := orig
	relogin.MpayToken = "1-t1"
	if err := saveAccount(relogin); err != nil {
		t.Fatal(err)
	}

	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("应有 2 条记录，实际 %d", len(list))
	}
	var d *Account
	for i := range list {
		if list[i].CopyKey == "abc-2" {
			d = &list[i]
		} else if list[i].CopyKey == "" && list[i].MpayToken != "1-t1" {
			t.Fatalf("原始行没拿到新 token: %+v", list[i])
		}
	}
	if d == nil {
		t.Fatal("副本丢了")
	}
	// 凭证按 PlayerID 同步：副本也该拿到新 token
	if d.MpayToken != "1-t1" {
		t.Fatalf("副本 token 未同步: %q", d.MpayToken)
	}
}

// 老文件里的随机 row_id（洛伦茨方案残留）读入时转成 "PlayerID-2" 后缀键，
// current.txt 里指向旧键的"当前账号"一起迁移。
func TestLegacyRowIDConversion(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	old := []map[string]any{
		{"channel": "netease", "player_id": "abc", "user_id": "abc", "name": "原始"},
		{"channel": "netease", "player_id": "abc", "user_id": "abc", "name": "旧副本", "row_id": "deadbeef00112233"},
	}
	raw, _ := json.Marshal(old)
	if err := os.WriteFile(accountsPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentPath(), []byte("deadbeef00112233"), 0o600); err != nil {
		t.Fatal(err)
	}

	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if list[1].CopyKey != "abc-2" {
		t.Fatalf("旧副本键未转换: %+v", list[1])
	}
	cur, _ := os.ReadFile(currentPath())
	if string(cur) != "abc-2" {
		t.Fatalf("current.txt 未迁移: %q", cur)
	}
	// 再读一次应幂等（copy_key 不再触发转换）
	list2, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if list2[1].CopyKey != "abc-2" || list2[0].CopyKey != "" {
		t.Fatalf("二次读取不幂等: %+v", list2)
	}
}
