package app

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"skylogin/internal/win"
)

func TestGroupSettingsResolveEachFieldWithoutChangingAccount(t *testing.T) {
	a := Account{PlayerID: "a", QualityMode: "Default", QualityFps: 30, DisplayMode: "windowed"}
	g := LaunchGroup{Layout: "fullscreen", DefMode: "HighQuality", DefFps: 60, DefDisp: "fullscreen", Slots: []GroupSlot{{AccountKey: "a", Mode: "LowPower", Display: "windowed"}}}
	p := planGroupLaunch(g, accMap(a), 1920, 1080)
	got := p.picked[0]
	if got.QualityMode != "LowPower" || got.QualityFps != 60 || got.DisplayMode != "windowed" {
		t.Fatalf("成员和组应逐字段覆盖：%+v", got)
	}
	if a.QualityMode != "Default" || a.QualityFps != 30 || a.DisplayMode != "windowed" {
		t.Fatal("不得修改账号本身")
	}
	g.DefMode, g.DefFps, g.DefDisp, g.Slots[0].Mode, g.Slots[0].Display = "", 0, "", "", ""
	got = planGroupLaunch(g, accMap(a), 1920, 1080).picked[0]
	if got.QualityMode != "Default" || got.QualityFps != 30 || got.DisplayMode != "fullscreen" {
		t.Fatalf("画质帧率跟随账号，全屏布局应作为组级显示默认：%+v", got)
	}
	g.DefDisp = "windowed"
	if got := planGroupLaunch(g, accMap(a), 1920, 1080).picked[0]; got.DisplayMode != "windowed" {
		t.Fatal("独立组显示默认应覆盖旧全屏布局")
	}
	g.Layout, g.DefDisp = "tile", ""
	if got := planGroupLaunch(g, accMap(a), 1920, 1080).picked[0]; got.DisplayMode != "windowed" {
		t.Fatal("未设组显示的窗口布局应跟随账号")
	}
}

func TestGroupFullscreenDoesNotInvalidateWindowRects(t *testing.T) {
	g := LaunchGroup{Layout: "tile", ScreenW: 1920, ScreenH: 1080, Slots: []GroupSlot{
		{AccountKey: "a", HasRect: true, X: 20, Y: 30, W: 800, H: 600},
		{AccountKey: "b", Display: "fullscreen"},
	}}
	p := planGroupLaunch(g, accMap(Account{PlayerID: "a"}, Account{PlayerID: "b"}), 1920, 1080)
	if !p.useRects {
		t.Fatalf("全屏成员无位置不应拖累窗口成员：%+v", p)
	}
	cells, rects := groupWindowCells(p, p.picked, map[string]win.ClientCell{"a": {PID: 1}, "b": {PID: 2}})
	if len(cells) != 1 || cells[0].PID != 1 || rects[0].X != 20 {
		t.Fatalf("全屏不能被切回窗口排列：%+v %+v", cells, rects)
	}
}

func TestGroupRectsRemainBoundToAccountAfterFailure(t *testing.T) {
	g := LaunchGroup{ScreenW: 1920, ScreenH: 1080, Slots: []GroupSlot{
		{AccountKey: "a", HasRect: true, W: 800, H: 600, X: 10}, {AccountKey: "b", HasRect: true, W: 800, H: 600, X: 900},
	}}
	p := planGroupLaunch(g, accMap(Account{PlayerID: "a"}, Account{PlayerID: "b"}), 1920, 1080)
	cells, rects := groupWindowCells(p, p.picked[1:], map[string]win.ClientCell{"b": {PID: 2}})
	if len(cells) != 1 || cells[0].PID != 2 || rects[0].X != 900 {
		t.Fatalf("失败后不能把 b 摆进 a 的位置：%+v %+v", cells, rects)
	}
}

func TestGroupLayoutFallbackAndCapacity(t *testing.T) {
	g := LaunchGroup{Layout: "cascade", Cols: 1, Rows: 1, ScreenW: 1920, ScreenH: 1080, Slots: []GroupSlot{{AccountKey: "a", HasRect: true, W: 800, H: 600}, {AccountKey: "b", HasRect: true, W: 800, H: 600}}}
	p := planGroupLaunch(g, accMap(Account{PlayerID: "a"}, Account{PlayerID: "b"}), 2560, 1440)
	if groupArrangeMode(g, p) != "cascade:cols" || !strings.Contains(p.rectReason, "屏幕") {
		t.Fatalf("失效坐标应回到标题布局并保留失效原因：%+v", p)
	}
	if err := validateGroupPlan(g, p); err != nil {
		t.Fatal("旧网格容量不应阻止可执行的后端规划")
	}
	g.Slots[0].HasRect, g.Slots[1].HasRect = false, false
	p = planGroupLaunch(g, accMap(Account{PlayerID: "a"}, Account{PlayerID: "b"}), 2560, 1440)
	if groupArrangeMode(g, p) != "cascade:cols" || p.rectReason != "" {
		t.Fatal("自动堆叠不应误报屏幕改变")
	}
	if err := validateGroupPlan(g, p); err != nil {
		t.Fatal(err)
	}
}

func TestGroupBatchSingleAndMultiplePreserveOverrides(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var picked []Account
			for i := 0; i < count; i++ {
				picked = append(picked, Account{PlayerID: fmt.Sprint(i), QualityMode: "Presentation", QualityFps: 120, DisplayMode: "fullscreen"})
			}
			var mu sync.Mutex
			prepared, launched := 0, 0
			ops := groupBatchOps{
				precheck: func(list []Account) ([]Account, []string, []string, int) {
					var refreshed []Account
					var tickets, channels []string
					for _, a := range list {
						a.QualityMode = "Default"
						a.QualityFps = 30
						a.DisplayMode = "windowed"
						a.MpayToken = "新凭证"
						refreshed = append(refreshed, a)
						tickets = append(tickets, "预签票")
						channels = append(channels, "netease")
					}
					return refreshed, tickets, channels, 0
				},
				before: func() error { return nil },
				prepare: func(a Account) (string, error) {
					if a.QualityMode != "Presentation" || a.QualityFps != 120 || a.DisplayMode != "fullscreen" || a.MpayToken != "新凭证" {
						t.Errorf("刷新后丢失本次设置或新凭证：%+v", a)
					}
					prepared++
					return "镜像" + a.PlayerID, nil
				},
				start: func(a Account, path, tk, ch string, order int64) error {
					mu.Lock()
					defer mu.Unlock()
					if prepared != count {
						t.Error("所有镜像准备完之前不能启动任何客户端")
					}
					if path != "镜像"+a.PlayerID || tk != "预签票" || ch != "netease" || order < 0 || order >= int64(count) {
						t.Errorf("启动参数未透传：%+v %s %s %s %d", a, path, tk, ch, order)
					}
					if a.QualityFps != 120 || a.DisplayMode != "fullscreen" {
						t.Error("启动设置丢失")
					}
					launched++
					return nil
				},
			}
			got, err := executeGroupBatch(groupLaunchPlan{picked: picked}, ops)
			if err != nil || len(got) != count || launched != count {
				t.Fatalf("单多成员应走同一批次：%+v %v", got, err)
			}
		})
	}
}

func TestGroupBatchPrecheckFailureDoesNotStopClients(t *testing.T) {
	before := false
	ops := groupBatchOps{precheck: func([]Account) ([]Account, []string, []string, int) { return nil, nil, nil, 1 }, before: func() error { before = true; return nil }}
	_, err := executeGroupBatch(groupLaunchPlan{picked: []Account{{PlayerID: "a", Name: "星野"}}}, ops)
	if before || err == nil || !strings.Contains(err.Error(), "星野") || !strings.Contains(err.Error(), "网络") {
		t.Fatalf("校验失败应先报具体账号、保留已有客户端：%v before=%v", err, before)
	}
}

func TestGroupBatchReportsPartialFailure(t *testing.T) {
	list := []Account{{PlayerID: "a", Name: "甲"}, {PlayerID: "b", Name: "乙"}}
	ops := groupBatchOps{
		precheck: func([]Account) ([]Account, []string, []string, int) {
			return list, []string{"t1", "t2"}, []string{"c", "c"}, 0
		},
		before: func() error { return nil }, prepare: func(a Account) (string, error) { return a.PlayerID, nil },
		start: func(a Account, _, _, _ string, _ int64) error {
			if a.PlayerID == "a" {
				return fmt.Errorf("模拟权限失败")
			}
			return nil
		},
	}
	got, err := executeGroupBatch(groupLaunchPlan{picked: list}, ops)
	if len(got) != 1 || got[0].PlayerID != "b" || err == nil || !strings.Contains(err.Error(), "甲：模拟权限失败") || !strings.Contains(err.Error(), "1/2") {
		t.Fatalf("部分启动失败应保留成功名单并说明详情：%+v %v", got, err)
	}
}

func TestStopGroupClientsWaitsForOriginalPID(t *testing.T) {
	checks := 0
	var sleeps []time.Duration
	stopped := false
	err := stopGroupClients(map[string]uint32{"a": 42}, func(id string) error { stopped = id == "a"; return nil }, func(pid uint32) bool {
		if pid != 42 {
			t.Fatal("等待错了进程")
		}
		checks++
		return checks < 3
	}, func(d time.Duration) { sleeps = append(sleeps, d) })
	if err != nil || !stopped || checks != 3 || !reflect.DeepEqual(sleeps, []time.Duration{250 * time.Millisecond, 250 * time.Millisecond, 1200 * time.Millisecond}) {
		t.Fatalf("必须等原进程消失再留收尾时间：%v checks=%d sleeps=%v", err, checks, sleeps)
	}
}

func TestStopGroupClientsTimeoutDoesNotProceed(t *testing.T) {
	sleeps := 0
	err := stopGroupClients(map[string]uint32{"a": 42}, func(string) error { return fmt.Errorf("模拟停止失败") }, func(uint32) bool { return true }, func(time.Duration) { sleeps++ })
	if err == nil || sleeps != 60 {
		t.Fatalf("停止失败不能继续启动：%v sleeps=%d", err, sleeps)
	}
}

// 模拟真实刷新边界的整行落盘，防止仅检查值拷贝而漏掉刷新时保存组临时设置。
func TestGroupPrecheckRefreshCannotPersistGroupOverrides(t *testing.T) {
	original := Account{PlayerID: "a", Channel: "huawei", QualityMode: "Default", QualityFps: 30, DisplayMode: "windowed"}
	setupStore(t, []Account{original})
	oldRefresh := refreshFn
	defer func() { refreshFn = oldRefresh }()
	called := false
	refreshFn = func(a Account) (Account, error) {
		called = true
		if a.QualityMode != "Default" || a.QualityFps != 30 || a.DisplayMode != "windowed" {
			t.Fatalf("预检拿到了组临时覆盖：%+v", a)
		}
		a.AccessToken = "模拟刷新后的凭证"
		return a, saveAccount(a)
	}
	g := LaunchGroup{DefMode: "Presentation", DefFps: 120, DefDisp: "fullscreen", Slots: []GroupSlot{{AccountKey: "a"}}}
	byKey := accMap(original)
	plan := planGroupLaunch(g, byKey, 1920, 1080)
	prepared := false
	_, err := executeGroupBatch(plan, groupBatchOps{
		precheck: func(a []Account) ([]Account, []string, []string, int) { return precheckGroupAccounts(a, byKey, nil) },
		before:   func() error { return nil },
		prepare: func(a Account) (string, error) {
			prepared = true
			if a.QualityMode != "Presentation" || a.QualityFps != 120 || a.DisplayMode != "fullscreen" || a.AccessToken != "模拟刷新后的凭证" {
				t.Fatalf("镜像仍应收到组设置和新凭证：%+v", a)
			}
			return "测试镜像", nil
		},
		start: func(Account, string, string, string, int64) error { return nil },
	})
	if err != nil || !called || !prepared {
		t.Fatalf("未走通安全预检和镜像边界：%v", err)
	}
	stored, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].QualityMode != "Default" || stored[0].QualityFps != 30 || stored[0].DisplayMode != "windowed" || stored[0].AccessToken != "模拟刷新后的凭证" {
		t.Fatalf("账号库应只更新凭证，不保存组覆盖：%+v", stored)
	}
}

func TestGroupWallRequiresCurrentGenerationAndLiveOriginalBatch(t *testing.T) {
	cases := []struct {
		name                string
		noAuto              bool
		generation, current uint64
		pids                []uint32
		alive               map[uint32]bool
		want                bool
	}{
		{name: "当前批次两个存活", generation: 2, current: 2, pids: []uint32{10, 20}, alive: map[uint32]bool{10: true, 20: true}, want: true},
		{name: "旧组回调不能替新组开墙", generation: 1, current: 2, pids: []uint32{10, 20}, alive: map[uint32]bool{10: true, 20: true}},
		{name: "关闭自动开墙", noAuto: true, generation: 2, current: 2, pids: []uint32{10, 20}, alive: map[uint32]bool{10: true, 20: true}},
		{name: "旧进程退出不能用历史快照开墙", generation: 2, current: 2, pids: []uint32{10, 20}, alive: map[uint32]bool{30: true, 40: true}},
		{name: "只剩一个原进程", generation: 2, current: 2, pids: []uint32{10, 20}, alive: map[uint32]bool{10: true, 30: true}},
		{name: "重复PID只算一个", generation: 2, current: 2, pids: []uint32{10, 10, 0}, alive: map[uint32]bool{10: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldAutoOpenGroupWall(tc.noAuto, tc.generation, tc.current, tc.pids, func(pid uint32) bool { return tc.alive[pid] })
			if got != tc.want {
				t.Fatalf("开墙判断=%v，预期%v", got, tc.want)
			}
		})
	}
}

func TestGroupWallRechecksLivenessAfterBlockingArrange(t *testing.T) {
	live := true
	alive := func(uint32) bool { return live }
	batch := []uint32{10, 20}
	if !shouldAutoOpenGroupWall(false, 1, 1, batch, alive) {
		t.Fatal("排列前本批应该可开墙")
	}
	// 模拟排列阻塞期间原客户端全部退出，其他组启动；回调必须重新检查。
	live = false
	if shouldAutoOpenGroupWall(false, 1, 1, batch, alive) {
		t.Fatal("排列前的快照不能代替排列后的存活检查")
	}
	live = true
	if shouldAutoOpenGroupWall(false, 1, 2, batch, alive) {
		t.Fatal("世代变化后即使旧PID仍存活也不能开墙")
	}
}
