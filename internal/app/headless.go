package app

// 无头调用 mpay.dll 的进程封装。
//
// mpayhost 里的 LoginByPassword / LoginByToken 等函数是"同一线程里跑一次动作"，
// 不能直接在主界面进程调：DLL 会建窗口、起异步线程、写本地库，失败时还可能
// 把进程带走。所以统一走子进程（cmd/skylogin 的 --headless-login），这里负责：
//
//	① 组请求（参数 + 密码）、起子进程、超时兜底、读结果
//	② 把结果按"凭证身份"落库：游戏身份进 MpayToken，平台身份进 A50Token
//
// 密码只走 stdin（不进命令行、不发日志），结果文件落在 %TEMP%，读完即删。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"skylogin/internal/fever"
	"skylogin/internal/profile"
)

// headlessRequest 与 cmd/skylogin/headless.go 里的结构一一对应。
type headlessRequest struct {
	Action   string `json:"action"`
	SkyDir   string `json:"sky"`
	Skin     string `json:"skin"`
	GameID   string `json:"game"`
	LogPath  string `json:"log"`
	UID      string `json:"uid"`
	Password string `json:"password"`
	Token    string `json:"token"`
	Ticket   string `json:"ticket"`
	Platform string `json:"platform"`
	Device   string `json:"device"`
	WaitSec  int    `json:"wait"`
}

// HeadlessResult 是子进程回给主进程的结果。
type HeadlessResult struct {
	OK       bool
	Error    string
	Ticket   string
	UserID   string
	DeviceID string
	GameID   string
	Token    string
	Platform bool // true = 平台身份凭证（要先换成游戏凭证才能声明渠道）
	Slots    []string
}

// headlessDirs 决定加载哪份 mpay.dll、用哪个皮肤资源。
// 与登录窗保持一致：默认平台目录那份（4.21），可在开发者调试里改。
func headlessDirs() (skyDir, skin string, err error) {
	sky := resolveSkyPath()
	if sky == "" {
		return "", "", fmt.Errorf("还没找到光遇，请先在设置里指定 sky.exe")
	}
	gameDir := filepath.Dir(sky)
	if _, statErr := os.Stat(filepath.Join(gameDir, "mpay.dll")); statErr != nil {
		return "", "", fmt.Errorf("游戏目录里没有 mpay.dll")
	}
	skin = fever.FindMpaySkin()
	if skin == "" {
		return "", "", fmt.Errorf("没找到网易发烧平台的皮肤资源")
	}
	dir, _ := mpayDebugDLLDir(gameDir, skin)
	return dir, skin, nil
}

// runHeadless 起子进程跑一次无头动作。超时会杀掉子进程并报错。
func runHeadless(req headlessRequest, timeout time.Duration) (HeadlessResult, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	exe, err := os.Executable()
	if err != nil {
		return HeadlessResult{}, fmt.Errorf("取自身路径失败: %w", err)
	}
	// 测试里宿主是 app.test.exe，不认识 --headless-login；允许显式指定。
	// 正常运行时不会设这个变量。
	if alt := os.Getenv("SKYLOGIN_EXE"); alt != "" {
		exe = alt
	}
	outPath := filepath.Join(os.TempDir(),
		fmt.Sprintf("skylogin-headless-%d.json", time.Now().UnixNano()))
	defer os.Remove(outPath)

	body, err := json.Marshal(req)
	if err != nil {
		return HeadlessResult{}, fmt.Errorf("组请求失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--headless-login", "--out", outPath)
	cmd.Stdin = bytes.NewReader(body)
	runErr := cmd.Run()

	raw, readErr := os.ReadFile(outPath)
	if readErr != nil {
		if ctx.Err() != nil {
			return HeadlessResult{}, fmt.Errorf("无头调用超时（%s）", timeout)
		}
		if runErr != nil {
			return HeadlessResult{}, fmt.Errorf("无头子进程退出: %v", runErr)
		}
		return HeadlessResult{}, fmt.Errorf("无头子进程没有留下结果")
	}
	var r HeadlessResult
	var wire struct {
		OK       bool     `json:"ok"`
		Error    string   `json:"error"`
		Ticket   string   `json:"ticket"`
		UserID   string   `json:"userId"`
		DeviceID string   `json:"deviceId"`
		GameID   string   `json:"gameId"`
		Token    string   `json:"token"`
		Platform bool     `json:"platform"`
		Slots    []string `json:"slots"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return HeadlessResult{}, fmt.Errorf("解析无头结果失败: %w", err)
	}
	r = HeadlessResult{
		OK: wire.OK, Error: wire.Error, Ticket: wire.Ticket,
		UserID: wire.UserID, DeviceID: wire.DeviceID, GameID: wire.GameID,
		Token: wire.Token, Platform: wire.Platform, Slots: wire.Slots,
	}
	if !r.OK {
		if r.Error == "" {
			r.Error = "无头登录没有成功"
		}
		return r, fmt.Errorf("%s", r.Error)
	}
	return r, nil
}

// HeadlessPasswordLogin 用账号密码无头登录。
//
// gameID 传 profile.LongGameID 得到游戏身份凭证（能按渠道签发），
// 传 profile.HostedGameID 得到平台身份凭证（只能换票 / 换 sid）。
func HeadlessPasswordLogin(user, password, gameID string) (HeadlessResult, error) {
	skyDir, skin, err := headlessDirs()
	if err != nil {
		return HeadlessResult{}, err
	}
	if gameID == "" {
		gameID = profile.LongGameID
	}
	return runHeadless(headlessRequest{
		Action:   "urs",
		SkyDir:   skyDir,
		Skin:     skin,
		GameID:   gameID,
		LogPath:  filepath.Join(dataDir(), "headless.log"),
		UID:      user,
		Password: password,
		WaitSec:  45,
	}, 120*time.Second)
}

// HeadlessTokenLogin 用已有凭证让 DLL 认一次（不重新输密码）。
func HeadlessTokenLogin(user, token, device, gameID string) (HeadlessResult, error) {
	skyDir, skin, err := headlessDirs()
	if err != nil {
		return HeadlessResult{}, err
	}
	if gameID == "" {
		gameID = profile.LongGameID
	}
	return runHeadless(headlessRequest{
		Action:  "token",
		SkyDir:  skyDir,
		Skin:    skin,
		GameID:  gameID,
		LogPath: filepath.Join(dataDir(), "headless.log"),
		UID:     user,
		Token:   token,
		Device:  device,
		WaitSec: 30,
	}, 90*time.Second)
}

// HeadlessListAccounts 读 DLL 记住的账号列表（诊断 / 界面展示用）。
func HeadlessListAccounts() (HeadlessResult, error) {
	skyDir, skin, err := headlessDirs()
	if err != nil {
		return HeadlessResult{}, err
	}
	return runHeadless(headlessRequest{
		Action:  "list",
		SkyDir:  skyDir,
		Skin:    skin,
		GameID:  profile.LongGameID,
		LogPath: filepath.Join(dataDir(), "headless.log"),
		WaitSec: 15,
	}, 60*time.Second)
}

// HeadlessLoginAccount 用账号密码无头添加/更新一个网易账号。
//
// 和"账号密码登录"（弹官方窗）的区别：全程没有窗口，密码是我们收的，
// 所以能顺手存下来供以后自动重登；凭证也是干净的**游戏身份**（ma75）。
//
// 2026-11-09 起一次登录**拿两份凭证**：
//
//	ma75（游戏身份）→ MpayToken，用来启动游戏、按渠道签发
//	a50 （平台身份）→ A50Token， 用来换加载湾 sid，也就是"当寄宿宿主"
//
// 为什么两份都要：宿主资格只看 A50Token，而换 sid 只认平台身份。
// 没有它，这个号就没法给扫码号提供槽位。
//
// 为什么登录后必须立刻落盘：同设备重新登录会把旧 token 顶掉
// （实测旧 A50Token 报 1000 sauth error，新登录的能正常换 sid）。
// 所以两份都在拿到后立刻存，不能只在内存里用。
//
// region 为空表示"先不选"；name 为空时用账号名当显示名。
func HeadlessLoginAccount(user, password, region, name string) (map[string]any, error) {
	if user == "" || password == "" {
		return nil, fmt.Errorf("账号和密码都不能为空")
	}
	res, err := HeadlessPasswordLogin(user, password, profile.LongGameID)
	if err != nil {
		return nil, err
	}
	if res.Platform {
		return nil, fmt.Errorf("这次登录拿到的是平台身份凭证，需要先换成游戏凭证；请先按游戏身份重登一次")
	}
	if res.UserID == "" || res.Token == "" {
		return nil, fmt.Errorf("登录成功但没拿到完整凭证")
	}

	now := time.Now().Unix()
	var acc Account
	existed := false
	if old, ok := findAccountByUserID(res.UserID); ok {
		acc = old
		existed = true
	} else {
		display := name
		if display == "" {
			display = user
		}
		acc = Account{
			Name:     display,
			NickName: display,
			Channel:  "netease",
			UserID:   res.UserID,
			PlayerID: res.UserID,
			Source:   "headless",
		}
	}
	acc.Username = user
	acc.MpayToken = res.Token
	if res.DeviceID != "" {
		acc.DeviceID = res.DeviceID
	}
	acc.GameID = res.GameID
	acc.LoginMethod = "password"
	acc.CredState = "ok"
	acc.LastError = ""
	acc.LastCheck = now
	acc.UpdatedAt = now
	if region != "" {
		if code, nerr := normalizeRegion(region); nerr == nil {
			acc.Region = code
		}
	}
	if err := saveAccount(acc); err != nil {
		return nil, fmt.Errorf("保存账号失败: %w", err)
	}
	// 密码单独存（DPAPI 加密），失败不影响这次登录，只是以后不能自动重登
	if err := saveAccountPassword(acc.PlayerID, user, password); err != nil {
		logf("%s：账号密码没能保存（%v），以后需要手动刷新凭证", displayName(acc), err)
	}
	logf("网易账号已就绪：%s（无头登录，凭证 %d 字符%s）", displayName(acc), len(res.Token),
		map[bool]string{true: "，已更新", false: "，新增"}[existed])

	// 第二份：平台身份。失败不影响这次登录，只是这个号暂时当不了宿主。
	hostOK := false
	hostRes, hostErr := HeadlessPasswordLogin(user, password, profile.HostedGameID)
	switch {
	case hostErr != nil:
		logf("%s：平台身份登录失败（%v），这个号暂时不能当寄宿宿主", displayName(acc), hostErr)
	case !hostRes.Platform:
		// 理论上不该发生：上下文传的就是 a50。真发生了说明 DLL 行为变了。
		logf("%s：平台登录没拿到平台身份凭证，暂不能当宿主", displayName(acc))
	default:
		if serr := saveHeadlessCred(acc.PlayerID, hostRes); serr != nil {
			logf("%s：平台凭证保存失败（%v），暂不能当宿主", displayName(acc), serr)
		} else {
			hostOK = true
			logf("%s：已同时取得平台身份凭证，可以当寄宿宿主", displayName(acc))
		}
	}

	return map[string]any{
		"id":       acc.PlayerID,
		"name":     displayName(acc),
		"userId":   acc.UserID,
		"existed":  existed,
		"region":   acc.Region,
		"credSize": len(res.Token),
		"canHost":  hostOK,
	}, nil
}

//
// 按身份分流：游戏身份进 MpayToken（能按渠道签发），平台身份进 A50Token。
// 设备号必须跟着 token 一起写——confirm_login / login-ticket 都要求两者配套。
func saveHeadlessCred(target string, r HeadlessResult) error {
	if r.Token == "" || r.UserID == "" {
		return fmt.Errorf("凭证不完整（token=%d 字符, uid=%q）", len(r.Token), r.UserID)
	}
	list, err := loadAccounts()
	if err != nil {
		return err
	}
	acc, ok := findAccount(list, target)
	if !ok {
		acc, ok = findAccountByUserID(r.UserID)
	}
	if !ok {
		return fmt.Errorf("没找到账号（%s）", target)
	}
	if r.Platform {
		acc.A50Token = r.Token
		if r.DeviceID != "" {
			acc.A50DeviceID = r.DeviceID
		}
		logf("%s：已更新平台身份凭证（a50，%d 字符）", displayName(acc), len(r.Token))
	} else {
		if acc.MpayToken != "" && tokenGI(acc.MpayToken) == profile.ShortGameID {
			// 手里已经是游戏身份凭证，别用同级的新凭证覆盖——避免把设备号换掉后
			// 正在跑的客户端对不上（令牌和 confirm 用的设备号必须是一对）。
			logf("%s：已有游戏身份凭证，本次不覆盖", displayName(acc))
			return nil
		}
		acc.MpayToken = r.Token
		if r.DeviceID != "" {
			acc.DeviceID = r.DeviceID
		}
		logf("%s：已更新游戏身份凭证（%d 字符，可签发）", displayName(acc), len(r.Token))
	}
	acc.CredState = "ok"
	acc.LastError = ""
	acc.LastCheck = time.Now().Unix()
	acc.UpdatedAt = time.Now().Unix()
	return saveAccount(acc)
}
