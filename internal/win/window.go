package win

import (
	"sync/atomic"
	"syscall"
	"unsafe"
)

// 窗口几何与公共句柄：矩形/点类型、窗口位置查询、DWM 属性。
// 本包所有文件共用这里的 DLL 句柄与小类型，不再各自声明。

var (
	user32dll   = syscall.NewLazyDLL("user32.dll")
	kernel32dll = syscall.NewLazyDLL("kernel32.dll")
	shell32dll  = syscall.NewLazyDLL("shell32.dll")
	dwm32       = syscall.NewLazyDLL("dwmapi.dll")
)

var (
	procGetWindowRect         = user32dll.NewProc("GetWindowRect")
	procGetClientRect         = user32dll.NewProc("GetClientRect")
	procGetClassNameW         = user32dll.NewProc("GetClassNameW")
	procDwmSetWindowAttribute = dwm32.NewProc("DwmSetWindowAttribute")
)

// windowRect / point 是 Win32 RECT / POINT 的最小映射，包内通用。
type windowRect struct {
	left   int32
	top    int32
	right  int32
	bottom int32
}

type point struct {
	X int32
	Y int32
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

// WindowAspect 窗口客户区宽高比（监控墙格子布局基准用），量不到返回 0。
func WindowAspect(h uintptr) float64 {
	var cr windowRect
	if ret, _, _ := procGetClientRect.Call(h, uintptr(unsafe.Pointer(&cr))); ret == 0 ||
		cr.right <= cr.left || cr.bottom <= cr.top {
		return 0
	}
	return float64(cr.right-cr.left) / float64(cr.bottom-cr.top)
}

// applyDarkFrame 让标题栏跟随深色主题（Windows 11 还会给圆角）。
func applyDarkFrame(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dark := int32(1)
	// 20 是 Windows 11 / 19045 的属性号，19 是老版本的，两个都设一遍更稳
	procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), 4)
	procDwmSetWindowAttribute.Call(hwnd, 19, uintptr(unsafe.Pointer(&dark)), 4)
	round := int32(2)
	procDwmSetWindowAttribute.Call(hwnd, 33, uintptr(unsafe.Pointer(&round)), 4) // DWMWA_WINDOW_CORNER_PREFERENCE
}
