package win

// 监控墙：一个原生小窗口，把每个客户端窗口的实时画面缩进格子里。
//
// 画面来自 DWM 缩略图（Alt+Tab 预览用的同一套系统机制）：系统合成器
// 直接读窗口的渲染表面来画，游戏进程全程无感知——不注入、不改窗口、
// 不发消息给游戏，零封号风险。点某个格子把对应的真实窗口调到前台，
// 操作仍然发生在真实窗口里（同一时刻本来也只能操作一个号）。
//
// 线程模型：墙窗口跑在独立线程上（锁 OS 线程 + 消息循环），创建前把
// 该线程切成物理像素感知，125% 缩放下画面和文字不糊。别的 goroutine
// 通过 WallUpdate/CloseWall 投递任务，用 PostMessage 唤醒消息循环。

import (
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"
)

var (
	procDwmRegister    = dwm32.NewProc("DwmRegisterThumbnail")
	procDwmUpdateProps = dwm32.NewProc("DwmUpdateThumbnailProperties")
	procDwmUnregister  = dwm32.NewProc("DwmUnregisterThumbnail")

	procPostMessageW = user32dll.NewProc("PostMessageW")
	procIsWindow     = user32dll.NewProc("IsWindow")
	procLoadCursor   = user32dll.NewProc("LoadCursorW")
	procGetDpiForWin = user32dll.NewProc("GetDpiForWindow")
	procInvalidate   = user32dll.NewProc("InvalidateRect")
	procSetWindowPos = user32dll.NewProc("SetWindowPos")

	procBeginPaint  = user32dll.NewProc("BeginPaint")
	procEndPaint    = user32dll.NewProc("EndPaint")
	procFillRect    = user32dll.NewProc("FillRect")
	procDrawTextW   = user32dll.NewProc("DrawTextW")
	procGetSysBrush = user32dll.NewProc("GetSysColorBrush")

	// 文本和字体相关的在 gdi32
	procSetBkMode    = gdi32.NewProc("SetBkMode")
	procSetTextColor = gdi32.NewProc("SetTextColor")
	procSelectObject = gdi32.NewProc("SelectObject")
	procCreateFontW  = gdi32.NewProc("CreateFontW")
	procDeleteObject = gdi32.NewProc("DeleteObject")
)

var gdi32 = syscall.NewLazyDLL("gdi32.dll")

const (
	wmWallTask     = 0x0400 + 2 // WM_APP+2（托盘占了 +1）
	wmSizeWin      = 0x0005
	wmPaintWin     = 0x000F
	wmEraseBkgnd   = 0x0014
	wmLButtonDownW = 0x0201

	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	idcArrow           = 32512
	colorBtnFace       = 15

	dtCenter      = 0x0001
	dtVCenter     = 0x0004
	dtSingleLine  = 0x0020
	dtEndEllipsis = 0x8000
	bkTransparent = 1

	// DWM_THUMBNAIL_PROPERTIES 的 dwFlags 位
	dwmTnpRectDest   = 0x1
	dwmTnpOpacity    = 0x4
	dwmTnpVisible    = 0x8
	dwmTnpClientOnly = 0x10
)

// WallClient 监控墙的一格：主窗口句柄 + 格子下方显示的标签（账号名）。
// Ratio 是格子布局用的基准宽高比（一般传主窗口的）；0 = 用主窗口自己的。
// Rebind = 这格的缩略图注销重建一次（弹窗出现/消失后让 DWM 重新绑定
// 主窗表面，弹窗本身由系统合成器自动叠显）。
type WallClient struct {
	Hwnd   uintptr
	Label  string
	Ratio  float64
	Rebind bool
}

// wallTile 一个格子的全部状态（只在墙线程上访问）。
type wallTile struct {
	hwnd   uintptr
	label  string
	thumb  uintptr    // 主窗口缩略图（铺满格子）
	rect   windowRect // 画面区（墙客户区坐标）
	stripR windowRect // 标签条
	aspect float64    // 主窗口当前宽高比
}

var (
	wallMu    sync.Mutex
	wallHwnd  uintptr
	wallTasks chan func()
	// 下面三个只在墙线程上访问
	wallTiles []wallTile
	wallFont  uintptr
	wallRatio float64 // 画面宽高比（来自源窗口客户区），默认 64:45
	// 墙线程在 wallMu 下维护的格子位置快照，给 WallTileCenter 跨线程读
	wallRects []windowRect
	// 每个源窗口连续被判白屏的轮数：连续 2 轮才重建缩略图，避免误伤静止画面
	wallWhiteCount map[uintptr]int
)

// WallOpen 报告监控墙是否开着。
func WallOpen() bool {
	wallMu.Lock()
	defer wallMu.Unlock()
	return wallHwnd != 0
}

// OpenWall 打开监控墙窗口。已经开着就什么都不做（返回 nil）。
func OpenWall() error {
	wallMu.Lock()
	if wallHwnd != 0 {
		wallMu.Unlock()
		return nil
	}
	ready := make(chan error, 1)
	wallTasks = make(chan func(), 8)
	wallMu.Unlock()
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		runWall(ready)
	}()
	return <-ready
}

// CloseWall 关闭监控墙。没开着就静默返回。
func CloseWall() {
	wallMu.Lock()
	h := wallHwnd
	wallMu.Unlock()
	if h != 0 {
		procPostMessageW.Call(h, wmClose, 0, 0)
	}
}

// WallUpdate 全量更新墙上的格子（增删客户端、改标签都会跟着变）。
// 墙没开时静默返回；已注册的缩略图会复用，不会闪。
func WallUpdate(clients []WallClient) {
	wallMu.Lock()
	h, ch := wallHwnd, wallTasks
	wallMu.Unlock()
	if h == 0 || ch == nil {
		return
	}
	select {
	case ch <- func() { applyWallUpdate(clients) }:
		procPostMessageW.Call(h, wmWallTask, 0, 0)
	default: // 队列满说明墙线程卡了，这轮丢弃
	}
}

// ---- 墙线程 ----

type paintStruct struct {
	HDC         uintptr
	FErase      int32
	RCPaint     windowRect
	FRestore    int32
	FIncUpdate  int32
	RGBReserved [32]byte
}

func runWall(ready chan<- error) {
	// 创建窗口前切物理像素感知：这个线程上建出来的窗口按 PMv2 处理，
	// 125%/150% 缩放下不糊。线程退出即结束，不用恢复。
	_, _ = enterPhysicalPixels()

	hInst, _, _ := procGetModule.Call(0)
	cursor, _, _ := procLoadCursor.Call(0, idcArrow)
	clsName, _ := syscall.UTF16PtrFromString("SkyLoginWallWnd")
	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   syscall.NewCallback(wallWndProc),
		HInstance:     hInst,
		HCursor:       cursor,
		HbrBackground: uintptr(colorBtnFace + 1),
		LpszClassName: clsName,
	}
	// 类注册失败不算错：进程内第二次开墙时类还在（ERROR_CLASS_ALREADY_EXISTS），
	// CreateWindowExW 会直接用现成的类；真没注册上时下面创建窗口会失败报出来
	_, _, _ = procRegisterCls.Call(uintptr(unsafe.Pointer(&wc)))

	// 默认尺寸：工作区的六成左右，夹在合理范围内，屏幕居中
	wa := primaryWorkArea()
	workW, workH := wa.right-wa.left, wa.bottom-wa.top
	w := clampInt(int(workW)*3/5, 880, 1400)
	h := clampInt(int(workH)*3/4, 620, 920)
	x := wa.left + (workW-int32(w))/2
	y := wa.top + (workH-int32(h))/2

	title, _ := syscall.UTF16PtrFromString("光遇登录器 · 监控墙")
	hwnd, _, _ := procCreateWinEx.Call(0, uintptr(unsafe.Pointer(clsName)),
		uintptr(unsafe.Pointer(title)), wsOverlappedWindow|wsVisible,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		0, 0, hInst, 0)
	if hwnd == 0 {
		ready <- syscall.EINVAL
		return
	}
	applyDarkFrame(hwnd)

	// 固定在最顶层：挂机看号时墙不能被游戏窗口盖住
	// (HWND_TOPMOST=-1, SWP_NOSIZE|SWP_NOMOVE|SWP_NOACTIVATE)
	procSetWindowPos.Call(hwnd, ^uintptr(0), 0, 0, 0, 0, 0x0001|0x0002|0x0010)

	// 墙是置顶窗口，不需要抢前台：自动开墙往往发生在游戏已经接管前台
	// 之后，抢一次焦点就会让正在玩的游戏收到失焦、立刻松开鼠标。
	dpi, _, _ := procGetDpiForWin.Call(hwnd)
	wallFont = makeWallFont(int(dpi))
	wallRatio = 64.0 / 45.0

	wallMu.Lock()
	wallHwnd = hwnd
	wallMu.Unlock()
	ready <- nil

	var msg trayMsg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
	}

	if wallFont != 0 {
		procDeleteObject.Call(wallFont)
		wallFont = 0
	}
	wallMu.Lock()
	wallHwnd = 0
	wallMu.Unlock()
}

var cursor, _, _ = procLoadCursor.Call(0, idcArrow)

func wallWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmWallTask:
		for {
			var done bool
			wallMu.Lock()
			select {
			case f := <-wallTasks:
				wallMu.Unlock()
				f()
			default:
				wallMu.Unlock()
				done = true
			}
			if done {
				break
			}
		}
		return 0
	case wmSizeWin:
		relayoutWall()
		return 0
	case wmPaintWin:
		paintWall(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1 // 背景在 WM_PAINT 里整块重画，避免闪
	case wmLButtonDownW:
		wallClick(int32(int16(lparam&0xFFFF)), int32(int16((lparam>>16)&0xFFFF)))
		return 0
	case wmClose:
		for i := range wallTiles {
			if wallTiles[i].thumb != 0 {
				procDwmUnregister.Call(wallTiles[i].thumb)
			}
		}
		// 缩略图句柄跟着窗口一起失效，清掉旧格子，下次开墙从零注册
		wallTiles = nil
		procDestroyWin.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMsg.Call(0)
		return 0
	}
	r, _, _ := procDefWinProc.Call(hwnd, msg, wparam, lparam)
	return r
}

// applyWallUpdate 全量对齐格子列表：新窗口注册缩略图，消失的注销，
// 留下的复用（只更新标签），最后统一重排。
func applyWallUpdate(clients []WallClient) {
	var kept []wallTile
	for _, c := range clients {
		if c.Hwnd == 0 {
			continue
		}
		if r, _, _ := procIsWindow.Call(c.Hwnd); r == 0 {
			continue
		}
		// 复用同主窗的缩略图；Rebind（窗口数量变化）时注销重建——
		// DWM 从头绑定源表面，陈旧的空白帧一并清掉
		var thumb uintptr
		for _, old := range wallTiles {
			if old.hwnd != c.Hwnd {
				continue
			}
			if !c.Rebind {
				thumb = old.thumb
			} else if old.thumb != 0 {
				procDwmUnregister.Call(old.thumb)
			}
			break
		}
		kept = append(kept, wallTile{
			hwnd:   c.Hwnd,
			label:  c.Label,
			thumb:  thumb,
			aspect: clientRatio(c.Ratio, c.Hwnd),
		})
	}
	// 没被复用的旧缩略图注销
	for _, old := range wallTiles {
		if old.thumb == 0 {
			continue
		}
		reused := false
		for _, k := range kept {
			if k.thumb == old.thumb {
				reused = true
				break
			}
		}
		if !reused {
			procDwmUnregister.Call(old.thumb)
		}
	}

	// 格子布局比例用客户端传的基准（主窗口），不跟被监控窗口跳
	if len(clients) > 0 && clients[0].Ratio > 0 {
		wallRatio = clients[0].Ratio
	} else if wallRatio <= 0 {
		wallRatio = 64.0 / 45.0
	}

	wallTiles = kept
	relayoutWall()

	// 标题带上数量，任务栏一眼能认
	n := len(wallTiles)
	t := "监控墙（没有正在运行的客户端）"
	if n > 0 {
		t = "光遇登录器 · 监控墙（" + strconv.Itoa(n) + " 个客户端）"
	}
	if s2, err := syscall.UTF16PtrFromString(t); err == nil {
		procSetWindowTextW.Call(hwndOfWall(), uintptr(unsafe.Pointer(s2)))
	}
}

func hwndOfWall() uintptr {
	wallMu.Lock()
	defer wallMu.Unlock()
	return wallHwnd
}

// relayoutWall 按当前客户区大小给每个格子算位置，格子取最大的
// 等比缩放（列数在 1..n 里挑能铺得最满的），然后更新缩略图属性。
func relayoutWall() {
	h := hwndOfWall()
	if h == 0 {
		return
	}
	var cr windowRect
	if ret, _, _ := procGetClientRect.Call(h, uintptr(unsafe.Pointer(&cr))); ret == 0 {
		return
	}
	W, H := cr.right, cr.bottom
	n := len(wallTiles)
	if n == 0 {
		wallMu.Lock()
		wallRects = nil
		wallMu.Unlock()
		procInvalidate.Call(h, 0, 1)
		return
	}
	dpi, _, _ := procGetDpiForWin.Call(h)
	gap := int32(dpi) * 8 / 96
	strip := int32(dpi) * 22 / 96

	// 每种列数算一遍能铺出的最大格子，取最大的那种
	bestCols, bestTile := 1, int32(0)
	for c := 1; c <= n; c++ {
		rows := int32((n + c - 1) / c)
		tw := (W - int32(c+1)*gap) / int32(c)
		if tw <= 0 {
			continue
		}
		th := int32(float64(tw)/wallRatio) + strip
		rowsH := rows*th + (rows+1)*gap
		if rowsH > H {
			// 高度不够就按高度反推格子宽
			avail := (H - (rows+1)*gap) / rows
			if avail <= strip {
				continue
			}
			th = avail
			tw = int32(float64(th-strip) * wallRatio)
			if tw <= 0 {
				continue
			}
		}
		if tw > bestTile {
			bestTile, bestCols = tw, c
		}
	}
	if bestTile <= 0 {
		return
	}
	rows := (n + bestCols - 1) / bestCols
	th := int32(float64(bestTile)/wallRatio) + strip
	x0 := (W - (int32(bestCols)*bestTile + int32(bestCols+1)*gap)) / 2
	y0 := (H - (int32(rows)*th + int32(rows+1)*gap)) / 2

	for i := range wallTiles {
		c, r := int32(i%bestCols), int32(i/bestCols)
		lx := x0 + c*(bestTile+gap) + gap
		ly := y0 + r*(th+gap) + gap
		wallTiles[i].rect = windowRect{left: lx, top: ly, right: lx + bestTile, bottom: ly + th - strip}
		wallTiles[i].stripR = windowRect{left: lx, top: ly + th - strip, right: lx + bestTile, bottom: ly + th}

		t := wallTiles[i]
		// 最小化的窗口没有渲染表面，缩略图是空白——先还原再捕
		if iconic, _, _ := procIsIconic.Call(t.hwnd); iconic != 0 {
			procShowWindow.Call(t.hwnd, swRestore)
		}
		if t.thumb == 0 {
			var th32 uintptr
			if hr, _, _ := procDwmRegister.Call(h, t.hwnd, uintptr(unsafe.Pointer(&th32))); hr == 0 {
				wallTiles[i].thumb = th32
				t.thumb = th32
			}
		}
		if t.thumb != 0 {
			props := dwmThumbProps{
				dwFlags:  dwmTnpRectDest | dwmTnpOpacity | dwmTnpVisible,
				rcDest:   fitInside(wallTiles[i].rect, t.aspect),
				opacity:  255,
				fVisible: 1,
			}
			procDwmUpdateProps.Call(t.thumb, uintptr(unsafe.Pointer(&props)))
		}
	}
	// 发布位置快照，跨线程的 WallTileCenter 只读这份
	rects := make([]windowRect, len(wallTiles))
	for i := range wallTiles {
		rects[i] = wallTiles[i].rect
	}
	wallMu.Lock()
	wallRects = rects
	wallMu.Unlock()
	procInvalidate.Call(h, 0, 1)
}

// WallTileCenter 返回第 i 个格子中心的屏幕坐标（诊断/自动化测试用）。
func WallTileCenter(i int) (int, int, bool) {
	wallMu.Lock()
	h, rects := wallHwnd, wallRects
	wallMu.Unlock()
	if h == 0 || i < 0 || i >= len(rects) {
		return 0, 0, false
	}
	r := rects[i]
	pt := point{X: (r.left + r.right) / 2, Y: (r.top + r.bottom) / 2}
	procClientToScreen.Call(h, uintptr(unsafe.Pointer(&pt)))
	return int(pt.X), int(pt.Y), true
}

// RaiseWall 把监控墙提到前台（打开后和点击测试前用）。
func RaiseWall() {
	wallMu.Lock()
	h := wallHwnd
	wallMu.Unlock()
	if h != 0 {
		procSetForeground.Call(h)
	}
}

// dwmThumbProps 是 DWM_THUMBNAIL_PROPERTIES。
type dwmThumbProps struct {
	dwFlags     uint32
	rcDest      windowRect
	rcSource    windowRect
	opacity     byte
	fVisible    int32
	fClientOnly int32
}

// paintWall 画背景和标签条文字；画面本身是 DWM 合成器叠上来的。
func paintWall(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	bg, _, _ := procGetSysBrush.Call(colorBtnFace)
	var full windowRect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&full)))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), bg)

	if wallFont != 0 {
		procSelectObject.Call(hdc, wallFont)
	}
	procSetBkMode.Call(hdc, bkTransparent)
	procSetTextColor.Call(hdc, 0x003A3A3A) // 深灰（COLORREF 是 BGR）

	if len(wallTiles) == 0 {
		hint, _ := syscall.UTF16PtrFromString("没有正在运行的客户端——从主界面启动游戏后，这里会出现它们的实时画面")
		var r windowRect
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(hint)), ^uintptr(0),
			uintptr(unsafe.Pointer(&r)), dtCenter|dtVCenter|dtSingleLine)
		return
	}
	for _, t := range wallTiles {
		if t.label == "" {
			continue
		}
		s, err := syscall.UTF16PtrFromString(t.label)
		if err != nil {
			continue
		}
		r := t.stripR
		r.left += 6
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(s)), ^uintptr(0),
			uintptr(unsafe.Pointer(&r)), dtCenter|dtVCenter|dtSingleLine|dtEndEllipsis)
	}
}

// clientRatio 基准比例：传了就用，没传量源窗口客户区，量不到退 4:3。
func clientRatio(given float64, hwnd uintptr) float64 {
	if given > 0 {
		return given
	}
	var cr windowRect
	if ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr))); ret != 0 &&
		cr.right > cr.left && cr.bottom > cr.top {
		return float64(cr.right-cr.left) / float64(cr.bottom-cr.top)
	}
	return 64.0 / 45.0
}

// fitInside 在格子里按源比例等比居中（竖窗/横窗混排时不拉伸变形）。
func fitInside(r windowRect, aspect float64) windowRect {
	w, hh := r.right-r.left, r.bottom-r.top
	if w <= 0 || hh <= 0 {
		return r
	}
	if aspect <= 0 {
		return r
	}
	if float64(w)/float64(hh) > aspect {
		// 格子偏宽 → 左右留空
		nw := int32(float64(hh) * aspect)
		r.left += (w - nw) / 2
		r.right = r.left + nw
	} else {
		nh := int32(float64(w) / aspect)
		r.top += (hh - nh) / 2
		r.bottom = r.top + nh
	}
	return r
}

// wallClick 点到哪个格子就把哪个真实窗口调到前台（最小化的先还原）。
func wallClick(x, y int32) {
	for _, t := range wallTiles {
		if x < t.rect.left || x > t.rect.right || y < t.rect.top || y > t.stripR.bottom {
			continue
		}
		if r, _, _ := procIsIconic.Call(t.hwnd); r != 0 {
			procShowWindow.Call(t.hwnd, swRestore)
		}
		procSetForeground.Call(t.hwnd)
		return
	}
}

// makeWallFont 建 9pt 的界面字体（按窗口 DPI 换算像素高度）。
func makeWallFont(dpi int) uintptr {
	h := -dpi * 9 / 72
	if h >= 0 {
		h = -12
	}
	face, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	f, _, _ := procCreateFontW.Call(
		uintptr(h), 0, 0, 0, 400,
		0, 0, 0, 1, // DEFAULT_CHARSET
		0, 0, 5, // CLEARTYPE_QUALITY
		0, uintptr(unsafe.Pointer(face)))
	return f
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
