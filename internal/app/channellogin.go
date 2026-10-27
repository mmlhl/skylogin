package app

// 渠道网页登录的通用框架。
//
// 三个渠道（B站 / 4399 / vivo）的登录形态一致：起一个独立子进程打开
// 官方登录页，注入脚本把登录结果写回结果文件，主进程轮询结果文件、
// 在线验证凭证、存账号。渠道差异全部收在 channelSpec 里，加新渠道
// 只需要在 channel.Registry 里登记，这里的流程代码不用动。
//
// 为什么用子进程：一个进程只能跑一个 WebView2 消息循环，主界面已经
// 占了；登录窗崩了也不能带走主界面。

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"skylogin/internal/channel"
	"skylogin/internal/device"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

// channelSpec 一个渠道网页登录在主进程侧的全部差异点。
// （登录窗的外观与脚本在 channel.LoginWindowOf，由子进程取用。）
type channelSpec struct {
	resultFile string                     // 结果文件名（dataDir 下）
	profileDir func(target string) string // WebView2 会话目录（vivo 按账号归档）
	// 新登录（target 为空）时先清空会话目录：添加账号必须从零开始，
	// 不能复用上一次登录的会话（否则会"没输入就自动成功"）。
	wipeProfileOnNew bool
	// 登录成功后把会话目录归档到账号名下（vivo：add 临时目录 → vivo/<uid>），
	// 之后的静默续期才有 Cookie 可用。
	adoptProfile func(profileDir, uid string)
	loginURL     func(target string) (string, error)        // 登录页地址（4399 每次要新的）
	finalize     func(CapturedLoginResult) (Account, error) // 凭证验证 + 落库材料
}

// CapturedLoginResult 子进程回传的统一登录结果（结果文件的 schema）。
// secret 是该渠道的核心凭证（B站 access_key / 4399 state / vivo openToken），
// 渠道特有的附带材料放 extra（4399: accessToken / refreshToken）。
type CapturedLoginResult struct {
	OK       bool              `json:"ok"`
	UID      string            `json:"uid"`
	Secret   string            `json:"secret"`
	Uname    string            `json:"uname"`
	Extra    map[string]string `json:"extra"`
	Error    string            `json:"error"`
	Finished int64             `json:"finished"`
}

// m4399DevicePath 4399 设备身份的落盘位置。凭证与它绑定，必须稳定。
func m4399DevicePath() string { return filepath.Join(dataDir(), "s4399_device.json") }

// loadM4399Device 读 4399 设备身份，没有就生成一份并落盘。
func loadM4399Device() channel.M4399Device {
	if b, err := os.ReadFile(m4399DevicePath()); err == nil {
		var d channel.M4399Device
		if json.Unmarshal(b, &d) == nil && d.Udid != "" && d.DeviceIdentifier != "" {
			return d
		}
	}
	d := channel.NewM4399Device()
	b, _ := json.Marshal(d)
	_ = os.WriteFile(m4399DevicePath(), b, 0o600)
	return d
}

// sharedProfileDir 共享会话目录（登录态不区分账号的渠道用）。
func sharedProfileDir(key string) func(string) string {
	return func(string) string { return filepath.Join(webLoginRoot(), key) }
}

// fixedURL 固定登录页地址的渠道。
func fixedURL(u string) func(string) (string, error) {
	return func(string) (string, error) { return u, nil }
}

var channelSpecs = map[string]*channelSpec{
	channel.KeyBili: {
		resultFile: "bililogin-result.json",
		profileDir: sharedProfileDir("bilibili"),
		loginURL: fixedURL("https://sdk.biligame.com/login/?cef=true&gameId=" +
			channel.BiliGameID + "&appKey=" + channel.BiliAppKey + "&is_gov_ver=1"),
		finalize: finalizeBili,
	},
	channel.KeyM4399: {
		resultFile: "m4399login-result.json",
		profileDir: sharedProfileDir("m4399"),
		loginURL: func(string) (string, error) {
			// 4399 每次登录都要向 oauth.html 要一个新地址
			return channel.M4399LoginURL(loadM4399Device())
		},
		finalize: finalizeM4399,
	},
	channel.KeyVivo: {
		resultFile:       "vivologin-result.json",
		wipeProfileOnNew: true,
		profileDir: func(target string) string {
			if target == "" {
				// 添加账号：一次性隔离目录，成功后归档到账号名下
				return filepath.Join(webLoginRoot(), "vivo", "add")
			}
			// 刷新：用该账号自己的会话目录（Cookie 活着就能静默续期）
			return filepath.Join(webLoginRoot(), "vivo", sanitizeDirName(target))
		},
		loginURL:     fixedURL(channel.VivoPassportURL),
		finalize:     finalizeVivo,
		adoptProfile: adoptVivoProfile,
	},
}

// adoptVivoProfile 把"添加账号"用的临时会话目录归档到账号名下。
// 子进程刚被杀时 WebView2 可能还没完全释放文件，重试几秒再放弃；
// 归档失败只影响下次静默续期（需要重新登录一次），不影响本次结果。
func adoptVivoProfile(dir, uid string) {
	dst := filepath.Join(webLoginRoot(), "vivo", sanitizeDirName(uid))
	if dir == dst {
		return
	}
	for i := 0; i < 5; i++ {
		time.Sleep(time.Second)
		if err := os.Rename(dir, dst); err == nil {
			logf("vivo 会话已归档到 %s", dst)
			return
		}
	}
	logf("vivo 会话归档失败（%s → %s），下次刷新需要重新登录", dir, dst)
}

// loginProgress 渠道网页登录的进行状态，界面轮询它。
type loginProgress struct {
	Running bool   `json:"running"`
	Phase   string `json:"phase"` // waiting | done | failed
	Message string `json:"message"`
	Account string `json:"account"`
	UID     string `json:"uid"`
}

var (
	loginProgressMu   sync.Mutex
	loginProgressByCh = map[string]*loginProgress{}
	loginProgressProc = map[string]*os.Process{}
)

func loginProgressOf(key string) loginProgress {
	if s := loginProgressByCh[key]; s != nil {
		return *s
	}
	return loginProgress{Phase: "idle"}
}

// ChannelLoginStatus 给界面查询某个渠道的登录进度。
func ChannelLoginStatus(key string) map[string]any {
	loginProgressMu.Lock()
	defer loginProgressMu.Unlock()
	s := loginProgressOf(key)
	return map[string]any{
		"running": s.Running,
		"phase":   s.Phase,
		"message": s.Message,
		"account": s.Account,
		"uid":     s.UID,
	}
}

// StartChannelWebLogin 起一个子进程打开渠道官方登录页（可见窗口）。
// targetUserID 非空表示"给某个已有账号刷新登录"，完成后校验一致性。
func StartChannelWebLogin(key, targetUserID string) error {
	return startChannelWebLogin(key, targetUserID, false)
}

// StartChannelWebLoginHidden 同上，但窗口不可见（挂在屏幕外）。
// 静默续期用：Cookie 会话还活着时页面脚本自己完成取号换票，用户不该看到窗口。
func StartChannelWebLoginHidden(key, targetUserID string) error {
	return startChannelWebLogin(key, targetUserID, true)
}

func startChannelWebLogin(key, targetUserID string, hidden bool) error {
	spec, ok := channelSpecs[key]
	if !ok {
		return fmt.Errorf("不支持的渠道: %s", key)
	}

	loginProgressMu.Lock()
	if s := loginProgressOf(key); s.Running {
		loginProgressMu.Unlock()
		return fmt.Errorf("%s登录窗口已经打开了", channel.LabelOf(key))
	}
	msg := "等待在窗口里登录"
	if hidden {
		msg = "正在静默续期"
	}
	loginProgressByCh[key] = &loginProgress{Running: true, Phase: "waiting", Message: msg}
	loginProgressMu.Unlock()

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	profileDir := spec.profileDir(targetUserID)
	if spec.wipeProfileOnNew && targetUserID == "" {
		// 隔离：新登录不复用任何旧会话
		_ = os.RemoveAll(profileDir)
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return err
	}
	loginURL, err := spec.loginURL(targetUserID)
	if err != nil {
		loginProgressMu.Lock()
		loginProgressByCh[key] = &loginProgress{Running: true, Phase: "failed", Message: err.Error()}
		loginProgressMu.Unlock()
		return err
	}
	outPath := filepath.Join(dataDir(), spec.resultFile)
	_ = os.Remove(outPath) // 清掉上一次的结果，避免读到旧数据

	args := []string{"--channellogin", key, "--url", loginURL, "--profile", profileDir, "--out", outPath, "--target", targetUserID}
	if hidden {
		args = append(args, "--hidden")
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		loginProgressMu.Lock()
		loginProgressByCh[key] = &loginProgress{Running: true, Phase: "failed", Message: "打不开登录窗口: " + err.Error()}
		loginProgressMu.Unlock()
		return err
	}

	loginProgressMu.Lock()
	loginProgressProc[key] = cmd.Process
	loginProgressMu.Unlock()

	go watchChannelLogin(key, cmd, outPath, targetUserID, profileDir)
	return nil
}

// watchChannelLogin 等子进程写出结果，然后走渠道的 finalize 落库。
func watchChannelLogin(key string, cmd *exec.Cmd, outPath, targetUserID, profileDir string) {
	loginProgressMu.Lock()
	proc := loginProgressProc[key]
	loginProgressMu.Unlock()

	// 子进程崩溃（比如会话目录被残留进程占用导致 WebView2 创建失败）时
	// 快速失败，不让用户干等超时
	childDone := make(chan struct{})
	go func() { _ = cmd.Wait(); close(childDone) }()

	defer func() {
		loginProgressMu.Lock()
		if s := loginProgressByCh[key]; s != nil {
			s.Running = false
		}
		delete(loginProgressProc, key)
		loginProgressMu.Unlock()
		_ = os.Remove(outPath)
	}()

	fail := func(msg string) {
		loginProgressMu.Lock()
		loginProgressByCh[key] = &loginProgress{Phase: "failed", Message: msg}
		loginProgressMu.Unlock()
		if proc != nil {
			_ = proc.Kill()
		}
	}

	spec := channelSpecs[key]

	// 轮询结果文件；给足 10 分钟（短信 / 扫码可能要等很久）
	deadline := time.Now().Add(10 * time.Minute)
	var res CapturedLoginResult
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(outPath); err == nil && len(b) > 0 {
			if json.Unmarshal(b, &res) == nil && (res.OK || res.Error != "") {
				break
			}
		}
		select {
		case <-childDone:
			// 进程已退出但没写出结果：窗口被关/崩溃，直接报失败
			fail("窗口已关闭，未获取到登录信息")
			return
		default:
		}
		time.Sleep(500 * time.Millisecond)
	}
	if res.Error != "" || !res.OK {
		msg := res.Error
		if msg == "" {
			msg = "登录超时，请重试"
		}
		fail(msg)
		return
	}
	if proc != nil {
		_ = proc.Kill() // 已拿到凭证，登录窗不再需要
	}

	// 防串号：给已有账号刷新时，必须还是同一个账号
	if targetUserID != "" && targetUserID != res.UID {
		fail(fmt.Sprintf("登录的账号与要刷新的账号不一致（期望 %s，实际 %s）", targetUserID, res.UID))
		return
	}

	acc, err := spec.finalize(res)
	if err != nil {
		fail(err.Error())
		return
	}
	// 验证通过 → 落库。这一步丢了就会出现"界面显示成功，
	// 账号列表和账号文件里却什么都没有"。
	if err := saveChannelAccount(acc); err != nil {
		fail("保存账号失败: " + err.Error())
		return
	}

	if spec.adoptProfile != nil {
		spec.adoptProfile(profileDir, acc.PlayerID)
	}

	loginProgressMu.Lock()
	loginProgressByCh[key] = &loginProgress{
		Phase: "done", Message: channel.LabelOf(key) + "账号登录成功，已保存",
		Account: acc.Name, UID: acc.PlayerID,
	}
	loginProgressMu.Unlock()
	logf("%s账号登录成功并已保存: %s (uid=%s)", channel.LabelOf(key), acc.Name, acc.PlayerID)
}

// saveChannelAccount 把验证过的渠道账号落库（保留用户起的别名）。
func saveChannelAccount(acc Account) error {
	if old, ok := findAccountByUserID(acc.PlayerID); ok {
		acc.Name = old.Name
		acc.NickName = old.NickName
		acc.PlayerLevel = old.PlayerLevel
		// 失败响应里可能缺附带材料，不能把存量值洗掉
		if acc.RefreshToken == "" {
			acc.RefreshToken = old.RefreshToken
		}
	}
	if err := saveAccount(acc); err != nil {
		return err
	}
	_ = setCurrent(acc.PlayerID)
	clearAuthHint()
	return nil
}

// channelSAUTH 用渠道身份走一次 uni_sauth：验证渠道凭证真的能换到光遇
// 登录态。三个渠道落库前都要过这一关。
func channelSAUTH(key, uid, secret string) error {
	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		return fmt.Errorf("设备标识不可用")
	}
	custom := map[string]any{
		"login_channel":      channel.InfoOf(key).Login,
		"app_channel":        channel.InfoOf(key).Login,
		"source_app_channel": channel.InfoOf(key).Login,
		"sdk_version":        channel.InfoOf(key).SDKVersion,
	}
	if _, err := netsec.PostSigned(netsec.NewSAUTH(uid, secret, profile.ShortGameID, custom, dev), profile.ShortGameID); err != nil {
		return fmt.Errorf("渠道身份验证失败（%s）", err.Error())
	}
	return nil
}

// finalizeBili B站：auto.login 验证 access_key 活性（顺带续期 30 天、拿昵称）。
func finalizeBili(res CapturedLoginResult) (Account, error) {
	uid2, uname, err := channel.NewBili().Verify(res.Secret)
	if err != nil {
		return Account{}, fmt.Errorf("B站凭证验证失败（%s）", err.Error())
	}
	if uname == "" {
		uname = res.Uname
	}
	if uname == "" {
		uname = res.UID
	}
	uid := res.UID
	if uid2 != "" && uid2 != uid {
		uid = uid2 // 以验证接口返回的 uid 为准
	}
	if err := channelSAUTH(channel.KeyBili, uid, res.Secret); err != nil {
		return Account{}, err
	}
	a50dev, _ := ensureA50DeviceID()
	return Account{
		Name: uname, NickName: uname, Username: uname,
		Channel: channel.KeyBili, UserID: uid, PlayerID: uid,
		GameAuthSign: res.Secret, DeviceID: a50dev,
		Source: "biliweb", LoginMethod: "web",
		CredState: "ok", LastCheck: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}, nil
}

// finalizeM4399 4399：oauth-check 在线验证 state（网络原因查不了时不拦，
// 后面的 uni_sauth 会再验一次），然后验证渠道身份。
func finalizeM4399(res CapturedLoginResult) (Account, error) {
	accessToken := res.Extra["accessToken"]
	if accessToken == "" {
		return Account{}, fmt.Errorf("4399 登录材料不完整（缺 access_token）")
	}
	if valid, err := channel.M4399Check(res.UID, res.Secret); err == nil && !valid {
		return Account{}, fmt.Errorf("4399 凭证校验未通过，请重新登录")
	}
	if err := channelSAUTH(channel.KeyM4399, res.UID, res.Secret); err != nil {
		return Account{}, err
	}
	uname := res.Uname
	if uname == "" {
		uname = res.UID
	}
	a50dev, _ := ensureA50DeviceID()
	acc := Account{
		Name: uname, NickName: uname, Username: uname,
		Channel: channel.KeyM4399, UserID: res.UID, PlayerID: res.UID,
		GameAuthSign: res.Secret, AccessToken: accessToken, RefreshToken: res.Extra["refreshToken"],
		DeviceID: a50dev,
		Source:   "m4399web", LoginMethod: "web",
		CredState: "ok", LastCheck: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	if old, ok := findAccountByUserID(res.UID); ok && old.RefreshToken != "" && acc.RefreshToken == "" {
		acc.RefreshToken = old.RefreshToken
	}
	return acc, nil
}

// finalizeVivo vivo：没有独立的在线校验接口，uni_sauth 即验证。
func finalizeVivo(res CapturedLoginResult) (Account, error) {
	if err := channelSAUTH(channel.KeyVivo, res.UID, res.Secret); err != nil {
		return Account{}, err
	}
	uname := res.Uname
	if uname == "" {
		uname = res.UID
	}
	a50dev, _ := ensureA50DeviceID()
	return Account{
		Name: uname, NickName: uname, Username: uname,
		Channel: channel.KeyVivo, UserID: res.UID, PlayerID: res.UID,
		GameAuthSign: res.Secret, DeviceID: a50dev,
		Source: "vivoweb", LoginMethod: "web",
		CredState: "ok", LastCheck: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}, nil
}

// ensureChannelCredential 启动前的凭证保活：能用渠道自己的接口静默
// 续期就续期，全失败才标记过期让用户重新登录。
func ensureChannelCredential(acc *Account) error {
	switch acc.Channel {
	case channel.KeyBili:
		// auto.login 验证活性即续期
		if _, _, err := channel.NewBili().Verify(acc.GameAuthSign); err != nil {
			expireChannelCredential(acc, "B站登录已失效", "bili_expired", "B站登录已过期", "请重新扫码添加该账号")
			return fmt.Errorf("B站登录已过期，请重新添加该账号")
		}
		return nil
	case channel.KeyM4399:
		return ensureM4399Credential(acc)
	case channel.KeyVivo:
		// openToken 没有独立校验接口；uni_sauth（buildPayload 里）就是验证，
		// 失败时在那边翻译成"重新登录"提示
		return nil
	}
	return nil
}

// ensureM4399Credential 4399 的静默恢复链：check → refresh_token 轮换 →
// state 兜底，全失败才标记过期。
func ensureM4399Credential(acc *Account) error {
	dev := loadM4399Device()

	// oauth-check 是真值；查不了（网络原因）时继续尝试恢复
	if valid, err := channel.M4399Check(acc.UserID, acc.GameAuthSign); err == nil && valid {
		acc.CredState = "ok"
		acc.LastCheck = time.Now().Unix()
		_ = saveAccount(*acc)
		return nil
	}

	// refresh_token 轮换。成功响应不含 refresh_token，保留旧值
	rotate := func(res channel.M4399Login) bool {
		acc.GameAuthSign = res.State
		acc.AccessToken = res.AccessToken
		acc.CredState = "ok"
		acc.LastCheck = time.Now().Unix()
		acc.UpdatedAt = time.Now().Unix()
		if err := saveAccount(*acc); err != nil {
			logf("4399 凭证刷新落盘失败: %v", err)
		}
		logf("4399账号 %s：凭证已静默续期", displayName(*acc))
		return true
	}
	if res, err := channel.M4399RefreshByToken(dev, acc.UserID, acc.RefreshToken); err == nil && res.Complete() {
		rotate(res)
		return nil
	}
	if res, err := channel.M4399RefreshByState(dev, acc.GameAuthSign); err == nil && res.Complete() {
		rotate(res)
		return nil
	}

	expireChannelCredential(acc, "4399登录已失效", "m4399_expired", "4399登录已过期", "请重新登录该账号")
	return fmt.Errorf("4399登录已失效，请重新登录该账号")
}

// expireChannelCredential 标记过期并给界面推提示。
func expireChannelCredential(acc *Account, reason, hintKind, hintTitle, hintTail string) {
	setAccountCredState(acc.PlayerID, "expired", reason)
	setAuthHint(hintKind, hintTitle,
		fmt.Sprintf("%s 的%s，%s", displayName(*acc), hintTitle, hintTail), acc.Username, acc.PlayerID)
}
