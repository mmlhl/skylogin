package app

// 账号密码登录：交给官方 mpay.dll 弹它自己的登录窗。
//
// 为什么不自己发账号密码请求：
//   MPay 的账号密码接口里 params 是原生 SDK 加密的（密钥在 DLL 里，字符串表里也没有），
//   纯 Go 复现不了。让 DLL 完成登录、拿一张票，再用现成的 login/ticket 换 token，
//   既不用碰加密，也不用装证书/代理。
//
// 进程模型：主进程只起子进程（skylogin.exe --mpaylogin）并轮询结果文件。
// DLL 崩了最多带走子进程，界面还在。

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"skylogin/internal/fever"
	"skylogin/internal/mpay"
	"skylogin/internal/mpayhost"
	"skylogin/internal/profile"
	"skylogin/internal/win"
)

const mpayLoginTmpName = "mpaylogin-result.json"

// mpayLoginState 是界面上"正在等官方窗口登录"的状态。
type mpayLoginState struct {
	Running bool   `json:"running"`
	Phase   string `json:"phase"` // waiting | done | failed
	Message string `json:"message"`
	Account string `json:"account"`
	UserID  string `json:"userId"`
}

var (
	mpayLoginMu     sync.Mutex
	mpayLoginCur    = mpayLoginState{Phase: "idle"}
	mpayLoginProc   *os.Process
	mpayLoginCancel bool   // 用户主动点了"取消"
	uiTitle         string // 主界面标题，用来算弹窗位置
)

// MpayLoginStatus 给界面查询进度。
func MpayLoginStatus() map[string]any {
	mpayLoginMu.Lock()
	defer mpayLoginMu.Unlock()
	return map[string]any{
		"running": mpayLoginCur.Running,
		"phase":   mpayLoginCur.Phase,
		"message": mpayLoginCur.Message,
		"account": mpayLoginCur.Account,
		"userId":  mpayLoginCur.UserID,
	}
}

// StartMpayLogin 起子进程加载 mpay.dll 并弹出官方登录窗。
//
// targetUserID 非空表示"给已有账号刷新凭证"，登录完成后会校验账号一致，
// 避免把 A 的凭证覆盖成 B 的。
func StartMpayLogin(targetUserID string) error {
	mpayLoginMu.Lock()
	if mpayLoginCur.Running {
		mpayLoginMu.Unlock()
		return fmt.Errorf("登录窗口已经打开了")
	}
	mpayLoginMu.Unlock()

	sky := resolveSkyPath()
	if sky == "" {
		setMpayLoginState("failed", "还没找到光遇，请先在设置里指定 sky.exe", "", "")
		return fmt.Errorf("还没找到光遇，请先在设置里指定 sky.exe")
	}
	skyDir := filepath.Dir(sky)
	if _, err := os.Stat(filepath.Join(skyDir, "mpay.dll")); err != nil {
		setMpayLoginState("failed", "游戏目录里没有 mpay.dll，光遇可能没装完整", "", "")
		return fmt.Errorf("游戏目录里没有 mpay.dll")
	}
	skin := fever.FindMpaySkin()
	if skin == "" {
		setMpayLoginState("failed", "没找到网易发烧平台的皮肤资源，请先安装或更新发烧平台", "", "")
		return fmt.Errorf("没找到发烧平台的 skin.zip")
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	outPath := filepath.Join(dataDir(), mpayLoginTmpName)
	_ = os.Remove(outPath)
	logPath := filepath.Join(dataDir(), "log.txt")

	mpayLoginMu.Lock()
	mpayLoginCur = mpayLoginState{Running: true, Phase: "waiting", Message: "请在弹出的网易登录窗口里完成登录"}
	mpayLoginCancel = false
	mpayLoginMu.Unlock()

	args := []string{
		"--mpaylogin",
		"--sky", skyDir,
		"--skin", skin,
		"--game", profile.HostedGameID,
		"--out", outPath,
		"--log", logPath,
	}
	// 官方登录窗以宿主窗口为中心弹出，所以把主界面的中心点告诉子进程。
	// 先用 OpenWebView 记下来的窗口句柄（可靠），再退回按标题找。
	cx, cy, ok := win.MainWindowCenter()
	if !ok {
		cx, cy, ok = win.WindowCenterByTitle(uiTitle)
	}
	// 坐标异常（0 或负）就当没拿到：宁可让子进程用屏幕中心，
	// 也不能把登录窗丢到左上角去。
	if ok && cx > 0 && cy > 0 {
		logf("网易登录窗将居中到 (%d,%d)", cx, cy)
		args = append(args, "--center-x", strconv.Itoa(cx), "--center-y", strconv.Itoa(cy))
	} else {
		logf("没拿到有效的主界面位置（%d,%d），登录窗改用屏幕中心", cx, cy)
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = skyDir
	if err := cmd.Start(); err != nil {
		setMpayLoginState("failed", "打不开登录窗口: "+err.Error(), "", "")
		return err
	}

	mpayLoginMu.Lock()
	mpayLoginProc = cmd.Process
	mpayLoginMu.Unlock()

	go watchMpayLogin(cmd, outPath, targetUserID)
	return nil
}

// CancelMpayLogin 关掉登录窗口（用户点"取消"时）。
func CancelMpayLogin() {
	mpayLoginMu.Lock()
	p := mpayLoginProc
	mpayLoginCancel = true
	mpayLoginMu.Unlock()
	if p != nil {
		_ = p.Kill()
	}
}

// wasCancelled 判断这次失败是不是用户自己取消的。
func wasCancelled() bool {
	mpayLoginMu.Lock()
	defer mpayLoginMu.Unlock()
	return mpayLoginCancel
}

func setMpayLoginState(phase, msg, account, uid string) {
	mpayLoginMu.Lock()
	mpayLoginCur = mpayLoginState{Phase: phase, Message: msg, Account: account, UserID: uid}
	mpayLoginMu.Unlock()
}

// watchMpayLogin 等子进程写出结果，然后换 token 落库。
func watchMpayLogin(cmd *exec.Cmd, outPath, targetUserID string) {
	defer func() {
		mpayLoginMu.Lock()
		mpayLoginCur.Running = false
		mpayLoginProc = nil
		mpayLoginMu.Unlock()
		_ = os.Remove(outPath)
	}()

	deadline := time.Now().Add(6 * time.Minute)
	var res mpayhost.Result

	// 子进程退出后它写的文件才是最终结果，所以 Wait 单独放一个 goroutine，
	// 循环里既能轮询文件，也能发现"窗口被关掉但没写结果"。
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	readResult := func() bool {
		b, err := os.ReadFile(outPath)
		if err != nil || len(b) == 0 {
			return false
		}
		var r mpayhost.Result
		if json.Unmarshal(b, &r) != nil || r.Finished == 0 {
			return false
		}
		res = r
		return true
	}

	for time.Now().Before(deadline) {
		if readResult() {
			break
		}
		select {
		case <-exited:
			if !readResult() {
				setMpayLoginState("failed", "登录窗口已关闭，没有拿到登录结果", "", "")
				return
			}
		default:
		}
		time.Sleep(300 * time.Millisecond)
	}

	if res.Finished == 0 {
		_ = cmd.Process.Kill()
		setMpayLoginState("failed", "等待登录超时，请重试", "", "")
		return
	}

	if !res.OK {
		msg := res.Error
		if msg == "" {
			msg = "登录失败"
		}
		switch {
		case wasCancelled():
			msg = "已取消登录"
		case strings.Contains(res.Error, "登录窗口已关闭"):
			// 用户直接点了网易窗口右上角的 X，按取消处理
			msg = "已取消登录"
		default:
			logf("官方登录窗失败: %s %s", msg, mpayhost.Describe(res.Slots))
			msg = "登录没有完成，请重试"
		}
		setMpayLoginState("failed", msg, "", "")
		return
	}

	token, userID, account, err := resolveMPayCredentials(res)
	if err != nil {
		setMpayLoginState("failed", err.Error(), res.Account, "")
		return
	}
	if userID == "" || token == "" {
		setMpayLoginState("failed", "没有拿到有效的登录凭证", res.Account, "")
		return
	}

	// 防串号：给已有账号刷新时，必须还是同一个账号
	if targetUserID != "" && targetUserID != userID {
		setMpayLoginState("failed",
			fmt.Sprintf("登录的账号与要刷新的账号不一致（期望 %s，实际 %s），已取消", targetUserID, userID),
			account, userID)
		return
	}

	dev := res.DeviceID
	if dev == "" {
		dev, _ = ensureDeviceID()
	}

	acc := Account{
		Name:        account,
		Username:    account,
		Channel:     "netease",
		UserID:      userID,
		PlayerID:    userID,
		MpayToken:   token,
		DeviceID:    dev,
		Source:      "mpaydll",
		LoginMethod: "password",
		CredState:   "ok",
		LastCheck:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	if acc.Name == "" {
		acc.Name = userID
	}
	if old, ok := findAccountByUserID(userID); ok {
		// 老记录可能把用户 id 当成了别名（早先拿不到 display_username 时的兜底）。
		// 这次能拿到真正的账号名，就顺手把它修掉。
		if account != "" && looksLikeBareAccountID(old.Name) {
			acc.Name = account
		} else if old.Name != "" {
			acc.Name = old.Name
		}
		acc.NickName = old.NickName
		acc.PlayerLevel = old.PlayerLevel
		acc.SavedUser = old.SavedUser
		acc.SavedPass = old.SavedPass
	}

	if err := saveAccount(acc); err != nil {
		setMpayLoginState("failed", "保存账号失败: "+err.Error(), account, userID)
		return
	}
	_ = setCurrent(acc.PlayerID)
	clearAuthHint()
	logf("官方窗口登录成功，账号已保存")
	setMpayLoginState("done", "登录成功，已保存", account, userID)
}

// resolveMPayCredentials 把子进程的结果变成 (token, userId, 账号名)。
//
// 首选：用 DLL 给的票走 login/ticket 换 token（和手机号登录同一条链路）；
// 兜底：直接从回调里取 token（个别版本的 DLL 不出票，但回调里带 user_token）。
// 账号名单独再查一次用户资料——a50 上下文的 login/ticket 经常不带
// display_username，不查就只能拿 id 当别名。
func resolveMPayCredentials(res mpayhost.Result) (token, userID, account string, err error) {
	account = res.Account
	dev := res.DeviceID
	if dev == "" {
		dev, _ = ensureDeviceID()
	}

	if t := slotToken(res.Slots); t != "" {
		token = t
		userID = slotUserID(res.Slots)
	}

	if res.Ticket != "" {
		c := mpay.New()
		out, e := loginTicketAuto(c, res.Ticket, dev)
		if e != nil {
			if token == "" || userID == "" {
				return "", "", account, fmt.Errorf("用官方登录窗的票据换 token 失败: %w", e)
			}
			logf("用票据换 token 失败（改用回调里的 token）: %v", e)
		} else {
			user, _ := out["user"].(map[string]any)
			if v, _ := user["token"].(string); v != "" {
				token = v
			}
			if v, _ := user["id"].(string); v != "" {
				userID = v
			}
			if account == "" {
				account = mpay.DisplayNameOf(user)
			}
		}
	}

	if token == "" {
		return "", "", account, fmt.Errorf("没有拿到 MPay 登录凭证")
	}
	if userID == "" {
		return "", "", account, fmt.Errorf("没有识别出登录的账号 id，请重试")
	}

	// 仍然没有账号名：按官方启动器的方式查一次用户资料
	if account == "" {
		if user, e := mpay.New().GetUserInfo(token, userID, dev, profile.HostedGameID, "a50"); e == nil {
			account = mpay.DisplayNameOf(user)
		} else {
			logf("查账号显示名失败: %v", e)
		}
	}
	return token, userID, account, nil
}

// slotToken 在回调数组里找 user_token（形态是 1-eyJ…）。
func slotToken(slots []string) string {
	for _, s := range slots {
		if len(s) > 40 && strings.HasPrefix(s, "1-eyJ") {
			return s
		}
	}
	return ""
}

// slotUserID 取回调里的用户 id（槽位 0）。
func slotUserID(slots []string) string {
	if len(slots) > 0 {
		v := strings.TrimSpace(slots[0])
		if v != "" && v != "0" && !strings.Contains(v, "@") {
			return v
		}
	}
	return ""
}

// looksLikeBareAccountID 判断别名是不是一串 MPay 用户 id（不是邮箱/手机号）。
//
// 形如 aedemo0000000000：全小写字母+数字、长度 >= 16、没有 @。
func looksLikeBareAccountID(name string) bool {
	name = strings.TrimSpace(name)
	if len(name) < 16 || strings.Contains(name, "@") {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// repairBareAccountNames 修掉"别名是一串用户 id"的历史记录。
//
// 早先账号密码登录拿不到 display_username 时，保存的是 MPay 用户 id；
// 现在能查用户资料了，启动后在后台把这类记录改回邮箱/手机号。
// 失败（token 过期等）就跳过，不影响使用。
func repairBareAccountNames() {
	list, err := loadAccounts()
	if err != nil || len(list) == 0 {
		return
	}
	changed := false
	for i := range list {
		a := &list[i]
		if a.Channel != "netease" || a.MpayToken == "" || a.UserID == "" || a.DeviceID == "" {
			continue
		}
		if !looksLikeBareAccountID(a.Name) {
			continue
		}
		user, err := mpay.New().GetUserInfo(a.MpayToken, a.UserID, a.DeviceID, profile.HostedGameID, "a50")
		if err != nil {
			continue
		}
		if name := mpay.DisplayNameOf(user); name != "" {
			a.Name = name
			a.Username = name
			changed = true
			logf("已把账号别名从 id 修正为账号名: %s", name)
		}
	}
	if changed {
		_ = saveList(list)
	}
}

// cleanupStaleMirrors 清掉上次多开残留的镜像目录（比如工具被强杀、没来得及删）。
//
// 有 sky.exe 在跑时不能删（正在用的目录删了会崩），所以改成等它退出：
// 工具开着的时候每 5 秒看一次，游戏全部退出后立刻清掉。
func cleanupStaleMirrors() {
	sky := resolveSkyPath()
	if sky == "" {
		return
	}
	root := fever.MirrorRoot(sky, loadConfig().MirrorDir)
	if fever.CountProcess("sky.exe") == 0 {
		fever.RemoveMirrors(sky, root)
		return
	}
	logf("启动清理：还有光遇在运行，等它退出后再清镜像目录")
	go func() {
		for i := 0; i < 1440; i++ { // 最多盯 2 小时
			time.Sleep(5 * time.Second)
			if fever.CountProcess("sky.exe") == 0 {
				time.Sleep(5 * time.Second) // 给游戏收尾留点时间
				fever.RemoveMirrors(sky, root)
				if !fever.MirrorRootExists(root) {
					logf("启动清理：光遇已退出，上次的镜像目录已清理")
				}
				return
			}
		}
	}()
}
