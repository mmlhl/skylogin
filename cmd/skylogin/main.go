// 光遇账号登录器：双击即用的图形界面入口。
//
// 除了主界面，这个程序还承担另一个角色：网页登录子进程。
//
// 为什么用子进程而不是在主界面里再开一个窗口：
//
//	一个进程只应该跑一个 WebView2 消息循环（go-webview2 的 Run 会占用调用
//	线程的消息泵），主界面已经占用了。所以网页登录单独起一个进程，
//	它只做一件事：打开 fever.163.com，把登录拿到的 token 写到结果文件。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"skylogin/internal/app"
	"skylogin/internal/channel"
	"skylogin/internal/mpayhost"
	"skylogin/internal/win"
)

const windowTitle = "光遇账号登录器"

// webLoginResult 子进程写给主进程的结果。
type webLoginResult struct {
	OK       bool   `json:"ok"`
	UserID   string `json:"userId"`
	Token    string `json:"token"`
	Account  string `json:"account"`
	Method   string `json:"method"`
	Error    string `json:"error"`
	Finished int64  `json:"finished"`
}

func main() {
	// 被游戏当成 CEF 浏览器拉起时（镜像里的壳）：改参数后转发给真身
	if app.IsWebviewShim() {
		os.Exit(app.RunWebviewShim())
	}
	weblogin := flag.Bool("weblogin", false, "以网页登录子进程模式运行")
	channelLogin := flag.String("channellogin", "", "以渠道网页登录子进程模式运行（bilibili/m4399/vivo）")
	loginURL := flag.String("url", "", "渠道登录页地址（主进程解析后传入）")
	loginTarget := flag.String("target", "", "要刷新的账号 id（空 = 新登录）")
	mpaylogin := flag.Bool("mpaylogin", false, "以官方 mpay.dll 登录子进程模式运行")
	profile := flag.String("profile", "", "WebView2 数据目录（用于保存登录态）")
	out := flag.String("out", "", "结果输出文件")
	skyDir := flag.String("sky", "", "mpay.dll 所在目录")
	skin := flag.String("skin", "", "发烧平台的 skin.zip")
	game := flag.String("game", "", "MPay 长 game id")
	logPath := flag.String("log", "", "诊断日志文件")
	centerX := flag.Int("center-x", 0, "登录窗中心 X（屏幕坐标）")
	centerY := flag.Int("center-y", 0, "登录窗中心 Y（屏幕坐标）")
	hidden := flag.Bool("hidden", false, "登录窗口不可见（静默续期用，页面脚本照常执行）")
	flag.Parse()

	if *weblogin {
		runWebLogin(*profile, *out)
		return
	}
	if *channelLogin != "" {
		runChannelLogin(*channelLogin, *loginURL, *profile, *out, *loginTarget, *hidden)
		return
	}
	if *mpaylogin {
		runMpayLogin(*skyDir, *skin, *game, *out, *logPath, *centerX, *centerY)
		return
	}

	// 光遇本体要求管理员权限（sky.exe 的清单是 requireAdministrator），
	// 而且 Windows 的 UIPI 会阻止低权限进程向高权限窗口发消息，
	// 所以界面进程启动时就提权，保证后面能启动游戏并送达票据。
	win.RequireElevation()

	// 只允许一个实例：重复双击时激活已有窗口，避免窗口叠在一起。
	if !win.TryAcquireSingleInstance("SkyloginSingletonWindow") {
		win.FocusWindowByTitle(windowTitle)
		return
	}

	if err := app.RunUI(windowTitle); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
}

// runMpayLogin 加载官方 mpay.dll，弹官方登录窗，拿到票据后写结果文件退出。
//
// 交给子进程做，是因为 mpay.dll 是原生 SDK：它自己拉消息循环、起 WebView2，
// 崩起来是直接访问违例，放在主界面进程里会把界面一起带走。
func runMpayLogin(skyDir, skin, game, outPath, logPath string, centerX, centerY int) {
	if skyDir == "" || outPath == "" {
		fmt.Fprintln(os.Stderr, "mpaylogin 需要 --sky 和 --out")
		os.Exit(2)
	}
	res := mpayhost.Login(mpayhost.Options{
		SkyDir:  skyDir,
		SkinZip: skin,
		GameID:  game,
		Channel: "a50_sdk_cn",
		Timeout: 5 * time.Minute,
		LogPath: logPath,
		CenterX: centerX,
		CenterY: centerY,
	})
	res.Finished = time.Now().Unix()
	b, _ := json.Marshal(res)
	_ = os.WriteFile(outPath, b, 0o600)

	// 结果落盘后硬退出：mpay.dll 的 WebView2 线程偶尔会让正常退出卡住，
	// 表现就是"窗口关了但进程还挂着"。
	mpayhost.Shutdown()
}

// runWebLogin 打开网页登录窗口，拿到 token 后写结果文件并退出。
func runWebLogin(profileDir, outPath string) {
	if profileDir == "" || outPath == "" {
		fmt.Fprintln(os.Stderr, "weblogin 需要 --profile 和 --out")
		os.Exit(2)
	}

	// 结果无论成败都要落盘，主进程靠轮询这个文件拿结果
	write := func(r webLoginResult) {
		r.Finished = time.Now().Unix()
		b, _ := json.Marshal(r)
		_ = os.WriteFile(outPath, b, 0o600)
	}

	var hwnd uintptr
	onPayload := func(payload string) {
		var p struct {
			Kind    string  `json:"kind"`
			X       float64 `json:"x"`
			Y       float64 `json:"y"`
			UserID  string  `json:"userId"`
			Token   string  `json:"token"`
			Account string  `json:"account"`
			Method  string  `json:"method"`
			Error   string  `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			write(webLoginResult{OK: false, Error: "结果解析失败: " + err.Error()})
			return
		}
		// 页面报告头像坐标：登录按钮只认真实鼠标事件，这里发系统级点击
		if p.Kind == "avatar" && hwnd != 0 {
			win.ClickClientPoint(hwnd, int32(p.X), int32(p.Y))
			return
		}
		if p.Error != "" || p.Token == "" {
			write(webLoginResult{OK: false, Error: p.Error})
			return
		}
		write(webLoginResult{OK: true, UserID: p.UserID, Token: p.Token, Account: p.Account, Method: p.Method})
	}

	if err := win.OpenWebLogin("https://fever.163.com/", "网易账号登录", profileDir, 1280, 900, app.WebLoginScript(), onPayload, func(h uintptr) { hwnd = h }, false); err != nil {
		write(webLoginResult{OK: false, Error: err.Error()})
		return
	}
	if _, statErr := os.Stat(outPath); statErr != nil {
		write(webLoginResult{OK: false, Error: "窗口已关闭，未获取到登录信息"})
	}
}

// runChannelLogin 渠道网页登录子进程：打开主进程解析好的登录页，
// 注入该渠道的捕获脚本，把结果（统一 schema：uid + secret + extra）写到结果文件。
//
// net 诊断日志记什么由脚本决定（B站记完整 URL 和响应片段；4399/vivo 的
// 跳转 URL 带敏感参数，只记 host+path），这里原样落盘。
func runChannelLogin(key, loginURL, profileDir, outPath, target string, hidden bool) {
	if profileDir == "" || outPath == "" || loginURL == "" {
		fmt.Fprintln(os.Stderr, "channellogin 需要 --channellogin --url --profile 和 --out")
		os.Exit(2)
	}

	write := func(res app.CapturedLoginResult) {
		res.Finished = time.Now().Unix()
		b, _ := json.Marshal(res)
		_ = os.WriteFile(outPath, b, 0o600)
	}

	onPayload := func(payload string) {
		var p struct {
			Kind   string            `json:"kind"`
			URL    string            `json:"url"`
			Status int               `json:"status"`
			Body   string            `json:"body"`
			UID    string            `json:"uid"`
			Secret string            `json:"secret"`
			Uname  string            `json:"uname"`
			Extra  map[string]string `json:"extra"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return
		}
		// 网络诊断：脚本已按渠道脱敏，这里原样追加
		if p.Kind == "net" {
			f, err := os.OpenFile(filepath.Join(filepath.Dir(outPath), key+"-net.log"),
				os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err == nil {
				fmt.Fprintf(f, "%s %s %d %s\n", time.Now().Format("15:04:05"), p.URL, p.Status, p.Body)
				f.Close()
			}
			return
		}
		if p.UID == "" || p.Secret == "" {
			return
		}
		write(app.CapturedLoginResult{OK: true, UID: p.UID, Secret: p.Secret, Uname: p.Uname, Extra: p.Extra})
	}

	w := channel.LoginWindowOf(key)
	if w.Script == nil {
		write(app.CapturedLoginResult{Error: "不支持的渠道: " + key})
		return
	}
	if err := win.OpenWebLogin(loginURL, w.Title, profileDir, w.Width, w.Height, w.Script(target), onPayload, nil, hidden); err != nil {
		write(app.CapturedLoginResult{Error: err.Error()})
		return
	}
	if _, statErr := os.Stat(outPath); statErr != nil {
		write(app.CapturedLoginResult{Error: "窗口已关闭，未获取到登录信息"})
	}
}
