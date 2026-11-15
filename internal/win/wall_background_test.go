package win

import (
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// 所有原生窗口均由测试线程创建并销毁，不读取或修改游戏和其他用户窗口。
func setupWallBackgroundNativeTest(t *testing.T) func(uintptr, bool) uintptr {
	t.Helper()
	runtime.LockOSThread()
	oldWatching := wallBackgroundWatching.Load()
	wallBackgroundMu.Lock()
	oldStates := wallBackground
	wallBackground = make(map[uintptr]wallBackgroundWindow)
	wallBackgroundMu.Unlock()
	wallBackgroundWatching.Store(true)
	var handles []uintptr
	t.Cleanup(func() {
		cleanupWallBackground()
		for _, h := range handles {
			procDestroyWin.Call(h)
		}
		wallBackgroundMu.Lock()
		wallBackground = oldStates
		wallBackgroundMu.Unlock()
		wallBackgroundWatching.Store(oldWatching)
		runtime.UnlockOSThread()
	})
	return func(ex uintptr, maximized bool) uintptr {
		t.Helper()
		class, _ := syscall.UTF16PtrFromString("STATIC")
		style := uintptr(wsPopup)
		if maximized {
			style |= 0x01000000
		}
		h, _, err := procCreateWinEx.Call(ex, uintptr(unsafe.Pointer(class)), 0, style, 320, 210, 640, 480, 0, 0, 0, 0)
		if h == 0 {
			t.Fatalf("创建自有隐藏测试窗失败：%v", err)
		}
		handles = append(handles, h)
		// 从隐藏正常或最大化状态直接进入最小化，测试准备阶段不铺开可见窗口。
		procShowWindow.Call(h, 7)
		if iconic, _, _ := procIsIconic.Call(h); iconic == 0 {
			t.Fatal("测试源未进入真正最小化")
		}
		return h
	}
}

func nativeBackgroundPlacement(t *testing.T, h uintptr) windowPlacement {
	t.Helper()
	p := windowPlacement{length: uint32(unsafe.Sizeof(windowPlacement{}))}
	if ok, _, _ := procGetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&p))); ok == 0 {
		t.Fatal("读取原始位置失败")
	}
	return p
}

func nativeBackgroundLayer(t *testing.T, h uintptr) (uint32, byte, uint32) {
	t.Helper()
	var color, flags uint32
	var alpha byte
	if ok, _, _ := wallGetLayered.Call(h, uintptr(unsafe.Pointer(&color)), uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flags))); ok == 0 {
		t.Fatal("读取透明属性失败")
	}
	return color, alpha, flags
}

func TestWallBackgroundNativeConvertsAndReveals(t *testing.T) {
	create := setupWallBackgroundNativeTest(t)
	h := create(wsExToolWindow, false)
	originalStyle, _, _ := procGetWindowLongW.Call(h, gwlExStyle)
	original := nativeBackgroundPlacement(t, h)
	foreground, _ := ForegroundWindow()
	if !backgroundWallWindow(h, wallWindowPID(h)) {
		t.Fatal("后台收起失败")
	}
	if iconic, _, _ := procIsIconic.Call(h); iconic != 0 {
		t.Fatal("后台收起仍处于系统最小化")
	}
	style, _, _ := procGetWindowLongW.Call(h, gwlExStyle)
	if style&wallBackgroundStyle != wallBackgroundStyle {
		t.Fatalf("未设置透明、穿透和无激活属性：%x", style)
	}
	_, alpha, flags := nativeBackgroundLayer(t, h)
	if alpha != 0 || flags != 2 {
		t.Fatalf("后台收起并非全透明：alpha=%d flags=%d", alpha, flags)
	}
	var r windowRect
	procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	if r != original.rcNormal {
		t.Fatalf("后台收起不应移动原位置：原=%v 实际=%v", original.rcNormal, r)
	}
	if now, _ := ForegroundWindow(); now != foreground {
		t.Fatal("后台收起改变前台窗口")
	}
	revealBackgroundWindow(h)
	restored, _, _ := procGetWindowLongW.Call(h, gwlExStyle)
	if restored != originalStyle {
		t.Fatalf("显式展开未恢复原扩展样式：原=%x 实际=%x", originalStyle, restored)
	}
	if _, ok := backgroundWindowState(h); ok {
		t.Fatal("显式展开后仍残留后台状态")
	}
	if now, _ := ForegroundWindow(); now != foreground {
		t.Fatal("只恢复样式不应激活窗口")
	}
}

func TestWallBackgroundNativeCleanupRestoresMinimizedPlacement(t *testing.T) {
	for _, maximized := range []bool{false, true} {
		name := "正常来源"
		if maximized {
			name = "最大化来源"
		}
		t.Run(name, func(t *testing.T) {
			create := setupWallBackgroundNativeTest(t)
			h := create(wsExToolWindow, maximized)
			original := nativeBackgroundPlacement(t, h)
			if maximized && original.flags&2 == 0 {
				t.Fatal("最大化来源未保留恢复到最大化标记")
			}
			originalStyle, _, _ := procGetWindowLongW.Call(h, gwlExStyle)
			if !backgroundWallWindow(h, wallWindowPID(h)) {
				t.Fatal("后台收起失败")
			}
			cleanupWallBackground()
			if iconic, _, _ := procIsIconic.Call(h); iconic == 0 {
				t.Fatal("清理未恢复真正最小化")
			}
			restoredStyle, _, _ := procGetWindowLongW.Call(h, gwlExStyle)
			if restoredStyle != originalStyle {
				t.Fatalf("清理遗留透明样式：原=%x 实际=%x", originalStyle, restoredStyle)
			}
			restored := nativeBackgroundPlacement(t, h)
			if restored.rcNormal != original.rcNormal || restored.flags&2 != original.flags&2 {
				t.Fatalf("清理丢失原位置或最大化标记：原=%+v 实际=%+v", original, restored)
			}
			if _, ok := backgroundWindowState(h); ok {
				t.Fatal("清理后仍残留记录")
			}
		})
	}
}

func TestWallBackgroundNativePreservesExistingLayerAttributes(t *testing.T) {
	create := setupWallBackgroundNativeTest(t)
	h := create(wsExToolWindow|wallExLayered, false)
	const originalColor uint32 = 0x00112233
	const originalAlpha byte = 137
	const originalFlags uint32 = 3
	if ok, _, _ := wallSetLayered.Call(h, uintptr(originalColor), uintptr(originalAlpha), uintptr(originalFlags)); ok == 0 {
		t.Fatal("设置测试透明属性失败")
	}
	originalStyle, _, _ := procGetWindowLongW.Call(h, gwlExStyle)
	if !backgroundWallWindow(h, wallWindowPID(h)) {
		t.Fatal("已有分层窗口收起失败")
	}
	revealBackgroundWindow(h)
	color, alpha, flags := nativeBackgroundLayer(t, h)
	if color != originalColor || alpha != originalAlpha || flags != originalFlags {
		t.Fatalf("展开丢失原分层属性：color=%x alpha=%d flags=%d", color, alpha, flags)
	}
	style, _, _ := procGetWindowLongW.Call(h, gwlExStyle)
	if style != originalStyle {
		t.Fatalf("原分层样式未完整恢复：原=%x 实际=%x", originalStyle, style)
	}
}

func TestWallBackgroundNativeIgnoresDisabledAndWrongIdentity(t *testing.T) {
	create := setupWallBackgroundNativeTest(t)
	h := create(wsExToolWindow, false)
	originalStyle, _, _ := procGetWindowLongW.Call(h, gwlExStyle)
	pid := wallWindowPID(h)
	wallBackgroundWatching.Store(false)
	if backgroundWallWindow(h, pid) {
		t.Fatal("未监听时不应转换窗口")
	}
	wallBackgroundWatching.Store(true)
	if backgroundWallWindow(h, pid+1) {
		t.Fatal("进程身份不符不应转换窗口")
	}
	wallBackgroundMu.Lock()
	wallBackground[h] = wallBackgroundWindow{pid: pid + 1, exStyle: wallBackgroundStyle}
	wallBackgroundMu.Unlock()
	revealBackgroundWindow(h)
	if _, ok := backgroundWindowState(h); ok {
		t.Fatal("失效身份应从记录移除")
	}
	wallBackgroundMu.Lock()
	wallBackground[h] = wallBackgroundWindow{pid: pid + 1, exStyle: wallBackgroundStyle}
	wallBackgroundMu.Unlock()
	cleanupWallBackground()
	style, _, _ := procGetWindowLongW.Call(h, gwlExStyle)
	if style != originalStyle {
		t.Fatalf("错误身份恢复修改了其他窗口样式：原=%x 实际=%x", originalStyle, style)
	}
	if iconic, _, _ := procIsIconic.Call(h); iconic == 0 {
		t.Fatal("错误身份恢复改变了最小化状态")
	}
}

// 使用真实 WinEvent 及墙消息路由验证一次性最小化事件，不靠周期观测驱动还原。
func TestWallBackgroundNativeMinimizeEvent(t *testing.T) {
	create := setupWallBackgroundNativeTest(t)
	source := create(wsExNoActivate, false)
	wall := create(wsExNoActivate, false)
	oldHwnd, oldTiles, oldTasks := wallHwnd, wallTiles, wallTasks
	defer func() { wallHwnd, wallTiles, wallTasks = oldHwnd, oldTiles, oldTasks }()
	wallHwnd, wallTiles, wallTasks = wall, []wallTile{{hwnd: source, pid: wallWindowPID(source)}}, make(chan func(), 8)
	stop := startWallBackgroundWatch()
	defer stop()
	// 测试窗先透明，准备阶段也不把自有空白窗口铺到桌面。
	procSetWindowLongPtrW.Call(source, gwlExStyle, wsExNoActivate|wallExLayered|wallExTransparent)
	wallSetLayered.Call(source, 0, 0, 2)
	procShowWindow.Call(source, 4)
	procShowWindow.Call(source, 7)
	peek := user32dll.NewProc("PeekMessageW")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var msg trayMsg
		for {
			ok, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1)
			if ok == 0 {
				break
			}
			if msg.Message == uint32(wmWallBackgroundEvent) {
				handleWallBackgroundEvent(uintptr(msg.WParam), uintptr(msg.LParam))
			} else if msg.Message == uint32(wmWallTask) {
				for len(wallTasks) > 0 {
					(<-wallTasks)()
				}
			} else {
				procTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
				procDispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
			}
		}
		if _, ok := backgroundWindowState(source); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("最小化事件未转换为后台收起")
}

func TestWallBackgroundNativeIgnoresStaleForegroundEvent(t *testing.T) {
	create := setupWallBackgroundNativeTest(t)
	source := create(wsExNoActivate, false)
	stale := create(wsExNoActivate, false)
	wallBackgroundMu.Lock()
	wallBackground[source] = wallBackgroundWindow{pid: wallWindowPID(source), restoring: true}
	wallBackgroundMu.Unlock()
	handleWallBackgroundEvent(EvSystemForeground, stale)
	s, ok := backgroundWindowState(source)
	if !ok || !s.restoring {
		t.Fatal("排队过期的前台事件不能清除自恢复阶段")
	}
	// 这是手工构造的阶段记录，不交给清理恢复其空placement。
	wallBackgroundMu.Lock()
	delete(wallBackground, source)
	wallBackgroundMu.Unlock()
}
