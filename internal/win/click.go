package win

// 用系统级鼠标事件点窗口里的某个位置。
//
// 为什么需要：网页对登录按钮做了可信事件校验（event.isTrusted），
// 脚本里 dispatchEvent 合成的事件一律被忽略。但 SendInput 产生的是
// 系统级输入，页面看来和真人点击完全一样。

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32Click         = windows.NewLazySystemDLL("user32.dll")
	procSetCursorPos    = user32Click.NewProc("SetCursorPos")
	procClientToScreen  = user32Click.NewProc("ClientToScreen")
	procMouseEvent      = user32Click.NewProc("mouse_event")
	procShowWindowClick = user32Click.NewProc("ShowWindow")
)

const (
	swRestoreWin       = 9
	mouseEventLeftDown = 0x0002
	mouseEventLeftUp   = 0x0004
)

type point struct {
	X int32
	Y int32
}

// ClickClientPoint 在窗口客户区的 (x, y) 处做一次真实左键点击。
//
// x/y 是 CSS 像素（页面坐标系），调用方需要已经乘过缩放比。
func ClickClientPoint(hwnd uintptr, x, y int32) {
	if hwnd == 0 {
		return
	}
	// 先把窗口提到前台，否则点击会落到别的窗口上
	_, _, _ = procShowWindowClick.Call(hwnd, swRestoreWin)
	_, _, _ = procSetForeground.Call(hwnd)
	time.Sleep(150 * time.Millisecond)

	pt := point{X: x, Y: y}
	_, _, _ = procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	_, _, _ = procSetCursorPos.Call(uintptr(pt.X), uintptr(pt.Y))
	time.Sleep(60 * time.Millisecond)
	// 先移过去再按，模拟真人
	_, _, _ = procMouseEvent.Call(mouseEventLeftDown, 0, 0, 0, 0)
	time.Sleep(40 * time.Millisecond)
	_, _, _ = procMouseEvent.Call(mouseEventLeftUp, 0, 0, 0, 0)
}
