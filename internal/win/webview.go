package win

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// OpenWebView 用一个 WebView2 窗口打开本地页面，阻塞直到窗口关闭。
// dataPath 用于存放 WebView2 的缓存与本地存储（放在我们自己的目录里，避免污染系统）。
//
// width/height 是设计逻辑尺寸（96 DPI 下的像素）。实际开窗大小按主屏 DPI
// 等比放大并夹进工作区——本线程建出的窗口是 DPI 感知的，坐标就是物理
// 像素，不放大在高缩放屏上窗口只有应有的一半大。有几何记录（hooks）
// 且主屏分辨率没变时直接恢复记录，没有才用上面的默认算法。
func OpenWebView(url, title, dataPath string, width, height uint, hooks WebViewHooks) error {
	if width == 0 {
		width = 980
	}
	if height == 0 {
		height = 720
	}
	// 建窗 + 消息循环钉在同一个 OS 线程上：感知切换只对本线程生效，
	// goroutine 一迁移线程上下文就留在旧线程上了，窗口的 DPI 行为必须
	// 从头到尾钉死，不能看调度运气。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// 本线程建出的窗口按 PMv2 处理：高分屏不糊，坐标全是物理像素，
	// 与监控墙线程同一套约定。线程随窗口退出，不用恢复。
	_, _ = enterPhysicalPixels()

	wa := primaryWorkArea()
	workW, workH := int(wa.right-wa.left), int(wa.bottom-wa.top)
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = 96
	}
	// 默认算法：设计尺寸 × 屏幕缩放，夹进工作区后在工作区居中
	w, h := fitInWorkArea(int(width)*int(dpi)/96, int(height)*int(dpi)/96, workW, workH)
	x, y := int(wa.left)+(workW-w)/2, int(wa.top)+(workH-h)/2
	// 有记录且主屏分辨率没变 → 恢复记录；屏幕换了/没记录 → 用默认算法
	if hooks.SavedRect != nil {
		curW, curH := primaryScreenSize()
		if sx, sy, sw, sh, ssw, ssh, ok := hooks.SavedRect(); ok &&
			ssw == curW && ssh == curH && saneWinRect(sx, sy, sw, sh) {
			x, y, w, h = sx, sy, sw, sh
		}
	}

	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  filepath.Clean(dataPath),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  title,
			Width:  uint(w),
			Height: uint(h),
			Center: false,
		},
	})
	if wv == nil {
		return errors.New("创建 WebView2 窗口失败（可能没有安装 WebView2 运行时）")
	}
	defer wv.Destroy()

	hwnd := uintptr(wv.Window())
	atomic.StoreUintptr(&mainWindowHwnd, hwnd)
	// 库只会按整屏居中，任务栏在下面时会压住底边，统一按工作区摆位
	setWinRect(hwnd, x, y, w, h)
	applyDarkFrame(hwnd)
	if exe, err := os.Executable(); err == nil {
		SetWindowIcon(hwnd, exe)
	}

	// 几何记忆：每秒采样，停稳落盘；窗口关闭前再补最后一帧
	tracker := &winGeoTracker{hooks: hooks}
	stop := make(chan struct{})
	go tracker.run(hwnd, stop)

	wv.Navigate(url)

	// 窗口真正显示后再设一次深色标题栏（部分系统在建窗时设了不生效）
	go func() {
		time.Sleep(700 * time.Millisecond)
		applyDarkFrame(hwnd)
	}()

	wv.Run() // 阻塞到窗口关闭
	close(stop)
	tracker.flush() // 关窗前最后一两秒的挪动轮询没来得及落盘，这里补上
	return nil
}

// OpenWebLogin 打开网页登录窗口：加载指定网址，并注入取票脚本。
//
// script 由 app.WebLoginScript 生成，会在每次页面加载时执行；
// 页面脚本通过 window.__skyloginPayload(json) 把结果回传，这里转成 onPayload。
//
// 单独做成一个函数是因为网页登录跑在子进程里（一个进程一个 WebView2），
// 主界面的窗口和它不能共用同一个消息循环。
//
// hidden = true 时窗口不可见（静默续期用）：加载、脚本、网络照常，只是用户
// 看不到。做法是创建后立刻挪到屏幕外并隐藏（makeSilent）。
func OpenWebLogin(url, title, dataPath string, width, height uint, script string, onPayload func(string), onWindow func(hwnd uintptr), hidden bool) error {
	if width == 0 {
		width = 520
	}
	if height == 0 {
		height = 760
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  filepath.Clean(dataPath),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  title,
			Width:  width,
			Height: height,
			Center: true,
		},
	})
	if w == nil {
		return errors.New("创建 WebView2 窗口失败（可能没有安装 WebView2 运行时）")
	}
	defer w.Destroy()

	hwnd := uintptr(w.Window())
	atomic.StoreUintptr(&mainWindowHwnd, hwnd)
	if hidden {
		makeSilent(hwnd)
	}
	applyDarkFrame(hwnd)
	if exe, err := os.Executable(); err == nil {
		SetWindowIcon(hwnd, exe)
	}

	// 把窗口句柄交给调用方：网页里的登录按钮只认真实鼠标事件，
	// 需要它来发系统级点击
	if onWindow != nil {
		onWindow(hwnd)
	}
	// 页面调 window.__skyloginPayload(...) 时进入 Go
	if onPayload != nil {
		_ = w.Bind("__skyloginPayload", func(s string) { onPayload(s) })
	}
	// 先注册脚本再导航：Init 是"文档创建时执行"，
	// 顺序反了的话第一次加载收不到脚本（只能等下一次导航）
	if script != "" {
		w.Init(script)
	}
	w.Navigate(url)

	go func() {
		time.Sleep(700 * time.Millisecond)
		applyDarkFrame(hwnd)
	}()

	w.Run()
	return nil
}

var procSetWindowLongPtrW = user32dll.NewProc("SetWindowLongPtrW")

// makeSilent 需要的几个系统常量（wsExNoActivate 定义在 wall.go）。
const (
	swHide        = 0
	swpNoSize     = 0x1
	swpNoZOrder   = 0x4
	swpNoActivate = 0x10
)

// makeSilent 把刚创建的 WebView 窗口变成"不可见"：挪到屏幕外、不进任务栏、
// 不抢焦点，再整体隐藏。加载、脚本、网络照常工作。
// （库不支持注入自建宿主窗口，所以只能在创建后处理。）
func makeSilent(hwnd uintptr) {
	if style, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle); style != 0 {
		procSetWindowLongPtrW.Call(hwnd, gwlExStyle, style|wsExToolWindow|wsExNoActivate)
	}
	// -32000 与系统"最小化窗口"用的屏幕外坐标一致，任何分辨率都出不来
	off := int32(-32000)
	procSetWindowPos.Call(hwnd,
		0, uintptr(off), uintptr(off), 0, 0,
		swpNoSize|swpNoZOrder|swpNoActivate)
	procShowWindow.Call(hwnd, swHide)
}
