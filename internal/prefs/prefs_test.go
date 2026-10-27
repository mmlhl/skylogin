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
