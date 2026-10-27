package app

// 鼠标仲裁：多开启动后的一段时间里（多开面板可配，默认 3 分钟），处理
// "后台客户端自认持有鼠标"的问题。
//
// 机制：多开几乎同时点登录，后台启动的客户端从未被激活过——收不到
// 失活通知，引擎的焦点标志一直停在"我有焦点"，锁鼠标持续生效（拉光标
// 回窗口中心 + 视角跟着鼠标转）。
//
// 原则：绝不主动切换焦点，绝不给前台窗口发任何通知——多开的游戏窗口
// 尺寸位置完全重叠，任何抢焦点动作都会让鼠标"跳到另一个游戏"，而误发
// 的失活通知会让正在玩的游戏解锁鼠标（视角变光标）。
//
// 处置只有一种：给"确定不在前台"的后台客户端补发一组失活消息，让它的
// 引擎自己解锁。触发条件两条，都要求确凿：
//   - 光标反复被钉在某个后台客户端窗口中心（采样过半命中）
//   - 系统焦点事件显示它在请求焦点/鼠标捕获，且 400ms 后复核仍不在前台
// 每个窗口有 5 秒冷却与次数上限，避免刷屏和反复打扰。

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"skylogin/internal/win"
)

// arbiterUntil 仲裁窗口的截止时刻（unix 秒，0 = 关闭）。每次多开启动时重置。
var arbiterUntil atomic.Int64

const (
	arbiterDefaultMin = 3
	notifyCooldown    = 5 * time.Second // 同一窗口两次通知的最小间隔
	notifyMaxPerWin   = 8               // 单次多开里同一窗口最多通知几次
	fgGrace           = 2 * time.Second // 窗口刚当前台过，视为在用，不打扰
)

var (
	notifyMu     sync.Mutex
	notifyLastAt = map[uintptr]time.Time{}
	notifyCount  = map[uintptr]int{}
	lastFgAt     = map[uint32]time.Time{}
)

// ArmMouseArbiter 多开启动时调用：按设置武装仲裁窗口，并重置计数。
func ArmMouseArbiter() {
	notifyMu.Lock()
	notifyLastAt = map[uintptr]time.Time{}
	notifyCount = map[uintptr]int{}
	lastFgAt = map[uint32]time.Time{}
	notifyMu.Unlock()

	cfg := loadConfig()
	if !cfg.MouseArbiterEnabled {
		arbiterUntil.Store(0)
		return
	}
	min := cfg.MouseArbiterMin
	if min <= 0 {
		min = arbiterDefaultMin
	}
	arbiterUntil.Store(time.Now().Add(time.Duration(min) * time.Minute).Unix())
	logf("鼠标仲裁已启用，持续 %d 分钟", min)
}

func arbiterActive() bool {
	u := arbiterUntil.Load()
	return u != 0 && time.Now().Unix() <= u
}

// StartMouseArbiter 常驻巡检 goroutine（窗口外空转），同时挂上实时的
// 焦点事件监听。
func StartMouseArbiter() {
	win.StartFocusWatch(onFocusEvent)
	go func() {
		for {
			win.SleepMs(1000)
			arbiterCycle()
		}
	}()
}

func markForeground(pid uint32) {
	notifyMu.Lock()
	lastFgAt[pid] = time.Now()
	notifyMu.Unlock()
}

func lastForegroundAt(pid uint32) time.Time {
	notifyMu.Lock()
	defer notifyMu.Unlock()
	return lastFgAt[pid]
}

// notifyInactiveOnce 给一个"确定不在前台"的受管客户端补发失活通知。
// 前台检查、冷却、次数上限全部在此集中把关；返回是否真的发了。
func notifyInactiveOnce(pid uint32, h uintptr, why string) bool {
	if h == 0 {
		return false
	}
	// 它的任何一个窗口在前台 = 用户正在用它，绝不打扰。
	// 前台读不清（切换瞬态）时也按"可能在前台"处理，宁可晚一轮。
	fgHwnd, fgPid := win.ForegroundWindow()
	if fgHwnd == 0 || fgPid == pid {
		return false
	}
	notifyMu.Lock()
	cooling := time.Since(notifyLastAt[h]) < notifyCooldown
	over := notifyCount[h] >= notifyMaxPerWin
	if !cooling && !over {
		notifyLastAt[h] = time.Now()
		notifyCount[h]++
	}
	notifyMu.Unlock()
	if cooling || over {
		return false
	}
	win.NotifyInactive(h)
	logf("鼠标仲裁：%s %s，已补发失焦通知", displayNameOfPid(pid), why)
	return true
}

// arbiterCycle 兜底巡检：采样光标，被反复钉在某个后台客户端窗口中心
// = 它在把光标拉回中心（自认持有鼠标），给它补发一次失活通知。
func arbiterCycle() {
	if !arbiterActive() {
		return
	}
	pids := runningPids()
	if len(pids) < 2 {
		return
	}
	fgHwnd, fgPid := win.ForegroundWindow()
	if fgHwnd == 0 {
		return // 前台读不清，这一轮不判断
	}

	type cl struct {
		pid    uint32
		hwnd   uintptr
		cx, cy int32
	}
	cls := make([]cl, 0, len(pids))
	for _, pid := range pids {
		h := win.MainWindowOfPID(pid)
		if h == 0 {
			continue
		}
		if cx, cy, ok := win.WindowInputCenter(h); ok {
			cls = append(cls, cl{pid: pid, hwnd: h, cx: cx, cy: cy})
		}
	}
	if len(cls) < 2 {
		return
	}

	hits := map[uintptr]int{}
	total := 0
	for i := 0; i < 6; i++ {
		if x, y, ok := win.CursorPos(); ok {
			total++
			for _, c := range cls {
				if near2(x, y, c.cx, c.cy, 8) {
					hits[c.hwnd]++
				}
			}
		}
		win.SleepMs(90)
	}
	if total < 4 {
		return
	}
	for _, c := range cls {
		if c.pid == fgPid || hits[c.hwnd]*2 < total {
			continue // 前台那只 / 命中不过半：不算占用
		}
		if time.Since(lastForegroundAt(c.pid)) < fgGrace {
			continue // 刚当前台过，多半是切换中的瞬态
		}
		notifyInactiveOnce(c.pid, c.hwnd, "在后台自认持有鼠标")
	}
}

// onFocusEvent WinEventHook 回调（系统焦点/捕获事件，实时）。
// 只做轻量判断，重活异步做；绝不碰前台窗口、绝不切换焦点。
func onFocusEvent(event uint32, root, pid uintptr) {
	if !arbiterActive() {
		return
	}
	p := uint32(pid)

	switch event {
	case win.EvSystemForeground:
		if p == 0 {
			return
		}
		markForeground(p)
		// 钩子回调是排队投递的，事件可能已经落后于现状：前台马上又换了
		// 人（比如点墙格子 → 墙先激活 → 游戏立刻接走），这条事件作废。
		// 否则会把刚接管的游戏当成"后台"通知掉。
		if _, cur := win.ForegroundWindow(); cur != p {
			return
		}
		// 前台变化：其余受管客户端都该知道自己不在前台了。这是"从未被
		// 通知"的欠账最主要的还清时机，且对象全是真正的后台窗口。
		for _, x := range runningPids() {
			if x == p {
				continue
			}
			if h := win.MainWindowOfPID(x); h != 0 {
				notifyInactiveOnce(x, h, "前台已切换到别的窗口")
			}
		}

	case win.EvObjectFocus, win.EvSystemCaptureStart:
		managed := false
		for _, x := range runningPids() {
			if x == p {
				managed = true
				break
			}
		}
		if !managed {
			return
		}
		// 请求焦点的那一刻前台还没切换完，立刻判断会误伤正在玩的游戏。
		// 延迟复核：仍然不在前台、且光标被钉在它窗口中心，才认定是
		// 后台客户端在自认持有鼠标。
		go func(pid uint32) {
			win.SleepMs(400)
			if !arbiterActive() {
				return
			}
			if fgHwnd, fgPid := win.ForegroundWindow(); fgHwnd == 0 || fgPid == pid {
				return
			}
			if time.Since(lastForegroundAt(pid)) < fgGrace {
				return
			}
			h := win.MainWindowOfPID(pid)
			if h == 0 {
				return
			}
			cx, cy, ok := win.WindowInputCenter(h)
			if !ok {
				return
			}
			x, y, ok := win.CursorPos()
			if !ok || !near2(x, y, cx, cy, 8) {
				return // 光标不在它中心，没人被锁在里面
			}
			notifyInactiveOnce(pid, h, "在后台请求输入焦点")
		}(p)
	}
}

// displayNameOfPid pid → 账号显示名（日志用，拿不到就退回 pid）。
func displayNameOfPid(pid uint32) string {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if s := clients[pid]; s != nil && s.acc != nil {
		return displayName(*s.acc)
	}
	return fmt.Sprintf("pid=%d", pid)
}

// near2 两点距离是否都在容差内。
func near2(ax, ay, bx, by int32, tol int32) bool {
	dx, dy := ax-bx, ay-by
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	return dx <= tol && dy <= tol
}
