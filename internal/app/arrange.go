package app

// 窗口排列服务：把正在运行的客户端窗口按网格铺到主屏工作区。
//
// 网格只用正方形的（2×2 / 3×3 / 4×4）：16:9 的屏幕均分成 n×n，
// 每格恰好还是 16:9，游戏画面不变形。两个窗口就是四分屏占两格。

import (
	"fmt"
	"sort"
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
	if cols < 1 || rows < 1 {
		return "", fmt.Errorf("排列参数不对")
	}
	pids := runningPids()
	if len(pids) == 0 {
		return "", fmt.Errorf("没有正在运行的客户端")
	}
	if cols*rows < len(pids) {
		return "", fmt.Errorf("%d 个窗口放不进 %d×%d 的网格", len(pids), cols, rows)
	}
	if win.CountClientWindows(pids) == 0 {
		return "", fmt.Errorf("客户端窗口还没出现（游戏可能还在启动）")
	}

	// pid → 账号显示名（窗口标题用）
	cells := clientCells()

	moved := win.ArrangeByGrid(cells, cols, rows, w, h)
	if moved == 0 {
		return "", fmt.Errorf("窗口找到了但移不动（可能权限不够）")
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
			if pid, ok := runningByAccount[a.PlayerID]; ok && fever.ProcessAlive(pid) {
				labels[pid] = displayName(a)
			}
		}
		clientsMu.Unlock()
	}
	return labels
}

// arrangeWhenReady 多开后渐进排列：客户端窗口出一个摆一个，不等全部出完
// （排在后面的要登录加载，等齐再排，前面的窗口得干瞪眼十几秒）。
// mode = "cascade" 级联堆叠（文件窗式），"fullscreen" 全屏（不排窗口，
// 排也排不动）；"tile"/空 = 网格平铺。
// 全部就位后，3 个以上会自动打开监控墙（多开的主要查看方式，
// 全屏模式下切换客户端全靠它）。
func arrangeWhenReady(cols, rows, w, h int, mode string) {
	isCascade, _ := win.SplitCascadeMode(mode)
	if !isCascade && mode != "fullscreen" && (cols < 1 || rows < 1) {
		return
	}
	go func() {
		if mode == "fullscreen" {
			// 等第一个窗口出来，再给游戏两秒自行恢复全屏
			deadline := time.Now().Add(3 * time.Minute)
			for time.Now().Before(deadline) {
				time.Sleep(1 * time.Second)
				if len(runningPids()) == 0 {
					return
				}
				if win.CountClientWindows(runningPids()) >= 1 {
					break
				}
			}
			time.Sleep(2 * time.Second)
			autoOpenWall()
			return
		}

		// 启动批次快照：这一批 pid 的"发票成功"作为排列的业务信号——
		// 票据发出且被游戏接受 = 凭证链路已通、客户端已站稳。比纯看窗口
		// 出现靠谱：窗口出现时引擎还在初始化，也分不清凭证有没有问题。
		batch := runningPids()
		crashed := []string{}
		waitDeadline := time.Now().Add(3 * time.Minute)
		allSentAt := time.Time{}
		for time.Now().Before(waitDeadline) {
			time.Sleep(500 * time.Millisecond)
			if len(runningPids()) == 0 && len(batch) == 0 {
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
			setLaunchAlert("有客户端闪退",
				fmt.Sprintf("以下客户端在启动期间退出了：%s。常见原因：游戏单实例拦截、镜像被占用或凭证问题。可稍后单独再启动它们。",
					strings.Join(dedupeStrings(crashed), "、")))
		}

		cells := clientCells()
		if len(cells) == 0 {
			logf("自动排列：没有存活客户端，跳过")
			return
		}
		moved := win.ProgressiveArrange(cells, mode, cols, rows, w, h)
		if moved == 0 {
			logf("自动排列失败: 窗口找到了但移不动（可能权限不够）")
			return
		}
		logf("自动排列: %d/%d 个客户端已就位", moved, len(cells))
		autoOpenWall()
	}()
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

// autoOpenWall 3 个以上客户端时自动打开监控墙：游戏窗口最小 1024×720，
// 3 个以上屏幕必然互相遮挡。
func autoOpenWall() {
	if !win.WallOpen() && len(runningPids()) >= 3 {
		if _, err := ToggleWall(); err != nil {
			logf("自动打开监控墙失败: %v", err)
		} else {
			logf("已自动打开监控墙")
		}
	}
}
