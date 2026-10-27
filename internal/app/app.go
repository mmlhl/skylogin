package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"skylogin/internal/channel"
	"skylogin/internal/device"
	"skylogin/internal/fever"
	"skylogin/internal/huawei"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
	"skylogin/internal/web"
	"skylogin/internal/win"
)

// Account 是本地保存的账号（华为渠道 或 网易官方）。
type Account struct {
	Name         string `json:"name"`
	NickName     string `json:"nick_name"`
	ServiceToken string `json:"service_token"`
	AccessToken  string `json:"access_token"`
	PlayerID     string `json:"player_id"`
	PlayerLevel  string `json:"player_level"`
	GameAuthSign string `json:"game_auth_sign"`
	Ts           string `json:"ts"`
	OpenID       string `json:"open_id"`
	UpdatedAt    int64  `json:"updated_at"`

	// 以下字段只对网易账号有意义，华为账号留空。
	//
	// 网易账号靠 MPay token 持久化：token → create_ticket → 票。
	// 只要 token 存着，下次启动就不用再扫码。
	Channel    string `json:"channel,omitempty"`     // netease / bilibili / m4399，空 = 华为
	Username   string `json:"username,omitempty"`    // 网易账号显示名（手机号/邮箱）
	UserID     string `json:"user_id,omitempty"`     // MPay 用户 id / 渠道侧 uid
	MpayToken  string `json:"mpay_token,omitempty"`  // MPay token（1-eyJ…）
	DeviceID   string `json:"device_id,omitempty"`   // 建码/换票用的设备号
	LastTicket int64  `json:"last_ticket,omitempty"` // 上次发票时间（排查用）

	// 下面这些字段描述"这个账号是怎么来的、现在还能不能用"。
	//
	// 凭证有三种来源：
	//   scan     扫码登录（现有）
	//   weblogin 网页登录（网易账号密码/手机号，走 fever 的 H5 SDK 取 token）
	//   channel  其它渠道服（华为等）
	//
	// 状态机：ok（可用）→ stale（可能过期，待验证）→ expired（确认失效，需要重新登录）
	Source       string `json:"source,omitempty"`        // scan | weblogin | channel
	LoginMethod  string `json:"login_method,omitempty"`  // qrcode | password | sms
	CredState    string `json:"cred_state,omitempty"`    // ok | stale | expired
	LastCheck    int64  `json:"last_check,omitempty"`    // 上次验证凭证的时间
	LastError    string `json:"last_error,omitempty"`    // 上次失败原因（给用户看）
	RefreshToken string `json:"refresh_token,omitempty"` // 4399 专用：callback URL 里的长效续期凭证

	// 自动登录用。密码用 Windows DPAPI 加密后存，只有当前用户能解。
	// 用户不开启"记住登录"时这里为空。
	SavedUser string `json:"saved_user,omitempty"`
	SavedPass string `json:"saved_pass,omitempty"` // DPAPI 加密后的 base64
}

// dataDirFn 便于测试时替换成临时目录。
var dataDirFn = defaultDataDir

// dataDir 返回本地数据目录（账号、设备指纹、日志都在这里）。
func dataDir() string { return dataDirFn() }

func defaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	dir := filepath.Join(base, "SkyLogin")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// RunUI 启动本地控制面板，并用 WebView2 窗口显示（没有 WebView2 时退回默认浏览器）。
func RunUI(title string) error {
	logf("光遇账号登录器 %s 启动", Version)

	// 非 op13 的 IPC 包：原样记进日志。游戏登录失败时若会上报结果
	// （带错误状态的其它操作码），靠这个才能看见，否则包被静默丢弃。
	fever.SetPacketLogHook(func(op int32, body []byte) {
		const maxDump = 160
		text := make([]byte, 0, len(body))
		for _, c := range body {
			if c >= 0x20 && c < 0x7f {
				text = append(text, c)
			} else {
				text = append(text, '.')
			}
		}
		n := len(text)
		if n > maxDump {
			n = maxDump
		}
		logf("IPC 收到非取票包: op=%d len=%d body=%q", op, len(body), text[:n])
	})

	// 协议参数：内置默认 → 本地缓存 → 远程配置（都失败就用内置值）
	protocol.Init(dataDir(), os.Getenv("SKYLOGIN_PROTOCOL_URL"))
	logf("协议参数已载入（%s）", protocolSource())

	srv := web.New(web.Hooks{
		OnLogin: func(sess *huawei.Client, info *huawei.ScanCode, scan *huawei.ScanReply) (string, string, error) {
			acc, err := performLogin(sess, scan, nil)
			if err != nil {
				return "", "", err
			}
			return acc.NickName, acc.PlayerID, nil
		},
		Accounts:        accountsView,
		Start:           launchGame,
		StartMany:       launchMany,
		StartManyLayout: launchManyLayout,
		Arrange: func(cols, rows, w, h int) error {
			_, err := arrangeClients(cols, rows, w, h)
			return err
		},
		WallToggle:  ToggleWall,
		StopAccount: stopAccount,
		Status:      gameStatus,
		Select:      setCurrent,
		Rename:      renameAccount,
		Delete:      deleteAccount,
		Settings:    settingsView,
		SaveSettings: func(skyExe, deviceID, mirrorDir *string, closeLauncher *bool, multiQuality *string, multiFps *int, mouseArbiter *bool, arbiterMin *int) error {
			return applySettings(skyExe, deviceID, mirrorDir, closeLauncher, multiQuality, multiFps, mouseArbiter, arbiterMin)
		},
		Cleanup: func(accounts, cache, logs bool) ([]string, error) {
			return cleanupData(cleanupOptions{Accounts: accounts, Cache: cache, Logs: logs})
		},
		OpenLogs:    openLogFile,
		Reregister:  reregisterDevice,
		SetDeviceID: setCustomDeviceID,
		// 网页登录：打开子进程窗口，用网易官方 H5 SDK 拿 token
		SMSSend:        SendSMS,
		SMSLogin:       CompleteSMSLogin,
		WebLoginStart:  StartWebLogin,
		WebLoginStatus: WebLoginStatus,
		// 账号密码登录：交给官方 mpay.dll 的登录窗（和上网页那套并存，界面先只露出这个）
		MpayLoginStart:  StartMpayLogin,
		MpayLoginStatus: MpayLoginStatus,
		MpayLoginCancel: CancelMpayLogin,
		ScanStart: func() (map[string]any, error) {
			st, err := StartScanLogin()
			return scanToMap(st), err
		},
		ScanStop:           StopScanLogin,
		ScanNow:            func() map[string]any { return scanToMap(currentScan()) },
		ChannelLoginStart:  StartChannelWebLogin,
		ChannelLoginStatus: ChannelLoginStatus,
		AuthHint:           authHintView,
		Version:            func() string { return Version },
		SaveRailWidth:      saveRailWidth,
		SaveShell:          saveShell,
		LoadShell:          func() int { return loadConfig().UiShell },
		PickPath: func() (string, error) {
			return win.PickExeFile("选择光遇主程序 sky.exe")
		},
		PickFolder: func() (string, error) {
			return win.PickFolder("选择多开镜像目录")
		},
	})
	url, err := srv.Serve()
	if err != nil {
		return err
	}
	logf("界面已就绪: %s", url)

	// 后台修一下历史遗留的"别名是用户 id"的记录（不阻塞界面）
	go repairBareAccountNames()
	go cleanupStaleMirrors()

	// 鼠标仲裁：多开时防止后台客户端自认持有鼠标（进大厅后抢焦点、
	// 持续把光标拉回窗口中心）。客户端 <2 个时空转。
	StartMouseArbiter()

	// 托盘：常驻图标，右键有「显示主界面 / 启动光遇 / 退出」
	win.StartTray(win.TrayOptions{
		Title: title,
		OnShow: func() {
			win.FocusWindowByTitle(title)
		},
		OnLaunch: func() {
			if msg, err := launchGame(); err != nil {
				logf("托盘启动失败: %v", err)
			} else {
				logf("托盘启动: %s", msg)
			}
		},
		OnQuit: func() { os.Exit(0) },
	})

	if err := win.OpenWebView(url, title, filepath.Join(dataDir(), "webview"), 1400, 900); err != nil {
		logf("WebView2 窗口启动失败（%v），改用系统浏览器", err)
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		select {}
	}
	logf("窗口已关闭，退出")
	os.Exit(0)
	return nil
}

// gameStatus 给界面显示的游戏状态。
func gameStatus() string {
	if fever.IsSkyRunning() {
		return "运行中"
	}
	return "未启动"
}

// buildPayload 组装发往网易的登录包（SAUTH + uni_sauth + confirm 表单）。
func buildPayload(acc Account, dev *device.ClientProfile) (*netsec.LoginPayload, map[string]any, error) {
	ch := netsec.HuaweiChannelInfo()
	if channel.Known(acc.Channel) {
		ch = channel.InfoOf(acc.Channel)
	}
	custom := map[string]any{
		"anonymous":          "",
		"get_access_token":   ch.GetAccessToken,
		"extra_data":         acc.PlayerLevel,
		"timestamp":          acc.Ts,
		"realname":           ch.Realname,
		"login_channel":      ch.Login,
		"app_channel":        ch.Login,
		"source_app_channel": ch.Login,
		"sdk_version":        ch.SDKVersion,
	}
	sauth := netsec.NewSAUTH(acc.PlayerID, acc.GameAuthSign, profile.ShortGameID, custom, dev)
	resp, err := netsec.PostSigned(sauth, profile.ShortGameID)
	if err != nil {
		return nil, nil, err
	}
	// 4399 的 confirm token 是 access_token；SAUTH 的 session（GameAuthSign）
	// 是 state，两个值不同。其它渠道两者同值，不受影响。
	payloadToken := acc.GameAuthSign
	if acc.Channel == "m4399" && acc.AccessToken != "" {
		payloadToken = acc.AccessToken
	}
	payload, err := netsec.NewLoginPayloadChan(resp, acc.GameAuthSign, payloadToken, acc.PlayerID, acc.PlayerLevel, acc.Ts, dev, ch)
	if err != nil {
		return nil, resp, err
	}
	return payload, resp, nil
}

// toStr 把接口返回的任意类型安全转成字符串。
func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}
