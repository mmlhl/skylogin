package win

// 鼠标仲裁原语：多开时后台客户端可能自认持有鼠标（持续 ClipCursor +
// 把光标拉回自己窗口中心），这里提供识别与解除所需的系统调用封装。
// 仲裁循环在 app 层（mousearbiter.go），本文件只封装 Win32 调用。

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	procClipCursor     = user32dll.NewProc("ClipCursor")
	procGetClipCursorW = user32dll.NewProc("GetClipCursor")
	procGetClassNameW  = user32dll.NewProc("GetClassNameW")
	procGetForegroundW = user32dll.NewProc("GetForegroundWindow")
)

// CursorPos 当前光标位置（物理像素）。
func CursorPos() (int32, int32, bool) {
	var pt trayPoint
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return 0, 0, false
	}
	return pt.X, pt.Y, true
}

// MoveCursor 移动光标（识别"光标被拉回"用的轻推探针）。
func MoveCursor(x, y int32) bool {
	r, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	return r != 0
}

// Rect 屏幕矩形（导出给 app 层仲裁用）。
type Rect struct{ L, T, R, B int32 }

// W/H 宽高。
func (r Rect) W() int32 { return r.R - r.L }
func (r Rect) H() int32 { return r.B - r.T }

// Near 两个矩形近乎重合（游戏 clip 自己窗口时有 1~2px 误差）。
func (r Rect) Near(o Rect) bool {
	tol := int32(4)
	a := func(v, w int32) int32 {
		if v-w < 0 {
			return w - v
		}
		return v - w
	}
	return a(r.L, o.L) <= tol && a(r.T, o.T) <= tol && a(r.R, o.R) <= tol && a(r.B, o.B) <= tol
}

// CoversScreen 矩形盖住整个主屏（= 没有有效的鼠标占用）。
func (r Rect) CoversScreen() bool {
	sw, _, _ := procGetSystemMetrics.Call(0)
	sh, _, _ := procGetSystemMetrics.Call(1)
	return r.W() >= int32(sw)-8 && r.H() >= int32(sh)-8
}

// MouseClipRect 当前系统的鼠标 clip 矩形；没人占用时约等于全屏。
func MouseClipRect() (Rect, bool) {
	var r windowRect
	if ret, _, _ := procGetClipCursorW.Call(uintptr(unsafe.Pointer(&r))); ret == 0 {
		return Rect{}, false
	}
	out := Rect{L: r.left, T: r.top, R: r.right, B: r.bottom}
	return out, out.W() > 0 && out.H() > 0
}

// WindowRect 窗口的屏幕矩形。
func WindowRect(h uintptr) (Rect, bool) {
	var r windowRect
	if ret, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return Rect{}, false
	}
	out := Rect{L: r.left, T: r.top, R: r.right, B: r.bottom}
	return out, out.W() > 0 && out.H() > 0
}

// ReleaseMouseClip 解除鼠标占用（ClipCursor(NULL)）。
func ReleaseMouseClip() {
	procClipCursor.Call(0)
}

// WindowInputCenter 窗口客户区中心的屏幕坐标（游戏拉光标回的就是它）。
func WindowInputCenter(h uintptr) (int32, int32, bool) {
	var cr windowRect
	if ret, _, _ := procGetClientRect.Call(h, uintptr(unsafe.Pointer(&cr))); ret == 0 {
		return 0, 0, false
	}
	w, hh := cr.right-cr.left, cr.bottom-cr.top
	if w <= 0 || hh <= 0 {
		return 0, 0, false
	}
	pt := trayPoint{X: w / 2, Y: hh / 2}
	procClientToScr.Call(h, uintptr(unsafe.Pointer(&pt)))
	return pt.X, pt.Y, true
}

// ForegroundWindow 当前前台窗口及所属进程（只读，不会改变前台）。
func ForegroundWindow() (uintptr, uint32) {
	h, _, _ := procGetForegroundW.Call()
	if h == 0 {
		return 0, 0
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&pid)))
	return h, pid
}

// TopWindowOfPids 返回 pids 名下 Z 序最高的可见顶层窗口（用户正在玩
// 的那个）：EnumWindows 自顶向下枚举，第一个命中受管列表的即是。
func TopWindowOfPids(pids []uint32) uintptr {
	set := make(map[uint32]bool, len(pids))
	for _, p := range pids {
		set[p] = true
	}
	topMu.Lock()
	topSet, topHit = set, 0
	_, _, _ = procEnumWindows.Call(topEnumCallback, 0)
	h := topHit
	topMu.Unlock()
	return h
}

var (
	topMu  sync.Mutex
	topSet map[uint32]bool
	topHit uintptr
)

var topEnumCallback = syscall.NewCallback(func(h, _ uintptr) uintptr {
	if topHit != 0 {
		return 1
	}
	if vis, _, _ := procIsWindowVisible.Call(h); vis == 0 {
		return 1
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&pid)))
	if topSet[pid] {
		topHit = h
		return 0
	}
	return 1
})

// Sleep 毫秒级等待（仲裁节奏用）。
func SleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// WindowAspect 窗口客户区宽高比（格子布局基准用），量不到返回 0。
func WindowAspect(h uintptr) float64 {
	var cr windowRect
	if ret, _, _ := procGetClientRect.Call(h, uintptr(unsafe.Pointer(&cr))); ret == 0 ||
		cr.right <= cr.left || cr.bottom <= cr.top {
		return 0
	}
	return float64(cr.right-cr.left) / float64(cr.bottom-cr.top)
}

// WindowBrief 窗口的类名与客户区尺寸（诊断日志用）。
func WindowBrief(h uintptr) (string, int32, int32) {
	var buf [64]uint16
	procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	class := syscall.UTF16ToString(buf[:])
	var cr windowRect
	if ret, _, _ := procGetClientRect.Call(h, uintptr(unsafe.Pointer(&cr))); ret == 0 {
		return class, 0, 0
	}
	return class, cr.right - cr.left, cr.bottom - cr.top
}

const (
	wmActivateWin = 0x0006
	wmActivateApp = 0x001C
	wmNcActivate  = 0x0086
)

// NotifyInactive 给窗口补发一组"你已失焦"消息。多开几乎同时启动时，
// 后台客户端从未被激活过，等不到失活通知，会一直自认持有鼠标
// （拉光标回中心、转视角）。发一次 WA_INACTIVE 让引擎的焦点标志归位；
// 用户之后真正点击该窗口时，系统照常发激活消息，不影响正常游玩。
func NotifyInactive(h uintptr) {
	procPostMessageW.Call(h, wmActivateWin, 0, 0) // WA_INACTIVE
	procPostMessageW.Call(h, wmActivateApp, 0, 0)
	procPostMessageW.Call(h, wmNcActivate, 0, 0)
}

// 实时焦点事件监听：WinEventHook（WINEVENT_OUTOFCONTEXT，无需注入）。
// 系统在任何窗口请求焦点 / 开始鼠标捕获 / 前台切换的瞬间回调我们。
const (
	evSystemForeground   = 0x0003
	evSystemCaptureStart = 0x000A
	evObjectFocus        = 0x8005
	objidWindow          = 0
	gaRoot               = 2
)

// 系统焦点事件（导出给 app 层的仲裁过滤用）。
const (
	EvSystemForeground   = 0x0003
	EvSystemCaptureStart = 0x000A
	EvObjectFocus        = 0x8005
)

var (
	procSetWinEventHook = user32dll.NewProc("SetWinEventHook")
	procGetAncestorW    = user32dll.NewProc("GetAncestor")
)

var focusEventCallback = syscall.NewCallback(focusEventProc)
var focusEventHandler atomic.Value // func(event uint32, root, pid uintptr)

// StartFocusWatch 安装焦点事件钩子并常驻消息泵（进程生命周期内一次）。
// 事件到达时回调 onEvent(event, 根窗口, 进程 pid)；回调必须快（钩子线程）。
func StartFocusWatch(onEvent func(event uint32, root, pid uintptr)) {
	focusEventHandler.Store(onEvent)
	go func() {
		runtime.LockOSThread()
		procSetWinEventHook.Call(evSystemForeground, evSystemCaptureStart, 0, focusEventCallback, 0, 0, 3) // WINEVENT_OUTOFCONTEXT
		procSetWinEventHook.Call(evObjectFocus, evObjectFocus, 0, focusEventCallback, 0, 0, 3)
		var msg [7]uintptr
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
		}
	}()
}

// focusEventProc 钩子回调：只透传窗口级事件（OBJID_WINDOW），先取根窗口
// （弹窗/骨架都是 owned 窗口，归属主窗）和 pid，处理交给 app 层。
func focusEventProc(hHook, event, hwnd, idObject, idChild, thread, time uintptr) uintptr {
	if idObject != objidWindow || hwnd == 0 {
		return 0
	}
	if f, ok := focusEventHandler.Load().(func(uint32, uintptr, uintptr)); ok && f != nil {
		root, _, _ := procGetAncestorW.Call(hwnd, gaRoot)
		var pid uint32
		procGetWindowThreadProcessID.Call(root, uintptr(unsafe.Pointer(&pid)))
		f(uint32(event), root, uintptr(pid))
	}
	return 0
}
