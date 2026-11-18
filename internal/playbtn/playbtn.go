// Package playbtn 在光遇登录界面上找到播放按钮并点进去。
//
// 为什么单独成包：这是"看画面 + 点鼠标"的一小块能力，和签票、账号、界面
// 都没有关系；放进 app 包里会和登录流程互相纠缠，也没法单独测。
//
// 边界与安全约定（写死在包里，调用方不需要也不该关心）：
//   - 只在调用方给的 deadline 之前尝试；到点立刻停手，绝不在游戏里乱点
//   - 点之前必须确认按钮"白框"出现（悬停态），没白框就换下一轮
//   - 同一时刻只允许一个窗口在点（enterMu），多开时排队，避免抢焦点
//   - 只用 PrintWindow 读画面，不注入游戏进程、不读游戏内存
package playbtn

import (
	"sync"
	"time"

	"skylogin/internal/win"
)

const (
	// retryInterval 没找到按钮时的重试间隔。游戏登录界面出现后按钮
	// 有时会晚一两帧才画出来，给到 300ms 足够，又不会把 10 秒窗口耗光。
	retryInterval = 300 * time.Millisecond
	// firstClickGap 第一下点击（激活窗口）之后等游戏处理激活消息。
	firstClickGap = 220 * time.Millisecond
	// secondClickGap 第二次点击前重新把光标压到按钮上，留一点悬停时间。
	secondClickGap = 90 * time.Millisecond
	// clickWait 点完之后等游戏切画面，再做"有没有变化"的判定。
	clickWait = 500 * time.Millisecond
	// changeRatio 判定"画面已经变化"的像素比例阈值（约 5%）。
	changeRatio = 0.05
)

// enterMu 保证同一时刻只有一个窗口在被操作。
var enterMu sync.Mutex

// Logf 是日志函数类型，和 app 包的 logf 签名一致，方便直接透传。
type Logf func(format string, args ...any)

// Enter 在 deadline 之前尝试把 pid 这个客户端"点进游戏"。
//
// 返回 true 表示点击后画面确实变了（游戏开始加载/进入）；false 表示
// 到了 deadline 也没点成，或者画面一直没变化。调用方不需要处理错误。
func Enter(pid uint32, deadline time.Time, logf Logf) bool {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	for time.Now().Before(deadline) {
		if attempt(pid, logf) {
			return true
		}
		time.Sleep(retryInterval)
	}
	return false
}

// HasButton 只看不点：这个客户端当前的画面上有没有可点的播放按钮。
//
// 多开时用它等"第一个按钮出现"，避免把每个号的槽位浪费在空画面上。
func HasButton(pid uint32) bool {
	enterMu.Lock()
	defer enterMu.Unlock()
	hwnd := win.MainWindowOfPID(pid)
	if hwnd == 0 {
		return false
	}
	img, err := capture(hwnd)
	if err != nil || img == nil {
		return false
	}
	_, _, ok := locate(img)
	return ok
}

// EnterOnce 在 budget 内找到按钮并**只完成一次完整的点击**。
//
// 为什么一个窗口只点一次：点完之后游戏可能已经进去了，再点就是在大厅 /
// 商城界面上乱点，可能点到购买、丢弃之类，给用户造成实际损失。
// 所以这里只保证"找按钮"可以重试，"点"绝不复用同一个窗口。
//
// 返回 clicked 表示真的点下去了；gone 表示点完按钮消失（成功进入）。
func EnterOnce(pid uint32, budget, goneWindow time.Duration, logf Logf) (clicked, gone bool) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		found, clicked, gone := ClickOnce(pid, goneWindow, logf)
		if clicked {
			return true, gone
		}
		if found {
			// 找到了但被别的窗口挡住 / 没点下去：下一轮再试
		}
		time.Sleep(retryInterval)
	}
	return false, false
}

// TryClick 和 ClickOnce 一样只做一轮，但额外告诉调用方"排队等锁花了多久"。
//
// 多开时所有窗口共用一把 enterMu，一个窗口在等别人点的时候不该被扣自己的
// 计时预算（用户要求：等待的时间不算）。调用方拿到 waited 后把 deadline
// 往后推同样多即可。
func TryClick(pid uint32, goneWindow time.Duration, logf Logf) (found, clicked, gone bool, waited time.Duration) {
	start := time.Now()
	enterMu.Lock()
	waited = time.Since(start)
	defer enterMu.Unlock()
	found, clicked, gone = clickLocked(pid, goneWindow, logf)
	return found, clicked, gone, waited
}

// ClickOnce 做一次"找到按钮 → 点两下 → 等它消失"。
//
// found=false：这一轮没有可点的按钮（还在加载，或者已经进游戏）。
// clicked=true：真的点了；gone=true：点完在 goneWindow 内按钮从画面上消失。
func ClickOnce(pid uint32, goneWindow time.Duration, logf Logf) (found, clicked, gone bool) {
	enterMu.Lock()
	defer enterMu.Unlock()
	return clickLocked(pid, goneWindow, logf)
}

// clickLocked 是"看一帧 → 有按钮就点一次"的实现，调用方必须持有 enterMu。
func clickLocked(pid uint32, goneWindow time.Duration, logf Logf) (found, clicked, gone bool) {
	hwnd := win.MainWindowOfPID(pid)
	if hwnd == 0 {
		// 窗口还没建出来（多开时常见）
		return false, false, false
	}

	img, err := capture(hwnd)
	if err != nil || img == nil {
		return false, false, false
	}
	x, y, ok := locate(img)
	if !ok {
		return false, false, false
	}
	found = true

	// 点之前确认这一点真的落在游戏窗口上：监控墙（TOPMOST）或别的客户端
	// 盖住这块画面时，点下去只会落到挡路的窗口上。
	sx, sy := screenPointOf(hwnd, x, y)
	if !pointHitsProcess(pid, sx, sy) {
		activate(hwnd)
		if !pointHitsProcess(pid, sx, sy) {
			logf("自动进入：%s 的按钮被其它窗口挡住（可能是监控墙），本轮跳过", windowLabel(hwnd))
			return true, false, false
		}
	}

	// 第一下：多半只是把窗口/按钮激活（边框转白），但也有直接就进去的。
	click(hwnd, x, y)
	time.Sleep(firstClickGap)

	// 点完立刻看按钮还在不在：**已经进去了就绝不能再点第二下**。
	// 第二下会落在游戏大厅/商城界面上（同一个屏幕位置），可能点到购买、
	// 丢弃之类的按钮，给用户造成真实损失。这一条比"多点一下提高命中率"
	// 重要得多——宁可这个号没点进去，也不能在游戏里乱点。
	there, known := buttonStillThere(hwnd)
	if !known {
		logf("自动进入：%s 点完后看不清画面，为安全不再补点", windowLabel(hwnd))
		return true, true, false
	}
	if !there {
		logf("自动进入：%s 第一下就进去了（按钮已消失），不再补第二下", windowLabel(hwnd))
		return true, true, true
	}

	// 第二下：只在按钮仍然可见（还在登录界面）时才点。
	moveCursor(hwnd, x, y)
	time.Sleep(secondClickGap)
	click(hwnd, x, y)

	// 判定：按钮从画面上消失 = 已经进去了。这比"整屏像素变化"更贴用户
	// 看到的东西，也不受加载动画影响。
	gone = waitButtonGone(hwnd, goneWindow)
	if gone {
		logf("自动进入：%s 已进入游戏（按钮已消失）", windowLabel(hwnd))
		return true, true, true
	}
	logf("自动进入：%s 点了但按钮还在，本轮不再重试", windowLabel(hwnd))
	return true, true, false
}

// buttonStillThere 看一眼登录按钮还在不在。
//
// known=false 表示画面没抓到、判断不了——调用方应当按"不再补点"处理：
// 看不见画面时补点，等于闭着眼睛往游戏里点鼠标。
func buttonStillThere(hwnd uintptr) (there, known bool) {
	img, err := capture(hwnd)
	if err != nil || img == nil {
		return false, false
	}
	_, _, ok := locate(img)
	return ok, true
}

// waitButtonGone 在 window 内反复看画面，按钮消失就返回 true。
func waitButtonGone(hwnd uintptr, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		img, err := capture(hwnd)
		if err != nil || img == nil {
			continue
		}
		if _, _, ok := locate(img); !ok {
			return true
		}
	}
	return false
}

// attempt 做一轮"看一眼 → 有按钮就点"。
//
// 锁只保护一轮，不保护整个循环：多开时几个客户端几乎同时开始找按钮，
// 某个客户端一直找不到不能把别人的机会也占满 10 秒。
//
// 为什么固定点两下：实测（2026-11-16）第一次点击经常只起到"激活窗口/按钮"的
// 作用——按钮边框变白，但游戏没进；紧接着的第二下才真正触发进入。
// 所以先把"白框"从"点击的前置条件"改成了"点击后的观察结果"：
// 找到按钮就点，点完看画面变没变，变了即成功。
func attempt(pid uint32, logf Logf) bool {
	enterMu.Lock()
	defer enterMu.Unlock()

	hwnd := win.MainWindowOfPID(pid)
	if hwnd == 0 {
		// 窗口还没建出来（多开时常见）
		return false
	}

	img, err := capture(hwnd)
	if err != nil || img == nil {
		return false
	}
	x, y, ok := locate(img)
	if !ok {
		// 没有播放按钮：要么还在加载，要么已经进游戏了
		return false
	}

	// 点之前确认这一点真的落在游戏窗口上：监控墙（TOPMOST）或别的客户端
	// 盖住这块画面时，点下去只会落到挡路的窗口上。
	//
	// 顺序有意为之：先看命中，只有被挡住时才抢前台。这样窗口本来就在最上面时
	// 完全不碰 z-order——多开时七个客户端轮流抢前台会把整屏闪成一片。
	sx, sy := screenPointOf(hwnd, x, y)
	if !pointHitsProcess(pid, sx, sy) {
		activate(hwnd)
		if !pointHitsProcess(pid, sx, sy) {
			logf("自动进入：%s 的按钮被其它窗口挡住（可能是监控墙），本轮跳过", windowLabel(hwnd))
			return false
		}
	}

	// 第一下：多半只是把窗口/按钮激活（边框转白）。
	click(hwnd, x, y)
	time.Sleep(firstClickGap)

	// 第二下：这才是真正触发进入的那一下。中间保持悬停，别把光标挪走。
	moveCursor(hwnd, x, y)
	time.Sleep(secondClickGap)
	click(hwnd, x, y)
	time.Sleep(clickWait)

	after, err := capture(hwnd)
	if err != nil || after == nil {
		return false
	}
	if changed(img, after) {
		logf("自动进入：%s 已进入游戏", windowLabel(hwnd))
		return true
	}
	logf("自动进入：%s 连点两下仍未进入，下一轮再试", windowLabel(hwnd))
	return false
}
