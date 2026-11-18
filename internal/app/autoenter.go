package app

// 自动进入：替用户点掉登录界面上的播放按钮。
//
// 两个时间概念要分清：
//
//	签票时刻   —— 每个客户端各自拿到票的时间（handleTicket 里）
//	起跑时刻   —— 真正开始"找按钮 + 点击"的时间
//
// 单开：签票后等半秒就开始找，窗口 10 秒。
//
// 多开：**不是每个客户端各点各的**（8 开时只有 3 个被点到，后面的槽位被
// 前面反复重试吃光了）。改成一批一个协调者：
//
//  1. 等排列完成、监控墙拉起来（原有闸门）
//  2. 等**这一批全部发完票**（还活着的都要 TicketSent）
//  3. 确保监控墙真的开着（打不开也继续，只是点击会避开它）
//  4. 等任何一个客户端出现可点的按钮（最多 autoEnterFirstWait）
//  5. 按启动顺序**串行**处理：每个账号一个槽位（默认 4 秒，可配），
//     "找到按钮 → 点一次 → 2 秒内按钮消失 = 成功，立刻下一个"；
//     没消失就把这个号的槽位等满再下一个。一个号一次启动只点一次。
//
// 为什么是"点"不是"按空格"：实测这台机器上游戏会丢弃合成键盘事件
//（低级键盘钩子按 LLKHF_INJECTED 过滤），但鼠标点击是收的。
// 相关验证记录见 internal/playbtn 的包注释。

import (
	"sync"
	"time"

	"skylogin/internal/fever"
	"skylogin/internal/playbtn"
	"skylogin/internal/win"
)

const (
	// autoEnterWindow 单开时的尝试窗口。
	autoEnterWindow = 10 * time.Second
	// autoEnterDelay 等游戏把登录界面画出来再开始找，省掉几次空转。
	autoEnterDelay = 500 * time.Millisecond
	// autoEnterGateTimeout 等多开窗口稳定的上限。
	autoEnterGateTimeout = 90 * time.Second
	// autoEnterFirstWait 多开时等"第一个可点按钮出现"的上限：
	// 发完票到登录界面画出来通常几十秒，给足 3 分钟。
	autoEnterFirstWait = 3 * time.Minute
	// autoEnterGone 点完等按钮消失的时间（用户要求：2 秒内消失才算进去）。
	autoEnterGone = 2 * time.Second
	// autoEnterSlotDefault 每个账号的默认槽位。
	autoEnterSlotDefault = 10 * time.Second
)

// ── 多开的"窗口稳定"闸门 ─────────────────────────────────────────

var (
	autoEnterGateMu sync.Mutex
	// autoEnterGate 非 nil = 这一批多开还没稳定；关掉（close）= 可以开始点了。
	autoEnterGate chan struct{}
)

// armAutoEnterBatch 在多开启动前调用：给这一批挂上闸门和放行信号。
// want = 这一批要拉起的客户端数，用于等"全部发完票"。
func armAutoEnterBatch(want int) {
	resetClickClaims()
	armAutoEnterStart()
	aeMu.Lock()
	aeWant = want
	aeMu.Unlock()
	autoEnterGateMu.Lock()
	if autoEnterGate != nil {
		close(autoEnterGate)
	}
	autoEnterGate = make(chan struct{})
	autoEnterGateMu.Unlock()
}

// releaseAutoEnterBatch 在排列完成 / 监控墙拉起之后调用：放行。
func releaseAutoEnterBatch() {
	autoEnterGateMu.Lock()
	if autoEnterGate != nil {
		close(autoEnterGate)
		autoEnterGate = nil
	}
	autoEnterGateMu.Unlock()
}

// waitAutoEnterGate 拿到闸门并等它放行；没有闸门（单开）立即返回。
func waitAutoEnterGate() {
	autoEnterGateMu.Lock()
	gate := autoEnterGate
	autoEnterGateMu.Unlock()
	if gate == nil {
		return
	}
	select {
	case <-gate:
	case <-time.After(autoEnterGateTimeout):
		logf("自动进入：等了 %.0f 秒窗口仍未稳定，按已稳定处理", autoEnterGateTimeout.Seconds())
	}
}

// ── 开关与调度 ──────────────────────────────────────────────────

// autoEnterAllowed 判断这一行账号这次启动要不要自动进入。
//
// 优先级：账号单独设置 >（多开 / 单开）全局设置。
func autoEnterAllowed(acc Account, multi bool) bool {
	return autoEnterDecision(acc, multi, loadConfig())
}

// autoEnterDecision 是纯函数版本，便于测试（不读文件、不碰全局状态）。
func autoEnterDecision(acc Account, multi bool, c Config) bool {
	if acc.AutoEnter != nil {
		return *acc.AutoEnter
	}
	if multi {
		return c.MultiAutoEnterEnabled()
	}
	return c.AutoEnterPlayEnabled()
}

// autoEnterReq 是一个"这次启动要点进去"的客户端。
type autoEnterReq struct {
	pid   uint32
	acc   Account
	name  string
	order int64
}

var (
	aeMu    sync.Mutex
	aeQueue []autoEnterReq
	aeOn    bool
	// aeWant 这一批要拉起的客户端数（等"全部发完票"用）
	aeWant int
)

// scheduleAutoEnter 在发票成功后登记一次"进入游戏"。
//
// 调用方只在**首张票发出**时调用（重试发票时窗口早就开过了）。
func scheduleAutoEnter(pid uint32, acc Account, multi bool, why string) {
	if pid == 0 {
		return
	}
	if !autoEnterAllowed(acc, multi) {
		logf("自动进入：%s 已关闭，跳过（%s）", displayName(acc), why)
		return
	}
	if !multi {
		// 单开每次启动都是一个新窗口（pid 可能被系统复用，先清掉旧账）
		aeClicked.Delete(pid)
		go singleAutoEnter(pid, acc)
		return
	}

	order := int64(0)
	clientsMu.Lock()
	if s := clients[pid]; s != nil {
		order = s.seq
	}
	clientsMu.Unlock()

	aeMu.Lock()
	req := autoEnterReq{pid: pid, acc: acc, name: displayName(acc), order: order}
	aeQueue = append(aeQueue, req)
	start := !aeOn
	aeOn = true
	aeMu.Unlock()

	// 每个窗口一个自己的计时器：从批次放行开始算 10 秒（排队等待不计）。
	go runAutoEnterWindow(req, autoEnterStartChan())
	if start {
		go runAutoEnterBatch()
	}
}

// 一个窗口只许点一次：点完游戏可能已经进去了，再点就是在大厅/商城界面上
// 乱点（同一个屏幕位置），可能点到购买、丢弃之类，给用户造成实际损失。
// 这里按 pid 记账，任何路径都不能对同一个窗口点第二次。
var aeClicked sync.Map // pid -> struct{}

// claimClick 登记"这个窗口要点一次"；已经点过返回 false。
func claimClick(pid uint32) bool {
	_, loaded := aeClicked.LoadOrStore(pid, struct{}{})
	return !loaded
}

// resetClickClaims 换批时清账（pid 会被系统复用，不能留到下一批）。
func resetClickClaims() { aeClicked.Range(func(k, _ any) bool { aeClicked.Delete(k); return true }) }

// singleAutoEnter 单开的路径：签票后半秒开始，10 秒窗口内反复试。
func singleAutoEnter(pid uint32, acc Account) {
	name := displayName(acc)
	if !claimClick(pid) {
		return
	}
	// 监控墙可能压住按钮，这期间切成鼠标穿透。
	holdWallPassthrough(true)
	defer holdWallPassthrough(false)

	time.Sleep(autoEnterDelay)
	start := time.Now()
	clicked, gone := playbtn.EnterOnce(pid, autoEnterWindow, autoEnterGone, logf)
	if clicked && gone {
		logf("自动进入：%s 已进入游戏（用时 %v）", name, time.Since(start).Round(100*time.Millisecond))
		return
	}
	if clicked {
		logf("自动进入：%s 点了但按钮没消失，放弃（不影响游戏）", name)
		return
	}
	logf("自动进入：%s 在 %.0f 秒窗口内没找到可点的播放按钮，放弃（不影响游戏）",
		name, autoEnterWindow.Seconds())
}

// runAutoEnterBatch 多开的协调者：等齐（排列/开墙/全部发完票）→ 放行，
// 之后每个窗口自己找按钮、自己计时。
//
// 为什么不再"等第一个按钮出现"：登录界面什么时候画出来并不确定，等它会把
// 所有窗口一起卡住（实测卡了 180 秒，用户看到的就是"早就就绪了却没动静，
// 过很久突然全部点了一遍"）。现在放行后各窗口自己轮询：匹配不到就跳过、
// 下一轮再看，直到自己的时间用完。
func runAutoEnterBatch() {
	defer func() {
		aeMu.Lock()
		aeOn = false
		aeMu.Unlock()
	}()

	// ① 排列完成、监控墙拉起（闸门）
	waitAutoEnterGate()
	// ② 这一批全部发完票（还活着的）
	aeMu.Lock()
	want := aeWant
	aeMu.Unlock()
	sent := waitAllTicketsSent(3*time.Minute, want)
	// ③ 监控墙必须真的开着（等它建窗口，最多 5 秒）
	ensureWallForAutoEnter()

	// ④ 放行：每个窗口从这一刻开始算自己的时间
	closeAutoEnterStart()
	logf("自动进入：开始计时（%d 个窗口已发完票、排列完成、监控墙就绪；每窗口 %.0f 秒，匹配不到就跳过再试）",
		sent, loadConfig().AutoEnterSlot().Seconds())
}
// runAutoEnterWindow 一个窗口自己的计时器。
//
// 用户要的口径：按窗口单独算，从它能动手开始给 10 秒；等别的窗口点完的
// 时间不算。所以 deadline 每次都会把 TryClick 里"等锁"的时长补回去。
func runAutoEnterWindow(r autoEnterReq, start <-chan struct{}) {
	if start != nil {
		<-start
	}
	if !fever.ProcessAlive(r.pid) {
		logf("自动进入：%s 已退出，跳过", r.name)
		return
	}
	if !claimClick(r.pid) {
		return // 这个窗口已经点过了，绝不再点第二次
	}

	// 监控墙是 TOPMOST，可能压住按钮：点的时候切成鼠标穿透。
	holdWallPassthrough(true)
	defer holdWallPassthrough(false)

	budget := loadConfig().AutoEnterSlot()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		_, clicked, gone, waited := playbtn.TryClick(r.pid, autoEnterGone, logf)
		// 等别的窗口点完的时间不算进自己的预算
		deadline = deadline.Add(waited)
		if clicked {
			if gone {
				logf("自动进入：%s 已进入游戏（按钮已消失）", r.name)
			} else {
				logf("自动进入：%s 点了但按钮没消失，不再重试", r.name)
			}
			return
		}
		// 没匹配到：先跳过，过一会儿再看（不点空画面）`n`t`ttime.Sleep(300 * time.Millisecond)
	}
	logf("自动进入：%s 在 %.0f 秒内没匹配到登录按钮，跳过（不影响游戏）", r.name, budget.Seconds())
}

// ── 批次放行信号 ────────────────────────────────────────────────

var (
	aeStartMu sync.Mutex
	aeStartCh chan struct{}
)

// armAutoEnterStart 建这一批的放行信号（在 armAutoEnterBatch 里调）。
func armAutoEnterStart() {
	aeStartMu.Lock()
	if aeStartCh != nil {
		close(aeStartCh) // 上一批没等到放行就作废，别让它的窗口一直等
	}
	aeStartCh = make(chan struct{})
	aeStartMu.Unlock()
}

// autoEnterStartChan 取当前批次的放行信号（没有就返回 nil，表示立刻开始）。
func autoEnterStartChan() <-chan struct{} {
	aeStartMu.Lock()
	defer aeStartMu.Unlock()
	return aeStartCh
}

// closeAutoEnterStart 放行（重复调用安全）。
func closeAutoEnterStart() {
	aeStartMu.Lock()
	if aeStartCh != nil {
		close(aeStartCh)
		aeStartCh = nil
	}
	aeStartMu.Unlock()
}
// waitAllTicketsSent 等这一批还活着的客户端都发过票，返回已发数。
//
// want 是这批要拉起的数量；如果中途有客户端死了（路由没了），10 秒后按
// 当前活着的数量修正目标，否则会一直等一个永远不会发车的窗口。
func waitAllTicketsSent(max time.Duration, want int) int {
	deadline := time.Now().Add(max)
	begin := time.Now()
	lastSent := 0
	for time.Now().Before(deadline) {
		clientsMu.Lock()
		pids := make([]uint32, 0, len(clients))
		for pid := range clients {
			pids = append(pids, pid)
		}
		clientsMu.Unlock()

		sent, alive := 0, 0
		for _, st := range batchStatus(pids) {
			if st.TicketSent {
				sent++
			}
			if st.Alive {
				alive++
			}
		}
		if want <= 0 || sent >= want {
			return sent
		}
		// 软期限：卡在游戏"网络异常"弹窗上的客户端一直不发票，不能让它
		// 把自动进入拖住（90 秒后按已发出的算）。
		if time.Since(begin) > 90*time.Second {
			logf("自动进入：有客户端 90 秒还没发完票（可能卡在游戏弹窗），按当前的 %d 个开始", sent)
			return sent
		}
		lastSent = sent
		if len(pids) == 0 {
			return sent
		}
		// 启动 10 秒后仍凑不齐：按还活着的数量算（死了的不必再等）
		if time.Since(begin) > 10*time.Second && alive < want {
			want = alive
			if sent >= want {
				return sent
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	logf("自动进入：等全部发完票超时，按当前状态开始")
	return lastSent
}

// ensureWallForAutoEnter 确保监控墙开着；打不开不阻塞后续点击。
func ensureWallForAutoEnter() {
	if skipAutoWall.Load() {
		logf("自动进入：这套组关了自动开墙，跳过")
		return
	}
	if win.WallOpen() {
		return
	}
	if len(runningPids()) < 2 {
		return
	}
	if _, err := ToggleWall(); err != nil {
		logf("自动进入：打开监控墙失败: %v", err)
	}
	for i := 0; i < 50 && !win.WallOpen(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if win.WallOpen() {
		logf("自动进入：监控墙已确认打开")
	} else {
		logf("自动进入：监控墙没打开（不影响点击，会切成鼠标穿透）")
	}
}

// ── 监控墙鼠标穿透的引用计数 ────────────────────────────────────

var (
	wallPassMu sync.Mutex
	wallPassN  int
)

func holdWallPassthrough(on bool) {
	wallPassMu.Lock()
	defer wallPassMu.Unlock()
	if on {
		wallPassN++
		if wallPassN == 1 {
			win.SetWallClickThrough(true)
		}
		return
	}
	if wallPassN == 0 {
		return
	}
	wallPassN--
	if wallPassN == 0 {
		win.SetWallClickThrough(false)
	}
}
