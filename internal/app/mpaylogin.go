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
	"sort"
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

var (
	// mpayLoginTr 登录进度（状态机见 loginprogress.go）
	mpayLoginTr     = newLoginTracker()
	mpayLoginMu     sync.Mutex
	mpayLoginProc   *os.Process
	mpayLoginCancel bool // 用户主动点了"取消"
)

// MpayLoginStatus 给界面查询进度。
func MpayLoginStatus() map[string]any {
	s := mpayLoginTr.snapshot()
	return map[string]any{
		"running": s.Running,
		"phase":   s.Phase,
		"message": s.Message,
		"account": s.Account,
		"userId":  s.UID,
	}
}

// StartMpayLogin 起子进程加载 mpay.dll 并弹出官方登录窗。
//
// targetUserID 非空表示"给已有账号刷新凭证"，登录完成后会校验账号一致，
// 避免把 A 的凭证覆盖成 B 的。
func StartMpayLogin(targetUserID string) error {
	return startMpayLogin(targetUserID, "")
}

// StartChannelScanLogin 打开官方登录窗并强制用**游戏身份**（ma75）初始化，
// 让渠道服客户端（华为这类）也能扫码。扫完存成对应的渠道账号。
func StartChannelScanLogin() error {
	return startMpayLogin("", profile.LongGameID)
}

func startMpayLogin(targetUserID, forceGame string) error {
	mpayLoginMu.Lock()
	if mpayLoginTr.snapshot().Running {
		mpayLoginMu.Unlock()
		return fmt.Errorf("登录窗口已经打开了")
	}
	mpayLoginMu.Unlock()

	sky := resolveSkyPath()
	if sky == "" {
		mpayLoginTr.set("failed", "还没找到光遇，请先在设置里指定 sky.exe", "", "")
		return fmt.Errorf("还没找到光遇，请先在设置里指定 sky.exe")
	}
	skyDir := filepath.Dir(sky)
	if _, err := os.Stat(filepath.Join(skyDir, "mpay.dll")); err != nil {
		mpayLoginTr.set("failed", "游戏目录里没有 mpay.dll，光遇可能没装完整", "", "")
		return fmt.Errorf("游戏目录里没有 mpay.dll")
	}
	skin := fever.FindMpaySkin()
	if skin == "" {
		mpayLoginTr.set("failed", "没找到网易发烧平台的皮肤资源，请先安装或更新发烧平台", "", "")
		return fmt.Errorf("没找到发烧平台的 skin.zip")
	}
	// 用哪份 DLL、哪个 game id、要不要跳过清理登录态，全部来自设置页的
	// 「开发者调试」（见 mpaydebug.go），不再靠 .on 开关文件。
	dbg := mpayDebug()
	// 扫码窗默认用**平台目录**那份 mpay.dll（版本更新），并以游戏身份 ma75 初始化：
	// 这是实测下来扫码更稳的组合（拿到的凭证带游戏 g_i，寿命也更长）。
	// 用户在「开发者调试」里显式选过 DLL 时以他的选择为准。
	if forceGame != "" && dbg.DLL == "" {
		dbg.DLL = "platform"
	}
	skillDir, dllFrom := mpayDebugDLLDir(skyDir, skin)
	if skillDir != skyDir {
		logf("[调试] 加载 %s 的 mpay.dll：%s", dllFrom, skillDir)
		skyDir = skillDir
	}
	dataPath := ""
	skipLogout := dbg.SkipLogout
	if dbg.DataPath != "" {
		dataPath = dbg.DataPath
		logf("[调试] 数据目录：%s", dataPath)
	}
	// 默认给每次登录一个**临时数据目录**：
	//
	//	DLL 会把“上次登录的账号”“记住的平台选择”这类状态写在数据目录里。
	//	直接在用户的正式目录里登，就两头为难：不清态——会静默登录成上次
	//	那个号（加不了新账号）；清态——平台记忆也一起没了，登录完又弹
	//	“你玩哪个端”。
	//
	//	换成全新临时目录就都绕开了：本来就是空的，不需要清态，也不会有
	//	账号列表页；登录拿到结果后整个目录删掉，不污染正式数据。
	var tmpDataDir string
	if dataPath == "" {
		tmpDataDir = filepath.Join(os.TempDir(), "SkyLoginLogin",
			strconv.FormatInt(time.Now().UnixNano(), 36))
		if err := os.MkdirAll(tmpDataDir, 0o755); err == nil {
			dataPath = tmpDataDir
			logf("本次登录用临时数据目录：%s", tmpDataDir)
		} else {
			tmpDataDir = ""
			logf("临时数据目录创建失败（%v），改用正式目录", err)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	outPath := filepath.Join(dataDir(), mpayLoginTmpName)
	_ = os.Remove(outPath)
	logPath := filepath.Join(dataDir(), "log.txt")

	mpayLoginMu.Lock()
	mpayLoginTr.begin("请在弹出的网易登录窗口里完成登录")
	mpayLoginCancel = false
	mpayLoginMu.Unlock()

	// 诊断开关：用光遇自己的 game id 初始化 DLL。
	//   - a50（默认）：登录窗里的二维码是"平台码"，只能网易大神扫
	//   - ma75：变成"游戏内那种码"，光遇手游能扫，DLL 会自己走 exchange_token
	// 两种扫完回调的 token 等级可能不同，所以单独留个开关对比。
	gameID := mpayDebugGameID()
	if forceGame != "" {
		gameID = forceGame
		logf("渠道服扫码：登录窗用游戏身份初始化（%s）", gameID)
	} else if gameID == profile.HostedGameID {
		// 默认是 ma75（游戏身份）；只有在调试里显式选 a50 时才提醒：
		// a50 上下文登出来的凭证是平台身份，不能按渠道签发。
		logf("[调试] 登录窗用平台身份初始化（%s）：此凭证不能按渠道签发", gameID)
	}
	args := []string{
		"--mpaylogin",
		"--sky", skyDir,
		"--skin", skin,
		"--game", gameID,
		"--out", outPath,
		"--log", logPath,
	}
	// 渠道服扫码：用户只能扫码，让登录窗直接停在二维码页，
	// 省掉"先出来一个账号密码表单、再点右下角小二维码按钮"那一步。
	if forceGame != "" {
		args = append(args, "--qr-only")
	}
	// 官方登录窗以主界面为中心弹出，把主界面的中心点告诉子进程；
	// 拿不到（浏览器模式没有原生窗口）就让子进程用屏幕中心
	if cx, cy, ok := win.MainWindowCenter(); ok && cx > 0 && cy > 0 {
		logf("网易登录窗将居中到 (%d,%d)", cx, cy)
		args = append(args, "--center-x", strconv.Itoa(cx), "--center-y", strconv.Itoa(cy))
	}
	if dataPath != "" {
		args = append(args, "--data-path", dataPath)
	}
	if skipLogout {
		args = append(args, "--no-logout")
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = skyDir
	if err := cmd.Start(); err != nil {
		mpayLoginTr.set("failed", "打不开登录窗口: "+err.Error(), "", "")
		mpayLoginTr.finish()
		return err
	}

	mpayLoginMu.Lock()
	mpayLoginProc = cmd.Process
	mpayLoginMu.Unlock()

	go watchMpayLogin(cmd, outPath, targetUserID, tmpDataDir)
	return nil
}

// copyDbFiles 把平台 unisdk 目录里的凭证库等文件复制一份到 dst（诊断用）。
func copyDbFiles(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	copied := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".db") && !strings.HasSuffix(name, ".cfg") && !strings.HasSuffix(name, ".dat") {
			continue
		}
		if err := copyFile(filepath.Join(src, name), filepath.Join(dst, name)); err != nil {
			return err
		}
		copied++
	}
	if copied == 0 {
		return fmt.Errorf("%s 里没有凭证文件", src)
	}
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

// watchMpayLogin 等子进程写出结果，然后换 token 落库。
func watchMpayLogin(cmd *exec.Cmd, outPath, targetUserID, tmpDataDir string) {
	defer func() {
		mpayLoginTr.finish()
		mpayLoginMu.Lock()
		mpayLoginProc = nil
		mpayLoginMu.Unlock()
		_ = os.Remove(outPath)
		// 临时数据目录登录完就没用了：里面是这次会话的副本，留着只会占地方。
		// 子进程刚退出时 dl 可能还开着库文件，所以要重试几次，不能删失败就放弃。
		if tmpDataDir != "" {
			var lastErr error
			for i := 0; i < 10; i++ {
				if lastErr = os.RemoveAll(tmpDataDir); lastErr == nil {
					break
				}
				time.Sleep(300 * time.Millisecond)
			}
			if lastErr != nil {
				logf("清理临时登录目录失败（不影响登录结果）：%v", lastErr)
			}
		}
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
				mpayLoginTr.set("failed", "登录窗口已关闭，没有拿到登录结果", "", "")
				return
			}
		default:
		}
		time.Sleep(300 * time.Millisecond)
	}

	if res.Finished == 0 {
		_ = cmd.Process.Kill()
		mpayLoginTr.set("failed", "等待登录超时，请重试", "", "")
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
		mpayLoginTr.set("failed", msg, "", "")
		return
	}

	// 渠道服扫的码：不是网易账号，走渠道自己那套存
	if res.Channel != "" && res.Channel != "netease" {
		name, cerr := saveChannelScannedAccount(res)
		if cerr != nil {
			logf("渠道服扫码保存失败: %v", cerr)
			mpayLoginTr.set("failed", cerr.Error(), res.Account, "")
			return
		}
		clearAuthHint()
		mpayLoginTr.set("done", "已添加渠道账号："+name, name, "")
		return
	}

	token, userID, account, err := resolveMPayCredentials(res)
	if err != nil {
		mpayLoginTr.set("failed", err.Error(), res.Account, "")
		return
	}
	if userID == "" || token == "" {
		mpayLoginTr.set("failed", "没有拿到有效的登录凭证", res.Account, "")
		return
	}

	// 防串号：给已有账号刷新时，必须还是同一个账号
	if targetUserID != "" && targetUserID != userID {
		mpayLoginTr.set("failed",
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
		GameID:      res.GameID,
		MpayPath:    res.DLLPath,
		MpayVersion: res.DLLVersion,
		Source:      "mpaydll",
		LoginMethod: "password",
		CredState:   "ok",
		LastCheck:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	// 回调 token 若是 a50 签发的（g_i=a50），存进 A50Token——它才能换平台
	// 会话 sid → 槽位票（免渠道启动）。MpayToken 存 ma75 的，用于 create_ticket。
	// 2026-11-08 实测：无头密码登录（UILessUrsLogin）回调的就是 a50 token；
	// 官方窗登录回调的是 ma75 的，此时保留已有的 A50Token 不覆盖。
	if tokenGI(token) == profile.HostedGameID {
		acc.A50Token = token
	}
	if acc.Name == "" {
		acc.Name = userID
	}
	existedAcc, existed := findAccountByUserID(userID)
	if existed {
		old := existedAcc
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
		// a50 token 是身份级的长期凭证，别让这次（ma75 回调）把它清掉
		if acc.A50Token == "" {
			acc.A50Token = old.A50Token
		}
	}

	if err := saveAccount(acc); err != nil {
		mpayLoginTr.set("failed", "保存账号失败: "+err.Error(), account, userID)
		return
	}
	_ = setCurrent(acc.PlayerID)
	clearAuthHint()
	// 诊断开关：把这次登录回调的原始槽位落盘（setuser 注入实验的模板）。
	// 标记文件：%APPDATA%\SkyLogin\dump_slots.txt 存在才写，平时不落盘。
	if _, err := os.Stat(filepath.Join(dataDir(), "dump_slots.txt")); err == nil {
		if b, jerr := json.MarshalIndent(res.Slots, "", "  "); jerr == nil {
			p := filepath.Join(dataDir(), "netease_slots.json")
			if werr := os.WriteFile(p, b, 0o600); werr == nil {
				logf("已把登录回调槽位写到 %s（%d 个）", p, len(res.Slots))
			}
		}
	}
	logf("官方窗口登录成功，账号已保存")
	if existed {
		// 登的还是列表里已有的号：凭证更新了，但别说成"新增成功"
		logf("官方窗口登录的是已有账号，已更新凭证（不是新增）")
		mpayLoginTr.set("done", "这个账号已经在列表里了，凭证已更新", account, userID)
	} else {
		mpayLoginTr.set("done", "登录成功，已保存", account, userID)
	}
}

// resolveMPayCredentials 把子进程的结果变成 (token, userId, 账号名)。
//
// 优先用**回调里带的 user_token**（2026-10-29 抓包结论：DLL 窗口里登录
// ——包括窗口内选手机号验证码——内部走的就是 mobile/finish，回调上来的
// token 是账号级凭证，无 lt/od，长期有效）；DLL 不给 token 时才退回
// 「GetUserTicket 的票 → login/ticket」，那条路换出的是会话级 token
// （带 lt/od，会过期）。账号名单独再查一次用户资料——a50 上下文经常
// 不带 display_username，不查就只能拿 id 当别名。
func resolveMPayCredentials(res mpayhost.Result) (token, userID, account string, err error) {
	account = res.Account
	dev := res.DeviceID
	if dev == "" {
		dev, _ = ensureDeviceID()
	}

	token = slotToken(res.Slots)
	userID = slotUserID(res.Slots)
	if token != "" {
		logf("使用登录回调里的 token（%d 字符，凭证等级: %s，字段: %s）",
			len(token), tokenGrade(token), tokenKeyList(token))
	}
	// 回调里如果有 JSON 扩展信息，把**键**打出来（不打值）。
	// 扫码登录时 DLL 走的是 exchange_token，idv 那边正是从它的
	// extra_unisdk_data 里取 SAUTH 身份/EXT_ACCESS_TOKEN 来导入账号的——
	// 我们想知道回调里到底有没有这类字段。
	for i, s := range res.Slots {
		if strings.HasPrefix(strings.TrimSpace(s), "{") {
			logf("回调 slot%d 是 JSON：%s", i, jsonKeySummary(s))
		}
	}
	// 扫码回调里除了会话 token，还带一个 EXT_ACCESS_TOKEN（38 位十六进制）。
	// 实测它只有一分钟左右的命（刚登录时能查资料、能签票，下一分钟就 1306），
	// 不能当持久凭证，所以只留个诊断开关来观察它的寿命。
	if access := accessTokenFromSlots(res.Slots); access != "" && userID != "" && dev != "" && scanProbeEnabled() {
		logf("回调里带 EXT_ACCESS_TOKEN（%d 字符），后台观察寿命（不存它）", len(access))
		go watchAccessTokenLifetime(access, userID, dev)
	}

	// 回调没给 token：用 DLL 出的票换（会话级，兜底）
	if token == "" && res.Ticket != "" {
		out, e := loginTicketAuto(mpay.New(), res.Ticket, dev)
		if e != nil {
			return "", "", account, fmt.Errorf("用官方登录窗的票据换 token 失败: %w", e)
		}
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

	if token == "" {
		return "", "", account, fmt.Errorf("没有拿到 MPay 登录凭证")
	}
	if userID == "" {
		return "", "", account, fmt.Errorf("没有识别出登录的账号 id，请重试")
	}

	// 仍然没有账号名：按官方启动器的方式查一次用户资料。
	// 上下文必须跟 token 走：token 是哪个应用签的，就用那个应用的 game id 查，
	// 拿 a50 去查光遇（ma75）签的 token 会 1306，别名只能退回用 id。
	if account == "" {
		longID, shortID := ticketContextForToken(token)
		if user, e := mpay.New().GetUserInfo(token, userID, dev, longID, shortID); e == nil {
			account = mpay.DisplayNameOf(user)
		} else {
			logf("查账号显示名失败: %v", e)
		}
	}
	return token, userID, account, nil
}

// jsonKeySummary 把一段 JSON 概括成"键(类型,长度)"列表，值一概不输出。
func jsonKeySummary(raw string) string {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return "(解析失败)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s(str,%d)", k, len(v)))
		case nil:
			parts = append(parts, k+"(null)")
		default:
			parts = append(parts, fmt.Sprintf("%s(%T)", k, v))
		}
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// accessTokenFromSlots 从回调的 JSON 扩展信息里取 EXT_ACCESS_TOKEN。
//
// 结构是 slot 里一段 JSON → "extra_unisdk_data" 是一段**内层 JSON 字符串**
// → 里面 {"EXT_ACCESS_TOKEN":"yd_…"}。取不到就返回空串。
func accessTokenFromSlots(slots []string) string {
	for _, s := range slots {
		s = strings.TrimSpace(s)
		if !strings.HasPrefix(s, "{") {
			continue
		}
		var outer map[string]any
		if json.Unmarshal([]byte(s), &outer) != nil {
			continue
		}
		raw, _ := outer["extra_unisdk_data"].(string)
		if raw == "" {
			continue
		}
		var inner map[string]any
		if json.Unmarshal([]byte(raw), &inner) != nil {
			continue
		}
		if v, _ := inner["EXT_ACCESS_TOKEN"].(string); v != "" {
			return v
		}
	}
	return ""
}

// watchAccessTokenLifetime 观察 EXT_ACCESS_TOKEN 能活多久，只在失效时写一行日志。
func watchAccessTokenLifetime(access, userID, dev string) {
	longID, shortID := ticketContextForToken(access)
	for i := 1; i <= 30; i++ {
		time.Sleep(time.Minute)
		if _, err := mpay.New().GetUserInfo(access, userID, dev, longID, shortID); err != nil {
			logf("[寿命] EXT_ACCESS_TOKEN 第 %d 分钟已失效（%v）", i, err)
			return
		}
	}
	logf("[寿命] EXT_ACCESS_TOKEN 30 分钟后仍可用（观察结束）")
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
		longID, shortID := ticketContextForToken(a.MpayToken)
		user, err := mpay.New().GetUserInfo(a.MpayToken, a.UserID, a.DeviceID, longID, shortID)
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
		restoreAllShims(sky, root)
		fever.RemoveMirrors(sky, root)
		return
	}
	logf("启动清理：还有光遇在运行，等它退出后再清镜像目录")
	go func() {
		for i := 0; i < 1440; i++ { // 最多盯 2 小时
			time.Sleep(5 * time.Second)
			if fever.CountProcess("sky.exe") == 0 {
				time.Sleep(5 * time.Second) // 给游戏收尾留点时间
				restoreAllShims(sky, root)
				fever.RemoveMirrors(sky, root)
				if fever.MirrorsRemaining(sky, root) == 0 {
					logf("启动清理：光遇已退出，上次的镜像目录已清理")
				}
				return
			}
		}
	}()
}
