package win

import (
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

// 只创建隐藏测试窗，验证游戏同样置顶时墙仍在最前，不碰正在运行的游戏。
func TestWallPinAboveTopmostClientsNative(t *testing.T) {
	runtime.LockOSThread()
	var handles []uintptr
	defer func() {
		for _, h := range handles {
			procDestroyWin.Call(h)
		}
		runtime.UnlockOSThread()
	}()
	class, _ := syscall.UTF16PtrFromString("STATIC")
	create := func() uintptr {
		h, _, err := procCreateWinEx.Call(wsExTopmost|wsExNoActivate, uintptr(unsafe.Pointer(class)), 0, wsPopup,
			20, 30, 80, 60, 0, 0, 0, 0)
		if h == 0 {
			t.Fatalf("创建隐藏测试窗失败：%v", err)
		}
		handles = append(handles, h)
		return h
	}
	wall, a, b := create(), create(), create()
	foreground, _, _ := user32dll.NewProc("GetForegroundWindow").Call()
	var before windowRect
	procGetWindowRect.Call(wall, uintptr(unsafe.Pointer(&before)))
	wanted := map[uintptr]uint32{wall: 9, a: 1, b: 2}
	read := func() map[uint32]int {
		clientZEnum.Lock()
		defer clientZEnum.Unlock()
		clientZEnum.wanted, clientZEnum.top = wanted, nil
		procEnumWindows.Call(clientZCallback, 0)
		ranks := clientZRanks(clientZEnum.top, wanted)
		clientZEnum.wanted, clientZEnum.top = nil, nil
		return ranks
	}
	defer wallTrackingMenu.Store(0)
	for _, cells := range [][]ClientCell{{{PID: 1}, {PID: 2}}, {{PID: 2}, {PID: 1}}} {
		restoreClientZOrderWith(cells, func(pid uint32) uintptr {
			if pid == 1 {
				return a
			}
			return b
		}, func(h, after, flags uintptr) bool {
			ok, _, _ := procSetWindowPos.Call(h, after, 0, 0, 0, 0, flags)
			return ok != 0
		})
		// 模拟菜单的模态循环收到后台刷新：不能重新提墙，遮住或取消弹出菜单。
		menuRanks := read()
		if menuRanks[9] == 3 {
			t.Fatal("测试前提错误：游戏应已被提到墙前方")
		}
		wallTrackingMenu.Store(wall)
		pinWallAboveClients(wall)
		if got := read(); !reflect.DeepEqual(got, menuRanks) {
			t.Fatalf("菜单展开时不得改变拥有者层级：before=%v after=%v", menuRanks, got)
		}
		wallTrackingMenu.Store(0)
		pinWallAboveClients(wall)
		ranks := read()
		if ranks[9] != 3 || ranks[cells[0].PID] >= ranks[cells[1].PID] {
			t.Fatalf("监控墙必须在游戏之上，并保留游戏内部顺序：%v", ranks)
		}
		style, _, _ := procGetWindowLongW.Call(wall, gwlExStyle)
		var after windowRect
		procGetWindowRect.Call(wall, uintptr(unsafe.Pointer(&after)))
		now, _, _ := user32dll.NewProc("GetForegroundWindow").Call()
		if style&wsExTopmost == 0 || before != after || now != foreground {
			t.Fatalf("置顶不得改变几何或前台：style=%x before=%v after=%v", style, before, after)
		}
	}
	pinWallAboveClients(0)
}
