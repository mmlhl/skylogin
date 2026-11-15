package app

import (
	"fmt"
	"skylogin/internal/win"
	"testing"
)

func TestGroupAutomaticLayoutUsesPhysicalCapacity(t *testing.T) {
	old := groupLayoutPlanFn
	defer func() { groupLayoutPlanFn = old }()
	groupLayoutPlanFn = func(req win.LayoutRequest) win.LayoutPlan {
		return win.PlanLayout(req, win.TargetRect{W: 2560, H: 1390}, 1024, 720)
	}
	for _, count := range []int{1, 2, 3, 4, 5, 7, 9, 16} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			g := LaunchGroup{Layout: "tile", Cols: 2, Rows: 2, ScreenW: 2560, ScreenH: 1440}
			accounts := map[string]Account{}
			for i := 0; i < count; i++ {
				key := fmt.Sprint(i)
				accounts[key] = Account{PlayerID: key}
				g.Slots = append(g.Slots, GroupSlot{AccountKey: key})
			}
			p := planGroupLaunch(g, accounts, 2560, 1440)
			want := "tile"
			if count > 2 {
				want = "cascade:cols"
			}
			if got := groupArrangeMode(g, p); got != want {
				t.Fatalf("实际只能并排2窗，%d窗口应%s，得到%s", count, want, got)
			}
			if err := validateGroupPlan(g, p); err != nil {
				t.Fatal(err)
			}
			for i := range g.Slots {
				g.Slots[i].HasRect, g.Slots[i].W, g.Slots[i].H = true, 1024, 720
			}
			p = planGroupLaunch(g, accounts, 2560, 1440)
			if groupArrangeMode(g, p) != "rects" {
				t.Fatal("有效手动位置应保留")
			}
			p = planGroupLaunch(g, accounts, 1920, 1080)
			if groupArrangeMode(g, p) != want {
				t.Fatal("换屏应重新规划")
			}
		})
	}
}

func TestGroupLayoutCountsOnlyActualWindowMembers(t *testing.T) {
	old := groupLayoutPlanFn
	defer func() { groupLayoutPlanFn = old }()
	groupLayoutPlanFn = func(req win.LayoutRequest) win.LayoutPlan {
		return win.PlanLayout(req, win.TargetRect{W: 2560, H: 1390}, 1024, 720)
	}
	g := LaunchGroup{Layout: "tile"}
	accounts := map[string]Account{}
	for i := 0; i < 5; i++ {
		key := fmt.Sprint(i)
		accounts[key] = Account{PlayerID: key}
		g.Slots = append(g.Slots, GroupSlot{AccountKey: key})
	}
	g.Slots[2].Disabled = true
	g.Slots[3].Display = "fullscreen"
	delete(accounts, "4")
	p := planGroupLaunch(g, accounts, 2560, 1440)
	if groupWindowCount(p) != 2 || groupArrangeMode(g, p) != "tile" {
		t.Fatal("禁用、全屏、缺失成员不占物理容量")
	}
}

func TestGroupLayerOrderKeepsAccountRectangleBinding(t *testing.T) {
	g := LaunchGroup{ScreenW: 2560, ScreenH: 1440, Slots: []GroupSlot{{AccountKey: "a", HasRect: true, X: 10, W: 1024, H: 720, ZOrder: 2}, {AccountKey: "b", HasRect: true, X: 1000, W: 1024, H: 720, ZOrder: 1}}}
	p := planGroupLaunch(g, accMap(Account{PlayerID: "a"}, Account{PlayerID: "b"}), 2560, 1440)
	cells, rects := groupWindowCells(p, p.picked, map[string]win.ClientCell{"a": {PID: 1}, "b": {PID: 2}})
	if cells[0].PID != 1 || rects[0].X != 10 || cells[1].PID != 2 || rects[1].X != 1000 {
		t.Fatalf("层级排序必须带对应矩形一起排序：%+v %+v", cells, rects)
	}
	if p.picked[0].PlayerID != "a" {
		t.Fatal("不得改变启动顺序")
	}
}

func TestGroupAutomaticPositionIndependentFromLayerOrder(t *testing.T) {
	old := groupLayoutPlanFn
	defer func() { groupLayoutPlanFn = old }()
	groupLayoutPlanFn = func(req win.LayoutRequest) win.LayoutPlan {
		return win.PlanLayout(req, win.TargetRect{W: 2560, H: 1390}, 1024, 720)
	}
	g := LaunchGroup{Layout: "tile", Slots: []GroupSlot{{AccountKey: "a", ZOrder: 2}, {AccountKey: "b", ZOrder: 1}, {AccountKey: "full", Display: "fullscreen", ZOrder: 3}}}
	p := planGroupLaunch(g, accMap(Account{PlayerID: "a"}, Account{PlayerID: "b"}, Account{PlayerID: "full"}), 2560, 1440)
	byKey := map[string]win.ClientCell{"a": {PID: 1}, "b": {PID: 2}, "full": {PID: 3}}
	cells, rects := groupPlannedCells(g, p, p.picked, byKey)
	if len(cells) != 2 || cells[0].PID != 1 || rects[0].X >= rects[1].X {
		t.Fatalf("B置后仍在右，A仍在左：%+v %+v", cells, rects)
	}
	layers := groupLayerCells(p, p.picked, byKey)
	if len(layers) != 3 || layers[0].PID != 2 || layers[1].PID != 1 || layers[2].PID != 3 {
		t.Fatalf("最终层级也必须含全屏成员：%+v", layers)
	}
}

func TestOldArrangementBatchCannotTouchNewClients(t *testing.T) {
	original := []win.ClientCell{{PID: 1}, {PID: 2}}
	alive := func(pid uint32) bool { return pid == 2 || pid == 3 }
	if got := liveArrangementCells(original, 1, 2, alive); len(got) != 0 {
		t.Fatal("旧世代不得排列新组")
	}
	got := liveArrangementCells(original, 2, 2, alive)
	if len(got) != 1 || got[0].PID != 2 {
		t.Fatalf("只保留原PID集合，不能混入新PID3：%+v", got)
	}
}
