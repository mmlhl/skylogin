package win

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
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
//
// 只在**系统是浅色主题**时才去建议 DWM 画深色标题栏。系统本来就是深色
// 主题的话什么都不做：标题栏由系统自己画成深色，拖动 / 激活切换时也
// 不会变——这正是要的稳定行为。
//
// 为什么不能一律强制：DWMWA_USE_IMMERSIVE_DARK_MODE 对 DWM 只是建议，
// 窗口进入移动/尺寸循环、激活状态切换时它会把非客户区重算一遍，建议
// 被丢掉，标题栏瞬间回落成系统浅色，看着就像"反白"。没有 API 能锁住，
// 在消息里反复重设只是追着补。
func applyDarkFrame(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	// Win11 的圆角是独立的，深色浅色都设
	round := int32(2)
	procDwmSetWindowAttribute.Call(hwnd, 33, uintptr(unsafe.Pointer(&round)), 4) // DWMWA_WINDOW_CORNER_PREFERENCE
	if SystemDarkTheme() {
		return
	}
	dark := int32(1)
	// 20 是 Windows 11 / 19045 的属性号，19 是老版本的，两个都设一遍更稳
	procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), 4)
	procDwmSetWindowAttribute.Call(hwnd, 19, uintptr(unsafe.Pointer(&dark)), 4)
}

// SystemDarkTheme 报告系统（应用）主题是否深色。
//
// 读注册表并缓存：主题切换要重启应用才生效，运行期不会变。
// 读不到（老系统 / 权限问题）按浅色处理——那正是需要强制深色的情况。
var (
	darkThemeOnce sync.Once
	darkThemeVal  bool
)

func SystemDarkTheme() bool {
	darkThemeOnce.Do(func() {
		k, err := registry.OpenKey(registry.CURRENT_USER,
			`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
		if err != nil {
			return
		}
		defer k.Close()
		// AppsUseLightTheme：1 = 浅色，0 = 深色
		if v, _, err := k.GetIntegerValue("AppsUseLightTheme"); err == nil {
			darkThemeVal = v == 0
		}
	})
	return darkThemeVal
}

// ---- 无边框主窗口 ----
//
// 系统标题栏的深浅由 DWM 决定：系统是浅色主题、界面固定深色时，只能靠
// DWMWA_USE_IMMERSIVE_DARK_MODE 去"建议"，而 DWM 在拖动 / 激活切换时会
// 把它丢掉，标题栏瞬间反白。Win10 没有 DWMWA_CAPTION_COLOR（Win11 才有），
// 没有 API 能锁住。
//
// 所以主界面不要系统标题栏：前端画一条 36px 的标题条（可拖动、带窗口按钮），
// 窗口本身去 caption。三个配套缺一不可：
//   WM_NCCALCSIZE 返回 0 —— 否则 WS_THICKFRAME 会画一圈系统浅色边框
//   WM_NCHITTEST 判边缘 —— 否则拖边不能缩放
//   WM_GETMINMAXINFO 用工作区 —— 否则最大化会盖住任务栏
var (
	comctl32dll           = syscall.NewLazyDLL("comctl32.dll")
	procSetWindowSubclass = comctl32dll.NewProc("SetWindowSubclass")
	procDefSubclassProc   = comctl32dll.NewProc("DefSubclassProc")
	procGetAsyncKeyState  = user32dll.NewProc("GetAsyncKeyState")
	// procGetCursorPos 在 tray.go 已有声明（同一包）
)

const (
	wmNcLButtonDown = 0x00A1
	wsMinimizeBox   = 0x00020000
	wsMaximizeBox   = 0x00010000
	vkLButton       = 0x01
)

// DisableNonClientFrame 让 DWM 不再绘制窗口的非客户区边框。
//
// Win10 上即使 WM_NCCALCSIZE 返回 0（客户区占满窗口），DWM 仍会给
// WS_THICKFRAME 的窗口描一圈 1px 边框：浅色主题是白线、深色主题是灰线，
// 压在深色标题栏上方就是一条"内容没贴顶"的错位感。Win11 可以用
// DWMWA_BORDER_COLOR 指定颜色，Win10 没有，只能整个关掉非客户区渲染。
// 代价：Win11 的圆角也跟着没了（阴影仍由 DWM 单独负责）。
func DisableNonClientFrame(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	noNc := int32(1) // DWMNCRP_DISABLED
	procDwmSetWindowAttribute.Call(hwnd, 2 /*DWMWA_NCRENDERING_POLICY*/, uintptr(unsafe.Pointer(&noNc)), 4)
}

// MakeBorderless 去掉系统标题栏，改由前端自绘（调用一次即可）。
func MakeBorderless(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	style, _, _ := procGetWindowLongW.Call(hwnd, gwlStyle)
	// 去掉 WS_CAPTION（= WS_BORDER | WS_DLGFRAME）：系统不再画标题栏。
	// 保留 WS_THICKFRAME：仍有 Aero 阴影，边缘缩放由下面的 hit-test 负责。
	next := (style &^ wsCaption) | wsMinimizeBox | wsMaximizeBox
	_, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlStyle, next)
	_, _, _ = procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
	// 让 DWM 别画非客户区边框：Win10 上 WS_THICKFRAME 的无边框窗口仍会被
	// 画一圈 1px 深色边框，压在深色标题栏上方就是一条灰线，看着像内容没贴顶。
	// Win11 有 DWMWA_BORDER_COLOR 可以指定颜色，Win10 只能整个关掉。
	DisableNonClientFrame(hwnd)
	// 用子类化而不是替换 WNDPROC：WebView2 库自己也处理这个窗口的消息，
	// 替换会把它的链断掉。
	procSetWindowSubclass.Call(hwnd, borderlessCb, 0x424F5244, 0)
	// 让库按新的客户区重新摆 WebView 子窗口。
	// 库是在建窗时按"有标题栏"的客户区算的尺寸，caption 去掉后窗口大了
	// 一圈，子窗口没跟着长，右侧和底部会露出一条窗口底色（白边）。
	var cr windowRect
	if ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr))); ret != 0 {
		lp := uintptr(uint32(cr.right)&0xFFFF | uint32(cr.bottom)<<16)
		procPostMessageW.Call(hwnd, wmSizeWin, 0 /*SIZE_RESTORED*/, lp)
	}
}

var borderlessCb = syscall.NewCallback(func(hwnd, msg, wparam, lparam, idSubclass, refData uintptr) uintptr {
	switch msg {
	case wmNcCalcSize:
		if wparam != 0 {
			return 0 // 客户区占满整个窗口：把 THICKFRAME 的边框也去掉
		}

	case wmGetMinMax:
		mmi := (*minMaxInfo)(unsafe.Pointer(lparam))
		wa := primaryWorkArea()
		mmi.ptMaxPosition = point{X: wa.left, Y: wa.top}
		mmi.ptMaxSize = point{X: wa.right - wa.left, Y: wa.bottom - wa.top}
		return 0
	case wmNcHitTest:
		if ht := borderHitTest(hwnd, lparam); ht != 0 {
			return ht
		}
	}
	r, _, _ := procDefSubclassProc.Call(hwnd, msg, wparam, lparam)
	return r
})

// borderHitTest 只判断边缘（缩放）。标题栏拖动由前端调 StartWindowDrag 触发——
// WebView2 的客户区被 Chromium 吃掉鼠标事件，页面里的 mousedown 变不成
// 非客户区点击，所以那条路必须由页面发信号。
func borderHitTest(hwnd, lparam uintptr) uintptr {
	if z, _, _ := procIsZoomed.Call(hwnd); z != 0 {
		return 0 // 最大化时不该有边缘缩放
	}
	pt := point{X: int32(int16(lparam & 0xFFFF)), Y: int32(int16((lparam >> 16) & 0xFFFF))}
	procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	var cr windowRect
	if ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr))); ret == 0 {
		return 0
	}
	b := int32(resizeBorder)
	left, right := pt.X < b, pt.X >= cr.right-b
	top, bottom := pt.Y < b, pt.Y >= cr.bottom-b
	switch {
	case top && left:
		return htTopLeft
	case top && right:
		return htTopRight
	case bottom && left:
		return htBottomLeft
	case bottom && right:
		return htBottomRight
	case left:
		return htLeft
	case right:
		return htRight
	case top:
		return htTop
	case bottom:
		return htBottom
	}
	return 0
}

// StartWindowDrag 开始拖动主窗口（前端标题条 mousedown 时调）。
//
// 为什么不用标准的 ReleaseCapture + WM_NCLBUTTONDOWN(HTCAPTION)：
// 鼠标捕获在 Chromium 的子窗口上（mousedown 发生在页面里），捕获属于
// WebView2 进程，宿主这边 ReleaseCapture 够不着；把 NCLBUTTONDOWN 送进
// DefWindowProc 后，系统的移动循环收不到鼠标移动消息，窗口就"拖不动"。
//
// 所以自己跟光标：记下光标相对窗口的偏移，之后每 8ms 读一次光标位置、
// SetWindowPos 跟过去，直到左键松开。代价是没有 Aero Snap（贴边分屏），
// 换来的是不依赖任何捕获状态，WebView2 里也能稳定工作。
var dragBusy atomic.Bool

func StartWindowDrag() {
	h := atomic.LoadUintptr(&mainWindowHwnd)
	if h == 0 || !dragBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer dragBusy.Store(false)

		// 最大化时先还原（和系统标题栏的行为一致），再按还原后的位置算偏移
		if z, _, _ := procIsZoomed.Call(h); z != 0 {
			procShowWindow.Call(h, swRestore)
			time.Sleep(60 * time.Millisecond)
		}

		var pt point
		if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
			return
		}
		var wr windowRect
		if r, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&wr))); r == 0 {
			return
		}
		dx, dy := pt.X-wr.left, pt.Y-wr.top

		for {
			time.Sleep(8 * time.Millisecond)
			// 左键松了就结束（高位 0x8000 = 当前按下）
			if k, _, _ := procGetAsyncKeyState.Call(vkLButton); k&0x8000 == 0 {
				return
			}
			if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
				return
			}
			procSetWindowPos.Call(h, 0,
				uintptr(pt.X-dx), uintptr(pt.Y-dy), 0, 0,
				swpNoSize|swpNoZOrder|swpNoActivate)
		}
	}()
}

// ControlMainWindow 执行窗口按钮动作：min / max（含还原）/ close。
func ControlMainWindow(action string) error {
	h := atomic.LoadUintptr(&mainWindowHwnd)
	if h == 0 {
		return errors.New("主窗口还没创建")
	}
	switch action {
	case "min":
		procShowWindow.Call(h, swMinimize)
	case "max":
		if z, _, _ := procIsZoomed.Call(h); z != 0 {
			procShowWindow.Call(h, swRestore)
		} else {
			procShowWindow.Call(h, swMaximize)
		}
	case "close":
		procPostMessageW.Call(h, wmClose, 0, 0)
	default:
		return fmt.Errorf("未知的窗口动作: %s", action)
	}
	return nil
}

// MainWindowIsZoomed 报告主窗口是否最大化（前端按钮图标切换用）。
func MainWindowIsZoomed() bool {
	h := atomic.LoadUintptr(&mainWindowHwnd)
	if h == 0 {
		return false
	}
	z, _, _ := procIsZoomed.Call(h)
	return z != 0
}
