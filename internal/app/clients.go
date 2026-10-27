package app

// 客户端路由表：每个启动的游戏实例一条路由，按 pid 发票。
//
// 设计目标（这一版的重点）：
//   - 一个实例一条路由，互不影响：已经有客户端在玩时，切换另一个账号还能继续启动
//   - 登录成功后路由可以回收（省内存），不必一直挂着
//   - 每条路由都有超时（3 分钟），到点自动回收
//   - 全部客户端退出后，宿主窗口也一起关掉

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"

	"skylogin/internal/channel"
	"skylogin/internal/device"
	"skylogin/internal/fever"
	"skylogin/internal/prefs"
)

const (
	// replyWindow 应答次数的计算窗口：卡登录时连续重试有上限，
	// 但在网络错误页停留很久再点重试时重新计数，路由不会被次数卡死。
	replyWindow = 10 * time.Minute
	// routeSweep 多久检查一次
	routeSweep = 5 * time.Second
)

var (
	// mirrorSeq 本次会话建过几套镜像（只用来区分目录名）
	mirrorSeq int
	// launchSeq 客户端启动顺序号（窗口排列按它铺格子）
	launchSeq int64
	clientsMu sync.Mutex
	// clients: 游戏 pid → 路由
	clients = map[uint32]*multiSlot{}
	// fellback 只有"提权启动拿不到 pid"这类情况才用；正常按 pid 精确路由
	fellback *multiSlot
	// attempts: 每个 pid 已经应答过几次
	attempts = &pidCounter{m: map[uint32]int{}}
	// runningByAccount: 账号 → 它当前运行中的游戏 pid（UI 用，路由回收后仍保留）
	runningByAccount = map[string]uint32{}
	// janitorOn 避免重复起清理协程
	janitorOn bool
)

// ensureHostLocked 保证宿主窗口存在。调用方必须持有 hostMu。
func ensureHostLocked() error {
	if host != nil {
		return nil
	}
	var h *fever.Host
	h = fever.NewHostWithClass(hostClassFn(), func(req fever.TicketRequest) bool {
		return handleTicket(h, req)
	})
	if err := h.Start(); err != nil {
		return err
	}
	host = h
	logf("已冒充发烧平台（IPC 就绪）")

	clientsMu.Lock()
	startJanitor := !janitorOn
	janitorOn = true
	clientsMu.Unlock()
	if startJanitor {
		go routeJanitor()
	}
	return nil
}

// handleTicket 收到某个游戏实例的 op13：只给这个 pid 对应的账号发票。
func handleTicket(h *fever.Host, req fever.TicketRequest) bool {
	clientsMu.Lock()
	slot := clients[req.ProcessID]
	if slot == nil {
		slot = fellback
	}
	clientsMu.Unlock()
	if slot == nil {
		// 路由不在：只有客户端已退出才会回收。此时再来的取票请求
		// 大概率是之前登录失败的残留重试，回票也没有意义。
		logf("收到已回收路由的取票请求 pid=%d，忽略（该客户端此前未能登录成功）", req.ProcessID)
		return true
	}

	// 应答次数按窗口重置：错误页停留很久后点重试，应该能拿到新签的票
	if now := time.Now(); slot.firstReply.IsZero() || now.Sub(slot.firstReply) > replyWindow {
		slot.firstReply = now
		attempts.Reset(req.ProcessID)
	}

	n := attempts.Add(req.ProcessID, 1)
	if n > maxReplyPerClient {
		if n == maxReplyPerClient+1 {
			logf("%s（pid=%d）已应答 %d 次仍未成功，暂停应答（%v 后自动恢复）",
				displayName(*slot.acc), req.ProcessID, n, replyWindow)
			// 票据发了几次游戏都没登录成功：签票接口没报错，但凭证大概率
			// 已经失效（过期 / 被平台作废）。把它标记成待验证并推提示，
			// 别让这个问题只躺在游戏里——玩家看到的只是"网络错误"。
			setAccountCredState(slot.acc.UserID, "stale", "多次发票游戏都没能登录成功，凭证可能已失效")
			setAuthHint("login_stuck", "登录未完成",
				fmt.Sprintf("%s 多次发票都没能登录成功，凭证可能已失效，请刷新凭证", displayName(*slot.acc)),
				slot.acc.Username, slot.acc.UserID)
		}
		return true
	}
	// Identifier 是游戏给这次登录生成的一次性句柄：重试同一登录时不变，
	// 重新发起登录时会换。打出来方便排查"反复要票"到底是哪种情况。
	logf("收到游戏取票请求 #%d: pid=%d（%s，id=%x…）", n, req.ProcessID, displayName(*slot.acc), req.Identifier[:6])

	go func() {
		clientsMu.Lock()
		t, ch := slot.ticket, slot.channel
		slot.ticket = "" // 票是一次性的
		clientsMu.Unlock()

		if t == "" {
			var err error
			t, ch, err = mintTicket(*slot.acc, slot.dev)
			if err != nil {
				switch slot.acc.Channel {
				case channel.KeyVivo:
					// vivo 的 openToken 是短命票，uni_sauth 到取票这一步才会拒。
					// 用 Cookie 会话静默换一张新票后立刻重签；Cookie 也失效才
					// 需要重新登录。
					if rerr := refreshVivoCredential(slot.acc); rerr != nil {
						err = rerr
						setAccountCredState(slot.acc.UserID, "expired", "vivo登录已失效")
						setAuthHint("vivo_expired", "vivo登录已过期",
							fmt.Sprintf("%s 的 vivo 登录已失效，请重新登录该账号", displayName(*slot.acc)),
							slot.acc.Username, slot.acc.UserID)
					} else {
						t, ch, err = mintTicket(*slot.acc, slot.dev)
					}
				case channel.KeyM4399:
					// 4399 同病：oauth-check 说凭证活着，uni_sauth 却拒——
					// 跑一遍静默恢复链（check → refresh_token 轮换 → state
					// 兜底）再重签；链路全断才需要重新登录。
					if rerr := ensureM4399Credential(slot.acc); rerr != nil {
						err = rerr
						setAccountCredState(slot.acc.UserID, "expired", "4399登录已失效")
						setAuthHint("m4399_expired", "4399登录已过期",
							fmt.Sprintf("%s 的 4399 登录已失效，请重新登录该账号", displayName(*slot.acc)),
							slot.acc.Username, slot.acc.UserID)
					} else {
						t, ch, err = mintTicket(*slot.acc, slot.dev)
					}
				}
			}
			if err != nil {
				logf("现签票据失败（%s）: %v", displayName(*slot.acc), err)
				return
			}
		}
		if err := h.SendTicket(windows.Handle(req.SenderHwnd), ch, t); err != nil {
			logf("发票据失败: %v", err)
			return
		}
		clientsMu.Lock()
		slot.lastTicket = time.Now()
		clientsMu.Unlock()
		logf("票据已发送（%s），等待游戏进入", displayName(*slot.acc))
	}()
	return true
}

// registerClient 把刚启动的游戏进程登记到路由表里。
func registerClient(pid uint32, slot *multiSlot) {
	clientsMu.Lock()
	slot.pid = pid
	slot.seq = launchSeq
	launchSeq++
	slot.createdAt = time.Now()
	clients[pid] = slot
	runningByAccount[slot.acc.PlayerID] = pid
	if len(clients) == 1 {
		fellback = slot // 单实例时兜底（有些启动方式拿不到 pid）
	}
	clientsMu.Unlock()
	logf("路由已建立: %s（pid=%d）", displayName(*slot.acc), pid)
}

// routeJanitor 定期清理路由。
//
// 路由跟客户端同生命周期：进程活着就保留。之前"60 秒没来要票就当登录
// 完成回收"有两个问题——"没来要票"不代表登录成功（票据被服务端拒绝时，
// 游戏卡在错误页，60 秒后路由没了，用户点重试就直接被忽略），
// "已登录完成"那行日志也误导过排查。现在只在客户端退出时回收。
func routeJanitor() {
	for {
		time.Sleep(routeSweep)

		hostMu.Lock()
		if host == nil {
			clientsMu.Lock()
			janitorOn = false
			clientsMu.Unlock()
			hostMu.Unlock()
			return
		}

		clientsMu.Lock()
		for pid, slot := range clients {
			if fever.ProcessAlive(pid) {
				continue
			}
			logf("路由回收: %s（pid=%d）客户端已退出", displayName(*slot.acc), pid)
			delete(clients, pid)
			if fellback == slot {
				fellback = nil
			}
		}
		// 清掉已经退出的进程的"运行中"标记
		for accID, pid := range runningByAccount {
			if !fever.ProcessAlive(pid) {
				delete(runningByAccount, accID)
			}
		}
		empty := len(clients) == 0
		anyAlive := len(runningByAccount) > 0
		clientsMu.Unlock()

		// 客户端都退了：宿主窗口也关掉，彻底释放
		if empty && !anyAlive {
			host.Stop()
			host = nil
			clientsMu.Lock()
			janitorOn = false
			clientsMu.Unlock()
			logf("客户端已全部退出，已关闭路由")
			hostMu.Unlock()
			return
		}
		hostMu.Unlock()
	}
}

// accountRunning 返回某个账号当前是否有游戏在运行（用于界面按账号显示状态）。
func accountRunning(id string) bool {
	clientsMu.Lock()
	pid, ok := runningByAccount[id]
	clientsMu.Unlock()
	return ok && pid != 0 && fever.ProcessAlive(pid)
}

// liveClients 数一下还有几个客户端活着。
func liveClients() int {
	clientsMu.Lock()
	pids := make([]uint32, 0, len(runningByAccount))
	for _, pid := range runningByAccount {
		if pid != 0 {
			pids = append(pids, pid)
		}
	}
	clientsMu.Unlock()

	n := 0
	for _, pid := range pids {
		if fever.ProcessAlive(pid) {
			n++
		}
	}
	return n
}

// startClient 启动一个客户端并登记路由（单开用）。
func startClient(acc Account, dev *device.ClientProfile, ticket, channel string) (string, error) {
	exe := resolveSkyPath()
	if exe == "" {
		return "", fmt.Errorf("没有找到光遇主程序，请在设置里手动选择 sky.exe")
	}
	if accountRunning(acc.PlayerID) {
		return "", fmt.Errorf("这个账号已经在运行了，不能重复启动")
	}
	launchExe, err := chooseInstall(exe)
	if err != nil {
		return "", err
	}
	return launchAt(acc, dev, ticket, channel, launchExe)
}

// launchAt 用指定目录启动并登记（多开时目录已提前建好，这里只管拉进程）。
func launchAt(acc Account, dev *device.ClientProfile, ticket, channel, launchExe string) (string, error) {
	p, err := startSkyPidFn(launchExe)
	if err != nil {
		// 直接启动失败（通常是权限），退回提权启动；这种方式拿不到 pid
		if elevated, e2 := startSkyFn(launchExe); e2 == nil {
			slot := &multiSlot{acc: &acc, dev: dev, ticket: ticket, channel: channel, createdAt: time.Now()}
			clientsMu.Lock()
			fellback = slot
			clientsMu.Unlock()
			if elevated {
				logf("光遇需要管理员权限，已通过提权方式启动")
			}
			return fmt.Sprintf("已启动光遇\n游戏路径: %s", launchExe), nil
		}
		return "", fmt.Errorf("启动光遇失败: %w", err)
	}

	registerClient(uint32(p.Pid), &multiSlot{acc: &acc, dev: dev, ticket: ticket, channel: channel})
	logf("光遇已启动: %s（pid=%d）", launchExe, p.Pid)
	if exe := resolveSkyPath(); exe != "" {
		go watchMirrors(exe)
	}
	go func(pid uint32, accID, name string) {
		// 有的游戏会用"已有实例"检测直接退出，这里把这种情况记下来
		time.Sleep(8 * time.Second)
		if !fever.ProcessAlive(pid) {
			logf("客户端 %s（pid=%d）启动后很快退出：可能被游戏的单实例检测拦下了", name, pid)
			clientsMu.Lock()
			delete(clients, pid)
			if cur, ok := runningByAccount[accID]; ok && cur == pid {
				delete(runningByAccount, accID)
			}
			if fellback != nil && fellback.pid == pid {
				fellback = nil
			}
			clientsMu.Unlock()
		}
	}(uint32(p.Pid), acc.PlayerID, displayName(acc))
	return fmt.Sprintf("已启动光遇\n游戏路径: %s", launchExe), nil
}

// chooseInstall 决定这次用哪个安装目录。
//
// 第一个客户端用原目录；之后每个客户端都**新建**一套镜像目录，
// 全部退出后统一删除（见 watchMirrors），下次多开再重新建。
func chooseInstall(exe string) (string, error) {
	if liveClients() == 0 {
		return exe, nil
	}
	return prepareMirror(exe)
}

// prepareMirror 新建一套镜像目录，返回里面 sky.exe 的路径。
//
// 多开时所有镜像要在拉起游戏之前就建好：建目录要几十毫秒，
// 如果一边建一边启动，第一个客户端已经把窗口弹出来了，
// 后面的就会被"已有实例"检测拦住。
func prepareMirror(exe string) (string, error) {
	root := fever.MirrorRoot(exe, loadConfig().MirrorDir)
	clientsMu.Lock()
	mirrorSeq++
	seq := mirrorSeq
	clientsMu.Unlock()

	base := filepath.Base(filepath.Dir(exe))
	name := fmt.Sprintf("%s-%d-%d", base, time.Now().UnixMilli(), seq)
	m, err := fever.MirrorInstall(exe, name, root)
	if m == "" {
		return "", fmt.Errorf("创建镜像目录失败: %w", err)
	}
	if err != nil {
		logf("镜像目录已建但有告警（%v）", err)
	}
	makeIndependentPrefs(exe, m)
	logf("第 %d 套镜像已就绪: %s", seq+1, m)
	return filepath.Join(m, filepath.Base(exe)), nil
}

// nextFullscreen 标记"这一轮多开写全屏显示"（display = fullscreen）。
// launchManyLayout 拉起进程前置位、全部拉起后清除；预写设置发生在
// 建镜像的时候，读它就知道该写窗口还是全屏。
var nextFullscreen atomic.Bool

// makeIndependentPrefs 把镜像里 preferences.sav 的硬链接断开成独立副本，
// 并按设置预写多开默认：显示模式（窗口 / 全屏）+ 画质 / 帧率。
//
// 这份文件存着全屏/画质/帧率等本地设置（TGC 的 PREF 格式，见 internal/prefs）。
// 整目录硬链接的镜像里它和本体是同一份内容，所有客户端读写互相覆盖，
// 游戏启动时还都按上一次的状态恢复窗口（全屏多开就是这么把排列毁掉的）。
func makeIndependentPrefs(srcExe, mirrorRoot string) {
	rel := filepath.Join("data", "ThatGameCompany", "com.netease.sky", "preferences.sav")
	p := filepath.Join(mirrorRoot, rel)
	b, err := os.ReadFile(p)
	if err != nil {
		return // 镜像里没有这个文件就算了
	}
	if err := os.Remove(p); err != nil {
		logf("镜像设置断链失败: %v", err)
		return
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		logf("镜像设置独立副本写入失败: %v", err)
		return
	}

	// 预写默认值：显示模式（平铺 / 堆叠 = 窗口，全屏 = 全屏）+ 画质 / 帧率
	cfg := loadConfig()
	out, err := prefs.ApplyMultiDefaults(b, nextFullscreen.Load(), cfg.MultiQualityMode, cfg.MultiQualityFps)
	if err != nil {
		logf("镜像设置预写失败: %v", err)
		return
	}
	if err := os.WriteFile(p, out, 0o644); err != nil {
		logf("镜像设置写回失败: %v", err)
		return
	}
	extra := "窗口模式"
	if nextFullscreen.Load() {
		extra = "全屏"
	}
	if cfg.MultiQualityMode != "" {
		extra += "，画质 " + cfg.MultiQualityMode
	}
	if cfg.MultiQualityFps > 0 {
		extra += fmt.Sprintf("，帧率 %d", cfg.MultiQualityFps)
	}
	logf("镜像设置已独立并预写（%s）: %s", extra, rel)
}

// stopAccount 结束某个账号正在运行的游戏（连同它的子进程）。
func stopAccount(accountID string) error {
	clientsMu.Lock()
	pid, ok := runningByAccount[accountID]
	clientsMu.Unlock()
	if !ok || pid == 0 || !fever.ProcessAlive(pid) {
		return fmt.Errorf("这个账号没有在运行的光遇")
	}
	logf("停止账号 %s 的光遇（pid=%d）", accountID, pid)
	_ = fever.KillProcessTree(pid)
	clientsMu.Lock()
	delete(clients, pid)
	if cur, ok := runningByAccount[accountID]; ok && cur == pid {
		delete(runningByAccount, accountID)
	}
	if fellback != nil && fellback.pid == pid {
		fellback = nil
	}
	clientsMu.Unlock()
	return nil
}

// watchMirrors 等所有客户端退出后清理镜像目录。
func watchMirrors(exe string) {
	root := fever.MirrorRoot(exe, loadConfig().MirrorDir)
	for i := 0; i < 360; i++ { // 最多盯 30 分钟
		if liveClients() == 0 {
			time.Sleep(3 * time.Second) // 给游戏收尾留时间
			fever.RemoveMirrors(exe, root)
			if !fever.MirrorRootExists(root) {
				logf("客户端已全部退出，镜像目录已释放")
				return
			}
		}
		time.Sleep(5 * time.Second)
	}
}
