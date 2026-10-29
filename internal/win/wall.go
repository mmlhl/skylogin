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
	procSetBkMode     = gdi32.NewProc("SetBkMode")
	procSetTextColor  = gdi32.NewProc("SetTextColor")
	procSelectObject  = gdi32.NewProc("SelectObject")
	procCreateFontW   = gdi32.NewProc("CreateFontW")
	procDeleteObject  = gdi32.NewProc("DeleteObject")
	procCreateSolidBr = gdi32.NewProc("CreateSolidBrush")
	procCreatePen     = gdi32.NewProc("CreatePen")
	procRoundRect     = gdi32.NewProc("RoundRect")
	procMoveToEx      = gdi32.NewProc("MoveToEx")
	procLineTo        = gdi32.NewProc("LineTo")

	procFrameRect       = user32dll.NewProc("FrameRect")
	procTrackMouseEvent = user32dll.NewProc("TrackMouseEvent")
)

var gdi32 = syscall.NewLazyDLL("gdi32.dll")

const (
	wmWallTask     = 0x0400 + 2 // WM_APP+2（托盘占了 +1）
	wmSizeWin      = 0x0005
	wmPaintWin     = 0x000F
	wmEraseBkgnd   = 0x0014
	wmLButtonDownW = 0x0201
	wmMouseMove    = 0x0200
	wmMouseLeave   = 0x02A3
	wmExitSizeMove = 0x0232

	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	// 墙只看不接管：NOACTIVATE 让点击墙不会把前台从游戏手里抢走（游戏
	// 焦点不被打断，仲裁的"谁在前台"判断也不受墙干扰）；切客户端由
	// wallClick 主动做完整交接
	wsExNoActivate = 0x08000000
	wsExAppwindow  = 0x00040000
	idcArrow       = 32512
	colorBtnFace   = 15

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
type WallClient struct {
	Hwnd  uintptr
	Pid   uint32 // 源客户端进程（格子右上角 × 强停用）
	Label string
	Ratio float64
}

// wallTile 一个格子的全部状态（只在墙线程上访问）。
type wallTile struct {
	hwnd   uintptr
	pid    uint32 // 源客户端进程
	label  string
	thumb  uintptr    // 主窗口缩略图（铺满格子，每秒热替换）
	rect   windowRect // 画面区（墙客户区坐标）
	bandR  windowRect // 顶部标题带：左边账号名、右边红 ×（在缩略图区域外，永不被画面盖住）
	closeR windowRect // 标题带右端的强停按钮（红底白 ×）
	aspect float64    // 主窗口当前宽高比
}

// WallHooks 墙与 app 层的交互点（app 启动后注入一次；win 层不含业务）。
type WallHooks struct {
	SavedRect  func() (x, y, w, h int, ok bool) // 上次记住的窗口位置尺寸
	RectSink   func(x, y, w, h int)             // 位置尺寸变化（拖拽结束/关闭时回传）
	Actions    map[string]func()                // 快捷菜单按钮动作（key = 按钮串）
	StopClient func(pid uint32)                 // 格子右上角 ×：强行停止该客户端
}

// wallMenu 快捷菜单按钮（顶栏，从左到右）。
// 排列方案与多开弹窗同一套（dual 默认）；danger 类按钮红调示警。
var wallMenu = []struct {
	id, text string
	danger   bool
}{
	{"tile", "平铺", false},
	{"dual", "双摞", false},
	{"rows", "两行", false},
	{"cols", "两列", false},
	{"fan", "满扇", false},
	{"stopall", "全部停止", true},
}

// trackMouseEvent 是 TRACKMOUSEEVENT（收 WM_MOUSELEAVE 用）。
type trackMouseEvent struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

var (
	wallHooks     WallHooks
	wallMenuRects []windowRect // 顶栏按钮矩形（墙线程维护，wallClick 只读）
	// 强停按钮的悬停状态（墙线程维护）
	wallMouse      point
	wallMouseIn    bool
	wallHoverTile  int
	wallLeaveArmed bool
)

// SetWallHooks 注入 app 层交互点（开墙前调用一次）。
func SetWallHooks(h WallHooks) { wallHooks = h }

var (
	wallMu    sync.Mutex
	wallHwnd  uintptr
	wallTasks chan func()
	// 下面三个只在墙线程上访问
	wallTiles []wallTile
	wallFont  uintptr
	wallRatio float64 // 画面宽高比（来自源窗口客户区），默认 64:45
	// 界面小部件的画笔/画刷（墙线程创建、退出时销毁）
	wallRedBrush    uintptr // 强停按钮悬停时的深红底
	wallLightBrush  uintptr // 强停按钮平时的浅红底
	wallWhitePen    uintptr // 悬停时的白色 ×
	wallRedPen      uintptr // 平时的红 ×
	wallBtnBrush    uintptr // 菜单按钮淡金底
	wallBtnDanger   uintptr // 危险按钮淡红底
	wallBtnFrame    uintptr // 按钮边框画笔
	wallBtnFrameHot uintptr // 悬停/强调边框画笔
	// 墙线程在 wallMu 下维护的格子位置快照，给 WallTileCenter 跨线程读
	wallRects []windowRect
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

	// 默认尺寸：工作区的六成左右，夹在合理范围内，屏幕居中；
	// 记住过上次的就恢复上次的（显示器配置变了会被 saneWallRect 拒掉）
	wa := primaryWorkArea()
	workW, workH := wa.right-wa.left, wa.bottom-wa.top
	w := clampInt(int(workW)*3/5, 880, 1400)
	h := clampInt(int(workH)*3/4, 620, 920)
	x := wa.left + (workW-int32(w))/2
	y := wa.top + (workH-int32(h))/2
	if wallHooks.SavedRect != nil {
		if sx, sy, sw, sh, ok := wallHooks.SavedRect(); ok && saneWallRect(sx, sy, sw, sh) {
			x, y = int32(sx), int32(sy)
			w, h = sw, sh
		}
	}

	title, _ := syscall.UTF16PtrFromString("光遇登录器 · 监控墙")
	// APPWINDOW：任务栏 / Alt+Tab 里能看到监控墙（不然窗口被盖住后很难找回）。
	// NOACTIVATE 保留：点墙仍然不抢游戏前台（多开挂机的核心约束）。
	hwnd, _, _ := procCreateWinEx.Call(wsExNoActivate|wsExAppwindow, uintptr(unsafe.Pointer(clsName)),
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

	dpi, _, _ := procGetDpiForWin.Call(hwnd)
	wallFont = makeWallFont(int(dpi))
	wallRatio = 64.0 / 45.0
	// 界面小部件：红底白 ×、菜单按钮
	wallRedBrush, _, _ = procCreateSolidBr.Call(0x003539E5)   // 悬停底色 RGB(229,57,53)，COLORREF 是 BGR
	wallLightBrush, _, _ = procCreateSolidBr.Call(0x00E5E7FA) // 平时底色 RGB(250,231,229)
	// 菜单按钮：与应用主题一致的配色（米白底 + 浅灰边 + 深字，
	// 排列方案按钮 hover 感的金色点缀、危险按钮红调）
	wallBtnBrush, _, _ = procCreateSolidBr.Call(0x00F4EFDF)      // 淡金底 RGB(223,239,244)→BGR
	wallBtnDanger, _, _ = procCreateSolidBr.Call(0x00DDE8FB)     // 淡红底 RGB(251,232,221)
	wallBtnFrame, _, _ = procCreatePen.Call(0, 1, 0x00B8C6D8)    // 柔和蓝灰边 RGB(216,198,184)
	wallBtnFrameHot, _, _ = procCreatePen.Call(0, 1, 0x007CA8BC) // 主题金边 RGB(188,168,124)
	penW := int32(dpi) * 2 / 96
	if penW < 2 {
		penW = 2
	}
	wallWhitePen, _, _ = procCreatePen.Call(0, uintptr(penW), 0x00FFFFFF) // PS_SOLID
	wallRedPen, _, _ = procCreatePen.Call(0, uintptr(penW), 0x003438CB)   // RGB(203,56,52)

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

	for _, res := range []uintptr{wallFont, wallRedBrush, wallLightBrush, wallWhitePen, wallRedPen,
		wallBtnBrush, wallBtnDanger, wallBtnFrame, wallBtnFrameHot} {
		if res != 0 {
			procDeleteObject.Call(res)
		}
	}
	wallFont = 0
	wallRedBrush, wallWhitePen = 0, 0
	wallBtnBrush, wallBtnDanger, wallBtnFrame, wallBtnFrameHot = 0, 0, 0, 0
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
		relayoutWall(false)
		return 0
	case wmExitSizeMove:
		saveWallRect(hwnd)
		return 0
	case wmPaintWin:
		paintWall(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1 // 背景在 WM_PAINT 里整块重画，避免闪
	case wmLButtonDownW:
		wallClick(int32(int16(lparam&0xFFFF)), int32(int16((lparam>>16)&0xFFFF)))
		return 0
	case wmMouseMove:
		wallMouse = point{int32(int16(lparam & 0xFFFF)), int32(int16((lparam >> 16) & 0xFFFF))}
		wallMouseIn = true
		armWallMouseLeave(hwnd)
		h := hoverWallButton()
		if h != wallHoverTile {
			wallHoverTile = h
			procInvalidate.Call(hwnd, 0, 1) // 悬停目标变了才重绘
		}
		return 0
	case wmMouseLeave:
		wallLeaveArmed = false
		wallMouseIn = false
		if wallHoverTile != -1 {
			wallHoverTile = -1
			procInvalidate.Call(hwnd, 0, 1)
		}
		return 0
	case wmClose:
		saveWallRect(hwnd)
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

// applyWallUpdate 全量对齐格子列表（每秒一次，墙线程持有全部"已应用"
// 状态，投递丢失时下一轮观测自带完整信息，不存在漂移）。
// 缩略图通过 relayoutWall(hotSwap) 每秒热替换，无需任何重建扳机。
func applyWallUpdate(clients []WallClient) {
	var kept []wallTile
	for _, c := range clients {
		if c.Hwnd == 0 {
			continue
		}
		if r, _, _ := procIsWindow.Call(c.Hwnd); r == 0 {
			continue
		}
		var thumb uintptr
		for _, old := range wallTiles {
			if old.hwnd != c.Hwnd {
				continue
			}
			thumb = old.thumb
			break
		}
		kept = append(kept, wallTile{
			hwnd:   c.Hwnd,
			pid:    c.Pid,
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
	relayoutWall(true)

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
// relayoutWall 按当前客户区给每个格子算位置并刷新缩略图。
// hotSwap = 每格缩略图热替换一次（重新注册新图、设好属性、再注销旧图）：
// 新旧同源同位、画面相同，切换零闪烁。DWM 缩略图在游戏表面挂起/恢复、
// 全屏模式切换之后可能失去活链接——与其猜"什么事件之后该重建"，不如
// 每秒热替换一次，任何原因的失效都在 1 秒内自愈。墙窗口缩放走
// hotSwap=false（拖拽时 WM_SIZE 风暴，只更新属性不重注册）。
func relayoutWall(hotSwap bool) {
	h := hwndOfWall()
	if h == 0 {
		return
	}
	var cr windowRect
	if ret, _, _ := procGetClientRect.Call(h, uintptr(unsafe.Pointer(&cr))); ret == 0 {
		return
	}
	W, H := cr.right, cr.bottom
	dpi, _, _ := procGetDpiForWin.Call(h)
	gap := int32(dpi) * 8 / 96
	strip := int32(dpi) * 28 / 96 // 每格顶部标题带高
	barH := layoutWallMenu(int32(dpi))
	n := len(wallTiles)
	if n == 0 {
		wallMu.Lock()
		wallRects = nil
		wallMu.Unlock()
		procInvalidate.Call(h, 0, 1)
		return
	}
	H -= barH // 顶栏以下才是格子区

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
	y0 := barH + (H-(int32(rows)*th+int32(rows+1)*gap))/2

	// 格子右上角的强停按钮（平时浅色低调，悬停变深红）
	btn := int32(dpi) * 20 / 96
	for i := range wallTiles {
		c, r := int32(i%bestCols), int32(i/bestCols)
		lx := x0 + c*(bestTile+gap) + gap
		ly := y0 + r*(th+gap) + gap
		wallTiles[i].bandR = windowRect{left: lx, top: ly, right: lx + bestTile, bottom: ly + strip}
		wallTiles[i].rect = windowRect{left: lx, top: ly + strip, right: lx + bestTile, bottom: ly + th}
		cy := ly + (strip-btn)/2
		wallTiles[i].closeR = windowRect{
			left: lx + bestTile - btn - int32(dpi)*3/96, top: cy,
			right: lx + bestTile - int32(dpi)*3/96, bottom: cy + btn,
		}

		t := wallTiles[i]
		// 最小化的窗口没有渲染表面，缩略图是空白——先还原再捕
		if iconic, _, _ := procIsIconic.Call(t.hwnd); iconic != 0 {
			procShowWindow.Call(t.hwnd, swRestore)
		}
		rcDest := fitInside(wallTiles[i].rect, t.aspect)
		if hotSwap || t.thumb == 0 {
			var nu uintptr
			if hr, _, _ := procDwmRegister.Call(h, t.hwnd, uintptr(unsafe.Pointer(&nu))); hr == 0 {
				props := dwmThumbProps{
					dwFlags:  dwmTnpRectDest | dwmTnpOpacity | dwmTnpVisible,
					rcDest:   rcDest,
					opacity:  255,
					fVisible: 1,
				}
				procDwmUpdateProps.Call(nu, uintptr(unsafe.Pointer(&props)))
				if t.thumb != 0 {
					procDwmUnregister.Call(t.thumb)
				}
				wallTiles[i].thumb = nu
				continue
			}
			// 注册失败：退回旧缩略图继续用
		}
		if t.thumb != 0 {
			props := dwmThumbProps{
				dwFlags:  dwmTnpRectDest | dwmTnpOpacity | dwmTnpVisible,
				rcDest:   rcDest,
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

// layoutWallMenu 摆顶栏快捷菜单按钮，返回栏高。
// 按钮宽按 CJK 字数估（标签都是中文，14px/字 + 内边距，96dpi 基准）。
func layoutWallMenu(dpi int32) int32 {
	barH := int32(dpi) * 34 / 96
	scale := func(v int32) int32 { return v * dpi / 96 }
	x := scale(8)
	rects := make([]windowRect, 0, len(wallMenu))
	for _, b := range wallMenu {
		w := scale(int32(len([]rune(b.text)))*14 + 24)
		rects = append(rects, windowRect{left: x, top: scale(5), right: x + w, bottom: barH - scale(5)})
		x += w + scale(6)
	}
	wallMenuRects = rects
	return barH
}

// saneWallRect 上次记住的位置还能用吗：尺寸够用且与虚拟屏幕相交
// （显示器配置变了别把墙丢到看不见的地方）。
func saneWallRect(x, y, w, h int) bool {
	if w < 480 || h < 360 {
		return false
	}
	vx, _, _ := procGetSystemMetrics.Call(76) // SM_XVIRTUALSCREEN
	vy, _, _ := procGetSystemMetrics.Call(77)
	vw, _, _ := procGetSystemMetrics.Call(78)
	vh, _, _ := procGetSystemMetrics.Call(79)
	return x < int(vx)+int(vw) && y < int(vy)+int(vh) && x+w > int(vx) && y+h > int(vy)
}

// armWallMouseLeave 挂一次 WM_MOUSELEAVE 跟踪（已挂着就不重复）。
func armWallMouseLeave(hwnd uintptr) {
	if wallLeaveArmed {
		return
	}
	tme := trackMouseEvent{CbSize: uint32(unsafe.Sizeof(trackMouseEvent{})), DwFlags: 2, HwndTrack: hwnd}
	if r, _, _ := procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme))); r != 0 {
		wallLeaveArmed = true
	}
}

// hoverWallButton 鼠标当前悬停在哪个格子的强停按钮上，-1 = 都不是。
func hoverWallButton() int {
	if !wallMouseIn {
		return -1
	}
	for i, t := range wallTiles {
		if ptInRect(wallMouse, t.closeR) {
			return i
		}
	}
	return -1
}

// ptInRect 点是否在矩形内。
func ptInRect(p point, r windowRect) bool {
	return p.X >= r.left && p.X <= r.right && p.Y >= r.top && p.Y <= r.bottom
}

// saveWallRect 把墙当前位置尺寸回传 app 层持久化（拖拽结束/关闭时）。
func saveWallRect(hwnd uintptr) {
	if wallHooks.RectSink == nil {
		return
	}
	var r windowRect
	if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret != 0 && r.right > r.left {
		wallHooks.RectSink(int(r.left), int(r.top), int(r.right-r.left), int(r.bottom-r.top))
	}
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
		paintWallMenu(hdc)
		return
	}
	for i, t := range wallTiles {
		if t.label == "" {
			continue
		}
		s, err := syscall.UTF16PtrFromString(t.label)
		if err != nil {
			continue
		}
		r := t.bandR
		r.left += 6
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(s)), ^uintptr(0),
			uintptr(unsafe.Pointer(&r)), dtCenter|dtVCenter|dtSingleLine|dtEndEllipsis)
		// 右上角强停按钮：平时浅红低调，鼠标靠近才变深红
		bg, pen := wallLightBrush, wallRedPen
		if wallHoverTile == i {
			bg, pen = wallRedBrush, wallWhitePen
		}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&t.closeR)), bg)
		old, _, _ := procSelectObject.Call(hdc, pen)
		inset := (t.closeR.right - t.closeR.left) / 3
		lx, ty, rx, by := t.closeR.left+inset, t.closeR.top+inset, t.closeR.right-inset, t.closeR.bottom-inset
		procMoveToEx.Call(hdc, uintptr(lx), uintptr(ty), 0)
		procLineTo.Call(hdc, uintptr(rx), uintptr(by))
		procMoveToEx.Call(hdc, uintptr(rx), uintptr(ty), 0)
		procLineTo.Call(hdc, uintptr(lx), uintptr(by))
		procSelectObject.Call(hdc, old)
	}
	paintWallMenu(hdc)
}

// paintWallMenu 画顶栏快捷菜单按钮：圆角胶囊底 + 主题配色，
// 危险按钮（全部停止）红调与排列按钮区分开。
func paintWallMenu(hdc uintptr) {
	for i, b := range wallMenu {
		if i >= len(wallMenuRects) {
			break
		}
		r := wallMenuRects[i]
		fill, frame := wallBtnBrush, wallBtnFrame
		textColor := uintptr(0x00443EBE) // 主题金棕 RGB(190,62,68)
		if b.danger {
			fill, frame, textColor = wallBtnDanger, wallBtnFrame, uintptr(0x002A50D8) // 红 RGB(216,80,42)
		}
		// 圆角胶囊：先底色再描边（RoundRect 用当前画笔画轨、画刷填充）
		oldBr, _, _ := procSelectObject.Call(hdc, fill)
		oldPen, _, _ := procSelectObject.Call(hdc, frame)
		procRoundRect.Call(hdc, uintptr(r.left), uintptr(r.top), uintptr(r.right), uintptr(r.bottom),
			uintptr(r.bottom-r.top)/2, uintptr(r.bottom-r.top)/2)
		procSelectObject.Call(hdc, oldPen)
		procSelectObject.Call(hdc, oldBr)
		text, _ := syscall.UTF16PtrFromString(b.text)
		procSetTextColor.Call(hdc, textColor)
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(text)), ^uintptr(0),
			uintptr(unsafe.Pointer(&r)), dtCenter|dtVCenter|dtSingleLine)
		procSetTextColor.Call(hdc, 0x003A3A3A)
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

// wallClick 点哪个格子就激活哪个客户端的主窗——格子里显示的画面就是
// 主窗，弹窗（登录骨架等）是主窗的 owned 窗口，主窗激活后仍盖在上面、
// 照常接收点击，和 Alt+Tab 到游戏的行为一致。
//
// 不要按 Z 序挑"最上层窗口"：DUI 的透明影子窗（DuiShadowWnd）永远叠在
// 弹窗上面，激活它不产生有效输入，还会引发引擎把前台抢回主窗——
// "要点两次墙才能点登录按钮"就是这么来的。引擎自己想要的正是主窗。
func wallClick(x, y int32) {
	in := func(r windowRect) bool { return ptInRect(point{x, y}, r) }
	// 快捷菜单：动作放后台跑（重排/停止都可能要几秒），墙线程不等
	for i, r := range wallMenuRects {
		if !in(r) {
			continue
		}
		if act := wallHooks.Actions[wallMenu[i].id]; act != nil {
			go act()
		}
		return
	}
	for _, t := range wallTiles {
		// 格子右上角红 ×：立即强停该客户端
		if in(t.closeR) {
			if stop := wallHooks.StopClient; stop != nil {
				go stop(t.pid)
			}
			return
		}
		if x < t.bandR.left || x > t.bandR.right || y < t.bandR.top || y > t.rect.bottom {
			continue
		}
		if r, _, _ := procIsIconic.Call(t.hwnd); r != 0 {
			procShowWindow.Call(t.hwnd, swRestore)
		}
		activateWindow(t.hwnd)
		return
	}
}

// activateWindow 把窗口带到前台。墙是 NOACTIVATE 的，点墙不会让本进程
// 成为前台；但刚收到点击（输入事件）的进程直接设置一般能成功，
// 被前台锁拒绝时才挂到目标线程的输入队列再设一次。
func activateWindow(h uintptr) {
	if r, _, _ := procSetForeground.Call(h); r != 0 {
		return
	}
	var tid uint32
	procGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&tid)))
	our, _, _ := procGetCurrentThreadId.Call()
	if tid == 0 || uintptr(tid) == our {
		procSetForeground.Call(h)
		return
	}
	procAttachThreadInput.Call(our, uintptr(tid), 1)
	procSetForeground.Call(h)
	procSetFocus.Call(h)
	procAttachThreadInput.Call(our, uintptr(tid), 0)
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
