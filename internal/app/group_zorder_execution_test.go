package app

import (
	"reflect"
	"skylogin/internal/win"
	"testing"
)

func TestGroupAutomaticLayersUseNewPlanWithoutChangingPositionsOrNumbers(t *testing.T) {
	old := groupLayoutPlanFn
	t.Cleanup(func() { groupLayoutPlanFn = old })
	groupLayoutPlanFn = func(win.LayoutRequest) win.LayoutPlan {
		return win.LayoutPlan{Rects: []win.TargetRect{{X: 10, W: 1024, H: 720, ZOrder: 1}, {X: 1200, W: 1024, H: 720, ZOrder: 2}}}
	}
	started := []Account{{PlayerID: "a", DisplayMode: "windowed"}, {PlayerID: "b", DisplayMode: "windowed"}}
	plan := groupLaunchPlan{picked: started, zorders: map[string]int{"a": 2, "b": 1}}
	byKey := map[string]win.ClientCell{"a": {PID: 11}, "b": {PID: 22}}
	cells, rects := groupPlannedCells(LaunchGroup{Layout: "cascade:cols"}, plan, started, byKey)
	if cells[0].PID != 11 || cells[1].PID != 22 || rects[0].X != 10 || rects[1].X != 1200 || rects[0].ZOrder != 1 || rects[1].ZOrder != 2 {
		t.Fatalf("新方案不能套用旧层级或重排几何：%+v %+v", cells, rects)
	}
	layers := groupFinalLayerCells(plan, started, byKey, cells, rects)
	if layers[0].PID != 11 || layers[1].PID != 22 {
		t.Fatalf("最终恢复不能重新套用旧保存Z：%+v", layers)
	}
}

func TestGroupManualLayersRestoreSavedOrderAndFullscreenCannotOverwriteAutoWindows(t *testing.T) {
	started := []Account{{PlayerID: "a", DisplayMode: "windowed"}, {PlayerID: "b", DisplayMode: "windowed"}, {PlayerID: "c", DisplayMode: "fullscreen"}}
	byKey := map[string]win.ClientCell{"a": {PID: 11}, "b": {PID: 22}, "c": {PID: 33}}
	plan := groupLaunchPlan{picked: started, useRects: true, zorders: map[string]int{"a": 3, "b": 1, "c": 2}, rects: []win.TargetRect{{X: 10, W: 1024, H: 720}, {X: 1200, W: 1024, H: 720}, {}}}
	cells, rects := groupWindowCells(plan, started, byKey)
	if rects[0].ZOrder != 3 || rects[1].ZOrder != 1 {
		t.Fatal("有效手动位置必须采用保存Z")
	}
	layers := groupFinalLayerCells(plan, started, byKey, cells, rects)
	if layers[0].PID != 22 || layers[2].PID != 11 {
		t.Fatalf("手动配置层级不对：%+v", layers)
	}
	plan.useRects = false
	rects[0].ZOrder, rects[1].ZOrder = 1, 2
	layers = groupFinalLayerCells(plan, started, byKey, cells, rects)
	var windows []uint32
	for _, c := range layers {
		if c.PID != 33 {
			windows = append(windows, c.PID)
		}
	}
	if !reflect.DeepEqual(windows, []uint32{11, 22}) {
		t.Fatalf("混合全屏的旧全局Z破坏自动窗口层级：%v", layers)
	}
}

func TestGroupPlacementExecutesGeometryThenNativeLayerRestoreAndSkipsStaleBatch(t *testing.T) {
	oldPlace, oldRestore := groupArrangeByRectsFn, groupRestoreZOrderFn
	t.Cleanup(func() { groupArrangeByRectsFn, groupRestoreZOrderFn = oldPlace, oldRestore })
	cells := []win.ClientCell{{PID: 11}, {PID: 22}}
	rects := []win.TargetRect{{X: 10, ZOrder: 2}, {X: 1200, ZOrder: 1}}
	layers := []win.ClientCell{{PID: 22}, {PID: 11}}
	var calls []string
	current := true
	groupArrangeByRectsFn = func(got []win.ClientCell, r []win.TargetRect) int {
		if got[0].PID != 11 || r[0].X != 10 {
			t.Fatal("实际排列前不可按层级交换坐标")
		}
		calls = append(calls, "place")
		return 2
	}
	groupRestoreZOrderFn = func(got []win.ClientCell) int {
		if got[0].PID != 22 || got[1].PID != 11 {
			t.Fatal("实际恢复未采用期望层级")
		}
		calls = append(calls, "native-z")
		return 2
	}
	if got := executeGroupPlacement(cells, rects, layers, func() bool { return current }); got != 2 || !reflect.DeepEqual(calls, []string{"place", "native-z"}) {
		t.Fatalf("调用链错误：%d %v", got, calls)
	}
	calls = nil
	groupArrangeByRectsFn = func([]win.ClientCell, []win.TargetRect) int {
		calls = append(calls, "place")
		current = false
		return 2
	}
	executeGroupPlacement(cells, rects, layers, func() bool { return current })
	if !reflect.DeepEqual(calls, []string{"place"}) {
		t.Fatalf("旧批次结束后不得恢复新批层级：%v", calls)
	}
}
