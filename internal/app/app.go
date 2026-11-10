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
	"skylogin/internal/netdns"
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

	// 游戏画质偏好（这一行账号专属）：QualityMode 空 = 跟随游戏/多开默认；
	// QualityFps 0 = 同上。多开预写镜像设置时优先用它，其次才是全局默认。
	QualityMode string `json:"quality_mode,omitempty"`
	QualityFps  int    `json:"quality_fps,omitempty"`

	// CopyKey 是副本的本机键（"PlayerID-2" 这样的后缀），允许同一个
	// PlayerID 存多条记录（典型：同一网易账号再建一个副本在另一个区玩）。
	// 空 = 原始行，键就是 PlayerID；老数据没有这个字段，无需迁移。
	CopyKey string `json:"copy_key,omitempty"`
	// Region 是副本的可选标注：android / ios / 空。
	Region string `json:"region,omitempty"`

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
	// A50Token 是平台身份（a50）签发的账号级 token（g_i=a50, sa=-9, t=1）。
	// 来源：mpayhost 无头密码登录（UILessUrsLogin）后 DLL 自己推 devices/users
	// 拿到的回调。它才能换平台会话 sid → 槽位票（免渠道启动）。MpayToken 是
	// ma75 签发的，换不了 sid（1306），两者并存、各司其职。
	A50Token string `json:"a50_token,omitempty"`
	// A50DeviceID 是 a50 token 签发时 DLL 用的设备号（token 绑定它，
	// 换平台会话 sid 时必须配套传，否则 1000 sauth error）。
	A50DeviceID string `json:"a50_device_id,omitempty"`

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
	// UnisdkData 是渠道服扫码时 DLL 给的整包登录凭证（原文）。
	//
	// 华为那种 sdk_info 里带 accessToken 的渠道用不到它；vivo 这类
	// 只给整包的渠道就靠它登录（见 channelpackage.go）。
	UnisdkData string `json:"unisdk_data,omitempty"`
	// MyappCred 是应用宝（YSDK）的完整登录材料：openid / atk / rtk / pf / pfkey…
	//
	// 为什么要单独存一份：uni_sauth 的 extra_data 必须用**当前**这份拼
	// （微信 login_type=8 + hy_gameid + wc_actoken，QQ login_type=2 + openid +
	// kp_actoken），续期后 atk 会变，拿旧 atk 拼出来的 extra_data 会被服务端拒。
	// 微信有 rtk 可以一直滚；QQ 没有续期接口，60 天后只能重扫。
	MyappCred string `json:"myapp_cred,omitempty"`
	// RowKey 是"同一个玩家、不同来源"要各占一行时的行键。
	//
	// 典型场景：同一个 vivo 号，先用网页登录加过（channel=vivo），后来又用
	// 渠道服扫码加了一次。两行 PlayerID 一样，靠 RowKey 区分才不会互相覆盖、
	// 也不会出现"点一个另一个跟着亮"。空 = 用 PlayerID 当键（老行为）。
	RowKey string `json:"row_key,omitempty"`

	// 自动登录用。密码用 Windows DPAPI 加密后存，只有当前用户能解。
	// 用户不开启"记住登录"时这里为空。
	SavedUser string `json:"saved_user,omitempty"`
	SavedPass string `json:"saved_pass,omitempty"` // DPAPI 加密后的 base64

	// ── 登录环境与时间（为了回答"凭证到底能活多久"）──────────────
	//
	// 每次登录/启动都把这些记下来：以后同一个号过几天失效了，翻记录就能知道
	// 它当时是哪份 mpay.dll、哪个 game id 登出来的，不用再靠回忆。
	MpayPath    string `json:"mpay_path,omitempty"`    // 登录窗实际加载的 mpay.dll 完整路径
	MpayVersion string `json:"mpay_version,omitempty"` // 上面那份 DLL 的版本号（x.y.z）
	GameID      string `json:"game_id,omitempty"`      // 登录用的平台身份（ma75 / a50 …）
	CreatedAt   int64  `json:"created_at,omitempty"`   // 第一次添加到本机的时间
	LastLogin   int64  `json:"last_login,omitempty"`   // 最后一次成功启动（签出票据）的时间
	TokenSa     int    `json:"token_sa,omitempty"`     // 当前 token 的等级：-9 账号级 / 3 会话级 / -4 大神
	TokenAt     int64  `json:"token_at,omitempty"`     // 当前 token 的签发时间（JWT 里的 lt）
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
	// 先维护日志：老版本写下的日志是明文 token / 邮箱，升级后第一次启动
	// 就地脱敏一次，超过 2 MB 的顺手滚动归档。必须在第一条 logf 之前。
	maintainLogs()

	// 日志里带上 commit：2026-11-10 的反馈里同一台机器上跑了 1.6.0、1.1.1、1.6.1
	// 三个版本，光看版本号根本分不清"这次到底跑的是哪份代码"，排查全靠猜。
	logf("光遇账号登录器 %s 启动%s", Version, buildStamp())

	// 让 DNS 兜底把"系统解析失败、改用公共 DNS"写进同一个日志，
	// 否则下次再遇到解析问题，只能从"no such host"猜是网络还是代码。
	netdns.SetLogf(logf)

	// 窗口排列/全屏切换的诊断走日志：这些动作用户看不见过程，
	// 失败时只表现为"窗口没动"，日志里得能查到卡在哪一步。
	win.Diag = func(s string) { logf("%s", s) }

	// 标题栏配色取决于系统主题：深色主题交给系统画（拖动不变色），
	// 浅色主题才强制深色（拖动时 DWM 可能闪一下浅色，无解）
	if win.SystemDarkTheme() {
		logf("系统主题：深色（标题栏由系统绘制）")
	} else {
		logf("系统主题：浅色（标题栏强制深色）")
	}

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
		StartMany:       func(ids []string) (string, error) { return launchMany(ids, "", 0) },
		StartManyLayout: launchManyLayout,
		Arrange: func(cols, rows, w, h int) error {
			_, err := arrangeClients(cols, rows, w, h)
			return err
		},
		WallToggle:      ToggleWall,
		WindowControl:   win.ControlMainWindow,
		WindowDrag:      win.StartWindowDrag,
		WindowSuspend:   win.SuspendMainWebview,
		StopAccount:     stopAccount,
		Status:          gameStatus,
		Select:          setCurrent,
		Rename:          renameAccount,
		Delete:          deleteAccount,
		Duplicate:       duplicateAccount,
		SetRegion:       setAccountRegion,
		HeadlessLogin:   HeadlessLoginAccount,
		HostPool:        func() any { return HostPoolStatus() },
		SetMaskTitles:   setMaskTitlesOn,
		GetBuild:        func() int { return buildNumber() },
		SetReleaseState: setReleaseState,
		OpenURL: func(url string) error {
			return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		},
		Settings: settingsView,
		SaveSettings: func(skyExe, deviceID, mirrorDir *string, closeLauncher *bool, multiQuality *string, multiFps *int, mouseArbiter *bool, arbiterMin *int) error {
			return applySettings(skyExe, deviceID, mirrorDir, closeLauncher, multiQuality, multiFps, mouseArbiter, arbiterMin)
		},
		MpayChannelStart: StartChannelScanLogin,
		ChannelScanInfo:  ChannelScanInfo,
		LoginMethods:     LoginChannels,
		SaveMpayDebug: func(dll, game string, skipLogout bool, dataPath string) error {
			return SaveMpayDebug(MpayDebugConfig{DLL: dll, Game: game, SkipLogout: skipLogout, DataPath: dataPath})
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
		Relogin:        ReloginAccount,
		WebLoginStart:  StartWebLogin,
		WebLoginStatus: WebLoginStatus,
		// 账号密码登录：交给官方 mpay.dll 的登录窗（和上网页那套并存，界面先只露出这个）
		MpayLoginStart:  StartMpayLogin,
		MpayLoginStatus: MpayLoginStatus,
		MpayLoginCancel: CancelMpayLogin,
		ScanStart: func(hostUserID string) (map[string]any, error) {
			st, err := StartScanLogin(hostUserID)
			return scanToMap(st), err
		},
		ScanStop:           StopScanLogin,
		ScanNow:            func() map[string]any { return scanToMap(currentScan()) },
		ChannelLoginStart:  StartChannelWebLogin,
		ChannelLoginStatus: ChannelLoginStatus,
		// 协议登录（九游账号密码）：不开窗，直接算凭证
		ChannelPasswordStart: StartChannelMethodLogin,
		ChannelAnswer:        AnswerChannelAsk,
		ChannelCancel:        StopChannelMethodLogin,
		AuthHint:             authHintView,
		LaunchAlert:          launchAlertView,
		Version:              func() string { return Version },
		SaveRailWidth:        saveRailWidth,
		SaveShell:            saveShell,
		LoadShell:            loadShellIndex,
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
	// 上次关掉登录器后还在跑的游戏客户端：按窗口标题把路由认回来，
	// 状态显示 / 监控墙 / 取票应答才能继续工作
	go recoverRunningClients()
	// 会话级凭证滚动续期：每次启动把"好久没滚过"的 token 滚一遍，
	// 扫码号就不用因为过期重新扫码了（见 rolltoken.go）
	go rollStaleTokens()
	startCredCheck()

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
		OnQuit: func() { cleanupOnExit(); os.Exit(0) },
	})

	// 主窗口几何记忆：记录在 config 里（物理像素），屏幕换过就不恢复、
	// 按默认算法重新开，判断在 win 层做（它才知道当前主屏多大）
	if err := win.OpenWebView(url, title, filepath.Join(dataDir(), "webview"), 1400, 900, win.WebViewHooks{
		SavedRect: func() (int, int, int, int, int, int, bool, bool) {
			c := loadConfig()
			if c.WinW <= 0 || c.WinH <= 0 || c.WinScreenW <= 0 || c.WinScreenH <= 0 {
				return 0, 0, 0, 0, 0, 0, false, false
			}
			return c.WinX, c.WinY, c.WinW, c.WinH, c.WinScreenW, c.WinScreenH, c.WinMax == 1, true
		},
		SaveRect: func(x, y, w, h, screenW, screenH int, zoomed bool) {
			_ = saveWinRect(x, y, w, h, screenW, screenH, zoomed)
		},
	}); err != nil {
		logf("WebView2 窗口启动失败（%v），改用系统浏览器", err)
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		select {}
	}
	cleanupOnExit()
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

// sauthParts 是"发 uni_sauth 之前"的全部材料。
//
// 抽出来是为了让探针能拿同一份 SAUTH 去试不同口子（比如把 sessionid 换成
// refresh_token 看能不能续期），而不必复制一遍拼装逻辑。
type sauthParts struct {
	SAUTH   map[string]any
	UID     string
	Session string
	Token   string
	Channel netsec.ChannelInfo
}

// buildSauth 拼出 uni_sauth 的请求体。
func buildSauth(acc Account, dev *device.ClientProfile) (sauthParts, error) {
	ch := netsec.HuaweiChannelInfo()
	// 已登记的渠道服用自己的身份参数；未知渠道仍按华为（存量账号的 Channel 为空）
	if info, ok := channel.InfoOf(acc.Channel); ok {
		ch = info
	}
	p := sauthParts{
		UID:     acc.PlayerID,
		Session: acc.GameAuthSign,
		Token:   acc.GameAuthSign,
	}

	var pkg *channelPackage
	if acc.UnisdkData != "" {
		parsed, perr := parseChannelPackage(acc.UnisdkData)
		if perr == nil {
			pkg = parsed
			// 整包里的渠道名 / 版本 / uid / session 优先，别拿默认值盖掉
			if pkg.Channel != "" {
				ch = netsec.ChannelInfo{
					Login:          pkg.Channel,
					SDKVersion:     pkg.SDKVersion,
					GetAccessToken: "1",
					Realname:       pkg.Realname,
				}
			}
			if pkg.UID != "" {
				p.UID = pkg.UID
			}
			if pkg.Session != "" {
				p.Session = pkg.Session
				p.Token = pkg.Session
			}
			if pkg.Access != "" {
				p.Token = pkg.Access
			}
			logf("使用渠道整包登录：channel=%s uid=%s", pkg.Channel, pkg.UID)
		}
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
	// 应用宝：extra_data 必须用**当前**凭证现场拼，不能沿用 PlayerLevel 那个字段。
	//
	// 内容随渠道而变（微信 login_type=8 + hy_gameid + wc_actoken，
	// QQ login_type=2 + openid + kp_actoken），而且续期后 atk 会换新，
	// 拿旧的拼出来服务端会拒。
	if acc.Channel == channel.KeyMyapp {
		if d, ok := myappCredOf(acc); ok {
			// 三个地方要同时对齐，少一个票据就不对：
			//   ch.ExtraData        → 票据里的 extra_data（游戏靠它认渠道）
			//   ch.SessionOverride  → SAUTH 的 sessionid 与票据 token（必须是 atk）
			//   custom              → uni_sauth 请求体里的 extra_data
			ch.ExtraData = myappExtraData(d)
			ch.SessionOverride = d.ATK
			custom["extra_data"] = ch.ExtraData
			p.Session = d.ATK
			p.Token = d.ATK
			p.UID = d.OpenID
		}
	}

	// 整包里 DLL 拼好的字段全部照抄，只保留我们自己生成的那几个随机值
	if pkg != nil {
		for k, v := range pkg.Fields {
			custom[k] = v
		}
		custom["login_channel"] = ch.Login
		custom["app_channel"] = ch.Login
		if _, ok := pkg.Fields["source_app_channel"]; !ok {
			custom["source_app_channel"] = ch.Login
		}
	}

	// 4399 的 confirm token 是 access_token；SAUTH 的 session（GameAuthSign）
	// 是 state，两个值不同。其它渠道两者同值，不受影响。
	if acc.Channel == "m4399" && acc.AccessToken != "" && pkg == nil {
		p.Token = acc.AccessToken
	}

	p.Channel = ch
	p.SAUTH = netsec.NewSAUTH(p.UID, p.Session, profile.ShortGameID, custom, dev)
	return p, nil
}

// buildPayload 组装发往网易的登录包（SAUTH + uni_sauth + confirm 表单）。
//
// 三种账号来源在这里汇合：
//   - 网易账号 / 华为账号：uid + 一个 session（华为 ST 或 gameAuthSign）
//   - B站/4399/vivo 网页登录：uid + 渠道 secret，按 channel.InfoOf 拼字段
//   - 渠道服扫码（vivo 这类）：DLL 给了整包，字段直接盖上去
func buildPayload(acc Account, dev *device.ClientProfile) (*netsec.LoginPayload, map[string]any, error) {
	parts, err := buildSauth(acc, dev)
	if err != nil {
		return nil, nil, err
	}
	resp, err := netsec.PostSigned(parts.SAUTH, profile.ShortGameID)
	if err != nil {
		return nil, nil, err
	}
	payload, err := netsec.NewLoginPayloadChan(resp, parts.Session, parts.Token, parts.UID, acc.PlayerLevel, acc.Ts, dev, parts.Channel)
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

// cleanupOnExit 启动器退出前把镜像里的浏览器壳还原，再尽量删掉镜像目录。
//
// 退出路径有三条（关窗口、托盘退出、正常返回），都走这里，
// 保证即使目录删不掉，也不会在磁盘上留下一个被换过的浏览器。
func cleanupOnExit() {
	sky := resolveSkyPath()
	if sky == "" {
		return
	}
	root := fever.MirrorRoot(sky, loadConfig().MirrorDir)
	restoreAllShims(sky, root)
	fever.RemoveMirrors(sky, root)
}



