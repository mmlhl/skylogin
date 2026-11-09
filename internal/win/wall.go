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
	procScreenToClient = user32dll.NewProc("ScreenToClient")
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
	procRectangle     = gdi32.NewProc("Rectangle")
	procEllipse       = gdi32.NewProc("Ellipse")
	procGetStockObj   = gdi32.NewProc("GetStockObject")
	procMoveToEx      = gdi32.NewProc("MoveToEx")
	procLineTo        = gdi32.NewProc("LineTo")

	procSetTimer   = user32dll.NewProc("SetTimer")
	procKillTimer  = user32dll.NewProc("KillTimer")

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
	wmTimerWin     = 0x0113
	// 悬停提示的定时器 id（延迟 600ms 才显示）
	wallTipTimerID = 1
	wmNcHitTest    = 0x0084
	wmNcCalcSize   = 0x0083
	wmGetMinMax    = 0x0024

	// 无边框窗口的命中区：自己判断边缘，保住拖边缩放
	htCaption     = 2
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17
	resizeBorder  = 6 // 边缘可拖动的宽度（客户区像素）

	// wsPopup / wsThickFrame 在 arrange.go 里已有定义（窗口化/全屏切换用）
	wsClipChildren = 0x02000000
	swMinimize     = 6

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

// 快捷菜单分两组：左边一槽是"排列方案"（互斥，选中的那项高亮），
// 右边是工具按钮（打码、全部停止）。分组是为了让"摆窗口"和"动客户端"
// 这两类动作在视觉上分得开——以前七个长得一样的胶囊排一排，看不出主次。
const (
	menuGroupArrange = iota // 排列方案（互斥，选中的那项高亮）
	menuGroupTool           // 工具按钮（独立胶囊）
	menuGroupSys            // 窗口控制（最小化 / 最大化还原 / 关闭）
)

// wallMenu 顶栏按钮（从左到右）。
//
// 全部图标化：文字标签在 480 宽的最小窗口下会和右侧工具按钮挤在一起，
// 而且六个两字标签并排读起来像一排按钮，没有主次。图标按钮宽度固定，
// 布局不再随文字长度变化，窄窗口也不会重叠。text 只用于悬停提示。
//
// 排列方案与多开弹窗同一套（dual 默认）。
var wallMenu = []struct {
	id, text string
	group    int
	danger   bool
}{
	{"tile", "平铺", menuGroupArrange, false},
	{"dual", "双摞", menuGroupArrange, false},
	{"rows", "两行", menuGroupArrange, false},
	{"cols", "两列", menuGroupArrange, false},
	{"fan", "满扇", menuGroupArrange, false},
	{"full", "全屏", menuGroupArrange, false},
	{"mask", "打码", menuGroupTool, false},
	{"stopall", "全部停止", menuGroupTool, true},
	// 无边框后系统不再画窗口控制按钮，自己来（顺序同 Windows：最小化 / 最大化 / 关闭）
	{"min", "最小化", menuGroupSys, false},
	{"max", "最大化", menuGroupSys, false},
	{"close", "关闭", menuGroupSys, true},
}

// wallTooltipIdx 当前显示悬停提示的按钮下标（-1 = 不显示）。
var wallTooltipIdx = -1

// wallMenuOn 记录"开关型"按钮的状态（目前只有打码）：开着时图标点亮成金色。
var wallMenuOn = map[string]bool{}

// wallMenuDivs 分组之间的竖线位置（客户区 x，绘制用；布局时算好）。
var wallMenuDivs []int32

// wallBarH 顶栏高度（布局时算好，绘制用）。
var wallBarH int32

// wallDPI 墙窗口的 DPI（提示条排版用）。
var wallDPI int32 = 96


// wallMenuActive 当前高亮的排列方案（点过哪个就亮哪个，纯视觉状态）。
var wallMenuActive = "tile"

// wallSlotRect 排列组那个分段控件的整体槽（墙线程维护，绘制用）。
var wallSlotRect windowRect

// wallHoverMenu 鼠标悬停的菜单项下标，-1 = 没有。
var wallHoverMenu = -1


// SetWallMenuOn 设置"开关型"按钮的状态（目前只有打码），开着时图标点亮成金色。
// 图标按钮宽度固定，状态变化不需要重排，重绘一次就够。
func SetWallMenuOn(id string, on bool) {
	wallMu.Lock()
	changed := wallMenuOn[id] != on
	wallMenuOn[id] = on
	h := wallHwnd
	wallMu.Unlock()
	if !changed || h == 0 {
		return
	}
	procInvalidate.Call(h, 0, 1)
}

// postWallTask 把一段操作投到墙线程执行（墙没开就丢弃）。
func postWallTask(f func()) {
	wallMu.Lock()
	h, ch := wallHwnd, wallTasks
	wallMu.Unlock()
	if h == 0 || ch == nil {
		return
	}
	select {
	case ch <- f:
		procPostMessageW.Call(h, wmWallTask, 0, 0)
	default: // 队列满说明墙线程卡了，这轮丢弃
	}
}

// trackMouseEvent 是 TRACKMOUSEEVENT（收 WM_MOUSELEAVE 用）。
type trackMouseEvent struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

// minMaxInfo 是 MINMAXINFO（无边框窗口限制最大化范围用）。
type minMaxInfo struct {
	ptReserved     point
	ptMaxSize      point
	ptMaxPosition  point
	ptMinTrackSize point
	ptMaxTrackSize point
}

var (
	wallHooks     WallHooks
	wallMenuRects []windowRect // 顶栏按钮矩形（墙线程维护，wallClick 只读）
	wallMenuIcons []windowRect // 分段里的小示意图区（排列组才有）
	wallMenuTexts []windowRect // 分段里的文字区
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
	wallRedBrush   uintptr // 强停按钮悬停时的深红底
	wallLightBrush uintptr // 强停按钮平时的浅红底
	wallWhitePen   uintptr // 悬停时的白色 ×
	wallRedPen     uintptr // 平时的红 ×
	// 深色主题（跟主界面同一套：surface 分层 + 金色 accent）
	wallBgBrush     uintptr // 墙背景 RGB(20,20,22)
	wallBarBrush    uintptr // 顶栏底 RGB(27,27,31)
	wallHairlinePen uintptr // 1px 分隔线 RGB(38,38,42)
	wallHoverBrush  uintptr // 悬停底 RGB(32,32,36)
	wallAccentBrush uintptr // 选中下划线（金）RGB(216,180,124)
	// 顶栏图标：常态灰 / 悬停白 / 当前项金（复用 wallAccentBrush）
	wallGlyphBrush    uintptr
	wallGlyphHotBrush uintptr
	wallGlyphPen      uintptr
	wallGlyphHotPen   uintptr
	wallAccentPen     uintptr // 金色画笔（打码"隐藏中"的斜线用）
	wallCutPen        uintptr // 背景色画笔（把眼睛斜切一刀）
	// 全部停止图标：红色空心方块（比实心刺眼度低，但仍一眼看出是危险动作）
	wallStopGlyphPen    uintptr
	wallStopGlyphHotPen uintptr
	// 悬停提示条
	wallTipBrush uintptr
	wallTipPen   uintptr
	// 图标字体：Segoe MDL2 Assets 画动作图标（眼睛/全屏/窗口控制），
	// 抗锯齿由字体渲染负责——GDI 手绘线条在 14px 下锯齿太明显。
	wallIconFont uintptr
	// 符号字体：画 □ 这类空心方块（停止按钮用），雅黑里就有
	wallSymFont uintptr
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
	// 无边框（WS_POPUP 取代 WS_CAPTION）：系统标题栏的深浅由 DWM 决定，
	// 拖动时它会把 per-window 深色建议丢掉、回落成系统浅色（看着像反白），
	// 而 Win10 没有"指定标题栏颜色"的 API。干脆不要标题栏，顶栏自己画、
	// 自己处理拖动（WM_NCHITTEST 返回 HTCAPTION），颜色就永远是我们说了算。
	// WS_THICKFRAME 保留：窗口仍可拖边缩放，也才有 Aero 阴影。
	// APPWINDOW：任务栏 / Alt+Tab 里能看到监控墙（不然窗口被盖住后很难找回）。
	// NOACTIVATE 保留：点墙仍然不抢游戏前台（多开挂机的核心约束）。
	hwnd, _, _ := procCreateWinEx.Call(wsExNoActivate|wsExAppwindow, uintptr(unsafe.Pointer(clsName)),
		uintptr(unsafe.Pointer(title)), wsPopup|wsThickFrame|wsClipChildren|wsVisible,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		0, 0, hInst, 0)
	if hwnd == 0 {
		ready <- syscall.EINVAL
		return
	}
	applyDarkFrame(hwnd)
	// 同样去掉 DWM 描的那圈边框（深色顶栏上方一条灰线）
	DisableNonClientFrame(hwnd)

	// 固定在最顶层：挂机看号时墙不能被游戏窗口盖住
	// (HWND_TOPMOST=-1, SWP_NOSIZE|SWP_NOMOVE|SWP_NOACTIVATE)
	procSetWindowPos.Call(hwnd, ^uintptr(0), 0, 0, 0, 0, 0x0001|0x0002|0x0010)

	dpi, _, _ := procGetDpiForWin.Call(hwnd)
	wallFont = makeWallFont(int(dpi))
	wallIconFont = makeGlyphFont("Segoe MDL2 Assets", 15, int(dpi))
	wallSymFont = makeGlyphFont("Microsoft YaHei UI", 15, int(dpi))
	wallRatio = 64.0 / 45.0
	// 界面小部件：红底白 ×、菜单按钮
	wallRedBrush, _, _ = procCreateSolidBr.Call(0x003539E5)   // 悬停底色 RGB(229,57,53)，COLORREF 是 BGR
	wallLightBrush, _, _ = procCreateSolidBr.Call(0x00E5E7FA) // 平时底色 RGB(250,231,229)
	// 菜单按钮：与应用主题一致的配色（米白底 + 浅灰边 + 深字，
	// 排列方案按钮 hover 感的金色点缀、危险按钮红调）
	// 深色主题：墙背景、顶栏、分隔线、悬停底、选中金线
	wallBgBrush, _, _ = procCreateSolidBr.Call(0x00161414)     // RGB(20,20,22)
	wallBarBrush, _, _ = procCreateSolidBr.Call(0x001F1B1B)    // RGB(27,27,31)
	wallHairlinePen, _, _ = procCreatePen.Call(0, 1, 0x002A2626) // RGB(38,38,42)
	wallHoverBrush, _, _ = procCreateSolidBr.Call(0x00242020)  // RGB(32,32,36)
	wallAccentBrush, _, _ = procCreateSolidBr.Call(0x007CB4D8) // RGB(216,180,124)
	// 顶栏图标：常态灰、悬停白（金用 wallAccentBrush）
	wallGlyphBrush, _, _ = procCreateSolidBr.Call(0x009CA2A5)    // RGB(165,162,156)
	wallGlyphHotBrush, _, _ = procCreateSolidBr.Call(0x00EFF2F4) // RGB(244,242,239)
	wallGlyphPen, _, _ = procCreatePen.Call(0, 1, 0x009CA2A5)
	wallGlyphHotPen, _, _ = procCreatePen.Call(0, 1, 0x00EFF2F4)
	wallAccentPen, _, _ = procCreatePen.Call(0, 2, 0x007CB4D8)
	wallCutPen, _, _ = procCreatePen.Call(0, 2, 0x001F1B1B) // 顶栏底色，切断眼睛用
	wallStopGlyphPen, _, _ = procCreatePen.Call(0, 2, 0x003A5AE0)    // 红 RGB(224,90,58)
	wallStopGlyphHotPen, _, _ = procCreatePen.Call(0, 2, 0x00506BFF) // 亮红 RGB(255,107,80)
	wallTipBrush, _, _ = procCreateSolidBr.Call(0x002A2626)          // 提示条底 RGB(38,38,42)
	wallTipPen, _, _ = procCreatePen.Call(0, 1, 0x00403A3A)          // 提示条边 RGB(58,58,64)
	penW := int32(dpi) * 2 / 96
	if penW < 2 {
		penW = 2
	}
	wallWhitePen, _, _ = procCreatePen.Call(0, uintptr(penW), 0x00FFFFFF) // PS_SOLID
	wallRedPen, _, _ = procCreatePen.Call(0, uintptr(penW), 0x003438CB)   // RGB(203,56,52)

	wallMu.Lock()
	wallHwnd = hwnd
	wallMu.Unlock()
	// 建窗时的 WM_SIZE 早于 wallHwnd 赋值，那次布局被 hwndOfWall()==0 挡掉了；
	// 这里补一次，否则没有客户端时顶栏永远是空的（要等第一次 WallUpdate 才出来）。
	relayoutWall(false)
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
		wallBgBrush, wallBarBrush, wallHairlinePen, wallHoverBrush, wallAccentBrush,
		wallGlyphBrush, wallGlyphHotBrush, wallGlyphPen, wallGlyphHotPen, wallAccentPen, wallCutPen,
		wallStopGlyphPen, wallStopGlyphHotPen, wallTipBrush, wallTipPen,
		wallIconFont, wallSymFont} {
		if res != 0 {
			procDeleteObject.Call(res)
		}
	}
	wallFont = 0
	wallRedBrush, wallWhitePen = 0, 0
	wallBgBrush, wallBarBrush, wallHairlinePen = 0, 0, 0
	wallHoverBrush, wallAccentBrush = 0, 0
	wallGlyphBrush, wallGlyphHotBrush, wallGlyphPen, wallGlyphHotPen = 0, 0, 0, 0
	wallStopGlyphPen, wallStopGlyphHotPen, wallTipBrush, wallTipPen = 0, 0, 0, 0
	wallIconFont, wallSymFont = 0, 0
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
	case wmNcCalcSize:
		// 客户区占满整个窗口：WS_THICKFRAME 在浅色主题下会给无边框窗口
		// 画一圈浅色边框（深色界面上就是一道白边），返回 0 不调 DefWindowProc
		// 就把它去掉。边缘缩放改由 WM_NCHITTEST 自己判断。
		if wparam != 0 {
			return 0
		}
	case wmGetMinMax:
		// 没有标题栏之后系统不知道任务栏边界，最大化会盖住任务栏
		mmi := (*minMaxInfo)(unsafe.Pointer(lparam))
		wa := primaryWorkArea()
		mmi.ptMaxPosition = point{X: wa.left, Y: wa.top}
		mmi.ptMaxSize = point{X: wa.right - wa.left, Y: wa.bottom - wa.top}
		mmi.ptMinTrackSize = point{X: 480, Y: 360}
		return 0
	case wmNcHitTest:
		// 无边框窗口：边缘 → 缩放，顶栏空白 → 标题栏（交给系统拖动）。
		if ht := wallHitTest(hwnd, lparam); ht != 0 {
			return ht
		}
		r, _, _ := procDefWinProc.Call(hwnd, msg, wparam, lparam)
		return r
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
		h, m := hoverWallButton(), hoverWallMenu()
		if h != wallHoverTile || m != wallHoverMenu {
			wallHoverTile, wallHoverMenu = h, m
			// 悬停提示要延迟出现：扫过一排按钮时不该一路闪字
			procKillTimer.Call(hwnd, wallTipTimerID)
			wallTooltipIdx = -1
			if m >= 0 {
				procSetTimer.Call(hwnd, wallTipTimerID, 600, 0)
			}
			procInvalidate.Call(hwnd, 0, 1) // 悬停目标变了才重绘
		}
		return 0
	case wmTimerWin:
		if wparam == wallTipTimerID {
			procKillTimer.Call(hwnd, wallTipTimerID)
			if wallHoverMenu >= 0 && wallTooltipIdx != wallHoverMenu {
				wallTooltipIdx = wallHoverMenu
				procInvalidate.Call(hwnd, 0, 1)
			}
		}
		return 0
	case wmMouseLeave:
		wallLeaveArmed = false
		wallMouseIn = false
		procKillTimer.Call(hwnd, wallTipTimerID)
		if wallHoverTile != -1 || wallHoverMenu != -1 || wallTooltipIdx != -1 {
			wallHoverTile, wallHoverMenu, wallTooltipIdx = -1, -1, -1
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
	wallDPI = int32(dpi)
	gap := int32(dpi) * 8 / 96
	strip := int32(dpi) * 28 / 96 // 每格顶部标题带高
	barH := layoutWallMenu(int32(dpi), W)
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

// layoutWallMenu 摆顶栏，返回栏高。
//
// 全图标按钮，宽度固定 30（窗口控制 38），所以布局跟文字长度无关——
// 480 宽的最小窗口下也不会互相压住。分组之间留 6px + 一条 1px 竖线：
// 左边"怎么摆"、右边"对客户端做什么"、最右是窗口控制。
func layoutWallMenu(dpi, clientW int32) int32 {
	scale := func(v int32) int32 { return v * dpi / 96 }
	barH := scale(42)
	btn := scale(30)
	gap := scale(2)
	y0, y1 := (barH-btn)/2, (barH+btn)/2

	rects := make([]windowRect, len(wallMenu))

	// 左侧：排列组
	x := scale(10)
	left := x
	for i, b := range wallMenu {
		if b.group != menuGroupArrange {
			continue
		}
		rects[i] = windowRect{left: x, top: y0, right: x + btn, bottom: y1}
		x += btn + gap
	}
	wallSlotRect = windowRect{left: left, top: y0, right: x - gap, bottom: y1}

	// 分隔线 + 工具组：间距给足，让"摆窗口"和"动客户端"一眼分开
	x += scale(10)
	wallMenuDivs = []int32{x}
	x += scale(10) + scale(1)
	for i, b := range wallMenu {
		if b.group != menuGroupTool {
			continue
		}
		// 打码是开关，用文字标"点下去会发生什么"，比图标+颜色猜状态清楚
		w := btn
		if b.id == "mask" {
			w = scale(46)
		}
		rects[i] = windowRect{left: x, top: y0, right: x + w, bottom: y1}
		x += w + gap
	}

	// 右侧：窗口控制从右往左（比图标按钮宽一点，跟系统一致）
	winBtn := scale(38)
	winH := scale(26)
	x = clientW - scale(8)
	for i := len(wallMenu) - 1; i >= 0; i-- {
		if wallMenu[i].group != menuGroupSys {
			continue
		}
		rects[i] = windowRect{left: x - winBtn, top: (barH - winH) / 2, right: x, bottom: (barH + winH) / 2}
		x -= winBtn
	}
	wallMenuRects = rects
	wallBarH = barH
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

// hoverWallMenu 鼠标当前悬停在哪个顶栏按钮上，-1 = 都不是。
func hoverWallMenu() int {
	if !wallMouseIn {
		return -1
	}
	for i, r := range wallMenuRects {
		if r.right > r.left && ptInRect(wallMouse, r) {
			return i
		}
	}
	return -1
}

// wallHitTest 无边框窗口的命中判断：先边缘（缩放），再顶栏空白（拖动）。
// 返回 0 表示交给 DefWindowProc（画面区、按钮上）。
func wallHitTest(hwnd, lparam uintptr) uintptr {
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
	// 顶栏空白处 = 标题栏：能拖窗口、双击最大化
	if pt.Y >= 0 && pt.Y < wallSlotRect.bottom {
		for _, r := range wallMenuRects {
			if r.right > r.left && ptInRect(pt, r) {
				return 0 // 按钮上不拖动
			}
		}
		return htCaption
	}
	return 0
}

// ptInRect 点是否在矩形内。
func ptInRect(p point, r windowRect) bool {
	return p.X >= r.left && p.X <= r.right && p.Y >= r.top && p.Y <= r.bottom
}

// topBarDragZone 已并入 wallHitTest（无边框窗口要同时判断边缘缩放）。

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

	var full windowRect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&full)))
	// 深色底：跟主界面同一套 surface-0，缩略图边上留深色比浅灰耐看
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), wallBgBrush)

	if wallFont != 0 {
		procSelectObject.Call(hdc, wallFont)
	}
	procSetBkMode.Call(hdc, bkTransparent)
	procSetTextColor.Call(hdc, colTextOn)

	if len(wallTiles) == 0 {
		hint, _ := syscall.UTF16PtrFromString("没有正在运行的客户端——从主界面启动游戏后，这里会出现它们的实时画面")
		procSetTextColor.Call(hdc, colTextDim)
		var r windowRect
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(hint)), ^uintptr(0),
			uintptr(unsafe.Pointer(&r)), dtCenter|dtVCenter|dtSingleLine)
		paintWallMenu(hdc, hwnd)
		return
	}
	procSetTextColor.Call(hdc, colTextOn)
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
	paintWallMenu(hdc, hwnd)
}

// 顶栏配色（深色主题，COLORREF 是 BGR，注释写 RGB 便于对照）
const (
	colTextOn   = 0x00EFF2F4 // RGB(244,242,239) 主文字
	colTextDim  = 0x009CA2A5 // RGB(165,162,156) 次级文字
	colAccent   = 0x007CB4D8 // RGB(216,180,124) 当前项（金）
	colDanger   = 0x003A5AE0 // RGB(224,90,58)   危险动作
	colDangerOn = 0x00F0E0E8 // RGB(232,224,240) 危险动作悬停
)

// insetRect 四边各收 px（悬停底比 tab 略小一圈，不顶到相邻项）。
func insetRect(r windowRect, px int32) windowRect {
	return windowRect{left: r.left + px, top: r.top + px, right: r.right - px, bottom: r.bottom - px}
}

// fillRect 用画刷填一块矩形（FillRect 只要画刷，不需要画笔）。
func fillRect(hdc uintptr, r windowRect, brush uintptr) {
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
}

// drawGlyphText 用字体画一个图标字符，居中填满 box。
// 图标字体（Segoe MDL2 Assets）自带抗锯齿，比 GDI 手绘线条干净。
func drawGlyphText(hdc, font uintptr, s string, box windowRect, col uintptr) {
	if font == 0 {
		return
	}
	old, _, _ := procSelectObject.Call(hdc, font)
	procSetTextColor.Call(hdc, col)
	p, err := syscall.UTF16PtrFromString(s)
	if err == nil {
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(p)), ^uintptr(0),
			uintptr(unsafe.Pointer(&box)), dtCenter|dtVCenter|dtSingleLine)
	}
	procSelectObject.Call(hdc, old)
}

// strokeRect 用当前画笔描一个方框（LineTo 会连着画，最后回到起点）。
func strokeRect(hdc uintptr, l, t, r, b int32) {
	procMoveToEx.Call(hdc, uintptr(l), uintptr(t), 0)
	procLineTo.Call(hdc, uintptr(r), uintptr(t))
	procLineTo.Call(hdc, uintptr(r), uintptr(b))
	procLineTo.Call(hdc, uintptr(l), uintptr(b))
	procLineTo.Call(hdc, uintptr(l), uintptr(t))
}

// drawSysGlyph 画窗口控制按钮的符号（最小化 / 最大化-还原 / 关闭），
// 同样走图标字体：ChromeMinimize / ChromeMaximize / ChromeRestore / ChromeClose。
func drawSysGlyph(hdc, hwnd uintptr, id string, r windowRect) {
	glyph := "\uE921"
	switch id {
	case "min":
		glyph = "\uE921"
	case "max":
		if z, _, _ := procIsZoomed.Call(hwnd); z != 0 {
			glyph = "\uE923" // ChromeRestore
		} else {
			glyph = "\uE922" // ChromeMaximize
		}
	case "close":
		glyph = "\uE8BB" // ChromeClose
	}
	drawGlyphText(hdc, wallIconFont, glyph, r, colTextOn)
}

// paintWallMenu 画顶栏快捷菜单。
//
// 深色顶栏 + 金色当前项：排列方案是一排文字 tab，选中的那项金色文字、
// 底下压一条 2px 金线；工具按钮是纯文字（悬停才浮出淡底），危险动作用红。
// 没有边框、没有胶囊——顶栏是导航条，不是一组并列的按钮，画成一排
// 同样的圆角块只会显得像工具栏。
func paintWallMenu(hdc, hwnd uintptr) {
	barH := wallBarH
	if barH <= 0 {
		return
	}
	var full windowRect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&full)))
	fillRect(hdc, windowRect{left: 0, top: 0, right: full.right, bottom: barH}, wallBarBrush)
	// 底边 1px 分隔线，把顶栏和格子区分开
	old, _, _ := procSelectObject.Call(hdc, wallHairlinePen)
	procMoveToEx.Call(hdc, 0, uintptr(barH-1), 0)
	procLineTo.Call(hdc, uintptr(full.right), uintptr(barH-1))
	// 分组之间的竖线：高 16、居中
	for _, dx := range wallMenuDivs {
		procMoveToEx.Call(hdc, uintptr(dx), uintptr(barH/2-8), 0)
		procLineTo.Call(hdc, uintptr(dx), uintptr(barH/2+8))
	}
	procSelectObject.Call(hdc, old)

	for i, b := range wallMenu {
		if i >= len(wallMenuRects) {
			break
		}
		r := wallMenuRects[i]
		if r.right <= r.left {
			continue
		}
		hot := i == wallHoverMenu

		// 窗口控制按钮：无边框后系统不再画这些，自己来
		if b.group == menuGroupSys {
			if hot {
				if b.danger {
					fillRect(hdc, insetRect(r, 3), wallRedBrush)
				} else {
					fillRect(hdc, insetRect(r, 3), wallHoverBrush)
				}
			}
			drawSysGlyph(hdc, hwnd, b.id, r)
			continue
		}

		// 图标按钮：灰 → 悬停白 → 当前项/开关打开 金；危险动作恒为红
		active := b.id == wallMenuActive || wallMenuOn[b.id]
		brush := wallGlyphBrush
		col := uintptr(colTextDim)
		switch {
		case active:
			brush, col = wallAccentBrush, uintptr(colAccent)
		case hot:
			brush, col = wallGlyphHotBrush, uintptr(colTextOn)
		}
		if b.danger {
			col = uintptr(colDanger)
			if hot {
				col = uintptr(colDangerOn)
			}
		}
		if hot {
			fillRect(hdc, insetRect(r, 2), wallHoverBrush)
		}
		box := glyphBox(r)
		// 打码：睁眼 = 显示中，眼睛压一条斜线 = 隐藏中。
		// 图标字体里没有"闭眼"，斜线是最省事又一眼能懂的画法。
		if b.id == "mask" {
			drawGlyphText(hdc, wallIconFont, "\uE7B3", box, col)
			if wallMenuOn["mask"] {
				pen := wallGlyphPen
				switch {
				case active:
					pen = wallAccentPen
				case hot:
					pen = wallGlyphHotPen
				}
				// 斜线只压在眼睛身上：图标字符四周有留白，画满整个 box
				// 会拖出一条比眼睛还长的尾巴。内缩 3/14 正好贴着瞳孔到眼角。
				bw, bh := box.right-box.left, box.bottom-box.top
				old, _, _ := procSelectObject.Call(hdc, pen)
				procMoveToEx.Call(hdc,
					uintptr(box.left+bw*3/14), uintptr(box.bottom-bh*3/14), 0)
				procLineTo.Call(hdc,
					uintptr(box.right-bw*3/14), uintptr(box.top+bh*3/14))
				procSelectObject.Call(hdc, old)
			}
			continue
		}
		drawMenuGlyph(hdc, b.id, box, brush, col, active || hot)
		// 排列组：当前项在按钮下方压一条 2px 金线
		if b.group == menuGroupArrange && active {
			fillRect(hdc, windowRect{left: r.left + 6, top: r.bottom - 2, right: r.right - 6, bottom: r.bottom},
				wallAccentBrush)
		}
	}
	// 最后画悬停提示（要压在所有东西上面）
	paintTooltip(hdc, hwnd, full)
}

// paintTooltip 在悬停按钮下方画一条文字提示（GDI 窗口没有系统 tooltip）。
// 位置按按钮居中，靠边时夹回窗口内；上方放不下就翻到按钮上方。
func paintTooltip(hdc, hwnd uintptr, full windowRect) {
	if wallTooltipIdx < 0 || wallTooltipIdx >= len(wallMenuRects) || wallTooltipIdx >= len(wallMenu) {
		return
	}
	r := wallMenuRects[wallTooltipIdx]
	if r.right <= r.left {
		return
	}
	scale := func(v int32) int32 { return v * wallDPI / 96 }
	text := wallMenu[wallTooltipIdx].text
	// 打码是开关：提示直接说"现在是什么状态、点下去会怎样"
	if wallMenu[wallTooltipIdx].id == "mask" {
		if wallMenuOn["mask"] {
			text = "隐藏中，点击显示"
		} else {
			text = "显示中，点击隐藏"
		}
	}
	w := int32(len([]rune(text)))*scale(13) + scale(20)
	h := scale(26)
	x := (r.left+r.right)/2 - w/2
	y := r.bottom + scale(6)
	if x < scale(6) {
		x = scale(6)
	}
	if x+w > full.right-scale(6) {
		x = full.right - scale(6) - w
	}
	if y+h > full.bottom-scale(6) {
		y = r.top - h - scale(6)
	}
	box := windowRect{left: x, top: y, right: x + w, bottom: y + h}

	oldBr, _, _ := procSelectObject.Call(hdc, wallTipBrush)
	oldPen, _, _ := procSelectObject.Call(hdc, wallTipPen)
	rad := scale(8)
	procRoundRect.Call(hdc, uintptr(box.left), uintptr(box.top),
		uintptr(box.right), uintptr(box.bottom), uintptr(rad), uintptr(rad))
	procSelectObject.Call(hdc, oldPen)
	procSelectObject.Call(hdc, oldBr)

	s, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procSetTextColor.Call(hdc, colTextOn)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(s)), ^uintptr(0),
		uintptr(unsafe.Pointer(&box)), dtCenter|dtVCenter|dtSingleLine)
}
func glyphBox(r windowRect) windowRect {
	side := (r.right - r.left) * 14 / 30
	if h := r.bottom - r.top; side > h*14/26 {
		side = h * 14 / 26
	}
	cx, cy := (r.left+r.right)/2, (r.top+r.bottom)/2
	return windowRect{left: cx - side/2, top: cy - side/2, right: cx + side/2, bottom: cy + side/2}
}

// drawMenuGlyph 在 box 里画按钮图标。
//
// 动作类图标（打码 / 全屏 / 窗口控制）用系统图标字体 Segoe MDL2 Assets
// 画——GDI 手绘的线条在 14px 下锯齿明显，字体渲染自带抗锯齿，干净得多。
// 排列方案用实心方块示意（田字格 = 平铺、错层 = 堆叠），方块对齐像素就清晰。
func drawMenuGlyph(hdc uintptr, id string, box windowRect, brush, col uintptr, strong bool) {
	bw, bh := box.right-box.left, box.bottom-box.top
	if bw <= 0 || bh <= 0 {
		return
	}
	switch id {
	case "stopall":
		// 空心红方块：字体里的 □ 太细、太小，看着像个标点符号；
		// 用 Rectangle 画闭合矩形（4 条独立线拐角会不齐），3px 红笔够粗，
		// 小尺寸下反而比细线干净。
		pen := wallStopGlyphPen
		if strong {
			pen = wallStopGlyphHotPen
		}
		hollow, _, _ := procGetStockObj.Call(5) // NULL_BRUSH
		oldPen, _, _ := procSelectObject.Call(hdc, pen)
		oldBr, _, _ := procSelectObject.Call(hdc, hollow)
		procRectangle.Call(hdc,
			uintptr(box.left+bw/6), uintptr(box.top+bh/6),
			uintptr(box.right-bw/6), uintptr(box.bottom-bh/6))
		procSelectObject.Call(hdc, oldBr)
		procSelectObject.Call(hdc, oldPen)
		return
	case "full":
		// 全屏（四个箭头向外）
		drawGlyphText(hdc, wallIconFont, "\uE740", box, col)
		return
	}

	var parts []windowRect
	switch id {
	case "tile":
		parts = []windowRect{{0, 0, 6, 6}, {8, 0, 14, 6}, {0, 8, 6, 14}, {8, 8, 14, 14}}
	case "dual":
		parts = []windowRect{{0, 5, 5, 10}, {2, 2, 7, 7}, {7, 5, 12, 10}, {9, 2, 14, 7}}
	case "rows":
		parts = []windowRect{{0, 1, 14, 6}, {0, 8, 14, 13}}
	case "cols":
		parts = []windowRect{{1, 0, 6, 14}, {8, 0, 13, 14}}
	case "fan":
		parts = []windowRect{{0, 9, 6, 14}, {4, 5, 10, 10}, {8, 1, 14, 6}}
	default:
		return
	}
	for _, p := range parts {
		fillRect(hdc, windowRect{
			left:   box.left + bw*p.left/14,
			top:    box.top + bh*p.top/14,
			right:  box.left + bw*p.right/14,
			bottom: box.top + bh*p.bottom/14,
		}, brush)
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
		if r.right <= r.left || !in(r) {
			continue
		}
		if i >= len(wallMenu) {
			break
		}
		b := wallMenu[i]
		// 窗口控制：最小化 / 最大化-还原 / 关闭
		if b.group == menuGroupSys {
			h := hwndOfWall()
			if h == 0 {
				return
			}
			switch b.id {
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
			}
			return
		}
		if act := wallHooks.Actions[b.id]; act != nil {
			// 排列方案记住选中态（纯视觉：点哪个亮哪个）
			if b.group == menuGroupArrange {
				wallMenuActive = b.id
				if h := hwndOfWall(); h != 0 {
					procInvalidate.Call(h, 0, 1)
				}
			}
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

// makeGlyphFont 图标字体。face 传 "Segoe MDL2 Assets" 画线条图标，
// 传 "Microsoft YaHei UI" 画 □ 这类几何符号。
func makeGlyphFont(face string, px, dpi int) uintptr {
	h := -px * dpi / 96
	if h >= 0 {
		h = -px
	}
	name, _ := syscall.UTF16PtrFromString(face)
	f, _, _ := procCreateFontW.Call(
		uintptr(h), 0, 0, 0, 400,
		0, 0, 0, 1, // DEFAULT_CHARSET
		0, 0, 5, // CLEARTYPE_QUALITY
		0, uintptr(unsafe.Pointer(name)))
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
