package win

import (
	"runtime"
	"sync"
	"time"
	"unsafe"
)

// 主窗口几何记忆：记住位置尺寸 + 当时的主屏分辨率，恢复前先对屏幕。
//
// 坐标一律用物理像素：主窗线程在开窗前切了物理像素感知（见 webview.go），
// 这里量出来、存下去、下次恢复的都是同一套坐标，换缩放比例也不会错位。
// app 层通过 WebViewHooks 提供存取，win 层不含业务。

// WebViewHooks 主窗口几何记忆的 app 层回调（与 WallHooks 同思路）。
type WebViewHooks struct {
	// SavedRect 返回上次记住的位置尺寸（物理像素）。screenW/screenH 是
	// 记录时的主屏物理分辨率；ok=false 表示还没有记录。
	SavedRect func() (x, y, w, h, screenW, screenH int, ok bool)
	// SaveRect 在几何停稳变化后、以及窗口关闭前回调（物理像素 + 当时主屏）。
	SaveRect func(x, y, w, h, screenW, screenH int)
}

var procGetDpiForSystem = user32dll.NewProc("GetDpiForSystem")

// winGeoSnapshot 在物理像素空间取主窗矩形与主屏分辨率。
//
// 自带锁 OS 线程 + 切感知：它跑在跟踪 goroutine 上，不能依赖开窗线程的
// 上下文；感知切换只对本线程生效，不锁线程的话 goroutine 一迁移，
// enter 和 GetWindowRect 会落在两个线程上，量出来的就是虚拟坐标。
// 最小化时 Windows 会把窗口挪到 -32000，这种矩形不能存，返回 ok=false。
func winGeoSnapshot(hwnd uintptr) (r windowRect, screenW, screenH int, ok bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pp, switched := enterPhysicalPixels()
	if switched {
		defer pp.restore()
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return r, 0, 0, false
	}
	if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 ||
		r.right <= r.left || r.bottom <= r.top {
		return r, 0, 0, false
	}
	screenW, screenH = primaryScreenSize()
	return r, screenW, screenH, true
}

// primaryScreenSize 返回主屏的物理分辨率（须在物理像素上下文里调用）。
func primaryScreenSize() (int, int) {
	hmon, _, _ := procMonitorFromWindow.Call(0, 1) // MONITOR_DEFAULTTOPRIMARY
	if hmon != 0 {
		var mi monitorInfo
		mi.cbSize = uint32(unsafe.Sizeof(mi))
		if ret, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ret != 0 &&
			mi.rcMonitor.right > mi.rcMonitor.left && mi.rcMonitor.bottom > mi.rcMonitor.top {
			return int(mi.rcMonitor.right - mi.rcMonitor.left), int(mi.rcMonitor.bottom - mi.rcMonitor.top)
		}
	}
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	return int(w), int(h)
}

// fitInWorkArea 把想要的开窗尺寸夹进主屏工作区（四周留 24 物理像素），
// 保证窗口永远完整落在屏幕里。
func fitInWorkArea(w, h, workW, workH int) (int, int) {
	const margin = 24
	maxW, maxH := workW-margin*2, workH-margin*2
	if maxW < 320 {
		maxW = workW
	}
	if maxH < 240 {
		maxH = workH
	}
	if w > maxW {
		w = maxW
	}
	if h > maxH {
		h = maxH
	}
	return w, h
}

// saneWinRect 上次记住的主窗位置还能用吗：尺寸够用且与虚拟屏幕相交
// （记录对不上当前显示器布局时，别把主窗恢复到看不见的地方）。
func saneWinRect(x, y, w, h int) bool {
	if w < 500 || h < 400 {
		return false
	}
	vx, _, _ := procGetSystemMetrics.Call(76) // SM_XVIRTUALSCREEN
	vy, _, _ := procGetSystemMetrics.Call(77)
	vw, _, _ := procGetSystemMetrics.Call(78)
	vh, _, _ := procGetSystemMetrics.Call(79)
	return x < int(vx)+int(vw) && y < int(vy)+int(vh) && x+w > int(vx) && y+h > int(vy)
}

// setWinRect 物理像素摆位（在已切物理像素的线程上调用）。
func setWinRect(hwnd uintptr, x, y, w, h int) {
	procSetWindowPos.Call(hwnd, 0,
		uintptr(int32(x)), uintptr(int32(y)),
		uintptr(int32(w)), uintptr(int32(h)),
		swpNoZOrder|swpNoActivate)
}

// winGeoState 一次快照：窗口矩形 + 当时的主屏分辨率。
type winGeoState struct {
	r       windowRect
	screenW int
	screenH int
	valid   bool
}

// winGeoTracker 每秒采样窗口矩形，停稳后落盘；stop 关闭后结束。
type winGeoTracker struct {
	hooks WebViewHooks
	mu    sync.Mutex
	last  winGeoState // 最近一次有效快照
	saved winGeoState // 最近一次已落盘的快照
}

func (t *winGeoTracker) run(hwnd uintptr, stop <-chan struct{}) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var prev winGeoState
	var prevOK bool
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			r, sw, sh, ok := winGeoSnapshot(hwnd)
			if !ok {
				prevOK = false
				continue
			}
			cur := winGeoState{r: r, screenW: sw, screenH: sh, valid: true}
			t.mu.Lock()
			t.last = cur
			// 连续两拍一致才算停稳：拖拽/缩放过程中不写 config
			stable := prevOK && r == prev.r && (!t.saved.valid || r != t.saved.r)
			t.mu.Unlock()
			if stable {
				t.save(cur)
			}
			prev, prevOK = cur, true
		}
	}
}

func (t *winGeoTracker) save(g winGeoState) {
	if t.hooks.SaveRect == nil {
		return
	}
	t.hooks.SaveRect(int(g.r.left), int(g.r.top), int(g.r.right-g.r.left), int(g.r.bottom-g.r.top), g.screenW, g.screenH)
	t.mu.Lock()
	t.saved = g
	t.mu.Unlock()
}

// flush 把跟踪期间最后一次快照补写进存档（窗口关闭前调用；轮询有滞后，
// 不补的话用户关窗前最后一两秒的挪动会丢）。
func (t *winGeoTracker) flush() {
	t.mu.Lock()
	last, saved := t.last, t.saved
	t.mu.Unlock()
	if last.valid && (!saved.valid || last.r != saved.r) {
		t.save(last)
	}
}
