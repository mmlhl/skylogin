package app

// 横幅文案的回归测试：过期提示必须写清是哪个账号。
//
// 2026-11-11 用户反馈："这个叫刷新凭证，并没有说哪个账号"。
// 横幅是全局一条，只写「登录信息已过期」的话，用户既不知道点刷新会刷谁，
// 也看不出刷错没有。

import (
	"strings"
	"testing"
	"time"
)

func TestAuthHintNamesAccount(t *testing.T) {
	dir := t.TempDir()
	oldDir := dataDirFn
	dataDirFn = func() string { return dir }
	defer func() { dataDirFn = oldDir }()
	clearAuthHint()
	defer clearAuthHint()

	if err := saveList([]Account{{
		Channel: "netease", PlayerID: "2002", UserID: "2002", NickName: "木木",
		CredState: "ok", UpdatedAt: time.Now().Unix(),
	}}); err != nil {
		t.Fatal(err)
	}

	// 走最常用的那条路：凭证巡检 / 多开预检把号标成过期
	setAccountCredState("2002", "expired", "MPay 拒绝: 1306 用户登录失效")

	v := authHintView()
	if v["kind"] != "netease_expired" {
		t.Fatalf("提示类型不对: %v", v["kind"])
	}
	msg, _ := v["message"].(string)
	if !strings.Contains(msg, "木木") {
		t.Fatalf("横幅没写清是哪个账号: %q", msg)
	}
	if v["account"] != "木木" {
		t.Fatalf("account 字段应为显示名，实际 %v", v["account"])
	}
	// accountId 是界面「刷新」按钮定位账号用的，不能丢
	if v["accountId"] != "2002" {
		t.Fatalf("accountId 丢了，界面会去刷当前选中的号: %v", v["accountId"])
	}
}

// TestAuthHintListsEveryAccount 多个号先后过期时，提示要能列全，不能互相覆盖。
//
// 以前横幅只有一条全局的，后过期的会盖掉先过期的——用户只看到最后一个，
// 还以为只有一个号有问题。
func TestAuthHintListsEveryAccount(t *testing.T) {
	dir := t.TempDir()
	oldDir := dataDirFn
	dataDirFn = func() string { return dir }
	defer func() { dataDirFn = oldDir }()
	clearAuthHint()
	defer clearAuthHint()

	now := time.Now().Unix()
	if err := saveList([]Account{
		{Channel: "netease", PlayerID: "2002", UserID: "2002", NickName: "木木", CredState: "ok", UpdatedAt: now},
		{Channel: "netease", PlayerID: "3003", UserID: "3003", NickName: "小林", CredState: "ok", UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}

	setAccountCredState("2002", "expired", "1306 用户登录失效")
	setAccountCredState("3003", "expired", "1306 用户登录失效")

	v := authHintView()
	if v["count"] != 2 {
		t.Fatalf("两个号过期却只记了 %v 条", v["count"])
	}
	items, _ := v["items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("items 长度不对: %d", len(items))
	}
	// 按发生时间排：先过期的在前
	if items[0]["account"] != "木木" || items[1]["account"] != "小林" {
		t.Fatalf("顺序不对: %v / %v", items[0]["account"], items[1]["account"])
	}

	// 只清一个号：另一个的提示必须还在
	clearAuthHintFor("2002")
	v = authHintView()
	if v["count"] != 1 || v["account"] != "小林" {
		t.Fatalf("清掉一个号后剩下的不对: count=%v account=%v", v["count"], v["account"])
	}

	// 全清
	clearAuthHint()
	if len(authHintView()) != 0 {
		t.Fatal("clearAuthHint 之后应该没有提示")
	}
}
