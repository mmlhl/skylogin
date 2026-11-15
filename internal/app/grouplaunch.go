package app

// 按启动组拉起客户端：停旧的 → 拉新的 → 按记录的位置摆。
//
// 这条链上有三个"必须做对"的点，写在这里免得以后被简化掉：
//   1. 停旧的要**等它真退出**。游戏是单实例，前一个进程还在退出中，
//      后一个拉起会被"已有实例"拦下，表现成"点了启动没反应"。
//   2. 位置只在"记位置时的屏幕 == 现在的屏幕"时才用。换显示器/改分辨率后
//      那套坐标会把窗口摆到屏幕外面，用户只会看到"窗口不见了"。
//   3. 位置失效时要退化成网格并说明原因，不能静默按网格摆完什么都不讲。

import (
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"skylogin/internal/fever"
	"skylogin/internal/win"
)

// skipAutoWall 这一轮不要自动开监控墙（组级开关）。
var skipAutoWall atomic.Bool

// groupLaunchPlan 是"这套组这次要启动什么"的纯计算结果。
//
// 单独抽出来是为了能测：它不碰进程、不碰窗口，只回答四个问题：
// 启动哪些账号、每个窗口摆哪、位置能不能用、有多少槽位已经失效。
type groupLaunchPlan struct {
	picked   []Account
	rects    []win.TargetRect
	useRects bool
	missing  int
	disabled int
}

func planGroupLaunch(g LaunchGroup, byKey map[string]Account, screenW, screenH int) groupLaunchPlan {
	var p groupLaunchPlan
	// 位置可用性只看"这一次真的要启动的槽"：
	// 被禁用的、账号已删除的都不参与判断——它们不会被摆，没位置也不影响别人。
	allHaveRect := true
	for _, s := range g.Slots {
		if s.Disabled {
			p.disabled++
			continue
		}
		a, ok := byKey[s.AccountKey]
		if !ok {
			// 账号被删了：跳过，但记账（启动完要告诉用户，不能假装这套是完整的）
			p.missing++
			continue
		}
		p.picked = append(p.picked, a)
		p.rects = append(p.rects, win.TargetRect{X: s.X, Y: s.Y, W: s.W, H: s.H})
		if !s.HasRect || s.W <= 0 || s.H <= 0 {
			allHaveRect = false
		}
	}

	// 屏幕变了（换显示器/改分辨率）→ 那套坐标会把窗口摆到屏幕外，不能再用
	sameScreen := screenW > 0 && g.ScreenW == screenW && g.ScreenH == screenH
	p.useRects = len(p.picked) > 0 && allHaveRect && sameScreen
	if !p.useRects {
		p.rects = nil
	}
	return p
}

// stopAllClients 停掉所有正在运行的客户端，并等它们真的退出。
//
// 返回错误时不能继续往下走：单实例检测会把新拉起的进程挡回去，
// 用户看到的是"闪一下就没了"，比直接报"没停干净"难查得多。
func stopAllClients() error {
	clientsMu.Lock()
	ids := make([]string, 0, len(runningByAccount))
	for id := range runningByAccount {
		ids = append(ids, id)
	}
	clientsMu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	logf("启动组：先停掉 %d 个正在运行的客户端", len(ids))
	for _, id := range ids {
		if err := stopAccount(id); err != nil {
			logf("停止 %s 失败: %v", id, err)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if liveClients() == 0 {
			// 进程退了但游戏还在收尾，给一点余量再建镜像
			time.Sleep(1200 * time.Millisecond)
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("还有客户端没退出，稍等一下再启动（游戏是单实例，抢不过）")
}

// launchGroup 按一套启动组拉起客户端。
func launchGroup(id string) (string, error) {
	list, err := loadGroups()
	if err != nil {
		return "", fmt.Errorf("读启动组失败: %w", err)
	}
	var g *LaunchGroup
	for i := range list {
		if list[i].ID == id {
			g = &list[i]
			break
		}
	}
	if g == nil {
		return "", fmt.Errorf("找不到这套启动组")
	}

	accs, err := loadAccounts()
	if err != nil {
		return "", fmt.Errorf("读账号失败: %w", err)
	}
	byKey := make(map[string]Account, len(accs))
	for _, a := range accs {
		byKey[accountKey(a)] = a
	}

	sw, sh := win.ScreenSize()
	plan := planGroupLaunch(*g, byKey, sw, sh)
	if len(plan.picked) == 0 {
		return "", fmt.Errorf("这套里没有可启动的账号（%d 个已禁用，%d 个账号已不存在）", plan.disabled, plan.missing)
	}

	if g.StopFirst {
		if err := stopAllClients(); err != nil {
			return "", err
		}
	} else if n := liveClients(); n > 0 {
		// 没勾"先停后启"又已经有客户端在跑：直说，不要让它去撞单实例
		return "", fmt.Errorf("已经有 %d 个客户端在运行。这套组设成了不先停，请先手动停止", n)
	}

	// 全屏要在拉起之前置位：镜像里的 preferences 是在建镜像时写的
	fullscreen := g.Layout == "fullscreen"
	nextFullscreen.Store(fullscreen)
	defer nextFullscreen.Store(false)
	skipAutoWall.Store(g.NoAutoWall)
	defer skipAutoWall.Store(false)

	var msg string
	if len(plan.picked) >= 2 {
		ids := make([]string, 0, len(plan.picked))
		for _, a := range plan.picked {
			ids = append(ids, accountKey(a))
		}
		msg, err = launchMany(ids, g.DefMode, g.DefFps)
	} else {
		msg, err = launchAccount(plan.picked[0])
	}
	if err != nil {
		return msg, err
	}
	if plan.missing > 0 {
		msg += fmt.Sprintf("（%d 个账号已不存在，已跳过）", plan.missing)
	}
	if plan.disabled > 0 {
		msg += fmt.Sprintf("（%d 个这轮禁用）", plan.disabled)
	}

	switch {
	case fullscreen:
		arrangeWhenReady(0, 0, 0, 0, "fullscreen")
		msg += "（全屏：用监控墙或 Alt+Tab 切换）"
	case plan.useRects:
		// 位置模式和"排列方式"是互斥的：给了坐标就不再按网格算
		setArrangeRects(plan.rects)
		arrangeWhenReady(0, 0, 0, 0, "rects")
		msg += "（按这套记录的位置摆放）"
	case g.Layout == "cascade":
		arrangeWhenReady(g.Cols, g.Rows, 0, 0, "cascade")
		msg += "（窗口堆叠）"
	default:
		arrangeWhenReady(g.Cols, g.Rows, 0, 0, "tile")
		if len(g.Slots) > 0 {
			msg += fmt.Sprintf("（位置已失效，改按 %d×%d 网格排列：屏幕分辨率和记录时不一样）", g.Cols, g.Rows)
		}
	}
	logf("启动组「%s」：%s", g.Name, msg)
	return msg, nil
}

// clientRects 读当前运行中客户端的窗口矩形，给「记住当前各窗口位置」用。
//
// 顺序按启动序号排：界面把第 N 个窗口的位置写回第 N 个槽位，
// 用户拖完窗口直接记下来，不用再去填四个数字。
func clientRects() []map[string]any {
	out := make([]map[string]any, 0, 4)
	clientsMu.Lock()
	type item struct {
		key string
		pid uint32
		seq int64
	}
	items := make([]item, 0, len(runningByAccount))
	for key, pid := range runningByAccount {
		if pid == 0 || !fever.ProcessAlive(pid) {
			continue
		}
		var seq int64
		if s := clients[pid]; s != nil {
			seq = s.seq
		}
		items = append(items, item{key: key, pid: pid, seq: seq})
	}
	clientsMu.Unlock()

	sort.Slice(items, func(i, j int) bool { return items[i].seq < items[j].seq })
	accs, _ := loadAccounts()
	byKey := make(map[string]Account, len(accs))
	for _, a := range accs {
		byKey[accountKey(a)] = a
	}
	for _, it := range items {
		rect, full, ok := win.WindowRectOfPID(it.pid)
		if !ok {
			continue
		}
		name := it.key
		if a, ok := byKey[it.key]; ok {
			name = displayName(a)
		}
		out = append(out, map[string]any{
			"id":         it.key,
			"name":       name,
			"x":          rect.X,
			"y":          rect.Y,
			"w":          rect.W,
			"h":          rect.H,
			"fullscreen": full,
		})
	}
	return out
}
