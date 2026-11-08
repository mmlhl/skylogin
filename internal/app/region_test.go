package app

// 渠道（区）选择相关逻辑的单元测试。
//
// 这里只测纯函数和"只改本地记录"的路径，不发任何网络请求：
// 真正按渠道签发要连服务端，属于真机实验（见 .ai/记住渠道研究/05）。

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

// 存储值到 MPay platform 声明的映射：老数据用 android/ios，接口要 ad/ios/hm。
func TestPlatformCodeOf(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"android":  "ad",
		"Android":  "ad",
		" android": "ad",
		"ios":      "ios",
		"harmony":  "hm",
		"unknown":  "",
	}
	for in, want := range cases {
		if got := platformCodeOf(in); got != want {
			t.Errorf("platformCodeOf(%q)=%q 期望 %q", in, got, want)
		}
	}
}

// 只有"网易官方 + 已选渠道 + 账号级凭证(sa=-9)"才允许渠道签发。
// 任何一条不满足都必须返回 false，让调用方回退原有签票逻辑。
func TestChannelMintable(t *testing.T) {
	accountToken := "1-" + b64(t, `{"si":"s","s":"x","g_i":"aecfs5n2y4aaaakd","t":1,"sa":-9}`)
	sessionToken := "1-" + b64(t, `{"si":"s","s":"x","g_i":"aecfs5n2y4aaaakd","t":1,"sa":-4}`)

	cases := []struct {
		name string
		acc  Account
		want bool
	}{
		{"网易+安卓+账号级", Account{Channel: "netease", UserID: "u", MpayToken: accountToken, Region: "android"}, true},
		{"网易+iOS+账号级", Account{Channel: "netease", UserID: "u", MpayToken: accountToken, Region: "ios"}, true},
		{"未选渠道", Account{Channel: "netease", UserID: "u", MpayToken: accountToken}, false},
		{"扫码凭证(sa=-4)", Account{Channel: "netease", UserID: "u", MpayToken: sessionToken, Region: "android"}, false},
		{"渠道服账号", Account{Channel: "huawei", UserID: "u", MpayToken: accountToken, Region: "android"}, false},
		{"没有 token", Account{Channel: "netease", UserID: "u", Region: "android"}, false},
	}
	for _, c := range cases {
		if got := channelMintable(c.acc); got != c.want {
			t.Errorf("%s: channelMintable=%v 期望 %v", c.name, got, c.want)
		}
	}
}

// 设置渠道只允许网易官方账号；渠道服账号必须被拒绝。
func TestSetAccountRegion(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	list := []Account{
		{Channel: "netease", UserID: "n1", PlayerID: "n1", Name: "网易号"},
		{Channel: "huawei", UserID: "h1", PlayerID: "h1", Name: "华为号"},
	}
	b, _ := json.Marshal(list)
	if err := os.WriteFile(accountsPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := setAccountRegion("n1", "ios"); err != nil {
		t.Fatalf("给网易账号设渠道应当成功: %v", err)
	}
	got, _ := loadAccounts()
	if got[0].Region != "ios" {
		t.Fatalf("渠道没写进记录: %q", got[0].Region)
	}

	if err := setAccountRegion("h1", "ios"); err == nil {
		t.Fatal("渠道服账号不应允许设置渠道")
	}
	if err := setAccountRegion("n1", "switch"); err == nil {
		t.Fatal("非法渠道值应当被拒绝")
	}

	// 空值 = 先不选，也要能存下来
	if err := setAccountRegion("n1", ""); err != nil {
		t.Fatalf("清空渠道应当成功: %v", err)
	}
	got, _ = loadAccounts()
	if got[0].Region != "" {
		t.Fatalf("渠道没被清空: %q", got[0].Region)
	}
}

// 按 UserID 找到行（登录回调只给 userId，行键可能是 RowKey）。
func TestSetAccountRegionByUserID(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	list := []Account{
		{Channel: "netease", UserID: "uid9", PlayerID: "uid9", RowKey: "uid9 · 网页", Name: "网易号"},
	}
	b, _ := json.Marshal(list)
	if err := os.WriteFile(accountsPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setAccountRegion("uid9", "android"); err != nil {
		t.Fatalf("按 UserID 设置渠道失败: %v", err)
	}
	got, _ := loadAccounts()
	if got[0].Region != "android" {
		t.Fatalf("渠道没写进记录: %q", got[0].Region)
	}
}

// 副本可以带自己的渠道，且不影响原行的渠道。
func TestDuplicateWithRegion(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	list := []Account{
		{Channel: "netease", UserID: "p1", PlayerID: "p1", Name: "网易号", Region: "android"},
	}
	b, _ := json.Marshal(list)
	if err := os.WriteFile(accountsPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}

	key, err := duplicateAccount("p1", "iOS 分身", "ios")
	if err != nil {
		t.Fatalf("创建副本失败: %v", err)
	}
	got, _ := loadAccounts()
	var orig, copyRow *Account
	for i := range got {
		if got[i].CopyKey == "" {
			orig = &got[i]
		} else if got[i].CopyKey == key {
			copyRow = &got[i]
		}
	}
	if orig == nil || orig.Region != "android" {
		t.Fatal("原行渠道被副本改掉了")
	}
	if copyRow == nil || copyRow.Region != "ios" {
		t.Fatalf("副本渠道应为 ios，实际 %+v", copyRow)
	}

	// 渠道服账号不允许带渠道建副本
	if _, err := duplicateAccount("p1", "x", "switch"); err == nil {
		t.Fatal("非法渠道值应当被拒绝")
	}
}

// b64 是测试用的小工具：把 JSON 编成 token 里那段 base64。
func b64(t *testing.T, s string) string {
	t.Helper()
	return base64.RawStdEncoding.EncodeToString([]byte(s))
}
