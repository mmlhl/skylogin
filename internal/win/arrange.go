package win

// 客户端窗口排列：把若干进程的主窗口摆进主屏工作区的网格里。
//
// 为什么只用正方形网格（2×2 / 3×3 / 4×4）：16:9 的屏幕被均分成
// n×n 的正方形网格时，每一格恰好也是 16:9，游戏窗口不变形；
// 而"左右分屏"这类切法会把窗口压成 8:9，画面比例就毁了。
//
// 摆完之后会量一次实际尺寸再微调位置：游戏有最小窗口尺寸，
// MoveWindow 给的格子如果比它小，窗口会被引擎自己撑大、压到隔壁格子，
// 按实际尺寸重排一遍可以保证互不重叠（代价是可能超出屏幕边缘）。

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"strings"
)

var (
	procEnumWindows              = user32dll.NewProc("EnumWindows")
	procIsWindowVisible          = user32dll.NewProc("IsWindowVisible")
	procGetWindowThreadProcessID = user32dll.NewProc("GetWindowThreadProcessId")
	procGetWindowLongW           = user32dll.NewProc("GetWindowLongW")
	procMoveWindow               = user32dll.NewProc("MoveWindow")
	procSystemParametersInfoW    = user32dll.NewProc("SystemParametersInfoW")
	procGetSystemMetrics         = user32dll.NewProc("GetSystemMetrics")
	procKeybdEvent               = user32dll.NewProc("keybd_event")
	procAttachThreadInput        = user32dll.NewProc("AttachThreadInput")
	procSetFocus                 = user32dll.NewProc("SetFocus")
	procGetCurrentThreadId       = kernel32dll.NewProc("GetCurrentThreadId")
	procSetWindowTextW           = user32dll.NewProc("SetWindowTextW")
	// Win10 1607+；老系统上拿不到就按原样（虚拟坐标）工作
	procSetThreadDpiAwarenessContext = user32dll.NewProc("SetThreadDpiAwarenessContext")
	procMonitorFromWindow            = user32dll.NewProc("MonitorFromWindow")
	procGetMonitorInfoW              = user32dll.NewProc("GetMonitorInfoW")
)

const (
	// GWL_EXSTYLE 取扩展样式（GetWindowLongW 的参数是无符号透传，取补码形式）
	gwlExStyle     = ^uintptr(19) // -20
	gwlStyle       = ^uintptr(15) // -16
	wsExToolWindow = 0x80
	wsCaption      = 0x00C00000
	wsPopup        = 0x80000000
	wsMaximize     = 0x01000000
	wsThickFrame   = 0x00040000
	wsSysMenu      = 0x00080000
	spiGetWorkArea = 0x0048

	// SetWindowPos 的开关位：只改样式、不动位置尺寸（见 forceWindowedStyle）。
	// swpNoSize / swpNoZOrder / swpNoActivate 在 webview.go 里已有定义。
	swpNoMove       = 0x0002
	swpFrameChanged = 0x0020

	vkMenu     = 0x12 // ALT
	vkReturn   = 0x0D // ENTER
	keyEventUp = 0x0002
	smCXScreen = 0
	smCYScreen = 1
)

// ClientCell 一个待排列的客户端：进程 + 想显示的窗口标题（一般是账号名）。
type ClientCell struct {
	PID   uint32
	Label string
}

// Diag 诊断输出钩子：app 层接到日志里（win 层不依赖日志实现）。
//
// 排列、切全屏这些动作发生在用户看不见的地方，失败了只会表现为
// "窗口纹丝不动"或"还是全屏"，光看结果分不清卡在哪一步。默认丢弃。
var Diag = func(string) {}

// monitorInfo 是 GetMonitorInfoW 的入参/出参（只需要工作区矩形）。
type monitorInfo struct {
	cbSize            uint32
	rcMonitor, rcWork windowRect
	dwFlags           uint32
}

// physicalPixels 让当前线程临时切换到"物理像素"感知（Per-Monitor-V2）。
//
// 启动器进程整体是 DPI 不感知的：在 125%/150% 缩放的屏幕上，它量工作区
// 和自家窗口得到的是被系统缩小的虚拟坐标；而游戏窗口（DPI 感知）的
// GetWindowRect 返回的是物理坐标。两套坐标混着算，"量出来的实际尺寸"
// 永远和预期对不上，二次校准就会用错误的基准乱排。排列期间切到物理
// 像素空间，量的和摆的就都在同一套坐标系里了；只影响当前线程，界面的
// 缩放不受影响。老系统没有这个 API 时按原样工作（返回 ok=false）。
type physicalPixels struct{ old uintptr }

func enterPhysicalPixels() (physicalPixels, bool) {
	const perMonitorAwareV2 = ^uintptr(3) // -4
	old, _, _ := procSetThreadDpiAwarenessContext.Call(perMonitorAwareV2)
	if old == 0 {
		return physicalPixels{}, false
	}
	return physicalPixels{old: old}, true
}

func (g physicalPixels) restore() {
	if g.old != 0 {
		_, _, _ = procSetThreadDpiAwarenessContext.Call(g.old)
	}
}

// placedWindow 一个已经摆过的窗口：句柄 + 预期的位置和尺寸。
type placedWindow struct {
	hwnd        uintptr
	x, y, w, ht int32
}

// 枚举窗口的状态（EnumWindows 的回调不能闭包捕获，避免反复 NewCallback）。
var (
	enumMu       sync.Mutex
	enumCurPID   uint32
	enumBest     uintptr
	enumBestArea int64
)

// enumProc 是 EnumWindows 的回调：记录 enumCurPID 进程下面积最大的可见顶层窗口。
func enumProc(h, lparam uintptr) uintptr {
	if vis, _, _ := procIsWindowVisible.Call(h); vis == 0 {
		return 1
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&pid)))
	if pid != enumCurPID {
		return 1
	}
	// 跳过工具窗口（托盘图标、隐藏辅助窗之类）
	style, _, _ := procGetWindowLongW.Call(h, uintptr(gwlExStyle))
	if style&wsExToolWindow != 0 {
		return 1
	}
	var r windowRect
	if ret, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return 1
	}
	area := int64(r.right-r.left) * int64(r.bottom-r.top)
	if area > enumBestArea {
		enumBestArea = area
		enumBest = h
	}
	return 1
}

var enumCallback = syscall.NewCallback(enumProc)

// MainWindowOfPID 返回该进程"最大面积"的可见顶层窗口（即游戏主窗）。
func MainWindowOfPID(pid uint32) uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumCurPID = pid
	enumBest = 0
	enumBestArea = 0
	_, _, _ = procEnumWindows.Call(enumCallback, 0)
	return enumBest
}

// childPids 返回 parent 进程当下还活着的直接子进程列表。
// 游戏内弹的系统级对话框（渠道选择等）跑在独立子进程里，它的窗口
// 才是弹窗期间"活的画面"。
func ChildPids(parent uint32) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []uint32
	if err := windows.Process32First(snap, &e); err != nil {
		return nil
	}
	for {
		if e.ParentProcessID == parent {
			out = append(out, e.ProcessID)
		}
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}
	return out
}

// WindowsOfPids 返回这些进程名下全部可见的、非工具窗口的顶层窗口。
// 顺序：先主窗口（面积最大），其余按面积降序。
func WindowsOfPids(pids []uint32) []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumList = nil
	for _, pid := range pids {
		enumCurPID = pid
		_, _, _ = procEnumWindows.Call(enumAddCallback, 0)
	}
	// 面积降序
	for i := 1; i < len(enumList); i++ {
		for j := i; j > 0 && enumList[j].area > enumList[j-1].area; j-- {
			enumList[j], enumList[j-1] = enumList[j-1], enumList[j]
		}
	}
	hwnds := make([]uintptr, len(enumList))
	for i, e := range enumList {
		hwnds[i] = e.hwnd
	}
	return hwnds
}

type enumEntry struct {
	hwnd uintptr
	area int64
	w, h int32
}

var enumList []enumEntry

var enumAddCallback = syscall.NewCallback(func(h, _ uintptr) uintptr {
	if vis, _, _ := procIsWindowVisible.Call(h); vis == 0 {
		return 1
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&pid)))
	if pid != enumCurPID {
		return 1
	}
	style, _, _ := procGetWindowLongW.Call(h, uintptr(gwlExStyle))
	if style&wsExToolWindow != 0 {
		return 1
	}
	var r windowRect
	if ret, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return 1
	}
	enumList = append(enumList, enumEntry{
		hwnd: h,
		area: int64(r.right-r.left) * int64(r.bottom-r.top),
		w:    r.right - r.left,
		h:    r.bottom - r.top,
	})
	return 1
})

// CountClientWindows 数一数这些 pid 里已经有几个能找到主窗口。
// 游戏"进程启动"和"窗口可见"之间隔着登录加载，用窗口数当排列的就绪信号。
func CountClientWindows(pids []uint32) int {
	g, ok := enterPhysicalPixels()
	if ok {
		defer g.restore()
	}
	n := 0
	for _, pid := range pids {
		if MainWindowOfPID(pid) != 0 {
			n++
		}
	}
	return n
}

// primaryWorkArea 返回主屏工作区（不含任务栏）。
//
// 优先用显示器 API：在"线程已切物理像素感知、进程仍不感知"的混合状态
// 下，SystemParametersInfoW 会失败（实测返回 0），MonitorFromWindow +
// GetMonitorInfo 在感知上下文里定义明确，拿到的就是物理坐标。
func primaryWorkArea() windowRect {
	hmon, _, _ := procMonitorFromWindow.Call(0, 1) // MONITOR_DEFAULTTOPRIMARY
	if hmon != 0 {
		var mi monitorInfo
		mi.cbSize = uint32(unsafe.Sizeof(mi))
		if ret, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ret != 0 &&
			mi.rcWork.right > mi.rcWork.left && mi.rcWork.bottom > mi.rcWork.top {
			return mi.rcWork
		}
	}
	var r windowRect
	ret, _, _ := procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	if ret == 0 || r.right <= r.left || r.bottom <= r.top {
		return windowRect{left: 0, top: 0, right: 1920, bottom: 1080}
	}
	return r
}

// coversScreenRect 矩形是否盖住整个主屏（±4px 容差）。
// 只比尺寸不看样式：全屏窗口和"被引擎撑到屏幕大小"的窗口都算。
func coversScreenRect(r windowRect) bool {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	hh, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if w == 0 || hh == 0 {
		return false
	}
	const tol = 4
	return r.left <= tol && r.top <= tol &&
		r.right >= int32(w)-tol && r.bottom >= int32(hh)-tol
}

// coversPrimaryScreen 窗口当前是否盖满整个主屏。
func coversPrimaryScreen(h uintptr) bool {
	r, ok := getWindowRect(h)
	if !ok {
		return false
	}
	return coversScreenRect(r)
}

// isFullscreenWindow 判断窗口是否盖满整个主屏（不看标题栏）。
// 最大化的普通窗口会贴满工作区但底边到不了屏幕底边（任务栏），
// 所以不会被误判；任务栏自动隐藏时靠 WS_MAXIMIZE 兜住。
func isFullscreenWindow(h uintptr) bool {
	style, _, _ := procGetWindowLongW.Call(h, gwlStyle)
	if style&wsMaximize != 0 {
		return false
	}
	return coversPrimaryScreen(h)
}

// hasNonClientFrame 窗口有没有可见的非客户区（标题栏 / 边框）：
// 窗口矩形比客户区大就说明有。
//
// 为什么不用 WS_CAPTION 判：引擎自绘的窗口可以在样式里保留 WS_CAPTION，
// 同时把 WM_NCCALCSIZE 处理成"零非客户区"——样式看着像窗口，画面上
// 一条标题栏都没有。量窗口矩形与客户区之差，才是"眼睛能不能看到边框"。
func hasNonClientFrame(h uintptr) bool {
	var wr, cr windowRect
	if ret, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&wr))); ret == 0 {
		return false
	}
	if ret, _, _ := procGetClientRect.Call(h, uintptr(unsafe.Pointer(&cr))); ret == 0 {
		return false
	}
	return wr.right-wr.left > cr.right-cr.left || wr.bottom-wr.top > cr.bottom-cr.top
}

// forceWindowed 给全屏窗口发一次 Alt+Enter（多数引擎的全屏/窗口切换键）。
// 游戏焦点不在这时直接发键会丢，所以先把输入队列挂到目标窗口线程再给焦点。
func forceWindowed(h uintptr) {
	var tid uint32
	_, _, _ = procGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&tid)))
	our, _, _ := procGetCurrentThreadId.Call()
	if tid != 0 {
		_, _, _ = procAttachThreadInput.Call(our, uintptr(tid), 1)
	}
	_, _, _ = procSetForeground.Call(h)
	_, _, _ = procSetFocus.Call(h)
	time.Sleep(120 * time.Millisecond)

	// ALT ↓ ENTER ↓ ENTER ↑ ALT ↑
	_, _, _ = procKeybdEvent.Call(vkMenu, 0, 0, 0)
	time.Sleep(40 * time.Millisecond)
	_, _, _ = procKeybdEvent.Call(vkReturn, 0, 0, 0)
	time.Sleep(40 * time.Millisecond)
	_, _, _ = procKeybdEvent.Call(vkReturn, 0, keyEventUp, 0)
	time.Sleep(40 * time.Millisecond)
	_, _, _ = procKeybdEvent.Call(vkMenu, 0, keyEventUp, 0)

	if tid != 0 {
		_, _, _ = procAttachThreadInput.Call(our, uintptr(tid), 0)
	}
}

// forceWindowedStyle 直接把窗口样式改回"带标题栏的普通窗口"：
// 去掉 WS_POPUP，补回 WS_CAPTION / WS_THICKFRAME / WS_SYSMENU，
// 再 SetWindowPos 带 SWP_FRAMECHANGED 触发 WM_NCCALCSIZE / WM_SIZE，
// 让窗口管理器重新划出非客户区。
//
// 这是 Alt+Enter 不灵时的第二条路（有的引擎没绑这个快捷键）。
// 独占全屏（D3D exclusive）连窗口管理器都拿不到真实窗口，改样式也
// 救不回来，只能让用户在游戏内切成窗口模式。
func forceWindowedStyle(h uintptr) {
	style, _, _ := procGetWindowLongW.Call(h, gwlStyle)
	next := (style &^ wsPopup) | wsCaption | wsThickFrame | wsSysMenu
	_, _, _ = procSetWindowLongPtrW.Call(h, gwlStyle, next)
	_, _, _ = procSetWindowPos.Call(h, 0, 0, 0, 0, 0,
		swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
}

// exitFullscreen 让客户端从"无边框"退回"带标题栏的普通窗口"，
// 返回是否已经是普通窗口。
//
// 判据是"有没有非客户区边框"，不是"盖不盖满屏"：全屏多开启动的客户端
// 是无边框窗口，MoveWindow 其实摆得动（位置尺寸都对），但画面上一条
// 标题栏都没有——看着还像全屏，也没法拖动、没法点最小化。
//
// 顺序：盖满屏的先发 Alt+Enter（这是引擎自己的切换键，切完窗口是
// 引擎维护的，最干净）；没反应再改窗口样式硬加边框。已经不满屏、
// 只是无边框的，直接改样式——那种窗口发 Alt+Enter 反而会切回全屏。
func exitFullscreen(h uintptr) bool {
	if hasNonClientFrame(h) {
		return true
	}
	style, _, _ := procGetWindowLongW.Call(h, gwlStyle)
	Diag(fmt.Sprintf("排列：hwnd=%#x 无标题栏（style=%#x 盖满屏=%v）",
		h, style, isFullscreenWindow(h)))

	if isFullscreenWindow(h) {
		forceWindowed(h)
		time.Sleep(900 * time.Millisecond)
		if hasNonClientFrame(h) {
			Diag("排列：Alt+Enter 生效，已切回窗口模式")
			return true
		}
		Diag("排列：Alt+Enter 没反应，改窗口样式硬加边框")
	}

	forceWindowedStyle(h)
	time.Sleep(700 * time.Millisecond)
	if hasNonClientFrame(h) {
		Diag("排列：已有标题栏")
		return true
	}
	Diag("排列：加不上标题栏（引擎自绘非客户区），窗口仍是无边框")
	return false
}

// movedInto 复核一次摆放是否真的生效（wantW/wantH 是请求的格位尺寸）。
//
// 判据取"尺寸到位"或"不再盖满整屏"二者之一：引擎有最小窗口尺寸
// （实测 1024×720），请求的格比它小会被撑大，尺寸对不上但确实摆动了；
// 而独占全屏的窗口两种都不成立，MoveWindow 返回 TRUE 却没动。
func movedInto(h uintptr, wantW, wantH int32) bool {
	r, ok := getWindowRect(h)
	if !ok {
		return false
	}
	if abs32(r.right-r.left-wantW) <= 8 && abs32(r.bottom-r.top-wantH) <= 8 {
		return true
	}
	return !coversScreenRect(r)
}

// ArrangeFullscreen 把每个客户端都摆成"占满整屏"：多开全屏模式就是
// 所有客户端叠在一起，靠监控墙切换。返回成功切到全屏的窗口数。
//
// 与多开弹窗里选「全屏」启动是同一个目标状态，区别只在于这里是运行中
// 临时切——所以走 enterFullscreen，而不是预写 preferences.sav。
func ArrangeFullscreen(cells []ClientCell) int {
	if len(cells) == 0 {
		return 0
	}
	g, _ := enterPhysicalPixels()
	defer g.restore()
	moved := 0
	for _, c := range cells {
		h := MainWindowOfPID(c.PID)
		if h == 0 {
			continue
		}
		_, _, _ = procShowWindow.Call(h, swRestore) // 最小化的先还原
		if enterFullscreen(h) {
			moved++
		}
	}
	return moved
}

// enterFullscreen 让一个客户端占满整屏，返回是否成功。
//
// 顺序与 exitFullscreen 对称：已经是"无边框 + 盖满屏"就什么都不做；
// 普通窗口先发 Alt+Enter（它是 toggle，窗口模式按下去就进全屏）；
// 引擎不认这个键，就去掉标题栏边框、直接铺满主屏——观感与全屏一致，
// 而且仍在 DWM 管理下（监控墙的缩略图照常能缩）。
func enterFullscreen(h uintptr) bool {
	if coversPrimaryScreen(h) && !hasNonClientFrame(h) {
		return true
	}
	if hasNonClientFrame(h) {
		forceWindowed(h)
		time.Sleep(900 * time.Millisecond)
		if coversPrimaryScreen(h) && !hasNonClientFrame(h) {
			Diag("排列：Alt+Enter 已切回全屏")
			return true
		}
	}
	forceFullscreenStyle(h)
	sw, _, _ := procGetSystemMetrics.Call(smCXScreen)
	sh, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if sw == 0 || sh == 0 {
		return false
	}
	_, _, _ = procMoveWindow.Call(h, 0, 0, sw, sh, 1)
	time.Sleep(250 * time.Millisecond)
	if coversPrimaryScreen(h) {
		Diag("排列：已铺满主屏（无边框全屏）")
		return true
	}
	Diag("排列：切不到全屏（引擎可能只认游戏内设置）")
	return false
}

// forceFullscreenStyle 去边框：用 WS_POPUP 取代标题栏与可调整边框，
// 再 SWP_FRAMECHANGED 让窗口管理器重算非客户区（此时为零）。
func forceFullscreenStyle(h uintptr) {
	style, _, _ := procGetWindowLongW.Call(h, gwlStyle)
	next := (style &^ (wsCaption | wsThickFrame | wsSysMenu)) | wsPopup
	_, _, _ = procSetWindowLongPtrW.Call(h, gwlStyle, next)
	_, _, _ = procSetWindowPos.Call(h, 0, 0, 0, 0, 0,
		swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
}

// getWindowRect 读窗口矩形。
func getWindowRect(h uintptr) (windowRect, bool) {
	var r windowRect
	if ret, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return windowRect{}, false
	}
	if r.right <= r.left || r.bottom <= r.top {
		return windowRect{}, false
	}
	return r, true
}

// RenameClientWindow 把客户端主窗标题改成「槽位 · 标签」（打码切换时重写用）。
func RenameClientWindow(h uintptr, label string, slot int) {
	renameWindow(h, label, slot)
}

// renameWindow 把窗口标题改成「序号 · 账号名」，不然一排窗口全叫"光遇"没法分。
func renameWindow(h uintptr, label string, slot int) {
	title := fmt.Sprintf("%d · 光遇", slot)
	if label != "" {
		title = fmt.Sprintf("%d · %s", slot, label)
	}
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	_, _, _ = procSetWindowTextW.Call(h, uintptr(unsafe.Pointer(t)))
}

// ArrangeByGrid 把每个客户端的主窗口按 行优先 顺序摆进 cols×rows 网格。
//
// wantW/wantH > 0 时用自定义窗口尺寸，否则每格 = 工作区均分。
// 全屏状态的窗口会先被切回窗口模式（游戏自己的记忆是全屏时，摆了也白摆）。
// 返回成功移动的窗口数（找不到窗口或权限不够的会被跳过）。
func ArrangeByGrid(cells []ClientCell, cols, rows, wantW, wantH int) int {
	if cols < 1 || rows < 1 || len(cells) == 0 {
		return 0
	}
	if cols*rows < len(cells) {
		return 0
	}
	// 排列期间量与摆都在物理像素空间，见 enterPhysicalPixels
	g, _ := enterPhysicalPixels()
	defer g.restore()

	wa := primaryWorkArea()
	cw := (wa.right - wa.left) / int32(cols)
	ch := (wa.bottom - wa.top) / int32(rows)
	if wantW > 0 {
		cw = int32(wantW)
	}
	if wantH > 0 {
		ch = int32(wantH)
	}

	var list []placedWindow

	moved := 0
	for i, c := range cells {
		h := MainWindowOfPID(c.PID)
		if h == 0 {
			continue
		}
		// 全屏的先切窗口化：切换后引擎要重建交换链，稍等再摆。
		// 切不回来也要接着摆——MoveWindow 在少数引擎上仍能生效，
		// 复核那一步会把真实结果记进日志。
		exitFullscreen(h)
		if h2 := MainWindowOfPID(c.PID); h2 != 0 {
			h = h2
		}
		// 最小化的窗口 MoveWindow 不生效，先还原
		_, _, _ = procShowWindow.Call(h, swRestore)
		x := wa.left + int32(i%cols)*cw
		y := wa.top + int32(i/cols)*ch
		_, _, _ = procMoveWindow.Call(h, uintptr(x), uintptr(y), uintptr(cw), uintptr(ch), 1)
		// 复核：独占全屏的窗口 MoveWindow 返回 TRUE 却没动，不能算成功
		time.Sleep(200 * time.Millisecond)
		if movedInto(h, cw, ch) {
			moved++
		} else {
			Diag(fmt.Sprintf("排列：pid=%d 的窗口没排动（仍占满整屏）", c.PID))
		}
		list = append(list, placedWindow{hwnd: h, x: x, y: y, w: cw, ht: ch})
		renameWindow(h, c.Label, i+1)
	}

	// 二次校准：引擎可能把我们给的尺寸撑大/缩小造成相邻堆叠；量实际矩形，
	// 不一致就按实际尺寸重排（保证不重叠）。引擎改尺寸要几秒，收敛分两轮测。
	time.Sleep(800 * time.Millisecond)
	retileIfResized(list, cols, rows, wa, cw, ch)
	time.Sleep(1800 * time.Millisecond)
	retileIfResized(list, cols, rows, wa, cw, ch)
	return moved
}

// ArrangeByCascade 把客户端窗口按级联堆叠摆一次（监控墙快捷菜单用）。
// 与 ArrangeByGrid 同款的前置处理（全屏切窗口化、最小化还原、改名），
// 摆完不做二次校准：级联是带步长的错位扇，引擎把窗口撑到最小尺寸
// 也不会遮住别的标题条。不使用渐进排列——那会带 5 分钟的漂移回夹，
// 用户手动挪窗口会被弹回来。scheme 传空 = 双摞（默认方案）。
func ArrangeByCascade(cells []ClientCell, wantW, wantH int) int {
	return ArrangeByScheme(cells, "", wantW, wantH)
}

// ArrangeByScheme 按指定堆叠方案摆一次（scheme 见 SchemeDual 等常量）。
func ArrangeByScheme(cells []ClientCell, scheme string, wantW, wantH int) int {
	if len(cells) == 0 {
		return 0
	}
	g, _ := enterPhysicalPixels()
	defer g.restore()
	wa := primaryWorkArea()
	rects := planStacking(scheme, len(cells), wantW, wantH, wa)
	moved := 0
	for i, c := range cells {
		h := MainWindowOfPID(c.PID)
		if h == 0 {
			continue
		}
		// 全屏的先切窗口化：切换后引擎要重建交换链，稍等再摆
		exitFullscreen(h)
		if h2 := MainWindowOfPID(c.PID); h2 != 0 {
			h = h2
		}
		_, _, _ = procShowWindow.Call(h, swRestore)
		_, _, _ = procMoveWindow.Call(h, uintptr(rects[i].left), uintptr(rects[i].top),
			uintptr(rects[i].right-rects[i].left), uintptr(rects[i].bottom-rects[i].top), 1)
		// 同 ArrangeByGrid：独占全屏的窗口会"调用成功但没动"
		time.Sleep(200 * time.Millisecond)
		if movedInto(h, rects[i].right-rects[i].left, rects[i].bottom-rects[i].top) {
			moved++
		} else {
			Diag(fmt.Sprintf("排列：pid=%d 的窗口没排动（仍占满整屏）", c.PID))
		}
		renameWindow(h, c.Label, i+1)
	}
	return moved
}

// retileIfResized 量实际矩形，有窗口没被摆到位（被引擎撑大/缩小）就按
// 实际尺寸重排。引擎普遍有最小窗口尺寸（实测 1024×720）：请求的网格比
// 它小时窗口会被撑大、把网格撑出屏幕——这时按"屏幕能放下几列"收缩列数
// 重排，并把超出的行/列夹回工作区内：宁可叠在屏幕里（可点击切换），
// 也不掉到屏幕外摸不着。
// baseW/baseH 是"请求格尺寸"，没量到实际值的格子用它兜底。
func retileIfResized(list []placedWindow, cols, rows int, wa windowRect, baseW, baseH int32) {
	if len(list) == 0 {
		return
	}
	const epsilon = 2                    // 坐标换算的舍入容差，避免为 1 像素的偏差反复搬动
	sizes := make([][2]int32, len(list)) // 每个窗口的实际 w/h
	resized := false
	for i, p := range list {
		r, ok := getWindowRect(p.hwnd)
		if !ok {
			sizes[i] = [2]int32{baseW, baseH}
			continue
		}
		w, hh := r.right-r.left, r.bottom-r.top
		sizes[i] = [2]int32{w, hh}
		if abs32(w-p.w) > epsilon || abs32(hh-p.ht) > epsilon {
			resized = true
		}
	}
	if !resized {
		return
	}

	// 实测最小窗宽/高：引擎最小尺寸的观测值
	var minW, minH int32
	for _, s := range sizes {
		if s[0] > 0 && (minW == 0 || s[0] < minW) {
			minW = s[0]
		}
		if s[1] > 0 && (minH == 0 || s[1] < minH) {
			minH = s[1]
		}
	}
	if minW <= 0 {
		minW = baseW
	}
	if minH <= 0 {
		minH = baseH
	}

	// 屏幕一排放不下请求的列数时收缩列数（至少 1 列）
	workW := wa.right - wa.left
	effCols := cols
	if workW/minW < int32(effCols) {
		effCols = int(workW / minW)
		if effCols < 1 {
			effCols = 1
		}
	}
	effRows := (len(list) + effCols - 1) / effCols

	colW := make([]int32, effCols)
	rowH := make([]int32, effRows)
	for i, s := range sizes {
		c, rw := i%effCols, i/effCols
		if s[0] > colW[c] {
			colW[c] = s[0]
		}
		if s[1] > rowH[rw] {
			rowH[rw] = s[1]
		}
	}
	for c := 0; c < effCols; c++ {
		if colW[c] == 0 {
			colW[c] = baseW
		}
	}
	for r := 0; r < effRows; r++ {
		if rowH[r] == 0 {
			rowH[r] = baseH
		}
	}
	// 每行的 Y 坐标：总高放得下就顺序铺；放不下就等距级联（见 cascadeYPositions）
	yOf := cascadeYPositions(rowH, wa.top, wa.bottom)

	x := wa.left
	for c := 0; c < effCols; c++ {
		for rr := 0; rr < effRows; rr++ {
			idx := rr*effCols + c
			if idx < len(list) {
				// 列超出工作区时夹回来（行已由级联保证在屏幕里）
				xr := x
				if xr > wa.right-colW[c] {
					xr = wa.right - colW[c]
				}
				_, _, _ = procMoveWindow.Call(list[idx].hwnd, uintptr(xr), uintptr(yOf[rr]), uintptr(colW[c]), uintptr(rowH[rr]), 1)
			}
		}
		x += colW[c]
	}
}

// cascadeYPositions 算每行的 Y 坐标。
//
// 总高放得下：从顶部顺序往下铺。
// 放不下：等距级联——最后一行贴工作区底部，其余行从顶部往下等距错开，
// 保证每一行都露出一条可点击的标题（不然会全部叠死在同一处，只剩
// 最上面那个能点）。
func cascadeYPositions(rowH []int32, top, bottom int32) []int32 {
	effRows := len(rowH)
	yOf := make([]int32, effRows)
	if effRows == 0 {
		return yOf
	}
	needH := int32(0)
	for _, hh := range rowH {
		needH += hh
	}
	availH := bottom - top
	y := top
	for rr := 0; rr < effRows; rr++ {
		if rowH[rr] <= availH {
			// 底边不出屏；正常行这样夹，顶部最多裁掉一点
			if max := bottom - rowH[rr]; y > max {
				y = max
			}
		} else {
			y = top // 一行比整个工作区还高：钉在顶部，保住标题栏
		}
		yOf[rr] = y
		if needH > availH && effRows > 1 {
			// 级联：固定步长往下挪一行，但任何一行底边不出屏
			step := (availH - rowH[effRows-1]) / int32(effRows-1)
			if step > rowH[0] {
				step = rowH[0]
			}
			if step < 1 {
				step = 1
			}
			y = top + int32(rr+1)*step
			continue
		}
		y += rowH[rr]
	}
	return yOf
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// 引擎最小窗口尺寸（实测 Sky：物理像素 1024×720）：请求的格比它小，
// 窗口会被引擎撑大，所以格位预排直接按撑大后的尺寸算，免得后面的
// 格位全部被挤歪。
const (
	engineMinW = 1024
	engineMinH = 720
)

// planGrid 预排 n 个窗口的格位（物理坐标，行优先），渐进排列用。
// 列数比工作区还宽就收缩，行放不下就走级联（cascadeYPositions）。
func planGrid(n, cols, rows, wantW, wantH int, wa windowRect) []windowRect {
	if n <= 0 || cols < 1 || rows < 1 {
		return nil
	}
	workW, workH := wa.right-wa.left, wa.bottom-wa.top
	cw, ch := workW/int32(cols), workH/int32(rows)
	if wantW > 0 {
		cw = int32(wantW)
	}
	if wantH > 0 {
		ch = int32(wantH)
	}
	if cw < engineMinW {
		cw = engineMinW
	}
	if ch < engineMinH {
		ch = engineMinH
	}
	effCols := cols
	if int32(cols) > workW/cw {
		effCols = int(workW / cw)
	}
	if effCols < 1 {
		effCols = 1
	}
	effRows := (n + effCols - 1) / effCols
	rowH := make([]int32, effRows)
	for i := range rowH {
		rowH[i] = ch
	}
	yOf := cascadeYPositions(rowH, wa.top, wa.bottom)
	rects := make([]windowRect, n)
	for i := range rects {
		c, rr := int32(i%effCols), int32(i/effCols)
		x := wa.left + c*cw
		if max := wa.right - cw; x > max {
			x = max
		}
		rects[i] = windowRect{left: x, top: yOf[rr], right: x + cw, bottom: yOf[rr] + ch}
	}
	return rects
}

// planCascade 级联堆叠（文件窗式）：全部窗口同一尺寸，整摞在工作区居中，
// 逐个向右下错开。后摆的窗口 z 序在上，最后一个完整可见，第一个露出左上角。
//
// 步长按数量自适应（2026-10-29 对 8 开实测后的调整）：
//   - ≤4 开：44px 小步，紧凑一摞，只露标题条，省地方
//   - >4 开：满扇形——步长拉开到屏幕富余均分，窗口像扇面摊开。
//     实测 8 开在 2K 屏上 44px 步长平均每窗只见 21%（挤成一坨），
//     满扇后 47%，而且 >4 开时网格平铺本来也放不下（引擎最小 1024×720，
//     2K 屏最多 2×2），屏幕空着不如摊开看得见
func planCascade(n, wantW, wantH int, wa windowRect) []windowRect {
	if n <= 0 {
		return nil
	}
	cw, ch := int32(wantW), int32(wantH)
	if cw < engineMinW {
		cw = engineMinW
	}
	if ch < engineMinH {
		ch = engineMinH
	}
	if max := wa.right - wa.left; cw > max {
		cw = max
	}
	if max := wa.bottom - wa.top; ch > max {
		ch = max
	}
	spanX := (wa.right - wa.left) - cw
	spanY := (wa.bottom - wa.top) - ch
	if spanX < 0 {
		spanX = 0
	}
	if spanY < 0 {
		spanY = 0
	}
	// 紧凑步长（刚够露出完整标题条）；>4 开换满扇步长（富余均分，
	// 取横竖里较小者保证不出屏）。两者都再按屏幕富余收缩兜底。
	var step int32
	if n <= 1 {
		step = 0
	} else if n <= 4 {
		step = 44
	} else {
		avail := spanX
		if spanY < avail {
			avail = spanY
		}
		step = avail / int32(n-1)
	}
	// 屏幕富余不够时收缩步长（小屏兜底，保住"不出屏"优先于"露得多"）
	if n > 1 {
		if v := spanX / int32(n-1); v < step {
			step = v
		}
		if v := spanY / int32(n-1); v < step {
			step = v
		}
		if step < 1 {
			step = 1
		}
	}
	// 整摞在工作区里居中：首窗左上的空边 = 末窗右下的空边
	span := step * int32(n-1)
	x0 := wa.left + ((wa.right-wa.left)-cw-span)/2
	y0 := wa.top + ((wa.bottom-wa.top)-ch-span)/2
	if x0 < wa.left {
		x0 = wa.left
	}
	if y0 < wa.top {
		y0 = wa.top
	}
	rects := make([]windowRect, n)
	for i := 0; i < n; i++ {
		x := x0 + int32(i)*step
		y := y0 + int32(i)*step
		rects[i] = windowRect{left: x, top: y, right: x + cw, bottom: y + ch}
	}
	return rects
}

// clampWindow 尺寸保持原样，把位置夹回工作区（窗口被引擎撑大之后用）。
func clampWindow(h uintptr, r windowRect, wa windowRect) windowRect {
	w, hh := r.right-r.left, r.bottom-r.top
	x, y := r.left, r.top
	if max := wa.right - w; x > max {
		x = max
	}
	if max := wa.bottom - hh; y > max {
		y = max
	}
	if x < wa.left {
		x = wa.left
	}
	if y < wa.top {
		y = wa.top
	}
	if x != r.left || y != r.top {
		procMoveWindow.Call(h, uintptr(x), uintptr(y), uintptr(w), uintptr(hh), 1)
		r.left, r.top = x, y
	}
	return r
}

// 级联堆叠方案（多开「窗口堆叠」的排布姿势，用户可选）。
const (
	SchemeDual = "dual" // 双摞：左右两组斜向小摞（默认，比一整摞松快）
	SchemeRows = "rows" // 两行横向：上下两行、行内向右摊开
	SchemeCols = "cols" // 两列竖向：左右两列、列内向下摊开
	SchemeFan  = "fan"  // 满扇形：整摞对角摊开（旧的 >4 开默认）
)

// SplitCascadeMode 分解排列模式："cascade" / "cascade:<scheme>"。
// ok=false 表示不是级联（走网格平铺）。scheme 空或未知 → 双摞（默认）。
func SplitCascadeMode(mode string) (ok bool, scheme string) {
	if mode != "cascade" && len(mode) > 8 && mode[:8] == "cascade:" {
		return true, mode[8:]
	}
	return mode == "cascade", ""
}

// planStacking 按方案规划级联布局。窗口尺寸 = wantW/H（夹引擎最小值与工作区）。
// 各方案都保证不出屏、编号小的先摆（通常 z 序在下，标题条朝外露出）。
func planStacking(scheme string, n, wantW, wantH int, wa windowRect) []windowRect {
	switch scheme {
	case SchemeRows:
		return planTwoRows(n, wantW, wantH, wa)
	case SchemeCols:
		return planTwoCols(n, wantW, wantH, wa)
	case SchemeFan:
		return planCascade(n, wantW, wantH, wa)
	default: // "" / dual / 未知
		return planDualStack(n, wantW, wantH, wa)
	}
}

// clampSize 统一的窗口尺寸钳制：引擎最小值 → 工作区上限。
func clampSize(wantW, wantH int, wa windowRect) (int32, int32) {
	cw, ch := int32(wantW), int32(wantH)
	if cw < engineMinW {
		cw = engineMinW
	}
	if ch < engineMinH {
		ch = engineMinH
	}
	if max := wa.right - wa.left; cw > max {
		cw = max
	}
	if max := wa.bottom - wa.top; ch > max {
		ch = max
	}
	return cw, ch
}

// planDualStack 双摞：窗口分左右两半，各按 44px 斜摞、整组在工作区
// 上下居中。半区放不下一摞时收缩步长；n≤3 退化为单摞居中。
func planDualStack(n, wantW, wantH int, wa windowRect) []windowRect {
	if n <= 3 {
		return planCascade(n, wantW, wantH, wa)
	}
	cw, ch := clampSize(wantW, wantH, wa)
	waW, waH := wa.right-wa.left, wa.bottom-wa.top
	left := (n + 1) / 2 // 左摞个数（多的一边）
	stack := func(count int, xStart, xEnd int32) []windowRect {
		spanAvail := xEnd - xStart - cw
		step := int32(44)
		if count > 1 {
			if v := spanAvail / int32(count-1); v < step {
				step = v
			}
			if step < 1 {
				step = 1
			}
		}
		span := step * int32(count-1)
		x0 := xStart + (xEnd-xStart-cw-span)/2
		if x0 < xStart {
			x0 = xStart
		}
		y0 := wa.top + (waH-ch-span)/2
		if y0 < wa.top {
			y0 = wa.top
		}
		out := make([]windowRect, count)
		for i := 0; i < count; i++ {
			x, y := x0+int32(i)*step, y0+int32(i)*step
			out[i] = windowRect{left: x, top: y, right: x + cw, bottom: y + ch}
		}
		return out
	}
	mid := wa.left + waW/2
	out := append(stack(left, wa.left, mid), stack(n-left, mid, wa.right)...)
	// 第二摞排在后面 = cells 序号靠后 = 后启动，z 序大概率在上
	return out
}

// planTwoRows 两行横向：上行 ceil(n/2) 个、下行其余；行内向右摊开
// （步长 = 行内富余均分），行高不够两行时上下贴边（中段互叠，
// 下行后启动 z 序在上，标题条不受影响）。
func planTwoRows(n, wantW, wantH int, wa windowRect) []windowRect {
	if n <= 2 {
		return planCascade(n, wantW, wantH, wa)
	}
	cw, ch := clampSize(wantW, wantH, wa)
	waW := wa.right - wa.left
	k1 := (n + 1) / 2
	row := func(count int, y int32) []windowRect {
		step := int32(0)
		if count > 1 {
			step = (waW - cw) / int32(count-1)
			if step < 1 {
				step = 1
			}
		}
		out := make([]windowRect, count)
		for i := 0; i < count; i++ {
			x := wa.left + int32(i)*step
			out[i] = windowRect{left: x, top: y, right: x + cw, bottom: y + ch}
		}
		return out
	}
	yTop := wa.top
	yBottom := wa.bottom - ch
	out := append(row(k1, yTop), row(n-k1, yBottom)...)
	return out
}

// planTwoCols 两列竖向：左列 ceil(n/2) 个、右列其余；列内向下摊开
// （步长 = 纵向富余均分）。两列宽度放得下时左右完全分离；放不下时
// 右列贴右、与左列部分重叠（右列后启动 z 序在上）。
func planTwoCols(n, wantW, wantH int, wa windowRect) []windowRect {
	if n <= 2 {
		return planCascade(n, wantW, wantH, wa)
	}
	cw, ch := clampSize(wantW, wantH, wa)
	waW, waH := wa.right-wa.left, wa.bottom-wa.top
	k1 := (n + 1) / 2
	col := func(count int, x int32) []windowRect {
		step := int32(0)
		if count > 1 {
			step = (waH - ch) / int32(count-1)
			if step < 1 {
				step = 1
			}
		}
		out := make([]windowRect, count)
		for i := 0; i < count; i++ {
			y := wa.top + int32(i)*step
			out[i] = windowRect{left: x, top: y, right: x + cw, bottom: y + ch}
		}
		return out
	}
	var x1, x2 int32
	if waW >= cw*2+24 {
		x1 = wa.left + (waW-cw*2-24)/2
		x2 = x1 + cw + 24
	} else {
		x1 = wa.left
		x2 = wa.right - cw
	}
	out := append(col(k1, x1), col(n-k1, x2)...)
	return out
}

// placeWindow 把窗口摆进格位；引擎把尺寸撑大/缩小了就按实际尺寸夹回
// 屏幕内，返回最终落位矩形（给漂移检测当基准）。
// 同进程的登录/选渠道弹窗（MPAY 系列）跟着主窗一起平移，保持相对
// 位置——按 pid 归属，不会把别家的弹窗拖走。
func placeWindow(h uintptr, slot windowRect, wa windowRect) windowRect {
	var old windowRect
	hadOld := false
	if r, ok := getWindowRect(h); ok {
		old, hadOld = r, true
	}
	pid := pidOfWindow(h)
	procMoveWindow.Call(h, uintptr(slot.left), uintptr(slot.top), uintptr(slot.right-slot.left), uintptr(slot.bottom-slot.top), 1)
	r, ok := getWindowRect(h)
	if !ok {
		return slot
	}
	final := clampWindow(h, r, wa)
	if hadOld && pid != 0 {
		moveOwnedPopups(pid, h, old, final)
	}
	return final
}

// pidOfWindow 取窗口所属进程 id。
func pidOfWindow(h uintptr) uint32 {
	var pid uint32
	_, _, _ = procGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// 跟随主窗移动的弹窗类名（游戏内 mpay 的登录 / 选渠道 / 年龄提示 /
// DUI 投影窗）。都是"主窗的附属窗口"，不该留在原地变孤儿。
var ownedPopupClasses = map[string]bool{
	"MPAY_LOGIN":    true,
	"MPAY_AGE_TIPS": true,
	"DuiShadowWnd":  true,
}

// moveOwnedPopups 把 pid 名下、除主窗 h 以外的附属弹窗按主窗位移
// (old→now) 平移。窗口归 pid 所有，天然分得清是谁的弹窗。
func moveOwnedPopups(pid uint32, main uintptr, old, now windowRect) {
	dx, dy := now.left-old.left, now.top-old.top
	if dx == 0 && dy == 0 {
		return
	}
	// 枚举状态（EnumWindows 回调不能用闭包，走包级变量 + 锁）
	popupMu.Lock()
	popupPid, popupMain, popupDx, popupDy, popupFound = pid, main, dx, dy, popupFound[:0]
	popupMu.Unlock()
	_, _, _ = procEnumWindows.Call(enumPopupsProc, 0)
	popupMu.Lock()
	moved := popupFound
	popupMu.Unlock()
	for _, h := range moved {
		var r windowRect
		if ret, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r))); ret != 0 && r.right > r.left {
			procMoveWindow.Call(h, uintptr(r.left+popupDx), uintptr(r.top+popupDy),
				uintptr(r.right-r.left), uintptr(r.bottom-r.top), 1)
		}
	}
}

var (
	popupMu        sync.Mutex
	enumPopupsProc = syscall.NewCallback(func(h, _ uintptr) uintptr {
		var pid uint32
		_, _, _ = procGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&pid)))
		if pid != popupPid {
			return 1
		}
		if h == popupMain {
			return 1
		}
		var cls [32]uint16
		n, _, _ := procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&cls)), 32)
		if ownedPopupClasses[syscall.UTF16ToString(cls[:n])] {
			if v, _, _ := procIsWindowVisible.Call(h); v != 0 {
				popupFound = append(popupFound, h)
			}
		}
		return 1
	})
	// 仅供 enumPopupsProc 使用（popupMu 保护）
	popupPid   uint32
	popupMain  uintptr
	popupDx    int32
	popupDy    int32
	popupFound []uintptr
)

// ProgressiveArrange 渐进式排列：客户端窗口出一个摆一个，不等全部出完。
//
// cells 是期望名单（按启动顺序），格位按名单总数预先算好：第 i 个
// 出现的窗口固定落第 i 格，先出的不挪窝，后出的直接进自己的格子。
// mode = "cascade[:scheme]" 用级联堆叠（scheme 见 SchemeDual 等常量，
// 空 = 双摞默认），其余为网格平铺。
// 已摆过的窗口被引擎改了尺寸，下一轮巡检按实际尺寸夹回屏幕内。
// 全部就位、摆上第一个后 90 秒没有新窗口、或超时（5 分钟）就返回成功摆放的数量。
func ProgressiveArrange(cells []ClientCell, mode string, cols, rows, wantW, wantH int) int {
	if len(cells) == 0 {
		return 0
	}
	g, _ := enterPhysicalPixels()
	defer g.restore()
	wa := primaryWorkArea()
	var plan []windowRect
	if isCascade, scheme := SplitCascadeMode(mode); isCascade {
		plan = planStacking(scheme, len(cells), wantW, wantH, wa)
	} else {
		plan = planGrid(len(cells), cols, rows, wantW, wantH, wa)
	}

	type slot struct {
		done bool
		r    windowRect
		// 首摆前的稳定观测：窗口刚出现时引擎还在初始化（建交换链、
		// 默认尺寸切换），这时 MoveWindow 可能撞上引擎的自检导致闪退。
		// 记录上一拍的矩形，连续 stableAfter 不变才第一次摆。
		lastObs windowRect
		stable  time.Time
	}
	// 稳定窗口期：矩形（含位置与尺寸）连续这么久没变才算"稳定"。
	const stableAfter = 2500 * time.Millisecond
	st := make([]slot, len(cells))
	deadline := time.Now().Add(5 * time.Minute)
	lastNew := time.Now()
	total := 0
	for time.Now().Before(deadline) {
		for i, c := range cells {
			h := MainWindowOfPID(c.PID)
			if h == 0 {
				continue
			}
			r, ok := getWindowRect(h)
			drift := st[i].done && ok && (abs32(r.left-st[i].r.left) > 2 || abs32(r.top-st[i].r.top) > 2 ||
				abs32(r.right-st[i].r.right) > 2 || abs32(r.bottom-st[i].r.bottom) > 2)
			if st[i].done && !drift {
				continue
			}
			if !st[i].done {
				// 首摆前等稳定：矩形在变（引擎初始化中）就重置计时
				if ok && (abs32(r.left-st[i].lastObs.left) > 2 || abs32(r.top-st[i].lastObs.top) > 2 ||
					abs32(r.right-st[i].lastObs.right) > 2 || abs32(r.bottom-st[i].lastObs.bottom) > 2) {
					st[i].lastObs = r
					st[i].stable = time.Now()
				}
				if time.Since(st[i].stable) < stableAfter {
					continue
				}
			}
			st[i].r = placeWindow(h, plan[i], wa)
			if !st[i].done {
				st[i].done = true
				total++
				lastNew = time.Now()
				renameWindow(h, c.Label, i+1)
			}
		}
		allPlaced := true
		for i := range st {
			if !st[i].done {
				allPlaced = false
				break
			}
		}
		if allPlaced {
			return total
		}
		// 90 秒没新窗口：剩下的多半起不来了（摆上第一个之后才计时）
		if total > 0 && time.Since(lastNew) > 90*time.Second {
			return total
		}
		time.Sleep(400 * time.Millisecond)
	}
	return total
}

// ZOrderedWindowsOfPid 返回单个进程的可见顶层窗口，按 Z 序从上到下
// （EnumWindows 本身就是自顶向下枚举的），过滤掉小于 minW×minH 的
// （提示气泡一类的小窗）。用户眼睛看到的画面 = 这个列表的第一个。
func ZOrderedWindowsOfPid(pid uint32, minW, minH int32) []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumCurPID = pid
	enumList = nil
	_, _, _ = procEnumWindows.Call(enumAddCallback, 0)
	out := make([]uintptr, 0, len(enumList))
	for _, e := range enumList {
		if e.w >= minW && e.h >= minH {
			out = append(out, e.hwnd)
		}
	}
	return out
}

// ClientWindow 一个正在运行的游戏客户端主窗口。
type ClientWindow struct {
	PID   uint32
	Title string // 排列后是 "N · 账号名"，没排过是引擎默认标题
}

// EnumerateClientWindows 枚举所有 Sky.exe 进程的主窗口（pid + 标题）。
// 登录器重启后靠它把"正在运行的客户端"认回来：标题里的账号名就是
// 当初排列时写的（renameWindow），按名字就能恢复 pid → 账号路由。
func EnumerateClientWindows() []ClientWindow {
	g, _ := enterPhysicalPixels()
	defer g.restore()

	pids := pidsOfProcess("Sky.exe")
	out := make([]ClientWindow, 0, len(pids))
	for _, pid := range pids {
		h := MainWindowOfPID(pid)
		if h == 0 {
			continue
		}
		var buf [256]uint16
		n, _, _ := procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf)), 256)
		out = append(out, ClientWindow{PID: pid, Title: syscall.UTF16ToString(buf[:n])})
	}
	return out
}

// pidsOfProcess 按进程名（不区分大小写，含 .exe）列 pid。
func pidsOfProcess(name string) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	want := strings.ToLower(name)
	var out []uint32
	if err := windows.Process32First(snap, &e); err != nil {
		return nil
	}
	for {
		if strings.ToLower(windows.UTF16ToString(e.ExeFile[:])) == want {
			out = append(out, e.ProcessID)
		}
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}
	return out
}

var procGetWindowTextW = user32dll.NewProc("GetWindowTextW")
