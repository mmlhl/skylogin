package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"skylogin/internal/channel"
	"skylogin/internal/device"
	"skylogin/internal/fever"
	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

var (
	hostMu sync.Mutex
	host   *fever.Host
)

// 下面这些变量可以被测试替换，方便在没有网络、没有真游戏的环境里做端到端验证。
var (
	launcherPIDFn   = fever.LauncherPID
	killLauncherFn  = fever.KillLauncher
	findSkyFn       = fever.FindSky
	startSkyFn      = fever.StartSkyAuto
	startSkyPidFn   = func(exe string) (*os.Process, error) { return fever.StartSky(exe) }
	refreshFn       = refreshAccount
	newTicketSource = func() ticketSource { return hostedTicket{} }
	buildPayloadFn  = buildPayload
	hostClassFn     = func() string { return fever.DefaultClassName }
)

// ticketSource 抽象"取一次性票据"，便于测试注入。
type ticketSource interface {
	Login(payload map[string]string) (string, map[string]any, error)
}

// launchGame 是界面上「启动光遇」按钮的完整流程：
// 刷新凭证 → 取一次性票据 → 关闭发烧平台 → 冒充它 → 启动游戏。
func launchGame() (string, error) {
	acc, err := pickAccount()
	if err != nil {
		return "", err
	}
	// 游戏是全局单实例：已经有客户端在跑时，单开必然被拦，直接说清楚
	if n := liveClients(); n > 0 {
		return "", fmt.Errorf("已经有 %d 个客户端在运行。游戏只能同时启动一次，请先点「停止运行」", n)
	}
	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		return "", err
	}

	hostMu.Lock()
	defer hostMu.Unlock()

	logf("账号: %s (玩家ID %s)", acc.NickName, acc.PlayerID)

	// 已经有一个客户端在跑时，不要动它的宿主窗口
	firstClient := host == nil
	if acc.Channel == "bilibili" || acc.Channel == "m4399" || acc.Channel == "vivo" {
		// 渠道账号：启动前做凭证保活（能静默续期就续期，全失败才拦下）
		if err := ensureChannelCredential(&acc); err != nil {
			return "", err
		}
		logf("%s账号 %s：凭证就绪", channel.LabelOf(acc.Channel), acc.Username)
	} else if acc.Channel != "netease" {
		// 华为账号才需要刷新 ST / 生成登录包；网易账号走 token 现签票
		if refreshed, rerr := refreshFn(acc); rerr == nil {
			acc = refreshed
			logf("凭证已刷新")
		} else {
			logf("凭证刷新失败，继续使用本地缓存: %v", rerr)
		}
	}

	if _, ok := launcherPIDFn(); ok && firstClient {
		if !loadConfig().CloseLauncherEnabled() {
			logf("发烧平台正在运行，但设置里关掉了「启动前关闭」，可能抢不到 IPC 窗口")
		} else {
			logf("检测到发烧平台占用 IPC 窗口，正在关闭…")
			if err := killLauncherFn(); err != nil {
				return "", err
			}
			logf("发烧平台已关闭")
		}
	}

	// 网易账号不需要华为那套登录包（没有 ST 也没关系），直接跳到发票
	var payload struct{ Body map[string]string }
	if acc.Channel != "netease" {
		p, _, perr := buildPayloadFn(acc, dev)
		// vivo 的 openToken 很短命（实测一小时内就失效），uni_sauth 会拒绝：
		// 这不是"凭证过期"，是设计如此——参考客户端每次启动前都换新票。
		// 用 Cookie 会话静默换一张新票再试一次；Cookie 也失效了才会弹登录页。
		if perr != nil && acc.Channel == channel.KeyVivo {
			if rerr := refreshVivoCredential(&acc); rerr != nil {
				expireChannelCredential(&acc, "vivo登录已失效", "vivo_expired", "vivo登录已过期", "请重新登录该账号")
				return "", rerr
			}
			p, _, perr = buildPayloadFn(acc, dev)
		}
		if perr != nil {
			return "", fmt.Errorf("生成登录包失败: %w", perr)
		}
		payload.Body = p.Body
	}

	exe := resolveSkyPath()
	if exe == "" {
		return "", fmt.Errorf("没有找到光遇主程序，请在设置里手动选择 sky.exe")
	}
	if _, serr := os.Stat(exe); serr != nil {
		return "", fmt.Errorf("找不到光遇主程序: %s（可在 %s 里填写 sky_exe）",
			exe, filepath.Join(dataDir(), "config.json"))
	}

	// 先看有没有"手机扫码"留下的票（一次性，用完即弃）
	var ticket, channel string
	var ticketErr error
	if code, ch, ok := takePending(); ok {
		ticket, channel = code, ch
		logf("使用手机扫码的票据（channel=%s），本次启动不再向华为取票", ch)
	} else if canUseNetEase(acc) {
		// 网易账号：用存下来的 token 现签一张票，不用再登录。
		// 统一走 mintTicket：里面带重试（2 次）、按 token 的应用选上下文、
		// 连续失败会把这个账号标记成 expired，界面就能提示刷新凭证。
		logf("网易账号 %s：正在现签票据…", acc.Username)
		t, ch, terr := mintTicket(acc, dev)
		if terr != nil {
			logf("现签票据失败（已重试）: %v", terr)
			setAuthHint("netease_expired", "网易账号凭证已过期",
				"登录信息已过期，请在账号菜单里刷新凭证", acc.Username, acc.UserID)
			// 没有票就别启动游戏了，否则玩家只会看到一次失败
			return "", fmt.Errorf("网易账号凭证已过期，请在账号菜单里点「刷新凭证」")
		}
		ticket, channel = t, ch
		logf("已拿到票据（channel=%s，网易账号 %s）", ch, acc.Username)
		refreshNetEaseTicketTime(acc)
		// 拿票成功说明凭证有效，把之前可能的 expired 标记清掉
		setAccountCredState(acc.UserID, "ok", "")
		clearAuthHint()
	} else {
		logf("正在取一次性票据…")
		ticket, _, ticketErr = newTicketSource().Login(payload.Body)
		if ticketErr != nil {
			logf("取票失败: %v（仍会启动游戏，但可能需要手动登录）", ticketErr)
		} else {
			channel = profile.HuaweiChannel
			logf("已拿到票据（channel=%s）", channel)
		}
	}
	if channel == "" {
		channel = profile.HuaweiChannel
	}

	// 宿主窗口常驻：已经有客户端在跑时不能重建，否则会抢掉它的取票窗口。
	if err := ensureHostLocked(); err != nil {
		return "", err
	}

	msg, err := startClient(acc, dev, ticket, channel)
	if err != nil {
		return "", err
	}
	if ticketErr != nil {
		return "已启动光遇，但票据获取失败，可能需要手动登录", nil
	}
	return msg, nil
}

// resolveSkyPath 返回可用的 sky.exe 路径：配置优先，失效则自动检测。
func resolveSkyPath() string {
	if p := loadConfig().SkyExe; fileExists(p) {
		return p
	}
	if p, err := findSkyFn(); err == nil {
		return p
	}
	return ""
}

// canUseNetEase 判断当前账号能不能直接现签票据。
func canUseNetEase(a Account) bool {
	return a.Channel == "netease" && a.MpayToken != "" && a.UserID != ""
}

// refreshVivoCredential 用 vivo 的 Cookie 会话静默换一张新 openToken。
//
// 做法：重开该小号的登录窗。Cookie 会话还活着时，页面脚本会自动完成
// 取号 + 换票（几秒钟，窗口自己关闭）；Cookie 也失效了才需要用户在
// 窗口里重新登录。拿到新凭证后刷进 acc，启动流程接着走。
func refreshVivoCredential(acc *Account) error {
	logf("vivo账号 %s：openToken 已失效，用 Cookie 会话静默续期（窗口不可见）…", displayName(*acc))
	if err := StartChannelWebLoginHidden(channel.KeyVivo, acc.PlayerID); err != nil {
		return fmt.Errorf("vivo 静默续期没能启动（%s）", err.Error())
	}

	// 等登录流程出结果：静默时几秒钟；要重新登录时给足 3 分钟
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		st := ChannelLoginStatus(channel.KeyVivo)
		if st["phase"] == "done" {
			// 新凭证已由落库流程写进账号文件，刷回来接着用
			list, err := loadAccounts()
			if err != nil {
				return fmt.Errorf("vivo 续期完成但读账号失败")
			}
			for _, a := range list {
				if a.Channel == channel.KeyVivo && a.PlayerID == acc.PlayerID {
					*acc = a
					logf("vivo账号 %s：已续期", displayName(*acc))
					return nil
				}
			}
			return fmt.Errorf("vivo 续期完成但没找到账号数据")
		}
		if st["phase"] == "failed" {
			msg, _ := st["message"].(string)
			if msg == "" {
				msg = "未知原因"
			}
			return fmt.Errorf("vivo 静默续期失败（%s），请在窗口里重新登录", msg)
		}
	}
	return fmt.Errorf("vivo 静默续期超时，请重新登录该账号")
}

// refreshNetEaseTicketTime 记一下发票时间（失败不影响启动）。
func refreshNetEaseTicketTime(a Account) {
	a.LastTicket = time.Now().Unix()
	if err := saveAccount(a); err != nil {
		logf("更新账号时间失败: %v", err)
	}
}

// ── 多开 ──────────────────────────────────────────────────────────
//
// 光遇本身有单实例限制，但那是"启动时检查再创建互斥对象"的写法，
// 两个进程几乎同时启动时都能通过检查（实测：同时 Start 两次，两个都活着）。
//
// 所以多开的关键不是绕过游戏，而是：
//   1. 把 N 个 sky.exe 在极短时间内依次拉起
//   2. 每个客户端都会向平台窗口发 op13 取票，请求里带自己的 ProcessID
//   3. 我们按 ProcessID 把"这个客户端该用哪个账号的票"发回去
//
// 这也是为什么必须用 pid 路由：默认为第一个客户端发第一张票，
// 第二个客户端就会拿到别人的票，直接登录失败。

// multiSlot 一个待启动的客户端：账号 + 预先取好的票据
type multiSlot struct {
	acc        *Account
	dev        *device.ClientProfile
	ticket     string
	channel    string
	pid        uint32
	seq        int64 // 启动顺序（窗口排列按这个顺序铺格子）
	createdAt  time.Time
	firstReply time.Time // 本轮应答窗口起点（超过 replyWindow 重新计数）
	lastTicket time.Time
}

// launchMany 同时启动多个客户端，每个账号一个。
//
// ids 为空时默认取前两个可用账号。返回给界面的文案说明实际起了几个。
func launchMany(ids []string) (string, error) {
	hostMu.Lock()
	defer hostMu.Unlock()

	if n := liveClients(); n > 0 {
		return "", fmt.Errorf("已经有 %d 个客户端在运行。游戏只能同时启动一次：请先全部停止，再用多开一起启动", n)
	}
	all, aerr := loadAccounts()
	if aerr != nil || len(all) == 0 {
		return "", fmt.Errorf("还没有已保存的账号")
	}
	if len(ids) == 0 {
		for i := range all {
			ids = append(ids, all[i].PlayerID)
			if len(ids) >= 2 {
				break
			}
		}
	}
	if len(ids) < 2 {
		return "", fmt.Errorf("至少选两个账号才能多开")
	}

	var picked []Account
	for _, id := range ids {
		for i := range all {
			if all[i].PlayerID == id {
				picked = append(picked, all[i])
				break
			}
		}
	}
	if len(picked) < 2 {
		return "", fmt.Errorf("选中的账号不足两个")
	}
	for _, a := range picked {
		if accountRunning(a.PlayerID) {
			return "", fmt.Errorf("账号 %s 已经在运行了，请先停止它再开多开", a.NickName)
		}
	}

	exe := resolveSkyPath()
	if exe == "" {
		return "", fmt.Errorf("没有找到光遇主程序，请在设置里手动选择 sky.exe")
	}
	if _, serr := os.Stat(exe); serr != nil {
		return "", fmt.Errorf("找不到光遇主程序: %s", exe)
	}

	// 发烧平台会抢 IPC 窗口；只在还没有自己的宿主窗口时去关（否则会误杀自己的窗口）
	if _, ok := launcherPIDFn(); ok && host == nil && loadConfig().CloseLauncherEnabled() {
		logf("检测到发烧平台占用 IPC 窗口，正在关闭…")
		if err := killLauncherFn(); err != nil {
			return "", err
		}
	}

	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		return "", derr
	}
	// 逐个验证 / 刷新凭证：网易账号先验 token 活性（失效的签出的是废票，
	// 游戏里只会看到网络错误），华为账号刷新 ST
	alive := picked[:0]
	for _, a := range picked {
		if a.Channel == channel.KeyBili || a.Channel == channel.KeyM4399 || a.Channel == channel.KeyVivo {
			// 渠道账号：凭证保活，失败的这个多开就不带
			if err := ensureChannelCredential(&a); err != nil {
				logf("多开：%s 的凭证校验未通过，跳过（%v）", displayName(a), err)
				continue
			}
		} else if canUseNetEase(a) {
			if verr := validateNetEaseToken(a); verr != nil {
				setAccountCredState(a.UserID, "expired", verr.Error())
				logf("多开：%s 凭证已失效，跳过（%v）", displayName(a), verr)
				continue
			}
		} else {
			if refreshed, rerr := refreshFn(a); rerr == nil {
				a = refreshed
			} else {
				logf("多开：华为账号 %s 刷新凭证失败，用缓存: %v", a.NickName, rerr)
			}
		}
		alive = append(alive, a)
	}
	if len(alive) < 2 {
		return "", fmt.Errorf("凭证有效的账号不足两个，无法多开（失效的请先刷新凭证）")
	}
	picked = alive

	if err := ensureHostLocked(); err != nil {
		return "", err
	}

	// 先把所有安装目录准备好（每个客户端各一套独立镜像，第一个也用镜像：
	// 这样启动前预写的窗口模式 / 画质 / 帧率才能覆盖到所有客户端），
	// 再几乎同时把游戏拉起来：建目录要几十毫秒，夹在启动之间会让
	// 第一个客户端的窗口先弹出来，后面的就被单实例检测拦下。
	installs := make([]string, len(picked))
	for i := range picked {
		m, err := prepareMirror(exe)
		if err != nil {
			logf("多开：准备镜像失败: %v", err)
			return "", fmt.Errorf("准备镜像目录失败: %w", err)
		}
		installs[i] = m
	}

	type launchResult struct {
		name string
		err  error
	}
	out := make(chan launchResult, len(picked))
	var wg sync.WaitGroup
	for i, acc := range picked {
		wg.Add(1)
		go func(acc Account, path string) {
			defer wg.Done()
			_, err := launchAt(acc, dev, "", "", path)
			out <- launchResult{name: displayName(acc), err: err}
		}(acc, installs[i])
	}
	wg.Wait()
	close(out)

	names := make([]string, 0, len(picked))
	for r := range out {
		if r.err != nil {
			logf("多开：启动 %s 失败: %v", r.name, r.err)
			continue
		}
		names = append(names, r.name)
	}
	if len(names) < 2 {
		return "", fmt.Errorf("可用账号不足两个（启动失败）")
	}
	ArmMouseArbiter()
	return fmt.Sprintf("已启动 %d 个客户端：%s", len(names), strings.Join(names, "、")), nil
}

// launchManyLayout 多开。display = "fullscreen" 预写全屏、不做窗口排列，
// 窗口出来后自动开监控墙；"cascade" 窗口级联堆叠（文件窗式错开）；
// 其余（"tile"/空）窗口模式 + 渐进式网格平铺，cols/rows 生效；
// w/h > 0 为自定义窗口尺寸（0 = 铺满格子 / 引擎最小尺寸）。
func launchManyLayout(ids []string, cols, rows, w, h int, display string) (string, error) {
	fullscreen := display == "fullscreen"
	nextFullscreen.Store(fullscreen)
	defer nextFullscreen.Store(false)
	msg, err := launchMany(ids)
	if err != nil {
		return msg, err
	}
	switch {
	case fullscreen:
		arrangeWhenReady(0, 0, 0, 0, "fullscreen")
		return msg + "（全屏：用监控墙或 Alt+Tab 切换）", nil
	case display == "cascade":
		arrangeWhenReady(cols, rows, w, h, "cascade")
		return msg + "（窗口堆叠：每个都露出标题条，点标题切换）", nil
	}
	if cols >= 1 && rows >= 1 {
		arrangeWhenReady(cols, rows, w, h, "tile")
		msg = fmt.Sprintf("%s（将按 %d×%d 平铺）", msg, cols, rows)
	}
	return msg, nil
}

// validateNetEaseToken 用 MPay 的用户资料接口验证 token 是否还活着。
//
// 为什么要这一步：失效的 token 有时也能"成功签出票"（create_ticket 对
// 会话校验很松），游戏拿到才发现是废票——玩家看到的就是游戏里一直
// "网络错误"，工具侧毫无感知（199****3859 那个问题）。用户资料接口是
// 真实验证：token 死了必然报错。两个上下文都试，任一成功即认为活着。
func validateNetEaseToken(acc Account) error {
	c := mpay.New()
	ownLong, ownShort := ticketContextForToken(acc.MpayToken)
	for _, ctx := range []struct{ long, short string }{{ownLong, ownShort}, {a50Long, a50Short}} {
		if _, err := c.GetUserInfo(acc.MpayToken, acc.UserID, acc.DeviceID, ctx.long, ctx.short); err == nil {
			return nil
		}
	}
	return fmt.Errorf("token 已失效（用户资料查询失败）")
}

// mintTicket 给指定账号现签一张一次性票据。
//
// 单客户端和多开共用：每次游戏来取票都调这里，
// 保证不会把已经消费掉的票重复发出去。
//
// 签票上下文：PC 版光遇以发烧平台（a50）的票入场。实测用游戏上下文
// （ma75，扫码 / 短信登录拿到的 token 所属应用）签出的票 MPay 不报错，
// 但游戏服务端不认——进游戏一直"网络错误"，就是 199****3859 那个问题。
// 所以固定优先 a50 上下文，失败再退回 token 自带的上下文，
// 两条路都断才标记凭证过期（界面上会出现刷新入口）。
func mintTicket(acc Account, dev *device.ClientProfile) (string, string, error) {
	if canUseNetEase(acc) {
		// 先验证 token 活性：失效的 token 也能签出废票，等游戏里报
		// "网络错误"才知道就太晚了。验证失败当场拦下并推刷新提示。
		if verr := validateNetEaseToken(acc); verr != nil {
			setAccountCredState(acc.UserID, "expired", verr.Error())
			setAuthHint("netease_expired", "网易账号凭证已过期",
				fmt.Sprintf("%s 的凭证已失效，请刷新凭证", displayName(acc)),
				acc.Username, acc.UserID)
			return "", "", fmt.Errorf("网易账号 %s 凭证已失效，请刷新凭证", displayName(acc))
		}
		ownLong, ownShort := ticketContextForToken(acc.MpayToken)
		// a50 上下文要用 a50 登记过的设备号：token 不是 a50 原生时
		// （扫码 / 短信），acc.DeviceID 属于 ma75，a50 不认识
		a50Dev := acc.DeviceID
		if ownLong != a50Long {
			if d, derr := ensureA50DeviceID(); derr == nil {
				a50Dev = d
			} else {
				logf("a50 设备登记失败，退回本账号设备号: %v", derr)
			}
		}
		contexts := []struct {
			long, short, name, device string
		}{
			{a50Long, a50Short, "a50", a50Dev},
			{ownLong, ownShort, "token 自带", acc.DeviceID},
		}
		var lastErr error
		tried := 0
		for _, ctx := range contexts {
			for attempt := 0; attempt < 2; attempt++ {
				t, _, err := mpay.New().CreateTicketFor(acc.MpayToken, acc.UserID, ctx.device, ctx.long, ctx.short)
				if err == nil {
					if tried > 0 {
						logf("网易账号 %s：改用 %s 上下文签票成功", acc.Username, ctx.name)
					}
					return t, "netease", nil
				}
				tried++
				lastErr = err
				logf("签票失败（%s 上下文，第 %d 次）: %v", ctx.name, attempt+1, err)
				time.Sleep(700 * time.Millisecond)
			}
		}
		setAccountCredState(acc.UserID, "expired", lastErr.Error())
		return "", "", lastErr
	}
	p, _, err := buildPayloadFn(acc, dev)
	if err != nil {
		return "", "", err
	}
	t, _, err := newTicketSource().Login(p.Body)
	if err != nil {
		return "", "", err
	}
	return t, profile.HuaweiChannel, nil
}

// maxReplyPerClient 每个客户端最多应答几次。
//
// 票是一次性的，正常情况下第一次就该成功；多余的重试基本注定失败，
// 继续回票只会让游戏空转（登录界面明显变卡），所以到次数就停手。
const maxReplyPerClient = 3

// pidCounter 按进程统计应答次数。
type pidCounter struct {
	mu sync.Mutex
	m  map[uint32]int
}

func (c *pidCounter) Add(pid uint32, d int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[pid] += d
	return c.m[pid]
}

// Reset 清零某个 pid 的应答计数（应答窗口过期时调用）。
func (c *pidCounter) Reset(pid uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, pid)
}

// 签票上下文（MPay 的 long/short game id）。
//
// a50 = 发烧平台应用。PC 版光遇按"平台发的票"校验，所以优先用它；
// ma75 = 光遇游戏应用，扫码 / 短信登录拿到的 token 属于它。
const (
	a50Long   = "aecglf6ee4aaaarz-g-a50"
	a50Short  = "a50"
	gameLong  = "aecfs5n2y4aaaakd-g-ma75"
	gameShort = "ma75"
)

// ticketContextForToken 根据 token 里绑定的应用，返回它所属的签票上下文。
//
// MPay token 是 base64 的 JSON（前缀 1-），其中 g_i 是签发它的应用 id。
// 认不出来时按光遇处理（扫码登录的老路径）。注意 mintTicket 会优先尝试
// a50，这里返回的只是回退项。
func ticketContextForToken(token string) (longID, shortID string) {
	if strings.HasPrefix(token, "1-") {
		raw := strings.TrimPrefix(token, "1-")
		if pad := len(raw) % 4; pad != 0 {
			raw += strings.Repeat("=", 4-pad)
		}
		if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
			var payload struct {
				Game string `json:"g_i"`
			}
			if json.Unmarshal(b, &payload) == nil {
				switch payload.Game {
				case "aecglf6ee4aaaarz":
					return a50Long, a50Short
				case "aecfs5n2y4aaaakd":
					return gameLong, gameShort
				}
			}
		}
	}
	// 认不出来时按光遇处理（扫码登录的老路径）
	return gameLong, gameShort
}
