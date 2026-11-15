package app

// 启动组的落盘与校验测试。
//
// 背景（2026-11-15）：启动组是"可重复执行的启动配方"，用户可能存十几套。
// 这里钉死四件事：存得住、名字有兜底、槽位不重复、账号被删了要能看出来。

import (
	"encoding/json"
	"os"
	"testing"

	"skylogin/internal/web"
)

func TestSaveGroupRoundTrip(t *testing.T) {
	setupStore(t, []Account{
		{PlayerID: "a", Name: "星野"},
		{PlayerID: "b", Name: "云间"},
	})

	id, err := saveGroup(LaunchGroup{
		Name:      "  日常双开  ",
		Layout:    "tile",
		Cols:      2,
		Rows:      2,
		DefMode:   "HighQuality",
		DefFps:    60,
		StopFirst: true,
		Slots: []GroupSlot{
			{AccountKey: "a", Mode: "LowPower", Fps: 30, X: 0, Y: 0, W: 960, H: 540, HasRect: true},
			{AccountKey: "b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("应返回生成的 id")
	}

	list, err := loadGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("应存下 1 套，实际 %d 套", len(list))
	}
	g := list[0]
	if g.Name != "日常双开" {
		t.Fatalf("名字应去掉首尾空格，实际 %q", g.Name)
	}
	if len(g.Slots) != 2 || g.Slots[0].Mode != "LowPower" || g.Slots[0].W != 960 {
		t.Fatalf("槽位没存对：%+v", g.Slots)
	}
	if !g.Slots[0].HasRect || g.Slots[1].HasRect {
		t.Fatalf("HasRect 应逐槽保留：%+v", g.Slots)
	}
	if g.CreatedAt == 0 || g.UpdatedAt == 0 {
		t.Fatal("应记下创建/更新时间")
	}
}

// 同一个 accountKey 只能占一个槽；不同副本（key 不同）可以各占一个。
func TestSaveGroupDedupesSlots(t *testing.T) {
	setupStore(t, nil)
	_, err := saveGroup(LaunchGroup{
		Name: "去重",
		Slots: []GroupSlot{
			{AccountKey: "900"},
			{AccountKey: "900"},   // 重复，应被丢掉
			{AccountKey: "900-2"}, // 副本，保留
			{AccountKey: ""},      // 空键，丢掉
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := loadGroups()
	if len(list[0].Slots) != 2 {
		t.Fatalf("应只剩 2 个槽（原件 + 副本），实际 %d：%+v", len(list[0].Slots), list[0].Slots)
	}
}

// 非法值不拒收，改成安全值：用户点了保存，不该因为一个档位名整份丢掉。
func TestNormalizeGroupFallsBack(t *testing.T) {
	setupStore(t, nil)
	_, err := saveGroup(LaunchGroup{
		Name:    "   ",
		Layout:  "随便",
		Cols:    9,
		Rows:    0,
		DefMode: "Ultra",
		DefFps:  15,
		DefDisp: "picture-in-picture",
		GapSec:  -3,
		Slots: []GroupSlot{
			{AccountKey: "k", Mode: "Ultra", Fps: 15, Display: "x", X: -20, Y: -5, W: 0, H: 600, HasRect: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := loadGroups()
	got := g[0]
	if got.Name != "未命名启动组" {
		t.Fatalf("空名字应兜底，实际 %q", got.Name)
	}
	if got.Layout != "tile" || got.Cols < 1 || got.Cols > 16 || got.Rows < 1 || got.Rows > 16 {
		t.Fatalf("排列名称应兜底，旧网格数据应保持在兼容范围，实际 %s %d×%d", got.Layout, got.Cols, got.Rows)
	}
	if got.DefMode != "" || got.DefFps != 0 || got.DefDisp != "" {
		t.Fatalf("不认识的档位应清空，实际 %q/%d/%q", got.DefMode, got.DefFps, got.DefDisp)
	}
	if got.GapSec != 0 {
		t.Fatalf("负间隔应变 0，实际 %d", got.GapSec)
	}
	slot := got.Slots[0]
	if slot.X != 0 || slot.Y != 0 {
		t.Fatalf("负坐标应变 0，实际 %d,%d", slot.X, slot.Y)
	}
	if slot.HasRect {
		t.Fatal("宽为 0 的槽位不能标成记录过位置")
	}
}

// 账号被删掉之后，组里那条槽位要显示成"已删除的账号"，不能静默消失。
func TestGroupsViewFlagsMissingAccount(t *testing.T) {
	setupStore(t, []Account{{PlayerID: "a", Name: "星野"}})
	if _, err := saveGroup(LaunchGroup{
		Name:  "含失效号",
		Slots: []GroupSlot{{AccountKey: "a"}, {AccountKey: "已删除的键"}},
	}); err != nil {
		t.Fatal(err)
	}
	view := groupsView()
	if len(view) != 1 {
		t.Fatalf("应有 1 套，实际 %d", len(view))
	}
	slots, _ := view[0]["slots"].([]map[string]any)
	if len(slots) != 2 {
		t.Fatalf("两个槽都要在，实际 %d", len(slots))
	}
	if slots[0]["name"] != "星野" || slots[0]["missing"] == true {
		t.Fatalf("正常槽位名字不对：%+v", slots[0])
	}
	if slots[1]["missing"] != true || slots[1]["name"] != "已删除的账号" {
		t.Fatalf("失效槽位应被标出来：%+v", slots[1])
	}
	if view[0]["missing"] != 1 {
		t.Fatalf("应有 1 个失效槽，实际 %v", view[0]["missing"])
	}
}

// 删除要幂等：连点两次不该报错；也不该把别的组删掉。
func TestDeleteGroupIdempotent(t *testing.T) {
	setupStore(t, nil)
	id1, _ := saveGroup(LaunchGroup{Name: "A"})
	id2, _ := saveGroup(LaunchGroup{Name: "B"})
	if err := deleteGroup(id1); err != nil {
		t.Fatal(err)
	}
	if err := deleteGroup(id1); err != nil {
		t.Fatalf("重复删除应幂等，实际报错：%v", err)
	}
	list, _ := loadGroups()
	if len(list) != 1 || list[0].ID != id2 {
		t.Fatalf("应该只剩下 B，实际 %+v", list)
	}
}

// 启动组文件也要加密（和账号文件同一套 DPAPI）。
func TestGroupsFileIsEncrypted(t *testing.T) {
	setupStore(t, nil)
	if _, err := saveGroup(LaunchGroup{Name: "加密"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(groupsPath())
	if err != nil {
		t.Fatal(err)
	}
	var probe []LaunchGroup
	if json.Unmarshal(b, &probe) == nil {
		t.Fatal("启动组文件应该是加密的，不该能直接解析成 JSON")
	}
}

// 普通保存不能把旧坐标重新盖章成当前屏幕；漏传基准时必须沿用旧记录。
func TestSaveGroupPayloadPreservesCoordinateBasis(t *testing.T) {
	setupStore(t, nil)
	id, err := saveGroupFromPayload(web.GroupPayload{Name: "旧坐标", ScreenW: 1920, ScreenH: 1080, GapSec: 5, Slots: []web.GroupSlotPayload{{AccountKey: "a", HasRect: true, W: 800, H: 600}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = saveGroupFromPayload(web.GroupPayload{ID: id, Name: "只改名字", Slots: []web.GroupSlotPayload{{AccountKey: "a", HasRect: true, W: 800, H: 600}}})
	if err != nil {
		t.Fatal(err)
	}
	list, err := loadGroups()
	if err != nil {
		t.Fatal(err)
	}
	g := list[0]
	if g.ScreenW != 1920 || g.ScreenH != 1080 || g.Slots[0].W != 800 || g.GapSec != 0 {
		t.Fatalf("旧基准和坐标应保留，废弃间隔应归零：%+v", g)
	}
	_, err = saveGroupFromPayload(web.GroupPayload{ID: id, Name: "重新记录", ScreenW: 2560, ScreenH: 1440})
	if err != nil {
		t.Fatal(err)
	}
	list, _ = loadGroups()
	if list[0].ScreenW != 2560 || list[0].ScreenH != 1440 {
		t.Fatal("显式传入的新记录基准应更新")
	}
}

func TestNewGroupWithoutBasisDoesNotClaimCurrentScreen(t *testing.T) {
	setupStore(t, nil)
	_, err := saveGroupFromPayload(web.GroupPayload{Name: "未知来源坐标", Slots: []web.GroupSlotPayload{{AccountKey: "a", HasRect: true, W: 800, H: 600}}})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := loadGroups()
	if list[0].ScreenW != 0 || list[0].ScreenH != 0 {
		t.Fatal("未知基准不能猜成当前屏幕")
	}
}

func TestNextGroupIDSurvivesDeletionAtSameTimestamp(t *testing.T) {
	const stamp int64 = 123456789
	list := []LaunchGroup{{ID: "旧组", Name: "旧组"}}
	first := nextGroupID(list, stamp)
	list = append(list, LaunchGroup{ID: first})
	second := nextGroupID(list, stamp)
	list = append(list, LaunchGroup{ID: second})
	// 删除第一套后长度回退，但仍存在的第二套不能被后续新建覆盖。
	list = append(list[:1], list[2:]...)
	third := nextGroupID(list, stamp)
	if third == second || third == "旧组" {
		t.Fatalf("相同时刻删除再新建不得撞到仍存在的组：first=%s second=%s third=%s", first, second, third)
	}
	if list[0].ID != "旧组" || list[1].ID != second {
		t.Fatalf("生成新 ID 不能迁移或修改已有 ID：%+v", list)
	}
	list = append(list, LaunchGroup{ID: third})
	fourth := nextGroupID(list, stamp)
	for _, g := range list {
		if fourth == g.ID {
			t.Fatalf("连续碰撞必须继续递增，不能覆盖 %s", g.ID)
		}
	}
}

func TestSaveGroupKeepsExistingIDs(t *testing.T) {
	setupStore(t, nil)
	id, err := saveGroup(LaunchGroup{ID: "g旧格式123", Name: "旧组"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := saveGroup(LaunchGroup{ID: id, Name: "更新旧组"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := loadGroups()
	if err != nil || id != "g旧格式123" || updated != id || len(list) != 1 || list[0].Name != "更新旧组" {
		t.Fatalf("修改已有组必须保留其 ID：id=%s updated=%s list=%+v err=%v", id, updated, list, err)
	}
}
