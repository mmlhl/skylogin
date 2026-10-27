package app

// 监控墙管理：开关切换 + 开着时把活着的客户端实时喂给墙。
//
// 墙窗口本身在 internal/win（原生窗口 + DWM 缩略图）；这里只负责
// 每 2 秒枚举一次正在运行的客户端（排列同款的顺序和标签），全量
// 推给墙。窗口关了轮询自动停，客户端增减、退出都会跟着变。

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"skylogin/internal/win"
)

var (
	wallMu   sync.Mutex
	wallPoll chan struct{} // 非 nil = 轮询中
)

// ToggleWall 开着就关、关着就开，返回给界面显示的消息。
func ToggleWall() (string, error) {
	wallMu.Lock()
	defer wallMu.Unlock()
	if win.WallOpen() {
		stopWallLocked()
		win.CloseWall()
		logf("监控墙已关闭")
		return "监控墙已关闭", nil
	}
	// 上一轮的轮询可能还挂着（用户直接点了墙的 ✕），先收掉
	if wallPoll != nil {
		close(wallPoll)
		wallPoll = nil
	}
	if err := win.OpenWall(); err != nil {
		logf("监控墙打开失败: %v", err)
		return "", fmt.Errorf("监控墙窗口创建失败: %w", err)
	}
	n := wallRefresh()
	poll := make(chan struct{})
	wallPoll = poll
	go func() {
		for {
			select {
			case <-poll:
				return
			case <-time.After(1 * time.Second):
			}
			if !win.WallOpen() {
				return // 用户从墙的标题栏关的，轮询自己收尾
			}
			wallRefresh()
		}
	}()
	logf("监控墙已打开（%d 个客户端）", n)
	return fmt.Sprintf("监控墙已打开（%d 个客户端）", n), nil
}

// stopWallLocked 停掉轮询（调用方持有 wallMu）。
func stopWallLocked() {
	if wallPoll != nil {
		close(wallPoll)
		wallPoll = nil
	}
}

// wallRefresh 把当前活着的客户端窗口全量推给墙，返回推上去的数量。
//
// 监控源 = 该客户端 Z 序最上层的可见窗口：用户眼睛看到什么，墙就显示
// 什么——登录骨架/渠道弹窗开着时显示它们（它们盖在游戏上面），关了自动
// 回到主窗口。格子布局比例固定按主窗口算，源窗口不同就在格子里等比
// 居中，布局不跳。
func wallRefresh() int {
	pids := runningPids()
	if len(pids) == 0 {
		lastWinCount = map[uint32]int{}
		return 0
	}

	// 指纹 = 全部客户端的主窗句柄 + 各自窗口数 + 账号文件修改时间。
	// 指纹没变 = 屏幕上什么都没发生，整轮跳过（每秒巡检的大头就省了）。
	type item struct {
		pid   uint32
		main  uintptr
		count int
	}
	items := make([]item, 0, len(pids))
	fp := []string{}
	for _, pid := range pids {
		main := win.MainWindowOfPID(pid)
		if main == 0 {
			continue
		}
		count := len(win.ZOrderedWindowsOfPid(pid, 200, 150))
		fp = append(fp, fmt.Sprintf("%d:%#x:%d", pid, main, count))
		items = append(items, item{pid: pid, main: main, count: count})
	}
	if mod := accountsModTime(); !mod.IsZero() {
		fp = append(fp, mod.Format("20060102150405"))
	}
	fingerprint := strings.Join(fp, "|")
	if fingerprint == lastFingerprint {
		return len(items)
	}
	lastFingerprint = fingerprint

	labels := clientLabels()
	list := make([]win.WallClient, 0, len(items))
	for _, it := range items {
		// 窗口数量变化（登录窗/弹窗出现或消失）→ 该实例缩略图重建一次，
		// 让 DWM 重新绑定主窗表面；登录窗本身由系统合成显示
		rebind := lastWinCount[it.pid] != it.count
		if rebind {
			lastWinCount[it.pid] = it.count
			logf("监控墙：pid=%d 窗口数量变为 %d，缩略图重新渲染", it.pid, it.count)
		}
		list = append(list, win.WallClient{
			Hwnd:   it.main,
			Label:  labels[it.pid],
			Ratio:  win.WindowAspect(it.main),
			Rebind: rebind,
		})
	}
	win.WallUpdate(list)
	return len(list)
}

var lastFingerprint string

var lastWinCount = map[uint32]int{}

// accountsModTime 账号文件的修改时间（标签缓存失效判定用），拿不到返回零值。
func accountsModTime() time.Time {
	fi, err := os.Stat(accountsPath())
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}
