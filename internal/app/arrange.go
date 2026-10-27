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
// mode = "cascade" 级联堆叠（文件窗式），"tile"/空 = 网格平铺。
// 全部就位后，3 个以上会自动打开监控墙（多开的主要查看方式）。
func arrangeWhenReady(cols, rows, w, h int, mode string) {
	if mode != "cascade" && (cols < 1 || rows < 1) {
		return
	}
	go func() {
		// 等第一个窗口出来就立刻开排（pid 启动瞬间就有，不能当信号）；
		// 探测间隔压到 300ms，窗口一创建马上就会被摆进格位
		deadline := time.Now().Add(3 * time.Minute)
		for time.Now().Before(deadline) {
			time.Sleep(300 * time.Millisecond)
			if len(runningPids()) == 0 {
				logf("自动排列：客户端都已退出，不排了")
				return
			}
			if win.CountClientWindows(runningPids()) >= 1 {
				break
			}
		}
		cells := clientCells()
		moved := win.ProgressiveArrange(cells, mode, cols, rows, w, h)
		if moved == 0 {
			logf("自动排列失败: 窗口找到了但移不动（可能权限不够）")
			return
		}
		logf("自动排列: %d/%d 个客户端已就位", moved, len(cells))
		// 游戏窗口最小 1024×720，3 个以上屏幕必然互相遮挡——排完自动把
		// 监控墙带出来
		if !win.WallOpen() && len(runningPids()) >= 3 {
			if _, err := ToggleWall(); err != nil {
				logf("自动打开监控墙失败: %v", err)
			} else {
				logf("已自动打开监控墙")
			}
		}
	}()
}

// fullscreenWhenReady 全屏：窗口不做任何移动（全屏排不了），等出来后把
// 监控墙带出来——全屏模式下切换客户端全靠它。
func fullscreenWhenReady() {
	go func() {
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
		if !win.WallOpen() && len(runningPids()) >= 3 {
			if _, err := ToggleWall(); err == nil {
				logf("已自动打开监控墙（全屏切换用）")
			}
		}
	}()
}
