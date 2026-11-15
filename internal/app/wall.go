package app

// 监控墙管理：开关切换 + 开着时把客户端观测每秒喂给墙。
//
// 墙窗口本身在 internal/win（原生窗口 + DWM 缩略图）。这里仅投递只读观测，
// 墙线程比较已应用状态：内容未变就不重绘、不重注册缩略图、不改变窗口。
// 画面由 DWM 持续合成；墙开启时把用户最小化转换为全透明、鼠标穿透的后台收起，
// 以保留渲染，点选或手动重排才恢复可见，不在周期观测中反复恢复窗口。

import (
	"fmt"
	"sync"
	"time"

	"skylogin/internal/fever"
	"skylogin/internal/win"
)

var (
	wallMu       sync.Mutex
	wallPoll     chan struct{} // 非 nil = 轮询中
	wallHooksOne sync.Once
)

// ensureWallHooks 把墙需要的 app 层交互点注入 win 层（开墙前调用一次）。
func ensureWallHooks() {
	wallHooksOne.Do(func() {
		win.SetWallHooks(win.WallHooks{
			SavedRect: func() (x, y, w, h int, ok bool) {
				c := loadConfig()
				if c.WallW > 0 && c.WallH > 0 {
					return c.WallX, c.WallY, c.WallW, c.WallH, true
				}
				return 0, 0, 0, 0, false
			},
			RectSink: func(x, y, w, h int) { _ = saveWallRect(x, y, w, h) },
			Actions: map[string]func(){
				"tile":    func() { _ = wallArrange("tile") },
				"dual":    func() { _ = wallArrange("cascade:" + win.SchemeDual) },
				"rows":    func() { _ = wallArrange("cascade:" + win.SchemeRows) },
				"cols":    func() { _ = wallArrange("cascade:" + win.SchemeCols) },
				"fan":     func() { _ = wallArrange("cascade:" + win.SchemeFan) },
				"full":    func() { _ = wallArrange("fullscreen") },
				"mask":    func() { setMaskTitlesOn(!maskEnabled()) },
				"stopall": wallStopAll,
			},
			StopClient: wallStopClient,
			SaveTarget: wallSaveTarget,
			SaveGroup:  wallSaveGroup,
		})
	})
}

// wallArrange 监控墙快捷菜单：立即重排当前客户端窗口。
// tile = 自适应正方形网格（16:9 不变形）；cascade[:scheme] = 堆叠方案；
// fullscreen = 每个客户端各占满整屏（叠在一起，用监控墙切换）。
// 一次性摆放，不带渐进排列的漂移回夹——用户手动挪窗口不该被弹回来。
func wallArrange(mode string) error {
	cells := clientCells()
	if len(cells) == 0 {
		return fmt.Errorf("没有正在运行的客户端")
	}
	info := win.CurrentLayoutPlan(win.LayoutRequest{Count: len(cells), Mode: mode})
	if info.Reason != "" {
		setLaunchAlert("排列方案已调整", info.Reason)
	}
	mode = info.Mode
	win.SetWallLayoutMode(mode)
	var moved int
	if mode == "fullscreen" {
		moved = win.ArrangeFullscreen(cells)
	} else if isCascade, scheme := win.SplitCascadeMode(mode); isCascade {
		moved = win.ArrangeByScheme(cells, scheme, 0, 0)
	} else {
		moved = win.ArrangeByGrid(cells, 0, 0, 0, 0)
	}
	if moved == 0 {
		return fmt.Errorf("窗口没排动：全屏切不回窗口模式（详见日志）或权限不足")
	}
	logf("监控墙：快捷菜单重排（%s），%d 个客户端", mode, moved)
	return nil
}

// wallStopAll 监控墙快捷菜单：强行停止全部运行中的客户端。
func wallStopAll() {
	clientsMu.Lock()
	ids := make([]string, 0, len(runningByAccount))
	for id, pid := range runningByAccount {
		if pid != 0 && fever.ProcessAlive(pid) {
			ids = append(ids, id)
		}
	}
	clientsMu.Unlock()
	for _, id := range ids {
		_ = stopAccount(id)
	}
}

// wallStopClient 监控墙格子右上角 ×：按 pid 找到账号并强行停止。
func wallStopClient(pid uint32) {
	clientsMu.Lock()
	var accID string
	for id, p := range runningByAccount {
		if p == pid {
			accID = id
			break
		}
	}
	clientsMu.Unlock()
	if accID == "" {
		return // 窗口还在但路由已回收（正在退出），不必再停
	}
	_ = stopAccount(accID)
}

// ToggleWall 开着就关、关着就开，返回给界面显示的消息。
func ToggleWall() (string, error) {
	ensureWallHooks()
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

// wallRefresh 把当前活着的客户端观测全量推给墙，返回推上去的数量。
//
// 监控源 = 客户端主窗口（画面是游戏本身）；登录骨架 / 渠道弹窗开着时
// 墙仍显示主窗（表面挂起，episode 收尾后墙线程自动重建接回活画面）。
func wallRefresh() int {
	pids := runningPids()
	if len(pids) == 0 {
		// 客户端全没了：把空列表推给墙清掉旧格子——不推的话墙上会
		// 永远留着源窗口已消失的白块
		win.WallUpdate(nil)
		return 0
	}
	labels := clientLabels()
	list := make([]win.WallClient, 0, len(pids))
	for _, pid := range pids {
		main := win.MainWindowOfPID(pid)
		if main == 0 {
			continue
		}
		list = append(list, win.WallClient{
			Hwnd:  main,
			Pid:   pid,
			Label: labels[pid],
			Ratio: win.WindowAspect(main),
		})
	}
	win.WallUpdate(list)
	return len(list)
}
