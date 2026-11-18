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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"

	"skylogin/internal/channel"
	"skylogin/internal/device"
	"skylogin/internal/fever"
	"skylogin/internal/prefs"
	"skylogin/internal/win"
	"sort"
	"strconv"
)

const (
	// defaultReplyBurst / defaultReplyWindow 发票节流：每 window 最多 burst 个，
	// 其余排队。默认 4 个 / 200ms。
	defaultReplyBurst  = 4
	defaultReplyWindow = 200 * time.Millisecond
	// replyWindow 应答次数的计算窗口：卡登录时连续重试有上限，
	// 但在网络错误页停留很久再点重试时重新计数，路由不会被次数卡死。
	replyWindow = 10 * time.Minute
	// routeSweep 多久检查一次
	routeSweep = 5 * time.Second
	// crashWindow 启动观察期：这段时间里进程消失算"启动期闪退"，
	// 会记退出码并在多开时补启一次。游戏从拉起（10~20s 不等）
	// 到真正要票可能隔几十秒，窗口开太短会漏掉。
	crashWindow = 60 * time.Second
	// retryDelay 补启前的等待：给同批其他客户端一点时间站稳，
	// 别让"补启"变成第二波并发高峰。
	retryDelay = 8 * time.Second
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
	// retryMu 让"闪退补启"串行：同时崩三个时一次性再拉满会重演同样的竞争
	retryMu sync.Mutex
	// mirrorNames 本批建的镜像目录名（清理时按它删）；mirrorWatcherOn 保证
	// "全部退出后清理"只由一个协程负责，不再每个客户端各起一个。
	mirrorNames     []string
	mirrorWatcherOn bool
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
// ── 发票节流 ────────────────────────────────────────────────────
//
// 多开时十几个客户端几乎同时向平台要票。一次性全部回过去有两个问题：
// 游戏端会挤在一起重试（表现成"与平台通信失败"），我们自己的签票请求
// 也会一起打到服务器。默认每 200ms 只回 4 个，其余排队；
// 数字可以用配置覆盖（reply_burst / reply_window_ms），方便现场调。

type replyGate struct {
	mu     sync.Mutex
	burst  int
	window time.Duration
	tokens int
	last   time.Time
}

var ticketGate = &replyGate{burst: defaultReplyBurst, window: defaultReplyWindow, tokens: defaultReplyBurst, last: time.Now()}

func (g *replyGate) configure(burst int, window time.Duration) {
	if burst <= 0 || window <= 0 {
		return
	}
	g.mu.Lock()
	if g.burst != burst || g.window != window {
		g.burst, g.window = burst, window
		if g.tokens > burst {
			g.tokens = burst
		}
	}
	g.mu.Unlock()
}

// wait 拿一个令牌；拿不到就小睡再试（队列就是在这里排的）。
func (g *replyGate) wait() {
	for {
		g.mu.Lock()
		now := time.Now()
		gap := g.window / time.Duration(g.burst)
		if gap <= 0 {
			gap = time.Millisecond
		}
		if refill := int(now.Sub(g.last) / gap); refill > 0 {
			g.tokens += refill
			if g.tokens > g.burst {
				g.tokens = g.burst
			}
			g.last = g.last.Add(time.Duration(refill) * gap)
		}
		if g.tokens > 0 {
			g.tokens--
			g.mu.Unlock()
			return
		}
		g.mu.Unlock()
		time.Sleep(gap / 2)
	}
}

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
		// 发票节流：多开时十几个客户端几乎同时要票，一次性全部回过去会
		// 让游戏端排队重试、也把自己的签票请求挤在一起。默认每 200ms
		// 最多回 4 个（可在配置里改），其余在这里排队等令牌。
		ticketGate.configure(loadConfig().ReplyBurst(), loadConfig().ReplyWindow())
		ticketGate.wait()
		clientsMu.Lock()
		t, ch := slot.ticket, slot.channel
		slot.ticket = "" // 票是一次性的
		ready := slot.ready
		clientsMu.Unlock()

		// 预签还在路上就等它一下（通常启动后一两秒就好了），
		// 比现在才开始签快得多，能避开游戏的应答超时。
		if t == "" && ready != nil {
			select {
			case <-ready:
				clientsMu.Lock()
				t, ch = slot.ticket, slot.channel
				slot.ticket = ""
				clientsMu.Unlock()
			case <-time.After(6 * time.Second):
				logf("等预签票据超时（%s），改为现签", displayName(*slot.acc))
			}
		}

		if t == "" {
			var err error
			t, ch, err = mintTicket(*slot.acc, slot.dev)
			if err != nil {
				// 网络不通时不要往下走恢复链：那些链路（重开登录窗、换票）
				// 一样过不去，还会把账号误标成"已过期"。
				if channel.IsNetwork(err) {
					logf("现签票据失败（%s）: 网络不可达，凭证状态保持不变（%v）", displayName(*slot.acc), err)
					return
				}
				switch slot.acc.Channel {
				case channel.KeyVivo:
					// vivo 的 openToken 是短命票，uni_sauth 到取票这一步才会拒。
					// 用 Cookie 会话静默换一张新票后立刻重签；Cookie 也失效才
					// 需要重新登录。
					if rerr := refreshVivoCredential(slot.acc); rerr != nil {
						err = rerr
						expireChannelCredential(slot.acc, "vivo登录已失效", "vivo_expired", "vivo登录已过期", "请重新登录该账号")
					} else {
						t, ch, err = mintTicket(*slot.acc, slot.dev)
					}
				case channel.KeyM4399:
					// 4399 同病：oauth-check 说凭证活着，uni_sauth 却拒——
					// 跑一遍静默恢复链（check → refresh_token 轮换 → state
					// 兜底）再重签；链路全断才需要重新登录。
					if rerr := ensureM4399Credential(slot.acc); rerr != nil {
						err = rerr
						expireChannelCredential(slot.acc, "4399登录已失效", "m4399_expired", "4399登录已过期", "请重新登录该账号")
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
		// 只有首张票才开"自动进入"的时间窗：重试发票时游戏可能已经在大厅，
		// 那会儿再去点画面没有意义。
		firstTicket := slot.ticketSent.IsZero()
		if firstTicket {
			slot.ticketSent = slot.lastTicket
		}
		multi := slot.multi
		clientsMu.Unlock()
		logf("票据已发送（%s），等待游戏进入", displayName(*slot.acc))
		if firstTicket {
			scheduleAutoEnter(req.ProcessID, *slot.acc, multi, "平台签票成功")
		}
	}()
	return true
}

// registerClient 把刚启动的游戏进程登记到路由表里。
func registerClient(pid uint32, slot *multiSlot) {
	clientsMu.Lock()
	slot.pid = pid
	// 多开时顺序号已经按用户勾选顺序预设好了，这里不能覆盖——
	// 并发启动的进程谁先完成注册是不确定的，按注册顺序排会导致
	// 同样的账号组合每次排列顺序都不一样。
	if slot.seq < 0 {
		slot.seq = launchSeq
		launchSeq++
	}
	slot.createdAt = time.Now()
	clients[pid] = slot
	runningByAccount[accountKey(*slot.acc)] = pid
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
// BatchClientStatus 一个客户端在启动等待期的状态快照。
type BatchClientStatus struct {
	PID        uint32
	Name       string
	Alive      bool // 进程还在
	EverAlive  bool // 见过它活着（启动失败的从没活过，不算闪退）
	TicketSent bool // 至少一张票已成功发出（凭证链路已通）
}

// batchStatus 查询一批 pid 的启动状态。路由被回收（进程退出）的 pid
// 返回 Alive=false、EverAlive=true（能查到路由说明登记过=活过）。
func batchStatus(pids []uint32) []BatchClientStatus {
	clientsMu.Lock()
	type snap struct {
		name       string
		ticketSent bool
	}
	snaps := make(map[uint32]snap, len(pids))
	for _, pid := range pids {
		if slot := clients[pid]; slot != nil {
			snaps[pid] = snap{name: displayName(*slot.acc), ticketSent: !slot.ticketSent.IsZero()}
		}
	}
	clientsMu.Unlock()

	out := make([]BatchClientStatus, 0, len(pids))
	for _, pid := range pids {
		st := BatchClientStatus{PID: pid, EverAlive: true, Alive: fever.ProcessAlive(pid)}
		if sn, ok := snaps[pid]; ok {
			st.Name = sn.name
			st.TicketSent = sn.ticketSent
		} else {
			// 路由已回收：看门狗/退出清理删掉了，视为已退出
			st.Alive = false
		}
		out = append(out, st)
	}
	return out
}

func startClient(acc Account, dev *device.ClientProfile, ticket, channel string) (string, error) {
	exe := resolveSkyPath()
	if exe == "" {
		return "", fmt.Errorf("没有找到光遇主程序，请在设置里手动选择 sky.exe")
	}
	if accountRunning(accountKey(acc)) {
		return "", fmt.Errorf("这个账号已经在运行了，不能重复启动")
	}
	launchExe, err := chooseInstall(exe, acc, "", 0)
	if err != nil {
		return "", err
	}
	if launchExe == exe {
		// 用原目录单开：把账号的"游戏设置"写进游戏自己那份 preferences.sav。
		// 走镜像的（副本行）由 makeIndependentPrefs 在建镜像时写。
		applySingleLaunchPrefs(acc, filepath.Dir(exe))
	}
	return launchAt(acc, dev, ticket, channel, launchExe, "", 0, -1, false)
}

// ── 多开：挂起批量创建 + 统一放行 ────────────────────────────────
//
// 实测（tools/insttest，见 .ai/04 #82）：启动之间每个插 650ms 延迟，
// 10 开只活 2 个；改成 CREATE_SUSPENDED 先全部建好、再统一 ResumeThread，
// 同样 650ms 的创建开销下 10/10 全活。
//
// 原理：游戏的单实例检查是"先检查再创建互斥对象"，成败只取决于**所有
// CreateProcess 是否都赶在第一个进程执行到检查点之前**。挂起创建让所有
// 进程在放行前一行代码都没跑，检查点天然对齐；慢机器/杀软扫描只是让
// "创建"变慢，不再吃掉竞态窗口。
type suspendBatch struct {
	mu     sync.Mutex
	want   int
	held   int
	open   chan struct{}
	closed bool
}

var (
	suspendMu sync.Mutex
	suspend   *suspendBatch
)

// beginSuspendBatch 在拉起这一批之前调用；n <= 1 不启用（单开不用竞态）。
func beginSuspendBatch(n int) {
	if n <= 1 {
		return
	}
	b := &suspendBatch{want: n, open: make(chan struct{})}
	suspendMu.Lock()
	suspend = b
	suspendMu.Unlock()
	// 兜底：万一某个创建失败、凑不齐 want，也要在 4 秒内放行，
	// 不能把已经建好的进程一直挂在挂起态。
	time.AfterFunc(4*time.Second, b.release)
}

func currentSuspendBatch() *suspendBatch {
	suspendMu.Lock()
	defer suspendMu.Unlock()
	return suspend
}

// endSuspendBatch 收尾：清掉批次并保证放行（重复调用安全）。
func endSuspendBatch() {
	suspendMu.Lock()
	b := suspend
	suspend = nil
	suspendMu.Unlock()
	if b != nil {
		b.release()
	}
}

func (b *suspendBatch) add() {
	b.mu.Lock()
	b.held++
	if b.held >= b.want {
		b.closeLocked()
	}
	b.mu.Unlock()
}

func (b *suspendBatch) release() {
	b.mu.Lock()
	b.closeLocked()
	b.mu.Unlock()
}

func (b *suspendBatch) closeLocked() {
	if b.closed {
		return
	}
	b.closed = true
	close(b.open)
}

func (b *suspendBatch) wait(d time.Duration) {
	select {
	case <-b.open:
	case <-time.After(d):
	}
}

// launchAt 用指定目录启动并登记（多开时目录已提前建好，这里只管拉进程）。
// order 是窗口排列用的顺序号（多开按用户勾选的先后传入），-1 表示不指定、
// 由 registerClient 按注册顺序补；multi 表示这一轮是多开（决定自动进入
// 读哪个默认开关：账号自己的 > 多开默认 > 单开默认）。
func launchAt(acc Account, dev *device.ClientProfile, ticket, channel, launchExe string, ovMode string, ovFps int, order int64, multi bool) (string, error) {
	acc = wallLaunchSettings(acc, ovMode, ovFps)

	// 多开：优先"挂起创建"，等这一批全部建好再统一放行（见 suspendBatch 注释）。
	var held *fever.Suspended
	var batch *suspendBatch
	var pid uint32
	if multi {
		if b := currentSuspendBatch(); b != nil {
			if s, serr := fever.StartSkySuspended(launchExe); serr == nil {
				held, batch, pid = s, b, s.PID
				b.add()
			} else {
				logf("挂起创建失败，退回普通启动（%s）: %v", displayName(acc), serr)
			}
		}
	}
	if held == nil {
		p, err := startSkyPidFn(launchExe)
		if err != nil {
			// 直接启动失败（通常是权限），退回提权启动；这种方式拿不到 pid
			if elevated, e2 := startSkyFn(launchExe); e2 == nil {
				slot := &multiSlot{acc: &acc, dev: dev, ticket: ticket, channel: channel, createdAt: time.Now(), seq: order, multi: multi}
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
		pid = uint32(p.Pid)
	}

	slot := &multiSlot{acc: &acc, dev: dev, ticket: ticket, channel: channel, seq: order, multi: multi}
	if ticket == "" {
		// 没带预签票：启动后立刻并行签一张，等游戏来要时直接回，
		// 避免 op13→op14 这段走网络导致游戏超时弹错误页。
		slot.ready = make(chan struct{})
	}
	registerClient(pid, slot)

	// 放行：所有实例的创建都完成后才一起 ResumeThread。
	if held != nil {
		batch.wait(5 * time.Second)
		if err := held.Resume(); err != nil {
			logf("放行游戏进程失败（%s）: %v", displayName(acc), err)
			held.Kill()
			held.Close()
			return "", fmt.Errorf("放行游戏进程失败: %w", err)
		}
		held.Close()
	}

	if slot.ready != nil {
		go preMint(slot)
	}
	logf("光遇已启动: %s（pid=%d）", launchExe, pid)
	if exe := resolveSkyPath(); exe != "" {
		go watchMirrors(exe)
	}
	// 设了"窗口启动"就必须真的是窗口：预写的 preferences.sav 有可能被游戏
	// 在启动早期重写完（Fullscreen 丢了），所以再用窗口本身兜一次底。
	// 只对无边框/全屏的窗口动手，正常窗口不碰。
	if acc.DisplayMode == "windowed" {
		go func(pid uint32, name string) {
			if win.EnsureWindowed(pid, 30*time.Second) {
				logf("单开：%s 的窗口已切回窗口模式（预写没生效，按窗口样式兜底）", name)
			}
		}(pid, displayName(acc))
	}
	go func(pid uint32, accID, name, root string) {
		// 有的游戏会用"已有实例"检测直接退出，这里把这种情况记下来。
		// 8 秒只是第一个观察点：多开时真正死于单实例检测/加载失败的往往在
		// 10~20 秒之间（CDP/CEF、资源加载都在这段），所以后面继续盯到 60 秒，
		// 死了就带退出码记一笔，并（多开时）补启一次。
		deadline := time.Now().Add(crashWindow)
		loggedPrefs := false
		for time.Now().Before(deadline) {
			time.Sleep(time.Second)
			if fever.ProcessAlive(pid) {
				if !loggedPrefs && time.Since(slot.createdAt) >= 8*time.Second {
					loggedPrefs = true
					// 看看游戏有没有把设置文件按自己的值改回去
					// （"画质/窗口化写了却不生效"最常见的一种情况）
					logPrefsState("启动 8 秒后（"+name+"）", root)
				}
				continue
			}
			code, ok := fever.ExitCodeOf(pid)
			logClientExit(slot, name, pid, time.Since(slot.createdAt), code, ok)
			clientsMu.Lock()
			delete(clients, pid)
			if cur, ok := runningByAccount[accID]; ok && cur == pid {
				delete(runningByAccount, accID)
			}
			if fellback != nil && fellback.pid == pid {
				fellback = nil
			}
			// 只在"多开、从没发过票、没重试过、不是用户手动停的"时补启：
			// 发过票说明凭证链路是通的，再启一次会把这个账号顶掉，比不启更糟；
			// 用户点「停止」之后更不能再把它拉起来。
			retry := mayRetryCrash(slot.stopped, !slot.ticketSent.IsZero(), slot.retried, multi)
			clientsMu.Unlock()
			if retry {
				go retryCrashedClient(slot, name, ovMode, ovFps)
			}
			return
		}
	}(pid, accountKey(acc), displayName(acc), filepath.Dir(launchExe))
	return fmt.Sprintf("已启动光遇\n游戏路径: %s", launchExe), nil
}

// shouldRetryCrash 判断一次启动期退出要不要补启。
//
// 规则刻意保守：只补"多开、还没发出过票、也没补过"的那一次。
// 发过票说明凭证链路是通的，再拉一次会把这个号顶掉（单设备有效），
// 比少一个窗口更糟；补过还退说明不是偶发竞争，继续重试只是刷日志。
func shouldRetryCrash(ticketSent, retried, multi bool) bool {
	return multi && !ticketSent && !retried
}

// mayRetryCrash 在 shouldRetryCrash 之上再加一条：用户手动停掉的不许补启
// （否则"点停止 → 8 秒后它自己又起来了"）。
func mayRetryCrash(stopped, ticketSent, retried, multi bool) bool {
	return !stopped && shouldRetryCrash(ticketSent, retried, multi)
}

// logClientExit 记录"启动期退出"，带上退出码。
//
// 这一行是排查"多开只起来一部分"的关键：0/1 是游戏自己退出（重复实例、
// 自身检测），0xC0000xxx 是真的崩了（缺 DLL / 访问冲突），两者处理方式
// 完全不同，以前日志里只有一句"客户端已退出"分不出来。
func logClientExit(slot *multiSlot, name string, pid uint32, lived time.Duration, code uint32, ok bool) {
	detail := "退出码拿不到（可能已被系统回收）"
	if ok {
		detail = fmt.Sprintf("退出码=%d（0x%X，%s）", code, code, exitHint(code))
	}
	state := "还没发过票"
	if !slot.ticketSent.IsZero() {
		state = "已经发过票"
	}
	logf("客户端 %s（pid=%d）启动 %.1f 秒后退出：%s；%s", name, pid, lived.Seconds(), detail, state)
}

// exitHint 把常见退出码翻译成一句人话。
func exitHint(code uint32) string {
	switch code {
	case 0, 1, 2:
		return "正常退出，多半是游戏的重复实例检测或自身放弃"
	case 0xC0000005:
		return "访问冲突，进程崩了"
	case 0xC0000135:
		return "缺少 DLL"
	case 0xC0000142:
		return "DLL 初始化失败"
	case 0xC0000409:
		return "快速失败（栈溢出/安全检查）"
	case 0xC000001D:
		return "非法指令（CPU 不支持）"
	case 0xC0000006:
		return "页错误（文件被删/被占用）"
	}
	return "异常退出"
}

// retryCrashedClient 在一个启动期闪退的客户端上再试一次（每批每号最多一次）。
//
// 前提很硬：**游戏是全局单实例**，只要本轮还有别的客户端在跑，新拉的进程
// 一定过不了启动检查，补启只会白跑一趟。所以这里先看 liveClients()：
// 只有"本轮已经全空了"（所有客户端都退出）才真的补；否则只记一行日志说明
// 为什么没补——比让用户点一次"启动"再被拦一次强。
//
// 补启串行 + 有延迟，避免几个补启又形成一波并发。
func retryCrashedClient(slot *multiSlot, name, ovMode string, ovFps int) {
	retryMu.Lock()
	defer retryMu.Unlock()
	time.Sleep(retryDelay)

	if n := liveClients(); n > 0 {
		logf("闪退补启（%s）：还有 %d 个客户端在跑，游戏全局单实例，这时候补不进去；本轮全部退出后再单独启动它", name, n)
		return
	}

	// 只补"还没发过票"的：发过票说明凭证链路通，再启一次会把这个号顶掉。
	clientsMu.Lock()
	ticket, channel := slot.ticket, slot.channel
	acc := *slot.acc
	order := slot.seq
	clientsMu.Unlock()
	if accountRunning(accountKey(acc)) {
		return
	}

	exe := resolveSkyPath()
	if exe == "" {
		return
	}
	// 原镜像可能已经被清理协程删掉，重建一套最稳（硬链接镜像只要几十毫秒）。
	m, err := prepareMirror(exe, acc, ovMode, ovFps)
	if err != nil {
		logf("闪退补启（%s）：建镜像失败: %v", name, err)
		return
	}
	if _, err := launchAt(acc, slot.dev, ticket, channel, m, ovMode, ovFps, order, true); err != nil {
		logf("闪退补启（%s）失败: %v", name, err)
		return
	}
	clientsMu.Lock()
	if pid := runningByAccount[accountKey(acc)]; pid != 0 {
		if s := clients[pid]; s != nil {
			s.retried = true
		}
	}
	clientsMu.Unlock()
	logf("闪退补启：已单独重新拉起 %s（每批每号只补一次）", name)
}

// chooseInstall 决定这次用哪个安装目录。//
// 第一个客户端用原目录；之后每个客户端都**新建**一套镜像目录，
// 全部退出后统一删除（见 watchMirrors），下次多开再重新建。
//
// 例外：副本（CopyKey 非空）永远走镜像 ——
// 镜像里装了浏览器壳，壳会把 CEF channel 改成这条记录专属的名字，
// 于是它在游戏里选的安卓区/iOS 区会被一直记住。
func chooseInstall(exe string, acc Account, ovMode string, ovFps int) (string, error) {
	if acc.CopyKey == "" && liveClients() == 0 {
		return exe, nil
	}
	return prepareMirror(exe, acc, ovMode, ovFps)
}

// prepareMirror 新建一套镜像目录，返回里面 sky.exe 的路径。
//
// 多开时所有镜像要在拉起游戏之前就建好：建目录要几十毫秒，
// 如果一边建一边启动，第一个客户端已经把窗口弹出来了，
// 后面的就会被"已有实例"检测拦住。
//
// channelKey 非空时，把这条记录专属的 CEF channel 写进镜像，
// 浏览器壳按它改 profile 名（同一行账号 → 同一个 profile → 记住选区）。
func prepareMirror(exe string, acc Account, ovMode string, ovFps int) (string, error) {
	channelKey := accountKey(acc)
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
	makeIndependentPrefs(exe, m, acc, ovMode, ovFps)
	if err := installWebviewShim(m); err != nil {
		logf("浏览器壳安装失败: %v", err)
	}
	writeShimChannelHint(m, channelKey)
	trackMirror(name)
	logf("第 %d 套镜像已就绪: %s", seq+1, m)
	return filepath.Join(m, filepath.Base(exe)), nil
}

// nextFullscreen 标记"这一轮多开写全屏显示"（display = fullscreen）。
// launchManyLayout 拉起进程前置位、全部拉起后清除；预写设置发生在
// 建镜像的时候，读它就知道该写窗口还是全屏。
var nextFullscreen atomic.Bool

// inMultiBatch 标记"当前正在批量建镜像"。
//
// 为什么需要：建镜像这件事单开（副本行）也会走，而显示方式的来源不同——
// 多开听多开弹窗的，单开听账号自己的。用这个标志区分，避免单开的副本
// 被"多开默认 = 窗口"覆盖掉。
var inMultiBatch atomic.Bool

// makeIndependentPrefs 把镜像里 preferences.sav 的硬链接断开成独立副本，
// 并按设置预写多开默认：显示模式（窗口 / 全屏）+ 画质 / 帧率。
//
// 这份文件存着全屏/画质/帧率等本地设置（TGC 的 PREF 格式，见 internal/prefs）。
// 整目录硬链接的镜像里它和本体是同一份内容，所有客户端读写互相覆盖，
// 游戏启动时还都按上一次的状态恢复窗口（全屏多开就是这么把排列毁掉的）。
func makeIndependentPrefs(srcExe, mirrorRoot string, acc Account, ovMode string, ovFps int) {
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

	// 预写默认值：显示模式（平铺 / 堆叠 = 窗口，全屏 = 全屏）+ 画质 / 帧率。
	// 取值优先级：多开弹窗的本次覆盖 > 账号自己的配置 > 跟随游戏（不写）。
	//
	// 显示方式单独处理：多开时一律听多开弹窗的（平铺/堆叠必须窗口模式，
	// 否则窗口排不动）；只有"副本单开"这种非多开批次才用账号自己的设置。
	mode, fps := effectiveQuality(acc, ovMode, ovFps)
	fullscreen := nextFullscreen.Load()
	if !inMultiBatch.Load() && acc.DisplayMode != "" {
		fullscreen = acc.DisplayMode == "fullscreen"
	}
	out, err := prefs.ApplyMultiDefaults(b, fullscreen, mode, fps)
	if err != nil {
		logf("镜像设置预写失败: %v", err)
		return
	}
	if err := os.WriteFile(p, out, 0o644); err != nil {
		logf("镜像设置写回失败: %v", err)
		return
	}
	extra := "窗口模式"
	if fullscreen {
		extra = "全屏"
	}
	if mode != "" {
		extra += "，画质 " + mode
	}
	if fps > 0 {
		extra += fmt.Sprintf("，帧率 %d", fps)
	}
	logf("镜像设置已独立并预写（%s）: %s", extra, rel)
	// 写完再从磁盘回读一遍：日志里"已预写"只说明调用成功，
	// 万一写的是另一份文件、或被安全软件改回去，回读才能看出来。
	logPrefsState("镜像预写回读", mirrorRoot)
}

// prefsPathOf 返回某个安装目录里的设置文件（PREF 格式）路径。
func prefsPathOf(installRoot string) string {
	return filepath.Join(installRoot, "data", "ThatGameCompany", "com.netease.sky", "preferences.sav")
}

// logPrefsState 打一行"这份设置文件现在是什么样"。
//
// 排查"画质/窗口化写了却不生效"时，光看"已预写"没用：要分清是
//
//	① 文件没写进去 / ② 镜像被清理掉了 / ③ 游戏启动后又按自己的值改回去了。
//
// 所以在预写后、启动后、退出前各读一次，日志里一对比就知道是哪一种。
func logPrefsState(tag, installRoot string) {
	p := prefsPathOf(installRoot)
	st, err := os.Stat(p)
	if err != nil {
		logf("%s：设置文件不存在（%s）", tag, p)
		return
	}
	b, rerr := os.ReadFile(p)
	if rerr != nil {
		logf("%s：设置文件读不了（%v）", tag, rerr)
		return
	}
	fs, _ := prefs.ReadValue(b, prefs.KeyFullscreen)
	qm, _ := prefs.ReadString(b, prefs.KeyQualityMode)
	qf, _ := prefs.ReadValue(b, prefs.KeyQualityFps)
	// 结构是否自洽：游戏自己写的一定自洽；不自洽说明被改坏过（含我们自己）。
	// 这一栏是"文件被重置/被写坏"的唯一直接证据，别省。
	sane := "自洽"
	if verr := prefs.Validate(b); verr != nil {
		sane = "不自洽（" + verr.Error() + "）"
	}
	logf("%s：%d 字节，改于 %s，Fullscreen=%d 画质=%q 帧率=%d，结构%s",
		tag, len(b), st.ModTime().Format("15:04:05"), fs, qm, qf, sane)
}

// effectiveQuality 计算某行账号本次启动实际预写的画质/帧率：
// 多开弹窗的本次覆盖 > 账号自己的配置 > 跟随游戏（返回空/0，什么都不写）。
func effectiveQuality(acc Account, ovMode string, ovFps int) (string, int) {
	mode, fps := acc.QualityMode, acc.QualityFps
	if ovMode != "" || ovFps > 0 {
		mode, fps = ovMode, ovFps
	}
	return mode, fps
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
	clientsMu.Lock()
	if s := clients[pid]; s != nil {
		// 用户手动停的：启动看护协程看到进程消失时不能再补启，
		// 否则"点停止 → 8 秒后又自己起来了"。
		s.stopped = true
	}
	clientsMu.Unlock()
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
//
// 只跑一个观察者：以前每个客户端启动都 spawn 一个，11 个客户端就有 11 个
// 协程在"全部退出"后同时扫同一批目录、各自重试删除、各自打一行日志
// （日志里那串重复的"镜像已清理"就是这么来的）。现在镜像名统一登记，
// 谁先退谁清理，其余观察者直接退出。
func watchMirrors(exe string) {
	if !startMirrorWatcher() {
		return
	}
	defer stopMirrorWatcher()

	root := fever.MirrorRoot(exe, loadConfig().MirrorDir)
	for i := 0; i < 360; i++ { // 最多盯 30 分钟
		if liveClients() == 0 {
			time.Sleep(3 * time.Second) // 给游戏收尾留时间
			if liveClients() > 0 {
				continue // 等待期间有客户端（或补启）又起来了，下一轮再说
			}
			names := takeMirrorNames()
			if len(names) == 0 {
				return
			}
			// 先删目录：壳（本程序的硬链接）跟着目录一起没了，不用先改名再删，
			// 省掉每个镜像一次 30MB 文件的删除+改名。删不掉的（浏览器子进程
			// 还占着文件）才还原壳，保证目录里不会留一个被换过的浏览器。
			if left := fever.RemoveMirrorDirs(root, names); len(left) > 0 {
				restoreShimsOf(root, left)
				fever.RemoveMirrorDirs(root, left)
			}
			logf("客户端已全部退出，镜像已清理（%d 套，仍在用的已跳过）", len(names))
			return
		}
		time.Sleep(5 * time.Second)
	}
}

// startMirrorWatcher 只允许一个观察者存在。
func startMirrorWatcher() bool {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if mirrorWatcherOn {
		return false
	}
	mirrorWatcherOn = true
	return true
}

func stopMirrorWatcher() {
	clientsMu.Lock()
	mirrorWatcherOn = false
	clientsMu.Unlock()
}

// trackMirror 登记本批创建的镜像目录名（清理时按它删，不扫全目录）。
func trackMirror(name string) {
	if name == "" {
		return
	}
	clientsMu.Lock()
	mirrorNames = append(mirrorNames, name)
	clientsMu.Unlock()
}

// takeMirrorNames 取走并清空登记的镜像名。
func takeMirrorNames() []string {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	names := mirrorNames
	mirrorNames = nil
	return names
}

// restoreShimsOf 只还原指定目录里的浏览器壳（清理时删不掉的那几个）。
func restoreShimsOf(root string, names []string) {
	for _, name := range names {
		dir := filepath.Join(root, name)
		if len(fever.ProcessesFrom(dir)) > 0 {
			continue // 还在用的镜像不动（浏览器壳还原会让运行中的游戏找不到文件）
		}
		restoreWebviewShim(dir)
	}
}

// preMint 提前签一张票（启动后立刻并行做）。
//
// 为什么要提前：游戏向平台要票是带超时的，如果我们收到请求才开始
// 校验凭证 + create_ticket（两三次网络往返），多开时几个客户端一起要票，
// 应答就容易超过游戏的等待时间，表现为"与平台通信失败"的错误页
// （那个错误页其实是游戏内置的浏览器兜底界面）。
func preMint(slot *multiSlot) {
	t, ch, err := mintTicket(*slot.acc, slot.dev)
	clientsMu.Lock()
	if err == nil && slot.ticket == "" {
		slot.ticket, slot.channel = t, ch
	}
	slot.ticketErr = err
	close(slot.ready)
	clientsMu.Unlock()
	if err != nil {
		logf("预签票据失败（%s）: %v", displayName(*slot.acc), err)
		return
	}
	logf("已预签票据（%s），等游戏来取", displayName(*slot.acc))
}

// restoreAllShims 把某个游戏的所有镜像里的浏览器壳还原（删壳、真身改回原名）。
//
// 客户端退出后、启动器退出前、下次启动清理残留时都调一次；
// 即使镜像目录删不掉，里面也不会留一个被换过的浏览器。
func restoreAllShims(exe, root string) {
	prefix := filepath.Base(filepath.Dir(exe)) + "-"
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			dir := filepath.Join(root, e.Name())
			if len(fever.ProcessesFrom(dir)) > 0 {
				continue // 还在用的镜像不动（浏览器壳还原会让运行中的游戏找不到文件）
			}
			restoreWebviewShim(dir)
		}
	}
}

// recoverRunningClients 登录器重启后，把"上次启动、还在跑"的游戏客户端
// 重新认回来：枚举 Sky.exe 主窗口，标题 "N · 账号名" 是当初排列时写的
// （renameWindow），按名字匹配账号重建 pid → 账号路由。恢复后状态栏、
// 监控墙、排列、取票应答全部照常工作（票据没了，游戏再要票时现签）。
//
// 匹配不上名字的窗口（没排过列、标题还是引擎默认"光·遇"）：
// 恰好只有一个时归给当前选中账号（单开场景）；多个时放弃识别并记日志，
// 宁可少认也不乱认（票据发错账号会串号）。
func recoverRunningClients() {
	wins := win.EnumerateClientWindows()
	if len(wins) == 0 {
		return
	}
	list, _ := loadAccounts()
	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		logf("恢复运行状态失败：设备指纹不可用（%v）", derr)
		return
	}

	// 名字 → 账号（匹配排列时写的 displayName；别名改过就退到用户名/昵称）
	byName := map[string]*Account{}
	for i := range list {
		a := &list[i]
		for _, key := range []string{displayName(*a), a.Username, a.NickName} {
			if key != "" {
				byName[key] = a
			}
		}
	}

	type recovered struct {
		pid  uint32
		slot *multiSlot
	}
	var got []recovered
	var unknown []win.ClientWindow
	for _, w := range wins {
		name, seq, ok := parseClientTitle(w.Title)
		var acc *Account
		if ok {
			acc = byName[name]
		}
		if acc == nil {
			unknown = append(unknown, w)
			continue
		}
		slot := &multiSlot{acc: acc, dev: dev, createdAt: time.Now()}
		if seq > 0 {
			slot.seq = seq
		}
		got = append(got, recovered{pid: w.PID, slot: slot})
	}

	// 恰好一个未识别 + 当前账号没被占用：单开（没排列过标题），归当前账号
	if len(unknown) == 1 && len(got) >= 0 {
		if cur, err := pickAccount(); err == nil {
			occupied := false
			for _, r := range got {
				if accountKey(*r.slot.acc) == accountKey(cur) {
					occupied = true
					break
				}
			}
			if !occupied {
				slot := &multiSlot{acc: &cur, dev: dev, createdAt: time.Now()}
				got = append(got, recovered{pid: unknown[0].PID, slot: slot})
				logf("恢复：唯一未识别客户端（pid=%d）按当前账号 %s 认领", unknown[0].PID, displayName(cur))
				unknown = nil
			}
		}
	}

	if len(got) == 0 {
		if len(unknown) > 0 {
			logf("发现 %d 个运行中的光遇客户端但识别不出账号（窗口标题未含账号名），未恢复路由", len(unknown))
		}
		return
	}

	// 顺序按标题序号排（重排时格子顺序稳定）
	sort.Slice(got, func(i, j int) bool { return got[i].slot.seq < got[j].slot.seq })
	for _, r := range got {
		registerClient(r.pid, r.slot)
	}
	// IPC 宿主窗口要建起来：游戏后续取票（重试/重新登录）靠它找到"平台"
	hostMu.Lock()
	if err := ensureHostLocked(); err != nil {
		logf("恢复：重建 IPC 宿主失败: %v", err)
	}
	hostMu.Unlock()
	// 上次会话若开着监控墙（3+ 客户端），重启后自动再开——"重启登录器
	// 一切照旧"，不该让用户手动找开关
	autoOpenWall()
	logf("已恢复 %d 个运行中客户端的识别（%d 个未识别）", len(got), len(unknown))
}

// parseClientTitle 解析排列时写的窗口标题 "N · 账号名"。
// 不是这个形态（引擎默认标题"光·遇"）返回 ok=false。
func parseClientTitle(title string) (name string, seq int64, ok bool) {
	i := strings.Index(title, "·")
	if i <= 0 || i+2 > len(title) {
		return "", 0, false
	}
	numPart := strings.TrimSpace(title[:i])
	// 序号必须是纯数字："光·遇" 这种引擎默认标题里"·"前不是数字
	if _, err := strconv.ParseInt(numPart, 10, 64); err != nil {
		return "", 0, false
	}
	name = strings.TrimSpace(title[i+2:])
	if name == "" {
		return "", 0, false
	}
	seq, _ = strconv.ParseInt(numPart, 10, 64)
	return name, seq, true
}
