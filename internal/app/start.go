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
	if acc.Channel != "netease" {
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
				"登录信息已过期，请在账号菜单里刷新凭证", acc.Username)
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
	createdAt  time.Time
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
	for i := range picked {
		if !canUseNetEase(picked[i]) {
			if refreshed, rerr := refreshFn(picked[i]); rerr == nil {
				picked[i] = refreshed
			} else {
				logf("多开：华为账号 %s 刷新凭证失败，用缓存: %v", picked[i].NickName, rerr)
			}
		}
	}

	if err := ensureHostLocked(); err != nil {
		return "", err
	}

	// 先把所有安装目录准备好（第一个用原目录，其余各建一套镜像），
	// 再几乎同时把游戏拉起来：建目录要几十毫秒，夹在启动之间会让
	// 第一个客户端的窗口先弹出来，后面的就被单实例检测拦下。
	installs := make([]string, len(picked))
	for i := range picked {
		if i == 0 {
			installs[i] = exe
			continue
		}
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
	return fmt.Sprintf("已启动 %d 个客户端：%s", len(names), strings.Join(names, "、")), nil
}

// replyCountRunning 是多开用的辅助函数，暂时只做日志用途。
func replyCountRunning() int { return 0 }

// mintTicket 给指定账号现签一张一次性票据。
//
// 单客户端和多开共用：每次游戏来取票都调这里，
// 保证不会把已经消费掉的票重复发出去。
func mintTicket(acc Account, dev *device.ClientProfile) (string, string, error) {
	if canUseNetEase(acc) {
		longID, shortID := ticketContextForToken(acc.MpayToken)
		// 签票失败可能只是网络抖动，重试 2 次再判定失效；
		// 连续失败就标记 expired，界面会提示用户刷新凭证。
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			t, _, err := mpay.New().CreateTicketFor(acc.MpayToken, acc.UserID, acc.DeviceID, longID, shortID)
			if err == nil {
				return t, "netease", nil
			}
			lastErr = err
			logf("签票失败（第 %d 次）: %v", attempt+1, err)
			if attempt < 2 {
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

// ticketContextForToken 根据 token 里绑定的应用，决定用哪个游戏上下文签票。
//
// MPay token 是 base64 的 JSON（前缀 1-），其中 g_i 是签发它的应用 id：
//
//	aecfs5n2y4aaaakd → 光遇（扫码登录拿到的）
//	aecglf6ee4aaaarz → 发烧平台 a50（网页登录拿到的）
//
// 用错上下文会返回 1306 用户登录失效。
func ticketContextForToken(token string) (longID, shortID string) {
	const (
		gameLong  = "aecfs5n2y4aaaakd-g-ma75"
		gameShort = "ma75"
		a50Long   = "aecglf6ee4aaaarz-g-a50"
		a50Short  = "a50"
	)
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
