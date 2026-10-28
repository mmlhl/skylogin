package win

// 鼠标仲裁原语：多开时后台客户端的引擎焦点标志可能停在"我有焦点"，
// 持续把光标拉回自己窗口中心、视角跟着鼠标转。这里提供识别（光标位置 /
// 窗口中心 / 前台窗口）与解除（补发失活消息）所需的 Win32 封装，以及
// 系统焦点事件的实时监听（WinEventHook，无需注入）。仲裁决策在 app 层
// （mousearbiter.go），本文件不含策略。

import (
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	procGetForegroundW = user32dll.NewProc("GetForegroundWindow")
)

// CursorPos 当前光标位置（物理像素）。
func CursorPos() (int32, int32, bool) {
	var pt point
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return 0, 0, false
	}
	return pt.X, pt.Y, true
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
	pt := point{X: w / 2, Y: hh / 2}
	procClientToScreen.Call(h, uintptr(unsafe.Pointer(&pt)))
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

// SleepMs 毫秒级等待（仲裁节奏用）。
func SleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

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

// 系统焦点事件：EVENT_SYSTEM_FOREGROUND / EVENT_SYSTEM_CAPTURESTART /
// EVENT_OBJECT_FOCUS（窗口级）。
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
		const outOfContext = 3 // WINEVENT_OUTOFCONTEXT
		procSetWinEventHook.Call(EvSystemForeground, EvSystemCaptureStart, 0, focusEventCallback, 0, 0, outOfContext)
		procSetWinEventHook.Call(EvObjectFocus, EvObjectFocus, 0, focusEventCallback, 0, 0, outOfContext)
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
	const objidWindow = 0
	const gaRoot = 2
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
