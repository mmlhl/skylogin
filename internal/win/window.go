package win

import (
	"sync/atomic"
	"syscall"
	"unsafe"
)

// 取窗口矩形（用于把网易登录窗摆到主界面中间）。
var procGetWindowRect = user32dll.NewProc("GetWindowRect")

type windowRect struct {
	left   int32
	top    int32
	right  int32
	bottom int32
}

// WindowCenterByTitle 返回某个窗口的中心点（屏幕坐标）。
//
// 用途：网易官方登录窗是以宿主窗口为中心弹出的，所以宿主得先放到
// 主界面的中心，登录窗才会出现在用户视线里，而不是屏幕左上角。
func WindowCenterByTitle(title string) (x, y int, ok bool) {
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0, 0, false
	}
	h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(t)))
	if h == 0 {
		return 0, 0, false
	}
	var r windowRect
	if ret, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return 0, 0, false
	}
	if r.right <= r.left || r.bottom <= r.top {
		return 0, 0, false
	}
	// 托盘图标的 tooltip 窗口标题也是这个字符串（0,0 136×38），
	// 不卡尺寸的话会把它当成主窗口，弹窗就被算到左上角去了。
	if r.right-r.left < 400 || r.bottom-r.top < 300 {
		return 0, 0, false
	}
	return int((r.left + r.right) / 2), int((r.top + r.bottom) / 2), true
}

// mainWindowHwnd 是主界面（WebView2）窗口句柄，由 OpenWebView 写进来。
// 用它算中心比按标题找窗口可靠——标题可能和托盘/隐藏窗口撞车。
var mainWindowHwnd uintptr

// MainWindowCenter 返回主界面窗口的中心点（屏幕坐标）。
func MainWindowCenter() (x, y int, ok bool) {
	h := atomic.LoadUintptr(&mainWindowHwnd)
	if h == 0 {
		return 0, 0, false
	}
	var r windowRect
	if ret, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return 0, 0, false
	}
	// 最小化时 Windows 会把窗口挪到 -32000，这种位置不能用
	if r.right <= r.left || r.bottom <= r.top || r.left < -10000 || r.top < -10000 {
		return 0, 0, false
	}
	return int((r.left + r.right) / 2), int((r.top + r.bottom) / 2), true
}
