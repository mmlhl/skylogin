package app

// 监控墙只保存当前仍在运行的账号快照，不从账号文件重新推算本次参数。
import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"skylogin/internal/fever"
	"skylogin/internal/win"
)

var wallGroupContext struct {
	sync.Mutex
	id   string
	pids map[string]uint32
}

// rememberWallGroup 在组成功启动后调用；PID 也是归属的一部分，重启同账号不能沿用旧归属。
func rememberWallGroup(g LaunchGroup, started []Account) {
	pids := make(map[string]uint32, len(started))
	clientsMu.Lock()
	for _, a := range started {
		key := accountKey(a)
		if pid := runningByAccount[key]; pid != 0 {
			pids[key] = pid
		}
	}
	clientsMu.Unlock()
	setWallGroupContext(g.ID, pids)
}

func setWallGroupContext(id string, pids map[string]uint32) {
	wallGroupContext.Lock()
	defer wallGroupContext.Unlock()
	wallGroupContext.id, wallGroupContext.pids = id, pids
}

// wallLaunchSettings 固化旧多开入口的临时覆盖，让路由保存的账号副本反映实际启动参数。
func wallLaunchSettings(a Account, mode string, fps int) Account {
	a.QualityMode, a.QualityFps = effectiveQuality(a, mode, fps)
	if inMultiBatch.Load() {
		a.DisplayMode = "windowed"
		if nextFullscreen.Load() {
			a.DisplayMode = "fullscreen"
		}
	}
	return a
}

type wallRunningSlot struct {
	key string
	pid uint32
	seq int64
	acc Account
}

func wallRunningSlots() []wallRunningSlot {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	var out []wallRunningSlot
	for key, pid := range runningByAccount {
		if pid == 0 || !fever.ProcessAlive(pid) {
			continue
		}
		s := wallRunningSlot{key: key, pid: pid}
		if slot := clients[pid]; slot != nil && slot.acc != nil {
			s.acc, s.seq = *slot.acc, slot.seq
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].seq != out[j].seq {
			return out[i].seq < out[j].seq
		}
		return out[i].key < out[j].key
	})
	return out
}

func wallOwnedGroup(running []wallRunningSlot) string {
	wallGroupContext.Lock()
	defer wallGroupContext.Unlock()
	if len(running) == 0 || wallGroupContext.id == "" {
		return ""
	}
	for _, s := range running {
		if wallGroupContext.pids[s.key] != s.pid {
			return ""
		}
	}
	return wallGroupContext.id
}

// wallSaveTarget 为原生菜单明确显示当前归属；被删除的组不能被覆盖保存意外复活。
func wallSaveTarget() (string, string, bool) {
	id := wallOwnedGroup(wallRunningSlots())
	if id == "" {
		return "", "", false
	}
	groupsMu.Lock()
	defer groupsMu.Unlock()
	list, err := loadGroups()
	if err == nil {
		for _, g := range list {
			if g.ID == id {
				return g.ID, g.Name, true
			}
		}
	}
	return id, "", false
}

// mergeWallSlots 按账号键更新，未运行及禁用成员保持原样；换屏时只撤销旧位置的生效标记。
func mergeWallSlots(g LaunchGroup, captured []GroupSlot, screenW, screenH int) LaunchGroup {
	g.Slots = append([]GroupSlot(nil), g.Slots...)
	byKey := make(map[string]GroupSlot, len(captured))
	for _, s := range captured {
		byKey[s.AccountKey] = s
	}
	for i, old := range g.Slots {
		if s, ok := byKey[old.AccountKey]; ok && !old.Disabled {
			g.Slots[i] = s
		} else if g.ScreenW != screenW || g.ScreenH != screenH {
			g.Slots[i].HasRect = false
		}
		delete(byKey, old.AccountKey)
	}
	for _, s := range captured {
		if _, ok := byKey[s.AccountKey]; ok {
			g.Slots = append(g.Slots, s)
			delete(byKey, s.AccountKey)
		}
	}
	g.ScreenW, g.ScreenH, g.GapSec = screenW, screenH, 0
	// 捕获层级只决定遮挡关系；补齐冲突排名不重排启动槽位。
	for _, s := range captured {
		if s.ZOrder > 0 {
			normalizeSlotZOrders(g.Slots)
			break
		}
	}
	return g
}

// persistWallGroup 的整个读改写都持 groupsMu，不能先读旧组再调用 saveGroup 覆盖并发编辑。
func persistWallGroup(id string, asNew bool, captured []GroupSlot, screenW, screenH int, now time.Time) (LaunchGroup, error) {
	if len(captured) == 0 {
		return LaunchGroup{}, fmt.Errorf("没有可保存的运行窗口")
	}
	groupsMu.Lock()
	defer groupsMu.Unlock()
	list, err := loadGroups()
	if err != nil && !os.IsNotExist(err) {
		return LaunchGroup{}, fmt.Errorf("读启动组失败: %w", err)
	}
	index := -1
	var g LaunchGroup
	for i, item := range list {
		if id != "" && item.ID == id {
			g, index = item, i
			break
		}
	}
	if !asNew && index < 0 {
		return LaunchGroup{}, fmt.Errorf("当前没有可覆盖的启动组，请另存为新组")
	}
	if asNew {
		// 另存从当前组复制，保留没启动或被禁用的成员；无归属时仅收录实际窗口。
		g.ID = nextGroupID(list, now.UnixNano())
		g.Name = "监控墙 " + now.Format("01-02 15:04:05")
		g.CreatedAt = now.Unix()
		if index < 0 {
			g.Layout, g.Cols, g.Rows, g.StopFirst = "tile", 2, 2, true
		}
	}
	g = mergeWallSlots(g, captured, screenW, screenH)
	g.UpdatedAt = now.Unix()
	if asNew {
		list = append(list, g)
	} else {
		list[index] = g
	}
	if err := saveGroups(list); err != nil {
		return LaunchGroup{}, fmt.Errorf("保存启动组失败: %w", err)
	}
	return g, nil
}

func wallSaveGroup(asNew bool, expectedID string) (string, error) {
	// 与启动入口使用同一把锁，防止读取参数后被下一批窗口替换。
	hostMu.Lock()
	defer hostMu.Unlock()
	running := wallRunningSlots()
	id := wallOwnedGroup(running)
	if id != expectedID {
		return "", fmt.Errorf("当前启动组归属已变化，请重新打开保存菜单")
	}
	if !asNew && id == "" {
		return "", fmt.Errorf("当前窗口没有唯一启动组归属，请另存为新组")
	}
	var captured []GroupSlot
	cells := make([]win.ClientCell, 0, len(running))
	for _, s := range running {
		cells = append(cells, win.ClientCell{PID: s.pid})
	}
	zOrders := win.ClientZOrders(cells)
	pids := make(map[string]uint32, len(running))
	for _, s := range running {
		if accountKey(s.acc) != s.key {
			return "", fmt.Errorf("客户端的本次启动参数不可用，请稍后重试")
		}
		if zOrders[s.pid] == 0 {
			return "", fmt.Errorf("%s 的窗口层级尚未就绪，请稍后重试", displayLabelOf(s.acc))
		}
		r, full, ok := win.WallWindowRectOfPID(s.pid)
		if !ok {
			return "", fmt.Errorf("%s 的窗口尚未就绪或已最小化，请恢复窗口后重试", displayLabelOf(s.acc))
		}
		display := "windowed"
		if full {
			display = "fullscreen"
		}
		captured = append(captured, GroupSlot{AccountKey: s.key, Mode: s.acc.QualityMode, Fps: s.acc.QualityFps,
			Display: display, X: r.X, Y: r.Y, W: r.W, H: r.H, HasRect: !full, ZOrder: zOrders[s.pid]})
		pids[s.key] = s.pid
	}
	// 停止按钮不持有 hostMu；窗口捕获期间若客户端退出，整次保存失败而非写入半套快照。
	latest := wallRunningSlots()
	if len(latest) != len(running) {
		return "", fmt.Errorf("运行客户端在保存期间发生变化，请重试")
	}
	for _, s := range latest {
		if pids[s.key] != s.pid {
			return "", fmt.Errorf("运行客户端在保存期间发生变化，请重试")
		}
	}
	sw, sh := win.ScreenSize()
	g, err := persistWallGroup(id, asNew, captured, sw, sh, time.Now())
	if err != nil {
		logf("监控墙保存失败: %v", err)
		return "", err
	}
	setWallGroupContext(g.ID, pids)
	msg := fmt.Sprintf("已保存 %d 个运行窗口的位置、前后层级及本次参数到「%s」", len(captured), g.Name)
	if asNew {
		msg += "\n可回到主界面的启动组中改名。"
	}
	logf("监控墙：%s", msg)
	return msg, nil
}
