package prefs

import (
	"encoding/binary"
	"testing"
)

// build 合成一份最小 PREF 文件（结构对实测样本）：
// 头部 + 三个键值对（bool / int / string）+ 字符串区。
// 字符串区顺序：LowPower（quality_mode 的值）→ 三个键名。
func build(t *testing.T) []byte {
	t.Helper()
	names := []string{"kUserPreference_Fullscreen", "quality_fps", "quality_mode"}
	values := map[string]uint32{
		"kUserPreference_Fullscreen": 1,
		"quality_fps":                30,
	}

	// 字符串区：先值串 "LowPower"，再键名（和实测样本里混排的结构一致）
	blob := []byte{}
	rel := map[string]int{}
	for _, s := range append([]string{"LowPower"}, names...) {
		rel[s] = len(blob)
		blob = append(blob, append([]byte(s), 0)...)
	}

	strBase := 28 + len(names)*8
	b := append([]byte(nil), "PREF"...)
	w := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }
	w(2)               // version
	w(3)               // 实测样本里这个位置是会变的数，含义未知
	w(2)               //
	w(5)               //
	w(5)               //
	w(uint32(strBase)) // stringsBase
	for _, name := range names {
		w(uint32(rel[name]))        // 键名相对偏移
		if name == KeyQualityMode { // 字符串键：值 = 值串的相对偏移
			w(uint32(rel["LowPower"]))
			continue
		}
		w(values[name]) // 数值键：值本体
	}
	return append(b, blob...)
}

func TestReadAndPatchValue(t *testing.T) {
	data := build(t)
	if v, err := ReadValue(data, KeyFullscreen); err != nil || v != 1 {
		t.Fatalf("read fullscreen: %v %d", err, v)
	}
	data, err := PatchValue(data, KeyFullscreen, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := ReadValue(data, KeyFullscreen); err != nil || v != 0 {
		t.Fatalf("patch fullscreen: %v %d", err, v)
	}
	// 其它键不被误伤
	if v, _ := ReadValue(data, KeyQualityFps); v != 30 {
		t.Fatalf("quality_fps 被误伤: %d", v)
	}
	if got, _ := ReadString(data, KeyQualityMode); got != "LowPower" {
		t.Fatalf("quality_mode 被误伤: %q", got)
	}
}

func TestPatchStringExisting(t *testing.T) {
	data := build(t)
	before := len(data)
	data, err := PatchString(data, KeyQualityMode, "LowPower")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != before {
		t.Fatal("指向已有字符串不该改变文件长度")
	}
	if got, _ := ReadString(data, KeyQualityMode); got != "LowPower" {
		t.Fatalf("got %q", got)
	}
}

func TestPatchStringAppend(t *testing.T) {
	data := build(t)
	data, err := PatchString(data, KeyQualityMode, "Presentation")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadString(data, KeyQualityMode); got != "Presentation" {
		t.Fatalf("got %q", got)
	}
	if v, _ := ReadValue(data, KeyQualityFps); v != 30 {
		t.Fatalf("quality_fps 被误伤: %d", v)
	}
}

func TestApplyMultiDefaults(t *testing.T) {
	data := build(t)
	data, err := ApplyMultiDefaults(data, false, "Presentation", 60)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := ReadValue(data, KeyFullscreen); v != 0 {
		t.Fatalf("fullscreen 应为 0（窗口），got %d", v)
	}
	if v, _ := ReadValue(data, KeyQualityFps); v != 60 {
		t.Fatalf("fps 应为 60，got %d", v)
	}
	if got, _ := ReadString(data, KeyQualityMode); got != "Presentation" {
		t.Fatalf("quality_mode got %q", got)
	}
}

func TestApplyMultiDefaultsFullscreen(t *testing.T) {
	data := build(t)
	data, err := ApplyMultiDefaults(data, true, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := ReadValue(data, KeyFullscreen); v != 1 {
		t.Fatalf("fullscreen 应为 1（全屏），got %d", v)
	}
	// 空档位 / 0 帧率 = 跟随游戏，不许误伤
	if v, _ := ReadValue(data, KeyQualityFps); v != 30 {
		t.Fatalf("quality_fps 被误伤: %d", v)
	}
	if got, _ := ReadString(data, KeyQualityMode); got == "Presentation" {
		t.Fatalf("quality_mode 不该被写")
	}
}

func TestReadString(t *testing.T) {
	data := build(t)
	if got, err := ReadString(data, KeyQualityMode); err != nil || got != "LowPower" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestRejectsGarbage(t *testing.T) {
	if _, err := PatchValue([]byte("XXXXXXXXXXXXXXXXXXXXXXXXXXXX"), "a", 1); err == nil {
		t.Fatal("应该拒绝非 PREF 数据")
	}
}

// TestEnsureAddsMissingKeys 老模板缺键时不能整次失败。
//
// 2026-11-11 之前：ApplyMultiDefaults 用 PatchValue，键不存在就报错，
// 调用方只好放弃整次预写——于是"窗口模式"也跟着写不进去，
// 游戏按模板（全屏）启动，用户看到的是"窗口化不生效"。
func TestEnsureAddsMissingKeys(t *testing.T) {
	// 只造一个带 Fullscreen 的文件，模拟"老模板没有 quality_*"
	names := []string{KeyFullscreen}
	blob := []byte{}
	rel := map[string]int{}
	for _, s := range names {
		rel[s] = len(blob)
		blob = append(blob, append([]byte(s), 0)...)
	}
	strBase := 28 + len(names)*8
	b := append([]byte(nil), "PREF"...)
	w := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }
	w(2)
	w(3)
	w(2)
	w(5)
	w(5)
	w(uint32(strBase))
	w(uint32(rel[KeyFullscreen]))
	w(1) // 原本全屏
	b = append(b, blob...)

	out, err := ApplyMultiDefaults(b, false, "LowPower", 60)
	if err != nil {
		t.Fatalf("缺键时不该整体失败: %v", err)
	}
	if v, _ := ReadValue(out, KeyFullscreen); v != 0 {
		t.Fatalf("窗口模式没写进去: %d", v)
	}
	if v, e := ReadValue(out, KeyQualityFps); e != nil || v != 60 {
		t.Fatalf("帧率没补上: %d (%v)", v, e)
	}
	if s, e := ReadString(out, KeyQualityMode); e != nil || s != "LowPower" {
		t.Fatalf("画质没补上: %q (%v)", s, e)
	}
	// 原有键不能被搞丢
	if _, e := ReadValue(out, KeyFullscreen); e != nil {
		t.Fatalf("原有键丢了: %v", e)
	}
	// 结构要自洽：stringsBase 前移了 8，键值对区仍是 8 字节一组
	base := int(u32(out, 24))
	if (base-28)%8 != 0 {
		t.Fatalf("键值对区不是 8 的整数倍: base=%d", base)
	}
}

// TestEnsureValueKeepsOtherKeys 往真实文件里补键时，其它键一个都不能动。
func TestEnsureValueKeepsOtherKeys(t *testing.T) {
	b := build(t)
	before := map[string]string{}
	base := int(u32(b, 24))
	for pos := 28; pos+8 <= base; pos += 8 {
		name, _ := cstring(b, base+int(u32(b, pos)))
		before[name] = ""
	}
	out, err := EnsureValue(b, "brand_new_key", 7)
	if err != nil {
		t.Fatal(err)
	}
	nb := int(u32(out, 24))
	for pos := 28; pos+8 <= nb; pos += 8 {
		name, _ := cstring(out, nb+int(u32(out, pos)))
		delete(before, name)
	}
	if len(before) != 0 {
		t.Fatalf("补键后原有键丢了: %v", before)
	}
	if v, _ := ReadValue(out, "brand_new_key"); v != 7 {
		t.Fatalf("新键读不回来: %d", v)
	}
}
