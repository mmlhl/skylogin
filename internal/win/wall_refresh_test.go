package win

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestWallUpdatesSeparateControlsAndSources(t *testing.T) {
	base := wallTile{hwnd: 10, pid: 20, label: "账号", thumb: 30, aspect: 1.5, style: wsCaption}
	cases := []struct {
		name              string
		change            func(*wallTile)
		controls, sources bool
	}{
		{"相同观测", func(*wallTile) {}, false, false},
		{"比例测量微差", func(n *wallTile) { n.aspect += 0.0001 }, false, false},
		{"修改标签", func(n *wallTile) { n.label = "新名字" }, true, false},
		{"手动最小化", func(n *wallTile) { n.iconic = true }, false, true},
		{"切换窗口模式", func(n *wallTile) { n.style = wsPopup }, false, true},
		{"缺少缩略图需重连", func(n *wallTile) { n.thumb = 0 }, false, true},
		{"窗口替换", func(n *wallTile) { n.hwnd = 11 }, true, true},
		{"窗口句柄被新进程复用", func(n *wallTile) { n.pid = 21 }, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			next := base
			c.change(&next)
			controls, sources := wallUpdateChanges([]wallTile{base}, []wallTile{next})
			if controls != c.controls || sources != c.sources {
				t.Fatalf("变化分类错误：%v %v", controls, sources)
			}
		})
	}
	minimized := base
	minimized.iconic = true
	minimized.thumb = 0
	if a, b := wallUpdateChanges([]wallTile{minimized}, []wallTile{minimized}); a || b {
		t.Fatal("最小化期间不能重复刷新")
	}
}

// 隐藏原生源窗口保持最小化；反复观测不产生文字重绘，标签改变才产生重绘。
func TestWallObservationDoesNotRestoreOrRepaintUnchangedNative(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := syscall.UTF16PtrFromString("STATIC")
	create := func(style uintptr) uintptr {
		h, _, err := procCreateWinEx.Call(wsExNoActivate, uintptr(unsafe.Pointer(class)), 0, style, 0, 0, 640, 480, 0, 0, 0, 0)
		if h == 0 {
			t.Fatalf("创建隐藏测试窗失败：%v", err)
		}
		return h
	}
	wall := create(wsPopup)
	defer procDestroyWin.Call(wall)
	source := create(wsPopup | 0x20000000)
	defer procDestroyWin.Call(source)
	oldHwnd, oldTiles, oldRatio := wallHwnd, wallTiles, wallRatio
	oldRects := wallRects
	defer func() { wallHwnd, wallTiles, wallRatio, wallRects = oldHwnd, oldTiles, oldRatio, oldRects }()
	wallHwnd, wallTiles = wall, nil
	clients := []WallClient{{Hwnd: source, Pid: 123, Label: "账号", Ratio: 1.5}}
	applyWallUpdate(clients)
	if iconic, _, _ := procIsIconic.Call(source); iconic == 0 {
		t.Fatal("后台布局还原了最小化源窗口")
	}
	oldInvalidate := wallInvalidateControls
	defer func() { wallInvalidateControls = oldInvalidate }()
	paints := 0
	wallInvalidateControls = func(h uintptr) { paints++; oldInvalidate(h) }
	for i := 0; i < 5; i++ {
		applyWallUpdate(clients)
	}
	if paints != 0 {
		t.Fatal("相同观测不应触发控件重画")
	}
	clients[0].Label = "修改标签"
	applyWallUpdate(clients)
	if paints != 1 {
		t.Fatal("标签变化应更新控件")
	}
	if iconic, _, _ := procIsIconic.Call(source); iconic == 0 {
		t.Fatal("标签刷新还原了源窗口")
	}
}
