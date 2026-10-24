package win

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// OpenWebView 用一个 WebView2 窗口打开本地页面，阻塞直到窗口关闭。
// dataPath 用于存放 WebView2 的缓存与本地存储（放在我们自己的目录里，避免污染系统）。
func OpenWebView(url, title, dataPath string, width, height uint) error {
	if width == 0 {
		width = 980
	}
	if height == 0 {
		height = 720
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
	applyDarkFrame(hwnd)
	if exe, err := os.Executable(); err == nil {
		SetWindowIcon(hwnd, exe)
	}

	w.Navigate(url)

	// 窗口真正显示后再设一次深色标题栏（部分系统在建窗时设了不生效）
	go func() {
		time.Sleep(700 * time.Millisecond)
		applyDarkFrame(hwnd)
	}()

	w.Run() // 阻塞到窗口关闭
	return nil
}

// OpenWebLogin 打开网页登录窗口：加载指定网址，并注入取票脚本。
//
// script 由 app.WebLoginScript 生成，会在每次页面加载时执行；
// 页面脚本通过 window.__skyloginPayload(json) 把结果回传，这里转成 onPayload。
//
// 单独做成一个函数是因为网页登录跑在子进程里（一个进程一个 WebView2），
// 主界面的窗口和它不能共用同一个消息循环。
func OpenWebLogin(url, title, dataPath string, width, height uint, script string, onPayload func(string), onWindow func(hwnd uintptr)) error {
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
