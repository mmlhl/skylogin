package playbtn

// Win32 原语：读窗口画面（PrintWindow）、抢前台、移动光标、点左键。
// 全部走 syscall，不引入 CGO，也不依赖任何图像库。

import (
	"errors"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procGetWindowDC          = user32.NewProc("GetWindowDC")
	procReleaseDC            = user32.NewProc("ReleaseDC")
	procPrintWindow          = user32.NewProc("PrintWindow")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procGetForegroundWindow  = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput    = user32.NewProc("AttachThreadInput")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop     = user32.NewProc("BringWindowToTop")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procSetCursorPos         = user32.NewProc("SetCursorPos")
	procMouseEvent           = user32.NewProc("mouse_event")
	procWindowFromPoint      = user32.NewProc("WindowFromPoint")
	procGetCurrentThreadID   = kernel32.NewProc("GetCurrentThreadId")
	procSetThreadDpiAware    = user32.NewProc("SetThreadDpiAwarenessContext")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

type rect struct {
	left, top, right, bottom int32
}

type bitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

type bitmapInfo struct {
	header bitmapInfoHeader
	colors [3]uint32
}

// image 是一张 32 位 BGRA 位图，行序自上而下。
type image struct {
	w, h int
	pix  []byte
}

const (
	pwRenderFullContent = 2
	swpNoSize           = 0x0001
	swpNoMove           = 0x0002
	swpShowWindow       = 0x0040
	mouseEventLeftDown  = 0x0002
	mouseEventLeftUp    = 0x0004
)

// physicalPixels 在当前线程临时切到"物理像素"坐标系。
//
// 为什么需要：主程序（webview 壳）和游戏可能不是同一套 DPI 感知，
// 不切的话窗口矩形、光标位置、PrintWindow 出来的图会差一个缩放倍数，
// 算出来的坐标点下去就偏了。切完必须还原，不能污染调用方的线程状态。
func physicalPixels(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4
	old, _, _ := procSetThreadDpiAware.Call(^uintptr(3))
	defer procSetThreadDpiAware.Call(old)
	fn()
}

// capture 用 PrintWindow 把整窗画到位图里。
//
// 为什么用 PrintWindow 而不是截屏：多开时窗口互相遮挡、甚至被移到屏幕外，
// 截屏拿到的是别人的画面；PrintWindow 让窗口自己画一份，遮挡不影响。
func capture(hwnd uintptr) (*image, error) {
	var out *image
	var capErr error
	physicalPixels(func() {
		out, capErr = captureLocked(hwnd)
	})
	return out, capErr
}

func captureLocked(hwnd uintptr) (*image, error) {
	var r rect
	if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return nil, errors.New("读窗口矩形失败")
	}
	w, h := int(r.right-r.left), int(r.bottom-r.top)
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 {
		return nil, errors.New("窗口尺寸异常")
	}

	hdc, _, _ := procGetWindowDC.Call(hwnd)
	if hdc == 0 {
		return nil, errors.New("取窗口 DC 失败")
	}
	defer procReleaseDC.Call(hwnd, hdc)

	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, errors.New("建内存 DC 失败")
	}
	defer procDeleteDC.Call(memDC)

	// 直接建 DIB 段：像素写进我们自己拿得到的缓冲区，
	// 省掉 GetDIBits 那一步（它对某些显卡/遮盖状态会失败）。
	bi := bitmapInfo{}
	bi.header.size = uint32(unsafe.Sizeof(bi.header))
	bi.header.width = int32(w)
	bi.header.height = -int32(h) // 负高度 = 自上而下
	bi.header.planes = 1
	bi.header.bitCount = 32
	bi.header.compression = 0 // BI_RGB

	var bits unsafe.Pointer
	dib, _, _ := procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&bi)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == nil {
		return nil, errors.New("建 DIB 段失败")
	}
	defer procDeleteObject.Call(dib)

	old, _, _ := procSelectObject.Call(memDC, dib)
	defer procSelectObject.Call(memDC, old)

	// 1 = PW_CLIENTONLY 不适用；用 2 才能拿到 DirectComposition 渲染的内容
	if ret, _, _ := procPrintWindow.Call(hwnd, memDC, pwRenderFullContent); ret == 0 {
		return nil, errors.New("PrintWindow 失败")
	}
	pix := unsafe.Slice((*byte)(bits), w*h*4)
	// 必须拷一份：DIB 段的内存随 DeleteObject 一起释放，
	// 直接把切片传出去会变成野指针（访问即崩）。
	out := make([]byte, len(pix))
	copy(out, pix)
	return &image{w: w, h: h, pix: out}, nil
}

// activate 把窗口提到前台并拿到焦点。
//
// 直接 SetForegroundWindow 会被前台锁拦掉（尤其我们的进程不是"用户正在操作的"
// 那个），所以先 AttachThreadInput 到当前前台线程和目标线程，做完再解挂。
func activate(hwnd uintptr) {
	physicalPixels(func() {
		me, _, _ := procGetCurrentThreadID.Call()
		fg, _, _ := procGetForegroundWindow.Call()
		tidFG, _, _ := procGetWindowThreadPID.Call(fg, 0)
		tidTarget, _, _ := procGetWindowThreadPID.Call(hwnd, 0)

		a1, _, _ := procAttachThreadInput.Call(me, tidFG, 1)
		a2, _, _ := procAttachThreadInput.Call(me, tidTarget, 1)
		defer func() {
			if a1 != 0 {
				procAttachThreadInput.Call(me, tidFG, 0)
			}
			if a2 != 0 {
				procAttachThreadInput.Call(me, tidTarget, 0)
			}
		}()

		procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
		procBringWindowToTop.Call(hwnd)
		procSetForegroundWindow.Call(hwnd)
		time.Sleep(120 * time.Millisecond)
	})
}

// click 在窗口内 (x,y) 处点一次左键。
func click(hwnd uintptr, x, y int) {
	moveCursor(hwnd, x, y)
	time.Sleep(40 * time.Millisecond)
	procMouseEvent.Call(mouseEventLeftDown, 0, 0, 0, 0)
	time.Sleep(40 * time.Millisecond)
	procMouseEvent.Call(mouseEventLeftUp, 0, 0, 0, 0)
}

// moveCursor 把光标移到窗口内坐标 (x,y) 对应的屏幕位置。
func moveCursor(hwnd uintptr, x, y int) {
	sx, sy := screenPointOf(hwnd, x, y)
	physicalPixels(func() {
		procSetCursorPos.Call(uintptr(sx), uintptr(sy))
	})
}

// screenPointOf 把窗口坐标换算成屏幕坐标（物理像素）。
func screenPointOf(hwnd uintptr, x, y int) (int, int) {
	var sx, sy int
	physicalPixels(func() {
		var r rect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		sx, sy = int(r.left)+x, int(r.top)+y
	})
	return sx, sy
}

// pointHitsProcess 判断屏幕点是否真的落在 pid 的窗口上。
//
// 用途：点击之前确认一下不会被别的窗口吃掉。最典型的遮挡是监控墙——
// 它 TOPMOST 钉在所有客户端上面，落在它身上的点击不会传给游戏。
// app 层在自动进入期间会把墙切成鼠标穿透，这里再兜一道：真的被挡住就
// 这轮不点，等下一轮，绝不把点击扔给不相干的窗口。
func pointHitsProcess(pid uint32, x, y int) bool {
	hit := false
	physicalPixels(func() {
		h := windowFromPoint(x, y)
		if h == 0 {
			return
		}
		var wpid uint32
		procGetWindowThreadPID.Call(h, uintptr(unsafe.Pointer(&wpid)))
		hit = wpid == pid
	})
	return hit
}

// windowFromPoint 调 WindowFromPoint。
//
// 注意签名：HWND WindowFromPoint(POINT Point) —— POINT 是**按值**传的 8 字节
// 结构体，x64 调用约定下塞进一个寄存器（低 32 位 x、高 32 位 y）。
// 传指针会得到 0（踩过：导致所有窗口都被判成"被挡住"）。
func windowFromPoint(x, y int) uintptr {
	raw := uintptr(uint64(uint32(int32(x))) | uint64(uint32(int32(y)))<<32)
	h, _, _ := procWindowFromPoint.Call(raw)
	return h
}

// changed 判断两张画面差异是否足够大（默认 5% 像素有明显变化）。
func changed(a, b *image) bool {
	if a == nil || b == nil || a.w != b.w || a.h != b.h {
		return false
	}
	diff, total := 0, a.w*a.h
	for i := 0; i < len(a.pix); i += 4 {
		d := int(a.pix[i]) - int(b.pix[i])
		if d < 0 {
			d = -d
		}
		dn := int(a.pix[i+1]) - int(b.pix[i+1])
		if dn < 0 {
			dn = -dn
		}
		if dn > d {
			d = dn
		}
		dr := int(a.pix[i+2]) - int(b.pix[i+2])
		if dr < 0 {
			dr = -dr
		}
		if dr > d {
			d = dr
		}
		if d > 25 {
			diff++
			if float64(diff)/float64(total) >= changeRatio {
				return true
			}
		}
	}
	return false
}

// windowLabel 取窗口标题，只用于日志。
func windowLabel(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return "游戏窗口"
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}
