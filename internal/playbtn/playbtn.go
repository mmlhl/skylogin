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
