package app

import (
	"reflect"
	"testing"
	"time"

	"skylogin/internal/web"
)

func TestGroupLayerNormalizationKeepsLaunchOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		z, want []int
	}{
		{"旧零值", []int{0, 0, 0}, []int{1, 2, 3}},
		{"独立层级", []int{3, 1, 2}, []int{3, 1, 2}},
		{"重复及缺省", []int{10, 10, 0, -2}, []int{3, 4, 1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var slots []GroupSlot
			for i, z := range tc.z {
				slots = append(slots, GroupSlot{AccountKey: string(rune('a' + i)), ZOrder: z})
			}
			normalizeSlotZOrders(slots)
			for i, s := range slots {
				if s.AccountKey != string(rune('a'+i)) || s.ZOrder != tc.want[i] {
					t.Fatalf("归一化不能重排启动顺序：%+v", slots)
				}
			}
			before := append([]GroupSlot(nil), slots...)
			normalizeSlotZOrders(slots)
			if !reflect.DeepEqual(slots, before) {
				t.Fatal("重复归一化必须稳定")
			}
		})
	}
}

func TestGroupPayloadPersistsIndependentLayerByAccount(t *testing.T) {
	setupStore(t, []Account{{PlayerID: "a", Name: "甲"}, {PlayerID: "b", Name: "乙"}})
	id, err := saveGroupFromPayload(web.GroupPayload{Name: "层级", Slots: []web.GroupSlotPayload{
		{AccountKey: "a", ZOrder: 2}, {AccountKey: "b", ZOrder: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	list, err := loadGroups()
	if err != nil || len(list) != 1 || list[0].ID != id {
		t.Fatalf("落盘失败：%+v %v", list, err)
	}
	if list[0].Slots[0].AccountKey != "a" || list[0].Slots[0].ZOrder != 2 || list[0].Slots[1].AccountKey != "b" || list[0].Slots[1].ZOrder != 1 {
		t.Fatalf("启动顺序和层级不可混用：%+v", list[0].Slots)
	}
	view := groupsView()[0]["slots"].([]map[string]any)
	if view[0]["accountKey"] != "a" || view[0]["zOrder"] != 2 || view[1]["zOrder"] != 1 {
		t.Fatalf("界面层级未按账号返回：%+v", view)
	}
}

func TestWallSaveLayerOrderSurvivesInactiveMembersAndSaveAs(t *testing.T) {
	for _, asNew := range []bool{false, true} {
		t.Run(map[bool]string{false: "保存", true: "另存"}[asNew], func(t *testing.T) {
			setupStore(t, nil)
			g := LaunchGroup{ID: "g", Name: "组", Layout: "cascade:cols", ScreenW: 1920, ScreenH: 1080, Slots: []GroupSlot{
				{AccountKey: "a", ZOrder: 3, Mode: "LowPower"},
				{AccountKey: "b", ZOrder: 1},
				{AccountKey: "c", ZOrder: 2, Disabled: true, Mode: "Presentation", X: 77},
			}}
			if err := saveGroups([]LaunchGroup{g}); err != nil {
				t.Fatal(err)
			}
			// 捕获顺序与启动顺序不同，层级以捕获的真实前后关系为准。
			captured := []GroupSlot{{AccountKey: "b", ZOrder: 2, W: 800, H: 600, HasRect: true}, {AccountKey: "a", ZOrder: 1, W: 800, H: 600, HasRect: true}, {AccountKey: "d", ZOrder: 3, W: 800, H: 600, HasRect: true}}
			saved, err := persistWallGroup("g", asNew, captured, 1920, 1080, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			list, err := loadGroups()
			if err != nil {
				t.Fatal(err)
			}
			var got LaunchGroup
			for _, item := range list {
				if item.ID == saved.ID {
					got = item
				}
			}
			if len(got.Slots) != 4 {
				t.Fatalf("成员丢失：%+v", got.Slots)
			}
			keys := []string{}
			z := map[string]int{}
			for _, s := range got.Slots {
				keys = append(keys, s.AccountKey)
				z[s.AccountKey] = s.ZOrder
			}
			if !reflect.DeepEqual(keys, []string{"a", "b", "c", "d"}) || !(z["a"] < z["b"] && z["b"] < z["d"]) {
				t.Fatalf("捕获层级与启动顺序未独立：%+v", got.Slots)
			}
			if !got.Slots[2].Disabled || got.Slots[2].Mode != "Presentation" || got.Slots[2].X != 77 {
				t.Fatal("未运行/禁用成员配置被改写")
			}
			if asNew && !reflect.DeepEqual(list[0], g) {
				t.Fatal("另存不能改变源组")
			}
		})
	}
}

func TestGroupLegacyLayoutNamesRemainCompatible(t *testing.T) {
	for from, to := range map[string]string{"cascade": "cascade:cols", "dual": "cascade:cols", "fan": "cascade:rows", "cascade:rows": "cascade:rows"} {
		if got := normalizeGroup(LaunchGroup{Layout: from, Cols: 4, Rows: 3}); got.Layout != to || got.Cols != 4 || got.Rows != 3 {
			t.Fatalf("旧布局兼容失败：%+v", got)
		}
	}
}
