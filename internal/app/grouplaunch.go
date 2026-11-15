package app

// 启动组先校验，再停旧进程、同时拉起新客户端，最后只排列本批的窗口成员。
// 位置记录必须沿用记录时的物理屏幕基准，不能被普通保存操作刷新。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/fever"
	"skylogin/internal/win"
)

// 旧多开入口仍使用这个开关；组批次在排列闭包中捕获自己的开墙偏好。
var skipAutoWall atomic.Bool

// 只有真正启动成功的组才推进世代，旧排列回调不能替后来关闭开墙的组打开监控墙。
var groupLaunchGeneration atomic.Uint64

// groupLaunchPlan 的账号都是本次启动副本，不把组覆盖写回账号文件。
type groupLaunchPlan struct {
	picked     []Account
	rects      []win.TargetRect
	useRects   bool
	missing    int
	disabled   int
	rectReason string
	zorders    map[string]int
}

func groupAccountSettings(a Account, g LaunchGroup, s GroupSlot) Account {
	if g.DefMode != "" {
		a.QualityMode = g.DefMode
	}
	if g.DefFps > 0 {
		a.QualityFps = g.DefFps
	}
	// 旧组的全屏布局同时表达组级全屏默认；成员和独立的组显示设置仍可覆盖。
	if g.Layout == "fullscreen" {
		a.DisplayMode = "fullscreen"
	}
	if g.DefDisp != "" {
		a.DisplayMode = g.DefDisp
	}
	if s.Mode != "" {
		a.QualityMode = s.Mode
	}
	if s.Fps > 0 {
		a.QualityFps = s.Fps
	}
	if s.Display != "" {
		a.DisplayMode = s.Display
	}
	if a.DisplayMode == "" {
		a.DisplayMode = "windowed"
	}
	return a
}

func planGroupLaunch(g LaunchGroup, byKey map[string]Account, screenW, screenH int) groupLaunchPlan {
	p := groupLaunchPlan{zorders: make(map[string]int)}
	allHaveRect, anyRect, windowed := true, false, 0
	for _, s := range g.Slots {
		if s.Disabled {
			p.disabled++
			continue
		}
		a, ok := byKey[s.AccountKey]
		if !ok {
			p.missing++
			continue
		}
		a = groupAccountSettings(a, g, s)
		p.picked = append(p.picked, a)
		p.zorders[accountKey(a)] = s.ZOrder
		p.rects = append(p.rects, win.TargetRect{X: s.X, Y: s.Y, W: s.W, H: s.H, ZOrder: s.ZOrder})
		// 全屏成员不接受位置排列，也不应让其他窗口的有效位置失效。
		if a.DisplayMode == "fullscreen" {
			continue
		}
		windowed++
		anyRect = anyRect || s.HasRect
		if !s.HasRect || s.W <= 0 || s.H <= 0 {
			allHaveRect = false
		}
	}
	sameScreen := screenW > 0 && screenH > 0 && g.ScreenW == screenW && g.ScreenH == screenH
	p.useRects = windowed > 0 && allHaveRect && sameScreen
	if anyRect && !p.useRects {
		if !sameScreen {
			p.rectReason = "屏幕分辨率和记录时不一样或缺少记录基准"
		} else {
			p.rectReason = "部分启用成员没有有效位置"
		}
	}
	if !p.useRects {
		p.rects = nil
	}
	return p
}

// stopGroupClients 保留停止前的 PID 快照：stopAccount 会先删注册表，不能拿表空当作进程退出。
func stopGroupClients(ids map[string]uint32, stop func(string) error, alive func(uint32) bool, sleep func(time.Duration)) error {
	for id := range ids {
		if err := stop(id); err != nil {
			logf("停止 %s 失败: %v", id, err)
		}
	}
	for attempt := 0; attempt < 60; attempt++ {
		pending := false
		for _, pid := range ids {
			if pid != 0 && alive(pid) {
				pending = true
				break
			}
		}
		if !pending {
			if len(ids) > 0 {
				sleep(1200 * time.Millisecond)
			}
			return nil
		}
		sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("还有客户端没退出，稍等一下再启动（游戏是单实例，抢不过）")
}

func stopAllClients() error {
	clientsMu.Lock()
	ids := make(map[string]uint32, len(runningByAccount))
	for id, pid := range runningByAccount {
		ids[id] = pid
	}
	clientsMu.Unlock()
	return stopGroupClients(ids, stopAccount, fever.ProcessAlive, time.Sleep)
}

// groupBatchOps 把进程与网络边界集中起来，回归测试不需要启动游戏或访问真实数据。
type groupBatchOps struct {
	precheck func([]Account) ([]Account, []string, []string, int)
	before   func() error
	prepare  func(Account) (string, error)
	start    func(Account, string, string, string, int64) error
}

// 凭证刷新可能保存整个账号，必须只传原账号，不能把组的临时画质覆盖一起落盘。
func precheckGroupAccounts(picked []Account, originals map[string]Account, dev *device.ClientProfile) ([]Account, []string, []string, int) {
	accounts := make([]Account, 0, len(picked))
	for _, a := range picked {
		if original, ok := originals[accountKey(a)]; ok {
			accounts = append(accounts, original)
		}
	}
	return precheckLaunchable(accounts, dev)
}

func executeGroupBatch(plan groupLaunchPlan, ops groupBatchOps) ([]Account, error) {
	picked, tickets, channels, unverified := ops.precheck(plan.picked)
	if len(picked) != len(plan.picked) {
		kept := make(map[string]bool, len(picked))
		for _, a := range picked {
			kept[accountKey(a)] = true
		}
		var failed []string
		for _, a := range plan.picked {
			if !kept[accountKey(a)] {
				failed = append(failed, displayName(a))
			}
		}
		reason := "凭证校验未通过，请先检查这些账号"
		if unverified > 0 {
			reason = "网络或 DNS 异常，暂时无法确认凭证"
		}
		return nil, fmt.Errorf("%s：%s；尚未停止现有客户端", reason, strings.Join(failed, "、"))
	}
	if len(tickets) != len(picked) || len(channels) != len(picked) {
		return nil, fmt.Errorf("启动组预检结果不完整；尚未停止现有客户端")
	}
	// 刷新凭证可能重新读出账号原设置；只恢复本次设置，保留刷新后的凭证。
	settings := make(map[string]Account, len(plan.picked))
	for _, a := range plan.picked {
		settings[accountKey(a)] = a
	}
	for i := range picked {
		a := settings[accountKey(picked[i])]
		picked[i].QualityMode, picked[i].QualityFps, picked[i].DisplayMode = a.QualityMode, a.QualityFps, a.DisplayMode
	}
	if err := ops.before(); err != nil {
		return nil, err
	}
	paths := make([]string, len(picked))
	for i, a := range picked {
		p, err := ops.prepare(a)
		if err != nil {
			return nil, fmt.Errorf("准备 %s 的镜像失败: %w", displayName(a), err)
		}
		paths[i] = p
	}
	// 必须先建齐镜像再同时启动：串行间隔会触发游戏全局单实例检测。
	errs := make([]error, len(picked))
	var wg sync.WaitGroup
	for i := range picked {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = ops.start(picked[i], paths[i], tickets[i], channels[i], int64(i))
		}(i)
	}
	wg.Wait()
	var started []Account
	var failed []string
	for i, a := range picked {
		if errs[i] != nil {
			failed = append(failed, fmt.Sprintf("%s：%v", displayName(a), errs[i]))
			continue
		}
		started = append(started, a)
	}
	if len(failed) > 0 {
		return started, fmt.Errorf("%d/%d 个客户端启动成功；启动失败：%s", len(started), len(picked), strings.Join(failed, "；"))
	}
	return started, nil
}

func groupWindowCount(p groupLaunchPlan) int {
	n := 0
	for _, a := range p.picked {
		if a.DisplayMode != "fullscreen" {
			n++
		}
	}
	return n
}

// groupLayoutPlanFn 让预览与执行消费同一个物理规划，测试可注入工作区而不依赖本机屏幕。
var groupLayoutPlanFn = win.CurrentLayoutPlan

func groupAutoLayout(g LaunchGroup, p groupLaunchPlan) win.LayoutPlan {
	mode := g.Layout
	// 全屏成员已过滤；显式窗口覆盖的成员仍需要正常规划。
	if mode == "fullscreen" {
		mode = ""
	}
	return groupLayoutPlanFn(win.LayoutRequest{Count: groupWindowCount(p), Mode: mode, Cols: g.Cols, Rows: g.Rows})
}

func groupArrangeMode(g LaunchGroup, p groupLaunchPlan) string {
	if p.useRects {
		return "rects"
	}
	return groupAutoLayout(g, p).Mode
}

func validateGroupPlan(g LaunchGroup, p groupLaunchPlan) error {
	if len(p.picked) == 0 {
		return fmt.Errorf("这套里没有可启动的账号（%d 个已禁用，%d 个账号已不存在）", p.disabled, p.missing)
	}
	// 真实并排放不下时规划会明确回退标题布局，不以旧网格容量拒绝整个启动组。
	return nil
}

func launchGroup(id string) (string, error) {
	list, err := loadGroups()
	if err != nil {
		return "", fmt.Errorf("读启动组失败: %w", err)
	}
	var g LaunchGroup
	found := false
	for _, item := range list {
		if item.ID == id {
			g, found = item, true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("找不到这套启动组")
	}
	legacyGap := g.GapSec > 0
	g = normalizeGroup(g)
	accs, err := loadAccounts()
	if err != nil {
		return "", fmt.Errorf("读账号失败: %w", err)
	}
	byKey := make(map[string]Account, len(accs))
	for _, a := range accs {
		byKey[accountKey(a)] = a
	}
	sw, sh := win.ScreenSize()
	plan := planGroupLaunch(g, byKey, sw, sh)
	if err := validateGroupPlan(g, plan); err != nil {
		return "", err
	}

	hostMu.Lock()
	defer hostMu.Unlock()
	exe := resolveSkyPath()
	if exe == "" {
		return "", fmt.Errorf("没有找到光遇主程序，请在设置里手动选择 sky.exe")
	}
	if _, err := os.Stat(exe); err != nil {
		return "", fmt.Errorf("找不到光遇主程序: %s", exe)
	}
	if !g.StopFirst && liveClients() > 0 {
		return "", fmt.Errorf("已经有客户端在运行。这套组设成了不先停，请先手动停止")
	}
	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		return "", err
	}
	// 启动组也是多开：自动进入要等这批窗口排好、监控墙拉起之后再计时
	armAutoEnterBatch()
	started, launchErr := executeGroupBatch(plan, groupBatchOps{
		precheck: func(a []Account) ([]Account, []string, []string, int) {
			return precheckGroupAccounts(a, byKey, dev)
		},
		before: func() error {
			if g.StopFirst {
				if err := stopAllClients(); err != nil {
					return err
				}
			}
			if _, ok := launcherPIDFn(); ok && host == nil && loadConfig().CloseLauncherEnabled() {
				if err := killLauncherFn(); err != nil {
					return err
				}
			}
			return ensureHostLocked()
		},
		// 不进入旧多开的全局显示覆盖，每个副本明确携带自己的显示设置。
		prepare: func(a Account) (string, error) { return prepareMirror(exe, a, "", 0) },
		start: func(a Account, path, tk, ch string, order int64) error {
			_, err := launchAt(a, dev, tk, ch, path, "", 0, order, true)
			if err == nil {
				markAccountLaunched(accountKey(a), "")
			}
			return err
		},
	})
	if len(started) == 0 {
		// 一个都没起来：没有"窗口稳定"可言，直接放行等待方
		releaseAutoEnterBatch()
		return "", launchErr
	}
	rollAfterMultiLaunch(started)
	ArmMouseArbiter()
	generation := groupLaunchGeneration.Add(1)
	rememberWallGroup(g, started)
	arrangeGroupWhenReady(g, plan, started, generation)
	var names []string
	for _, a := range started {
		names = append(names, displayName(a))
	}
	msg := fmt.Sprintf("已启动 %d 个客户端：%s", len(started), strings.Join(names, "、"))
	if plan.missing > 0 {
		msg += fmt.Sprintf("（%d 个账号已不存在，已跳过）", plan.missing)
	}
	if plan.disabled > 0 {
		msg += fmt.Sprintf("（%d 个这轮禁用）", plan.disabled)
	}
	if legacyGap {
		msg += "（多开需同时启动，已忽略旧启动间隔）"
	}
	if plan.useRects {
		msg += "（窗口按这套记录的位置摆放）"
	} else if groupWindowCount(plan) > 0 {
		layout := groupAutoLayout(g, plan)
		if plan.rectReason != "" {
			msg += "（位置已失效：" + plan.rectReason + "）"
		}
		if layout.Message != "" {
			msg += "（" + layout.Message + "）"
		}
	}
	logf("启动组「%s」：%s", g.Name, msg)
	return msg, launchErr
}

// groupWindowCells 按账号键找位置，不能在某成员失败后把后续成员挤进它的坐标。
func groupWindowCells(plan groupLaunchPlan, started []Account, byKey map[string]win.ClientCell) ([]win.ClientCell, []win.TargetRect) {
	rects := make(map[string]win.TargetRect, len(plan.rects))
	for i, r := range plan.rects {
		rects[accountKey(plan.picked[i])] = r
	}
	var cells []win.ClientCell
	var targets []win.TargetRect
	for _, a := range started {
		if a.DisplayMode == "fullscreen" {
			continue
		}
		if c, ok := byKey[accountKey(a)]; ok {
			cells = append(cells, c)
			r := rects[accountKey(a)]
			if plan.useRects {
				r.ZOrder = plan.zorders[accountKey(a)]
			}
			targets = append(targets, r)
		}
	}
	// 几何及标题编号始终跟随启动槽位，层级只在最后单独恢复。
	return cells, targets
}

// shouldAutoOpenGroupWall 重新检查原批次的活进程，不能把排列前的快照当成排列后的事实。
func shouldAutoOpenGroupWall(noAutoWall bool, generation, current uint64, pids []uint32, alive func(uint32) bool) bool {
	if noAutoWall || generation != current {
		return false
	}
	seen := make(map[uint32]bool, len(pids))
	count := 0
	for _, pid := range pids {
		if pid == 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		if alive(pid) {
			count++
			if count >= 2 {
				return true
			}
		}
	}
	return false
}

// groupPlannedCells 先按启动槽位确定几何位置，再把账号与矩形成对按层级排序。
// 前后层改变不能让账号从左侧跳到右侧；失败成员不挤占别人的保存位置。
func groupPlannedCells(g LaunchGroup, plan groupLaunchPlan, started []Account, byKey map[string]win.ClientCell) ([]win.ClientCell, []win.TargetRect) {
	if plan.useRects {
		return groupWindowCells(plan, started, byKey)
	}
	present := make(map[string]bool, len(started))
	count := 0
	for _, a := range started {
		key := accountKey(a)
		if _, ok := byKey[key]; ok && a.DisplayMode != "fullscreen" {
			present[key] = true
			count++
		}
	}
	mode := g.Layout
	if mode == "fullscreen" {
		mode = ""
	}
	layout := groupLayoutPlanFn(win.LayoutRequest{Count: count, Mode: mode, Cols: g.Cols, Rows: g.Rows})
	plan.rects = make([]win.TargetRect, len(plan.picked))
	at := 0
	for i, a := range plan.picked {
		if present[accountKey(a)] && at < len(layout.Rects) {
			plan.rects[i] = layout.Rects[at]
			at++
		}
	}
	return groupWindowCells(plan, started, byKey)
}

func groupLayerCells(plan groupLaunchPlan, started []Account, byKey map[string]win.ClientCell) []win.ClientCell {
	type layer struct {
		cell win.ClientCell
		z    int
	}
	var list []layer
	for i, a := range started {
		key := accountKey(a)
		if c, ok := byKey[key]; ok {
			z := plan.zorders[key]
			if z <= 0 {
				z = i + 1
			}
			list = append(list, layer{c, z})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].z < list[j].z })
	out := make([]win.ClientCell, len(list))
	for i, v := range list {
		out[i] = v.cell
	}
	return out
}

// groupFinalLayerCells 自动方案使用新计划层级；有效手动位置和纯全屏仍恢复保存层级。
// 混合显示时只替换窗口成员在全局序列中的相对顺序，不能让旧保存层级抹掉新方案。
func groupFinalLayerCells(plan groupLaunchPlan, started []Account, byKey map[string]win.ClientCell, cells []win.ClientCell, rects []win.TargetRect) []win.ClientCell {
	all := groupLayerCells(plan, started, byKey)
	if plan.useRects || len(cells) == 0 {
		return all
	}
	order := make([]int, len(cells))
	windowPIDs := make(map[uint32]bool, len(cells))
	for i, c := range cells {
		order[i] = i
		windowPIDs[c.PID] = true
	}
	z := func(i int) int {
		if i < len(rects) && rects[i].ZOrder > 0 {
			return rects[i].ZOrder
		}
		return i + 1
	}
	sort.SliceStable(order, func(i, j int) bool { return z(order[i]) < z(order[j]) })
	at := 0
	for i, c := range all {
		if windowPIDs[c.PID] {
			all[i] = cells[order[at]]
			at++
		}
	}
	return all
}

var groupArrangeByRectsFn = win.ArrangeByRects
var groupRestoreZOrderFn = win.RestoreClientZOrder

// executeGroupPlacement 把实际排列和最终层级调用作为可回归的执行边界；新世代不得恢复旧层级。
func executeGroupPlacement(cells []win.ClientCell, rects []win.TargetRect, layers []win.ClientCell, current func() bool) int {
	if !current() {
		return 0
	}
	moved := 0
	if len(cells) > 0 {
		moved = groupArrangeByRectsFn(cells, rects)
	}
	hostMu.Lock()
	if current() {
		groupRestoreZOrderFn(layers)
	}
	hostMu.Unlock()
	return moved
}

func arrangeGroupWhenReady(g LaunchGroup, plan groupLaunchPlan, started []Account, generation uint64) {
	// 捕获本批 PID，避免异步排列时把后来另一套组的窗口也摆进去。
	byKey := make(map[string]win.ClientCell, len(started))
	clientsMu.Lock()
	for _, a := range started {
		if pid := runningByAccount[accountKey(a)]; pid != 0 {
			byKey[accountKey(a)] = win.ClientCell{PID: pid, Label: displayLabelOf(a)}
		}
	}
	clientsMu.Unlock()
	go func() {
		// 排列（或提前放弃）就是"窗口这轮稳定了"：放行自动进入的等待方。
		// 放 defer 里，任何 return 都不会把等待方挂死。
		defer releaseAutoEnterBatch()
		var batch []uint32
		for _, c := range byKey {
			batch = append(batch, c.PID)
		}
		deadline, readyAt := time.Now().Add(3*time.Minute), time.Time{}
		for time.Now().Before(deadline) {
			if groupLaunchGeneration.Load() != generation {
				return
			}
			pending := false
			for _, s := range batchStatus(batch) {
				if s.Alive && !s.TicketSent {
					pending = true
				}
			}
			if !pending {
				if readyAt.IsZero() {
					readyAt = time.Now()
				}
				if time.Since(readyAt) >= 3*time.Second {
					break
				}
			} else {
				readyAt = time.Time{}
			}
			time.Sleep(500 * time.Millisecond)
		}
		for key, c := range byKey {
			if !fever.ProcessAlive(c.PID) {
				delete(byKey, key)
			}
		}
		if groupLaunchGeneration.Load() != generation {
			return
		}
		cells, rects := groupPlannedCells(g, plan, started, byKey)
		if len(cells) > 0 {
			recordWindowSlots(cells)
		}
		layers := groupFinalLayerCells(plan, started, byKey, cells, rects)
		moved := executeGroupPlacement(cells, rects, layers, func() bool { return groupLaunchGeneration.Load() == generation })
		logf("启动组自动排列: %d/%d 个窗口已就位", moved, len(cells))
		// 排列可能阻塞；与新组启动持同一把锁，避免旧回调为新组开墙。
		hostMu.Lock()
		defer hostMu.Unlock()
		if shouldAutoOpenGroupWall(g.NoAutoWall, generation, groupLaunchGeneration.Load(), batch, fever.ProcessAlive) {
			autoOpenWall()
		}
	}()
}

// clientRects 返回账号键，前端必须按键对回成员，不能按当前窗口数量顺序覆盖槽位。
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
	zcells := make([]win.ClientCell, len(items))
	for i, it := range items {
		zcells[i] = win.ClientCell{PID: it.pid}
	}
	zorders := win.ClientZOrders(zcells)
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
		out = append(out, map[string]any{"id": it.key, "name": name, "x": rect.X, "y": rect.Y, "w": rect.W, "h": rect.H, "fullscreen": full, "zOrder": zorders[it.pid]})
	}
	return out
}
