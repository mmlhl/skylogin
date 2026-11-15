package app

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWallSaveMergesByAccountAndPreservesInactive(t *testing.T) {
	setupStore(t, nil)
	original := LaunchGroup{ID: "daily", Name: "日常", Layout: "tile", Cols: 2, Rows: 2,
		DefMode: "Default", StopFirst: true, ScreenW: 1920, ScreenH: 1080,
		Slots: []GroupSlot{
			{AccountKey: "a", Mode: "LowPower", X: 10, W: 500, H: 400, HasRect: true},
			{AccountKey: "missing", Mode: "HighQuality", X: 20, W: 500, H: 400, HasRect: true},
			{AccountKey: "b", Disabled: true, Mode: "Presentation", X: 30, W: 500, H: 400, HasRect: true},
		}}
	if err := saveGroups([]LaunchGroup{original}); err != nil {
		t.Fatal(err)
	}
	captured := []GroupSlot{
		{AccountKey: "b", Mode: "Default", Display: "windowed", W: 900, H: 600, HasRect: true},
		{AccountKey: "a", Mode: "Presentation", Fps: 120, Display: "windowed", X: 100, Y: 80, W: 800, H: 500, HasRect: true},
	}
	got, err := persistWallGroup("daily", false, captured, 1920, 1080, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.Slots[0] != captured[1] || got.Slots[1] != original.Slots[1] || got.Slots[2] != original.Slots[2] {
		t.Fatalf("必须按键合并且保留未运行、禁用成员：%+v", got.Slots)
	}
	if got.Name != original.Name || got.DefMode != original.DefMode || !got.StopFirst {
		t.Fatalf("不应覆盖组级偏好：%+v", got)
	}
	list, err := loadGroups()
	if err != nil || len(list) != 1 || !reflect.DeepEqual(list[0], got) {
		t.Fatalf("磁盘结果不一致：%+v, %v", list, err)
	}
}

func TestWallSaveAsKeepsSourceAndDisabledMembers(t *testing.T) {
	setupStore(t, nil)
	original := LaunchGroup{ID: "daily", Name: "日常", Layout: "cascade", Cols: 2, Rows: 2,
		ScreenW: 1920, ScreenH: 1080, Slots: []GroupSlot{{AccountKey: "a"}, {AccountKey: "b", Disabled: true}}}
	if err := saveGroups([]LaunchGroup{original}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 22, 30, 0, 0, time.Local)
	captured := []GroupSlot{{AccountKey: "a", Mode: "LowPower", Fps: 30, Display: "fullscreen", W: 1920, H: 1080}}
	got, err := persistWallGroup("daily", true, captured, 1920, 1080, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == original.ID || !strings.HasPrefix(got.Name, "监控墙 ") || len(got.Slots) != 2 || !got.Slots[1].Disabled {
		t.Fatalf("另存必须复制组并生成合理名字：%+v", got)
	}
	list, _ := loadGroups()
	if len(list) != 2 || !reflect.DeepEqual(list[0], original) || list[1].Slots[0] != captured[0] {
		t.Fatalf("另存不能改变原组：%+v", list)
	}
}

func TestWallSaveWithoutOwnerRequiresSaveAs(t *testing.T) {
	setupStore(t, nil)
	captured := []GroupSlot{{AccountKey: "a", W: 800, H: 600, HasRect: true}}
	for _, id := range []string{"", "deleted"} {
		if _, err := persistWallGroup(id, false, captured, 1920, 1080, time.Now()); err == nil {
			t.Fatal("无归属或已删除的组不能覆盖保存")
		}
	}
	got, err := persistWallGroup("", true, captured, 1920, 1080, time.Now())
	if err != nil || len(got.Slots) != 1 || got.ID == "" {
		t.Fatalf("无归属仍应允许另存：%+v，%v", got, err)
	}
	if _, err := persistWallGroup(got.ID, false, nil, 1920, 1080, time.Now()); err == nil {
		t.Fatal("空快照不能擦掉已有组")
	}
}

func TestWallSaveReadErrorDoesNotOverwriteFile(t *testing.T) {
	setupStore(t, nil)
	bad := []byte("损坏的启动组数据")
	if err := os.WriteFile(groupsPath(), bad, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := persistWallGroup("", true, []GroupSlot{{AccountKey: "a"}}, 1920, 1080, time.Now())
	if err == nil {
		t.Fatal("读取失败必须反馈错误")
	}
	got, _ := os.ReadFile(groupsPath())
	if string(got) != string(bad) {
		t.Fatal("读取失败不能用新数据覆盖原文件")
	}
}

func TestWallSaveScreenChangeAndManyWindows(t *testing.T) {
	g := LaunchGroup{Layout: "tile", ScreenW: 1920, ScreenH: 1080, Slots: []GroupSlot{
		{AccountKey: "old", Disabled: true, X: 12, W: 600, H: 400, HasRect: true},
	}}
	var captured []GroupSlot
	for i := 0; i < 6; i++ {
		captured = append(captured, GroupSlot{AccountKey: fmt.Sprint(i), W: 700, H: 500, HasRect: true})
	}
	got := mergeWallSlots(g, captured, 2560, 1440)
	if got.Layout != "tile" || got.Slots[0].HasRect || !got.Slots[0].Disabled || got.Slots[0].X != 12 {
		t.Fatalf("保存应保留布局偏好，旧屏幕位置只能撤销生效标记：%+v", got)
	}
	if len(g.Slots) != 1 || !g.Slots[0].HasRect {
		t.Fatal("合并函数不得修改调用方原组")
	}
}

func TestWallOwnedGroupChecksPIDAndMixedClients(t *testing.T) {
	setWallGroupContext("daily", map[string]uint32{"a": 11, "b": 22})
	t.Cleanup(func() { setWallGroupContext("", nil) })
	if wallOwnedGroup([]wallRunningSlot{{key: "a", pid: 11}}) != "daily" {
		t.Fatal("部分成功或部分退出后仍属于原组")
	}
	for _, slots := range [][]wallRunningSlot{nil, {{key: "a", pid: 33}}, {{key: "a", pid: 11}, {key: "other", pid: 44}}} {
		if id := wallOwnedGroup(slots); id != "" {
			t.Fatalf("空客户端、重启或混入其他账号不能沿用旧组：%s", id)
		}
	}
}

func TestWallLaunchSettingsCapturesTemporaryOverrides(t *testing.T) {
	oldBatch, oldFull := inMultiBatch.Load(), nextFullscreen.Load()
	t.Cleanup(func() { inMultiBatch.Store(oldBatch); nextFullscreen.Store(oldFull) })
	inMultiBatch.Store(true)
	nextFullscreen.Store(true)
	a := Account{PlayerID: "a", QualityMode: "Presentation", QualityFps: 120, DisplayMode: "windowed", MpayToken: "凭证"}
	got := wallLaunchSettings(a, "LowPower", 30)
	if got.QualityMode != "LowPower" || got.QualityFps != 30 || got.DisplayMode != "fullscreen" || got.MpayToken != a.MpayToken {
		t.Fatalf("本次参数或凭证不对：%+v", got)
	}
	got = wallLaunchSettings(a, "", 60)
	if got.QualityMode != "" || got.QualityFps != 60 {
		t.Fatal("必须遵循旧多开覆盖参数的取值规则")
	}
	inMultiBatch.Store(false)
	if got := wallLaunchSettings(a, "", 0); !reflect.DeepEqual(got, a) {
		t.Fatal("无覆盖的组或单开必须保留参数及凭证")
	}
}

func TestWallSaveSerializesConcurrentGroupChanges(t *testing.T) {
	setupStore(t, nil)
	if _, err := saveGroup(LaunchGroup{ID: "daily", Name: "日常"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 6; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_, err := saveGroup(LaunchGroup{ID: fmt.Sprint(i), Name: "并发新组"})
			errs <- err
		}(i)
		go func() {
			defer wg.Done()
			_, err := persistWallGroup("daily", false, []GroupSlot{{AccountKey: "a", W: 800, H: 600}}, 1920, 1080, time.Now())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := loadGroups()
	if err != nil || len(list) != 7 {
		t.Fatalf("并发保存不能丢其他启动组：%d，%v", len(list), err)
	}
}
