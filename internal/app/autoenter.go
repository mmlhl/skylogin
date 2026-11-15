package app

// 自动进入：平台签票（op14 发出）之后，在 10 秒的窗口内替用户点一下登录界面
// 上的播放按钮。
//
// 两个时间概念要分清：
//
//	签票时刻   —— 每个客户端各自拿到票的时间（handleTicket 里）
//	起跑时刻   —— 真正开始"找按钮 + 点击"的时间
//
// 单开：起跑时刻 = 签票时刻 + 半秒。
//
// 多开：起跑时刻要等**这一批窗口全部稳定**（排列完成、监控墙拉起来）之后。
// 原因是多开启动时窗口还在陆续创建、被反复移动/置顶，这时候去点会点空、
// 或者抢焦点把排列搅乱；等窗口稳了再点，命中率最高，也不干扰排列。
// 所以多开用一道"闸门"（autoEnterGate）：排列协程干完活关闸，等待方放行，
// 放行之后才各自开始 10 秒计时。
//
// 为什么是"点"不是"按空格"：实测这台机器上游戏会丢弃合成键盘事件
//（低级键盘钩子按 LLKHF_INJECTED 过滤），但鼠标点击是收的。
// 相关验证记录见 internal/playbtn 的包注释。

import (
	"sync"
	"time"

	"skylogin/internal/playbtn"
	"skylogin/internal/win"
)

const (
	// autoEnterWindow 起跑之后的尝试窗口。
	autoEnterWindow = 10 * time.Second
	// autoEnterDelay 等游戏把登录界面画出来再开始找，省掉几次空转。
	autoEnterDelay = 500 * time.Millisecond
	// autoEnterGateTimeout 等多开窗口稳定的上限。排列最长会给 3 分钟，
	// 但真卡住时不该一直挂着等——超过这个点就按"窗口已经稳了"处理。
	autoEnterGateTimeout = 90 * time.Second
)

// ── 多开的"窗口稳定"闸门 ─────────────────────────────────────────

var (
	autoEnterGateMu sync.Mutex
	// autoEnterGate 非 nil = 这一批多开还没稳定；关掉（close）= 可以开始点了。
	autoEnterGate chan struct{}
)

// armAutoEnterBatch 在多开启动前调用：给这一批挂上闸门。
//
// 只由多开/启动组调用；单开不走闸门（起跑就是立刻）。
func armAutoEnterBatch() {
	autoEnterGateMu.Lock()
	if autoEnterGate != nil {
		// 上一批还没放行就被新一批顶掉了：先把旧的放掉，别让它的等待方干等
		close(autoEnterGate)
	}
	autoEnterGate = make(chan struct{})
	autoEnterGateMu.Unlock()
}

// releaseAutoEnterBatch 在排列完成 / 监控墙拉起之后调用：放行。
// 重复调用安全（已经放行过就什么都不做）。
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

// scheduleAutoEnter 在发票成功后启动一次"进入游戏"的尝试。
//
// 调用方必须只在**首张票发出**时调用：重试发票时窗口已经开过一次了，
// 再开一次没有意义，而且那时客户端多半已经在大厅里。
func scheduleAutoEnter(pid uint32, acc Account, multi bool, why string) {
	if pid == 0 {
		return
	}
	if !autoEnterAllowed(acc, multi) {
		logf("自动进入：%s 已关闭，跳过（%s）", displayName(acc), why)
		return
	}
	name := displayName(acc)
	go func() {
		// 多开：先等这一批窗口稳定（排列完成、监控墙起来），再开始计时
		if multi {
			waitAutoEnterGate()
		}
		// 监控墙是 TOPMOST 钉在所有客户端上面的：它可能正好压住播放按钮，
		// 那样点击会被墙吃掉。这期间把墙切成鼠标穿透（画面照旧显示），
		// 点完立刻还原；引用计数保证几个客户端同时在跑时不会互相提前还原。
		holdWallPassthrough(true)
		defer holdWallPassthrough(false)

		// 计时从"起跑"这一刻算，不是从签票那一刻
		deadline := time.Now().Add(autoEnterWindow)
		time.Sleep(autoEnterDelay)

		start := time.Now()
		if playbtn.Enter(pid, deadline, logf) {
			logf("自动进入：%s 已进入游戏（用时 %v）", name, time.Since(start).Round(100*time.Millisecond))
			return
		}
		logf("自动进入：%s 在 %.0f 秒窗口内没找到可点的播放按钮，放弃（不影响游戏）",
			name, autoEnterWindow.Seconds())
	}()
}

// ── 监控墙鼠标穿透的引用计数 ────────────────────────────────────
//
// 多开时几个客户端几乎同时进入"自动进入"，各自都会要求墙穿透。
// 如果谁先点完谁就把墙还原，还在点的那几个立刻又会被墙挡住。
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
