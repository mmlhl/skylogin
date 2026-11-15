package app

// 启动组的"这次启动什么"是纯计算，单独测。
//
// 这块最容易出错的不是启动本身，而是**决定**：
// 哪些槽这次要拉起、位置还能不能用、有多少槽已经失效。
// 一旦这里算错，用户看到的是"少开一个号"或"窗口跑到屏幕外"，很难查。

import "testing"

func accMap(list ...Account) map[string]Account {
	m := make(map[string]Account, len(list))
	for _, a := range list {
		m[accountKey(a)] = a
	}
	return m
}

func TestPlanGroupLaunchUsesRectsWhenScreenMatches(t *testing.T) {
	byKey := accMap(
		Account{PlayerID: "a", Name: "A"},
		Account{PlayerID: "b", Name: "B"},
	)
	g := LaunchGroup{
		ScreenW: 1920,
		ScreenH: 1080,
		Slots: []GroupSlot{
			{AccountKey: "a", HasRect: true, X: 0, Y: 0, W: 960, H: 540},
			{AccountKey: "b", HasRect: true, X: 960, Y: 540, W: 960, H: 540},
		},
	}
	p := planGroupLaunch(g, byKey, 1920, 1080)
	if len(p.picked) != 2 || !p.useRects || len(p.rects) != 2 {
		t.Fatalf("两槽齐全且屏幕一致时应按位置摆：%+v", p)
	}
	if p.rects[1].X != 960 || p.rects[1].H != 540 {
		t.Fatalf("矩形要和槽位一一对应：%+v", p.rects)
	}
}

func TestPlanGroupLaunchFallsBackWhenScreenChanged(t *testing.T) {
	byKey := accMap(Account{PlayerID: "a"})
	g := LaunchGroup{
		ScreenW: 1920,
		ScreenH: 1080,
		Slots:   []GroupSlot{{AccountKey: "a", HasRect: true, X: 10, Y: 10, W: 800, H: 600}},
	}
	// 换了显示器：坐标作废，必须退化成网格（不然窗口可能整块在屏幕外）
	p := planGroupLaunch(g, byKey, 2560, 1440)
	if p.useRects || p.rects != nil {
		t.Fatalf("屏幕不一致时不该用记录的位置：%+v", p)
	}
	if len(p.picked) != 1 {
		t.Fatal("账号还是照样要启动")
	}
}

func TestPlanGroupLaunchNeedsEverySlotHaveRect(t *testing.T) {
	byKey := accMap(Account{PlayerID: "a"}, Account{PlayerID: "b"})
	g := LaunchGroup{
		ScreenW: 1920, ScreenH: 1080,
		Slots: []GroupSlot{
			{AccountKey: "a", HasRect: true, W: 960, H: 540},
			{AccountKey: "b"}, // 没记过位置
		},
	}
	if p := planGroupLaunch(g, byKey, 1920, 1080); p.useRects {
		t.Fatal("只要有一个槽没位置，就整组按排列方式算（否则两个窗口会叠一起）")
	}
	// 宽高为 0 也算"没记过"
	g.Slots[1] = GroupSlot{AccountKey: "b", HasRect: true, W: 0, H: 540}
	if p := planGroupLaunch(g, byKey, 1920, 1080); p.useRects {
		t.Fatal("宽为 0 的位置不能用")
	}
}

func TestPlanGroupLaunchSkipsDisabledAndMissing(t *testing.T) {
	byKey := accMap(Account{PlayerID: "a"}, Account{PlayerID: "b"})
	g := LaunchGroup{
		ScreenW: 1920, ScreenH: 1080,
		Slots: []GroupSlot{
			{AccountKey: "a", HasRect: true, W: 960, H: 540},
			{AccountKey: "b", Disabled: true},        // 这轮禁用
			{AccountKey: "已删除的键", HasRect: true}, // 账号已不存在
		},
	}
	p := planGroupLaunch(g, byKey, 1920, 1080)
	if len(p.picked) != 1 || p.picked[0].PlayerID != "a" {
		t.Fatalf("只应启动 a：%+v", p.picked)
	}
	if p.disabled != 1 || p.missing != 1 {
		t.Fatalf("要分别记账（界面要提示）：disabled=%d missing=%d", p.disabled, p.missing)
	}
	// 位置判断只看"要启动的槽"：b 禁用、第三个已删，都不该把位置打成失效
	if !p.useRects || len(p.rects) != 1 {
		t.Fatalf("被禁用/已删除的槽不该影响位置可用性：%+v", p)
	}
}

func TestPlanGroupLaunchAllDisabled(t *testing.T) {
	byKey := accMap(Account{PlayerID: "a"})
	g := LaunchGroup{Slots: []GroupSlot{{AccountKey: "a", Disabled: true}}}
	if p := planGroupLaunch(g, byKey, 1920, 1080); len(p.picked) != 0 {
		t.Fatal("全禁用时不该有可启动的账号（调用方会报错，不该静默启动一个空的）")
	}
}
