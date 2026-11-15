package win

import (
	"fmt"
	"sort"
	"sync"
	"syscall"
)

// 每次枚举只关注各客户端的主窗口，登录辅助窗和同进程弹窗不能挤占层级。
var clientZEnum struct {
	sync.Mutex
	wanted map[uintptr]uint32
	top    []uintptr
}

var clientZCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
	if _, ok := clientZEnum.wanted[hwnd]; ok {
		clientZEnum.top = append(clientZEnum.top, hwnd)
	}
	return 1
})

// clientZRanks 把系统从顶到底的枚举转换为客户端内部从底一到顶 N 的排名。
func clientZRanks(top []uintptr, wanted map[uintptr]uint32) map[uint32]int {
	out := make(map[uint32]int, len(wanted))
	for i := len(top) - 1; i >= 0; i-- {
		pid, ok := wanted[top[i]]
		if !ok || pid == 0 || out[pid] != 0 {
			continue
		}
		out[pid] = len(out) + 1
	}
	return out
}

// ClientZOrders 读取客户端主窗口的实际前后遮挡关系，缺失窗口不产生伪造的层级。
func ClientZOrders(cells []ClientCell) map[uint32]int {
	wanted := make(map[uintptr]uint32, len(cells))
	for _, c := range cells {
		if c.PID != 0 {
			if hwnd := MainWindowOfPID(c.PID); hwnd != 0 {
				wanted[hwnd] = c.PID
			}
		}
	}
	clientZEnum.Lock()
	defer clientZEnum.Unlock()
	clientZEnum.wanted, clientZEnum.top = wanted, nil
	procEnumWindows.Call(clientZCallback, 0)
	ranks := clientZRanks(clientZEnum.top, wanted)
	clientZEnum.wanted, clientZEnum.top = nil, nil
	return ranks
}

const clientZRestoreFlags = swpNoMove | swpNoSize | swpNoActivate | 0x0200 // 不改变拥有者窗口的层级

// restoreClientZOrderWith 使用明确的插入链，先定位最前窗口，再把其余窗口逐个插到前窗之后。
// 不能只假定多次 HWND_TOP 的调用顺序一定等于最后的系统层级。
func restoreClientZOrderWith(cells []ClientCell, mainWindow func(uint32) uintptr, move func(uintptr, uintptr, uintptr) bool) int {
	seen := make(map[uint32]bool, len(cells))
	var handles []uintptr
	for _, c := range cells {
		if c.PID == 0 || seen[c.PID] {
			continue
		}
		seen[c.PID] = true
		if hwnd := mainWindow(c.PID); hwnd != 0 {
			handles = append(handles, hwnd)
		}
	}
	moved, after := 0, uintptr(0)
	for i := len(handles) - 1; i >= 0; i-- {
		if move(handles[i], after, clientZRestoreFlags) {
			moved++
			after = handles[i]
		}
	}
	return moved
}

// plannedClientZCells 只改变执行层级的副本，不能改变坐标、启动次序或窗口编号。
func plannedClientZCells(cells []ClientCell, rects []TargetRect) []ClientCell {
	order := make([]int, len(cells))
	for i := range order {
		order[i] = i
	}
	z := func(i int) int {
		if i < len(rects) && rects[i].ZOrder > 0 {
			return rects[i].ZOrder
		}
		return i + 1
	}
	sort.SliceStable(order, func(i, j int) bool { return z(order[i]) < z(order[j]) })
	out := make([]ClientCell, len(cells))
	for i, at := range order {
		out[i] = cells[at]
	}
	return out
}

func clientZOrderMatches(cells []ClientCell, ranks map[uint32]int) bool {
	seen := make(map[uint32]bool)
	last := 0
	for _, c := range cells {
		if c.PID == 0 || seen[c.PID] {
			continue
		}
		seen[c.PID] = true
		rank := ranks[c.PID]
		if rank <= last {
			return false
		}
		last = rank
	}
	return last > 0
}

func restoreClientZOrderVerifiedWith(cells []ClientCell, mainWindow func(uint32) uintptr, move func(uintptr, uintptr, uintptr) bool, readRanks func() map[uint32]int) int {
	moved := 0
	for attempt := 0; attempt < 2; attempt++ {
		moved = restoreClientZOrderWith(cells, mainWindow, move)
		if clientZOrderMatches(cells, readRanks()) {
			return moved
		}
	}
	return 0
}

// RestoreClientZOrder 接受从底到顶的客户端顺序，只恢复层级，不激活、不移动、不缩放窗口。
// 原生调用后读取真实 EnumWindows 排名核对，失败时不再假报成功。
func RestoreClientZOrder(cells []ClientCell) int {
	// 置顶投到墙线程后再检查菜单状态，避免后台已通过检查的调用在菜单打开后才落地。
	defer func() {
		postWallTask(func() { pinWallAboveClients(hwndOfWall()) })
	}()
	live := make([]ClientCell, 0, len(cells))
	for _, c := range cells {
		if c.PID != 0 && MainWindowOfPID(c.PID) != 0 {
			live = append(live, c)
		}
	}
	moved := restoreClientZOrderVerifiedWith(live, MainWindowOfPID, func(hwnd, after, flags uintptr) bool {
		ok, _, _ := procSetWindowPos.Call(hwnd, after, 0, 0, 0, 0, flags)
		return ok != 0
	}, func() map[uint32]int { return ClientZOrders(live) })
	if moved == 0 && len(live) > 0 {
		Diag(fmt.Sprintf("窗口层级恢复未通过原生排名核对（%d个窗口），可能有窗口置顶属性或权限限制", len(live)))
	}
	return moved
}

// RestorePlannedClientZOrder 由坐标计划的 zOrder 决定遮挡关系，原 cells 次序保留给几何与标题编号。
func RestorePlannedClientZOrder(cells []ClientCell, rects []TargetRect) int {
	return RestoreClientZOrder(plannedClientZCells(cells, rects))
}
