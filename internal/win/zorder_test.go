package win

import (
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestClientZRanksUseOnlyMappedMainWindows(t *testing.T) {
	got := clientZRanks([]uintptr{999, 20, 888, 10, 30}, map[uintptr]uint32{10: 72, 20: 31, 30: 99})
	if !reflect.DeepEqual(got, map[uint32]int{99: 1, 72: 2, 31: 3}) {
		t.Fatalf("主窗口排名错误：%v", got)
	}
}

func TestRestoreClientZOrderUsesExplicitNativeChain(t *testing.T) {
	cells := []ClientCell{{PID: 71}, {PID: 0}, {PID: 35}, {PID: 71}, {PID: 90}, {PID: 88}}
	var calls [][2]uintptr
	count := restoreClientZOrderWith(cells, func(pid uint32) uintptr {
		if pid == 90 {
			return 0
		}
		return uintptr(pid + 1000)
	}, func(hwnd, after, flags uintptr) bool {
		if flags&swpNoMove == 0 || flags&swpNoSize == 0 || flags&swpNoActivate == 0 || flags&swpNoZOrder != 0 {
			t.Fatalf("不应激活或移动：%x", flags)
		}
		calls = append(calls, [2]uintptr{hwnd, after})
		return true
	})
	want := [][2]uintptr{{1088, 0}, {1035, 1088}, {1071, 1035}}
	if count != 3 || !reflect.DeepEqual(calls, want) {
		t.Fatalf("必须从最前到最后建立明确插入链：%d %v", count, calls)
	}
}

func TestRestorePlannedLayersKeepsGeometryAndRetriesNativeMismatch(t *testing.T) {
	cells := []ClientCell{{PID: 1}, {PID: 2}, {PID: 3}}
	rects := []TargetRect{{X: 10, ZOrder: 3}, {X: 20, ZOrder: 1}, {X: 30, ZOrder: 2}}
	ordered := plannedClientZCells(cells, rects)
	if ordered[0].PID != 2 || ordered[1].PID != 3 || ordered[2].PID != 1 || cells[0].PID != 1 || rects[0].X != 10 {
		t.Fatal("层级排序不能改变几何绑定")
	}
	passes, checks := 0, 0
	got := restoreClientZOrderVerifiedWith(ordered, func(pid uint32) uintptr { return uintptr(pid) }, func(hwnd, after, flags uintptr) bool {
		if after == 0 {
			passes++
		}
		return true
	}, func() map[uint32]int {
		checks++
		if checks == 1 {
			return map[uint32]int{1: 1, 2: 2, 3: 3}
		}
		return map[uint32]int{2: 1, 3: 2, 1: 3}
	})
	if got != 3 || passes != 2 {
		t.Fatalf("原生未兑现时必须读取并重建链：%d %d", got, passes)
	}
}

// 使用不可见、无激活的测试窗验证真正的系统Z序；不会读取或移动任何游戏窗口。
func TestRestoreClientZOrderNativeHiddenWindows(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	create := user32dll.NewProc("CreateWindowExW")
	destroy := user32dll.NewProc("DestroyWindow")
	class, _ := syscall.UTF16PtrFromString("STATIC")
	wanted := map[uintptr]uint32{}
	handles := map[uint32]uintptr{}
	for pid := uint32(1); pid <= 3; pid++ {
		hwnd, _, err := create.Call(0, uintptr(unsafe.Pointer(class)), 0, 0x80000000, 0, 0, 40, 40, 0, 0, 0, 0)
		if hwnd == 0 {
			t.Fatalf("创建隐藏测试窗口失败：%v", err)
		}
		defer destroy.Call(hwnd)
		wanted[hwnd] = pid
		handles[pid] = hwnd
	}
	read := func() map[uint32]int {
		clientZEnum.Lock()
		defer clientZEnum.Unlock()
		clientZEnum.wanted, clientZEnum.top = wanted, nil
		procEnumWindows.Call(clientZCallback, 0)
		ranks := clientZRanks(clientZEnum.top, wanted)
		clientZEnum.wanted, clientZEnum.top = nil, nil
		return ranks
	}
	for _, cells := range [][]ClientCell{{{PID: 2}, {PID: 1}, {PID: 3}}, {{PID: 3}, {PID: 2}, {PID: 1}}} {
		moved := restoreClientZOrderVerifiedWith(cells, func(pid uint32) uintptr { return handles[pid] }, func(hwnd, after, flags uintptr) bool {
			ok, _, _ := procSetWindowPos.Call(hwnd, after, 0, 0, 0, 0, flags)
			return ok != 0
		}, read)
		if moved != 3 || !clientZOrderMatches(cells, read()) {
			t.Fatalf("真实原生层级未按计划恢复：moved=%d ranks=%v", moved, read())
		}
	}
}
