package app

// 窗口排列服务：把正在运行的客户端窗口按网格铺到主屏工作区。
//
// 网格只用正方形的（2×2 / 3×3 / 4×4）：16:9 的屏幕均分成 n×n，
// 每格恰好还是 16:9，游戏画面不变形。两个窗口就是四分屏占两格。

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"skylogin/internal/fever"
	"skylogin/internal/win"
	"strings"
)

// runningPids 按启动顺序返回活着的客户端 pid（网格按这个顺序填充）。
func runningPids() []uint32 {
	clientsMu.Lock()
	type entry struct {
		pid uint32
		seq int64
	}
	list := make([]entry, 0, len(runningByAccount))
	for _, pid := range runningByAccount {
		if pid == 0 || !fever.ProcessAlive(pid) {
			continue
		}
		seq := int64(0)
		if s := clients[pid]; s != nil {
			seq = s.seq
		}
		list = append(list, entry{pid: pid, seq: seq})
	}
	clientsMu.Unlock()

	sort.Slice(list, func(i, j int) bool { return list[i].seq < list[j].seq })
	pids := make([]uint32, len(list))
	for i, e := range list {
		pids[i] = e.pid
	}
	return pids
}

// arrangeClients 立即把现有客户端窗口排进网格。
// w/h > 0 时用自定义窗口尺寸，否则每格 = 工作区均分。
// 顺带把每个窗口标题改成「序号 · 账号名」，不然一排窗口全叫"光遇"。
func arrangeClients(cols, rows, w, h int) (string, error) {
	pids := runningPids()
	if len(pids) == 0 {
		return "", fmt.Errorf("没有正在运行的客户端")
	}
	if win.CountClientWindows(pids) == 0 {
		return "", fmt.Errorf("客户端窗口还没出现（游戏可能还在启动）")
	}

	// pid → 账号显示名（窗口标题用）
	cells := clientCells()

	moved := win.ArrangeByGrid(cells, cols, rows, w, h)
	if moved == 0 {
		return "", fmt.Errorf("窗口没排动：全屏切不回窗口模式（详见日志）或权限不足")
	}
	msg := fmt.Sprintf("已把 %d 个客户端按 %d×%d 排列", moved, cols, rows)
	if w > 0 && h > 0 {
		msg += fmt.Sprintf("（窗口 %d×%d）", w, h)
	}
	return msg, nil
}

// clientCells 按启动顺序返回活着的客户端名单（整批排列和渐进排列共用）。
func clientCells() []win.ClientCell {
	pids := runningPids()
	labels := clientLabels()
	cells := make([]win.ClientCell, len(pids))
	for i, pid := range pids {
		cells[i] = win.ClientCell{PID: pid, Label: labels[pid]}
	}
	return cells
}

// clientLabels 返回 pid → 账号显示名（排列窗口标题和监控墙标签共用）。
func clientLabels() map[uint32]string {
	labels := map[uint32]string{}
	if accs, err := loadAccounts(); err == nil {
		clientsMu.Lock()
		for _, a := range accs {
			if pid, ok := runningByAccount[accountKey(a)]; ok && fever.ProcessAlive(pid) {
				labels[pid] = displayLabelOf(a)
			}
		}
		clientsMu.Unlock()
	}
	return labels
}

// arrangeRects 下一批要按"记录的位置"摆的矩形（空 = 按排列方式算）。
//
// 用全局 + 互斥而不是参数透传：排列是后台协程干的，它从启动那一刻起
// 就不知道自己是哪套组拉起来的；这里跟 nextFullscreen 是同一个套路，
// 都是"拉起前置位、排列时取走"。
var (
	arrangeRectsMu sync.Mutex
	arrangeRects   []win.TargetRect
)

func setArrangeRects(r []win.TargetRect) {
	arrangeRectsMu.Lock()
	arrangeRects = r
	arrangeRectsMu.Unlock()
}

func takeArrangeRects() []win.TargetRect {
	arrangeRectsMu.Lock()
	defer arrangeRectsMu.Unlock()
	r := arrangeRects
	arrangeRects = nil
	return r
}

// arrangeWhenReady 多开后渐进排列：客户端窗口出一个摆一个，不等全部出完
// （排在后面的要登录加载，等齐再排，前面的窗口得干瞪眼十几秒）。
// mode = "cascade" 级联堆叠（文件窗式），"fullscreen" 全屏（不排窗口，
// 排也排不动）；"rects" 按记录的位置摆（矩形由 setArrangeRects 预置）；
// "tile"/空 = 网格平铺。
// 全部就位后，3 个以上会自动打开监控墙（多开的主要查看方式，
// 全屏模式下切换客户端全靠它）。
func arrangeWhenReady(cols, rows, w, h int, mode string) {
	generation := groupLaunchGeneration.Load()
	originalCells := clientCells()
	originalPIDs := make([]uint32, len(originalCells))
	for i, c := range originalCells {
		originalPIDs[i] = c.PID
	}
	pendingRects := takeArrangeRects()
	go func() {
		// 排列结束（或提前放弃）就是"窗口这轮稳定了"：放行自动进入的等待方。
		// 放在 defer 里，任何 return 路径都不会把等待方挂死。
		defer releaseAutoEnterBatch()
		// 开墙也放 defer：排列失败 / 没有窗口可排时也要把墙打开
		// （否则"自动进入"会等一个永远不来的墙，用户看到的就是"墙有时不开"）。
		// defer 是后进先出，这里比 releaseAutoEnterBatch 晚注册 → 先执行。
		defer func() {
			if groupLaunchGeneration.Load() == generation {
				autoOpenWall()
			}
		}()
		if mode == "fullscreen" {
			// 等第一个窗口出来，再给游戏两秒自行恢复全屏
			deadline := time.Now().Add(3 * time.Minute)
			for time.Now().Before(deadline) {
				time.Sleep(1 * time.Second)
				if groupLaunchGeneration.Load() != generation {
					return
				}
				if len(liveArrangementCells(originalCells, generation, groupLaunchGeneration.Load(), fever.ProcessAlive)) == 0 {
					return
				}
				if win.CountClientWindows(originalPIDs) >= 1 {
					break
				}
			}
			time.Sleep(2 * time.Second)
			if groupLaunchGeneration.Load() == generation {
				autoOpenWall()
			}
			return
		}

		// 启动批次快照：这一批 pid 的"发票成功"作为排列的业务信号——
		// 票据发出且被游戏接受 = 凭证链路已通、客户端已站稳。比纯看窗口
		// 出现靠谱：窗口出现时引擎还在初始化，也分不清凭证有没有问题。
		batch := append([]uint32(nil), originalPIDs...)
		crashed := []string{}
		waitStart := time.Now()
		waitDeadline := time.Now().Add(3 * time.Minute)
		// 软期限：有客户端会卡在游戏自己的"网络异常，确认重试"弹窗上，
		// 一直不发票；不能因为它把整批排列拖到几分钟后（用户看到的是
		// "都玩了几分钟才排列"）。到点先用已就绪的窗口排。
		softDeadline := time.Now().Add(45 * time.Second)
		allSentAt := time.Time{}
		for time.Now().Before(waitDeadline) {
			time.Sleep(500 * time.Millisecond)
			if groupLaunchGeneration.Load() != generation {
				return
			}
			if len(batch) == 0 {
				return
			}
			st := batchStatus(batch)
			pending := 0 // 还活着但票还没发出去的
			for _, c := range st {
				if c.Alive && !c.TicketSent {
					pending++
				}
				if !c.Alive && !c.TicketSent && c.Name != "" {
					// 启动等待期进程消失 = 闪退（或被单实例拦截）。
					// 持续到超时的（凭证一直没通）排列兜底，不算闪退。
					crashed = append(crashed, c.Name)
				}
			}
			// 闪退的从批次里剔除，别让死 pid 挡住排列
			if alive := liveBatchPids(st); len(alive) < len(batch) {
				batch = alive
			}
			if pending > 0 && time.Now().After(softDeadline) {
				logf("自动排列：有 %d 个客户端 45 秒还没发完票（可能卡在游戏弹窗上），先排列已就绪的窗口", pending)
				break
			}
			if pending == 0 {
				// 存活的都发票了：再给 3 秒宽限（游戏消化票据、
				// 可能连续要几张票），然后开始排列
				if allSentAt.IsZero() {
					allSentAt = time.Now()
					logf("自动排列：全部客户端凭证链路已通，%d 个待排列（%d 个闪退）", len(batch), len(crashed))
				}
				if time.Since(allSentAt) >= 3*time.Second {
					break
				}
			} else {
				allSentAt = time.Time{}
			}
			if len(batch) == 0 {
				break // 全军覆没
			}
		}
		if len(crashed) > 0 {
			// 别写"可稍后单独再启动"——游戏是全局单实例，只要还有客户端在跑，
			// 这些号补不进去（点启动只会被拦）。实测：多开时掉的那些，只有在
			// 本轮全部退出之后才能单独再拉起来。
			setLaunchAlert("有客户端闪退",
				fmt.Sprintf("以下客户端在启动期间退出了：%s。常见原因：游戏单实例拦截、镜像被占用或凭证问题。"+
					"游戏是全局单实例：只要还有客户端在运行，这些号就补不进去；等本轮全部退出后（或点「停止运行」后）再单独启动它们。",
					strings.Join(dedupeStrings(crashed), "、")))
		}

		logf("自动排列：等待 %.0f 秒后开始摆放窗口", time.Since(waitStart).Seconds())
		cells := liveArrangementCells(originalCells, generation, groupLaunchGeneration.Load(), fever.ProcessAlive)
		if len(cells) == 0 {
			logf("自动排列：没有存活客户端，跳过")
			return
		}
		recordWindowSlots(cells)
		// 有记录的位置就按位置摆；没有就按排列方式算。
		// 位置模式的矩形已经在启动前 setArrangeRects 预置好了。
		var moved int
		if rects := pendingRects; len(rects) >= len(originalCells) && len(cells) == len(originalCells) {
			moved = win.ArrangeByRects(cells, rects)
		} else {
			moved = win.ProgressiveArrange(cells, mode, cols, rows, w, h)
		}
		if moved == 0 {
			logf("自动排列失败: 窗口没排动（全屏切不回窗口模式或权限不足），详见上面的诊断")
			return
		}
		logf("自动排列: %d/%d 个客户端已就位", moved, len(cells))
		hostMu.Lock()
		if groupLaunchGeneration.Load() == generation && len(liveArrangementCells(originalCells, generation, groupLaunchGeneration.Load(), fever.ProcessAlive)) > 0 {
			autoOpenWall()
		}
		hostMu.Unlock()
	}()
}

// liveArrangementCells 只允许原批次当前仍活着的窗口；世代变化后旧任务立即失效。
func liveArrangementCells(cells []win.ClientCell, generation, current uint64, alive func(uint32) bool) []win.ClientCell {
	if generation != current {
		return nil
	}
	out := make([]win.ClientCell, 0, len(cells))
	for _, c := range cells {
		if alive(c.PID) {
			out = append(out, c)
		}
	}
	return out
}

// liveBatchPids 从批次状态里挑出还活着的 pid。
func liveBatchPids(st []BatchClientStatus) []uint32 {
	out := make([]uint32, 0, len(st))
	for _, c := range st {
		if c.Alive {
			out = append(out, c.PID)
		}
	}
	return out
}

// dedupeStrings 保序去重。
func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// autoOpenWall 多开（2 个及以上）时自动打开监控墙：窗口一多就有遮挡，
// 监控墙是主要的查看与切换方式。
func autoOpenWall() {
	// 组级开关：有的用户就是不想让墙挡在游戏前面
	if skipAutoWall.Load() {
		return
	}
	if !win.WallOpen() && len(runningPids()) >= 2 {
		if _, err := ToggleWall(); err != nil {
			logf("自动打开监控墙失败: %v", err)
			return
		}
		// 以前只看 ToggleWall 的返回值就报"已打开"，可窗口可能根本没建出来。
		// 这里再确认一次，没起来就明确记一笔（自动进入前还会再试）。
		for i := 0; i < 20 && !win.WallOpen(); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if win.WallOpen() {
			logf("已自动打开监控墙")
		} else {
			logf("自动打开监控墙：调用了但墙没起来，稍后自动进入会再试一次")
		}
	}
}
