package app

import (
	"encoding/json"
	"os"
	"testing"
)

// 回归：早期 B站 登录把 float64 uid 用 fmt.Sprint 存成了科学计数法
// （"4.13918109e+08"），游戏服务器认不了。loadAccounts 读档时必须
// 把它修回整数形式并回写文件，current.txt 里的同款坏 id 也要一起修。
func TestLoadAccountsRepairsScientificID(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	list := []Account{
		{Channel: "bilibili", UserID: "4.13918109e+08", PlayerID: "4.13918109e+08", Name: "mmlhl777"},
		{Channel: "netease", UserID: "123456789", PlayerID: "123456789", Name: "网易号"},
	}
	b, _ := json.Marshal(list)
	if err := os.WriteFile(accountsPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentPath(), []byte("4.13918109e+08"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if got[0].UserID != "413918109" || got[0].PlayerID != "413918109" {
		t.Fatalf("B站账号 id 应修复为 413918109，实际 %q / %q", got[0].UserID, got[0].PlayerID)
	}
	if got[1].UserID != "123456789" || got[1].PlayerID != "123456789" {
		t.Fatalf("正常账号不能被动，实际 %q / %q", got[1].UserID, got[1].PlayerID)
	}

	// 修复必须回写：下次进程读文件看到的就是干净数据
	b2, err := os.ReadFile(accountsPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(b2) == string(b) {
		t.Fatal("修复后应该把干净数据写回账号文件")
	}
	if cur, err := os.ReadFile(currentPath()); err != nil || string(cur) != "413918109" {
		t.Fatalf("current.txt 应修复为 413918109，实际 %q（err=%v）", string(cur), err)
	}
}

func TestNormalizeSciIDIgnoresNormalIDs(t *testing.T) {
	if v, ok := normalizeSciID("413918109"); ok || v != "413918109" {
		t.Fatalf("普通 id 不应被改动，得到 %q ok=%v", v, ok)
	}
	if v, ok := normalizeSciID("abc"); ok || v != "abc" {
		t.Fatalf("非数字不应被改动，得到 %q ok=%v", v, ok)
	}
	if v, ok := normalizeSciID("1.5e+09"); !ok || v != "1500000000" {
		t.Fatalf("科学计数法应转整数，得到 %q ok=%v", v, ok)
	}
}

// 渠道显示：B站账号必须显示成 B站渠道，不能落进华为的默认分支。
func TestAccountViewBilibiliChannel(t *testing.T) {
	v := accountView(Account{Channel: "bilibili", PlayerID: "413918109", NickName: "测试"})
	if v["channel"] != "bilibili" || v["channelLabel"] != "B站渠道" {
		t.Fatalf("B站账号渠道显示错误: %v / %v", v["channel"], v["channelLabel"])
	}
	w := accountView(Account{Channel: "netease", PlayerID: "123"})
	if w["channel"] != "netease" || w["channelLabel"] != "网易官方" {
		t.Fatalf("网易账号渠道显示错误: %v / %v", w["channel"], w["channelLabel"])
	}
	h := accountView(Account{Channel: "huawei", PlayerID: "456"})
	if h["channel"] != "huawei" || h["channelLabel"] != "华为渠道" {
		t.Fatalf("华为账号渠道显示错误: %v / %v", h["channel"], h["channelLabel"])
	}
}
