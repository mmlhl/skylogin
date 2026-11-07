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
	dataPath := flag.String("data-path", "", "mpay 数据目录（诊断用）")
	noLogout := flag.Bool("no-logout", false, "不清理旧登录态，直接静默登录（诊断用）")
	qrOnly := flag.Bool("qr-only", false, "登录窗只显示二维码页（渠道服扫码用）")
	hidden := flag.Bool("hidden", false, "登录窗口不可见（静默续期用，页面脚本照常执行）")
	probeToken := flag.Bool("probe-channel-token", false, "探测渠道服账号的 accessToken 还有没有效（后台定时用）")
	qrProbe := flag.Bool("qr-probe", false, "自己建一张游戏码并轮询，验证渠道 App 扫码后会不会吐回渠道凭证（开发用）")
	qrProbeSeconds := flag.Int("qr-probe-seconds", 900, "二维码等待秒数（过期会自动换新码）")
	qrProbeHosted := flag.Bool("qr-probe-hosted", false, "建托管码（a50 + dst_jf_game_id=ma75），而不是游戏码")
	pkgProbe := flag.Bool("probe-channel-package", false, "验证 channel_login_slots.json 里的渠道整包还能不能换出登录态（开发用）")
	refreshProbe := flag.Bool("probe-refresh", false, "验证渠道服账号能不能用 refresh_token 续期（开发用）")
	recallProbe := flag.String("probe-recall", "", "验证加载湾的\"记住槽\"认不认识某个账号（参数是 account_id）")
	accountsProbe := flag.Bool("probe-accounts", false, "打印本地账号清单（名字、键、渠道、uid）")
	sauthProbe := flag.String("probe-sauth", "", "试网易账号走 uni_sauth 并声明 platform（参数是账号 key，默认 ad）")
	sauthPlat := flag.String("probe-sauth-platform", "ad", "配合 --probe-sauth：声明成哪个平台（ad / ios / hm）")
	myappProbe := flag.Bool("probe-myapp", false, "应用宝微信扫码探针（开发用）")
	uilessMode := flag.String("probe-uiless", "", "诊断：直接调 mpay.dll 的无界面登录接口（setuser / token / ticket）")
	uilessFile := flag.String("uiless-file", "", "setuser 用的槽位文件（默认 channel_login_slots.json）")
	uilessUID := flag.String("uiless-uid", "", "token 模式：uid")
	uilessToken := flag.String("uiless-token", "", "token 模式：token")
	uilessAccount := flag.String("uiless-account", "", "ticket 模式：给哪个账号签票（空 = 第一条带整包的）")
	uilessAtk := flag.String("uiless-atk", "", "ticket 模式：直接用这个 atk 签票（不落库）")
	uilessOpenid := flag.String("uiless-openid", "", "配合 --uiless-atk：openid")
	uilessDevice := flag.String("uiless-device", "", "先 SetDeviceInfo 再调接口（换票要同一个设备号）")
	uilessCh := flag.String("uiless-channel", "", "init 用的 app_channel（空 = a50_sdk_cn；试 myapp）")
	uilessFake := flag.Bool("uiless-fake-api", false, "把服务地址指到本地假服务器，看 DLL 发什么")
	uilessPlatform := flag.String("uiless-platform", "", "ticket 模式登录成功后，顺手调 SetUserPlatform（ad / ios / hm）")
	loginTimeout := flag.Int("timeout", 0, "登录窗等待秒数（0 = 默认 5 分钟）")
	flag.Parse()

	// 定时探测模式：不开界面、不弹窗，测完就退出
	if *probeToken {
		app.ProbeChannelToken()
		return
	}
	// 渠道服扫码探针：不加载 mpay.dll、不开官方登录窗，自己建码自己轮询
	if *qrProbe {
		app.ProbeChannelQR(*qrProbeSeconds, *qrProbeHosted)
		return
	}
	// 渠道整包探针：离线验证扫码存下来的那坨凭证还能不能用
	if *pkgProbe {
		app.ProbeChannelPackage("")
		return
	}
	if *refreshProbe {
		app.ProbeRefreshToken()
		return
	}
	if *recallProbe != "" {
		if err := app.ProbeRecall(*recallProbe); err != nil {
			fmt.Fprintln(os.Stderr, "探针结果:", err)
			os.Exit(1)
		}
		return
	}
	if *accountsProbe {
		if err := app.ProbeAccounts(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *sauthProbe != "" {
		if err := app.ProbeNeteaseUnisdk(*sauthProbe, *sauthPlat); err != nil {
			fmt.Fprintln(os.Stderr, "探针结果:", err)
			os.Exit(1)
		}
		return
	}
	if *myappProbe {
		app.ProbeMyApp()
		return
	}
	if *uilessMode != "" {
		runUILessProbe(*uilessMode, *uilessFile, *uilessUID, *uilessToken, *uilessAccount, *uilessAtk, *uilessOpenid, *uilessDevice, *uilessCh, *uilessFake, *uilessPlatform, *skyDir, *skin, *game, *logPath)
		return
	}

	if *weblogin {
		runWebLogin(*profile, *out)
		return
	}
	if *channelLogin != "" {
		runChannelLogin(*channelLogin, *loginURL, *profile, *out, *loginTarget, *hidden)
		return
	}
	if *mpaylogin {
		runMpayLogin(*skyDir, *skin, *game, *out, *logPath, *centerX, *centerY, *dataPath, *noLogout, *qrOnly, time.Duration(*loginTimeout)*time.Second)
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

// runUILessProbe 诊断：直接调 mpay.dll 的无界面登录接口。
//
// 目的：验证"能不能让 DLL 自己认账号"——成了就不用去解 mpay.db 的加密。
// 结果写日志，进程跑完硬退出（和 mpaylogin 一样，避免 WebView2 线程卡住）。
func runUILessProbe(mode, slotsFile, uid, token, account, atk, openid, device, channel string, fakeAPI bool, platform, skyDir, skin, game, logPath string) {
	if skyDir == "" {
		fmt.Fprintln(os.Stderr, "probe-uiless 需要 --sky")
		os.Exit(2)
	}
	o := mpayhost.UILessOptions{
		Options: mpayhost.Options{
			SkyDir:  skyDir,
			SkinZip: skin,
			GameID:  game,
			Channel: "a50_sdk_cn",
			LogPath: logPath,
		},
		Action:   mpayhost.UILessAction(mode),
		UID:      uid,
		Token:    token,
		Device:   device,
		Ch:       channel,
		FakeURL:  fakeAPI,
		Platform: platform,
	}
	if slotsFile == "" {
		slotsFile = filepath.Join(os.Getenv("APPDATA"), "SkyLogin", "channel_login_slots.json")
	}
	if mode == string(mpayhost.ActionSetUser) {
		s, err := mpayhost.LoadSlotsFile(slotsFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读槽位失败:", err)
			os.Exit(2)
		}
		o.Slots = s
		fmt.Printf("槽位来自 %s（%d 个）\n", slotsFile, len(s))
	}
	// ticket 模式：先签一张票，再交给 DLL
	if mode == string(mpayhost.ActionTicket) && atk != "" && openid != "" {
		tk, err := app.MintTicketWithCred(openid, atk, "", "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "用外部凭证签票失败:", err)
			os.Exit(2)
		}
		o.Ticket = tk
		fmt.Printf("已用外部 atk 签票（%d 字符）\n", len(tk))
	}
	if mode == string(mpayhost.ActionTicket) && o.Ticket == "" {
		// 渠道服账号走整包签票；网易账号没有整包，得走 create_ticket 那条，
		// 两种都试一遍，省得每次还得记住该用哪个参数。
		tk, who, err := app.MintTicketForProbe(account)
		if err != nil {
			fmt.Fprintln(os.Stderr, "渠道整包签票没成，改试网易账号:", err)
			tk, who, err = app.MintTicketForNetEase(account)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "签票失败:", err)
			os.Exit(2)
		}
		o.Ticket = tk
		fmt.Printf("已为 %s 签票（%d 字符）\n", who, len(tk))
	}
	res := mpayhost.ProbeUILess(o)
	fmt.Printf("结果: ok=%v ticket=%d字符 err=%s\n", res.OK, len(res.Ticket), res.Error)
	mpayhost.Shutdown()
}

// runMpayLogin 加载官方 mpay.dll，弹官方登录窗，拿到票据后写结果文件退出。
//
// 交给子进程做，是因为 mpay.dll 是原生 SDK：它自己拉消息循环、起 WebView2，
// 崩起来是直接访问违例，放在主界面进程里会把界面一起带走。
func runMpayLogin(skyDir, skin, game, outPath, logPath string, centerX, centerY int, dataPath string, noLogout, qrOnly bool, timeout time.Duration) {
	if skyDir == "" || outPath == "" {
		fmt.Fprintln(os.Stderr, "mpaylogin 需要 --sky 和 --out")
		os.Exit(2)
	}
	res := mpayhost.Login(mpayhost.Options{
		SkyDir:     skyDir,
		SkinZip:    skin,
		GameID:     game,
		Channel:    "a50_sdk_cn",
		Timeout:    timeout,
		LogPath:    logPath,
		CenterX:    centerX,
		CenterY:    centerY,
		DataPath:   dataPath,
		SkipLogout: noLogout,
		QRCodeOnly: qrOnly,
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
