// Package prefs 读写 TGC 自有引擎的本地设置文件
// （data/ThatGameCompany/com.netease.sky/preferences.sav，"PREF" 格式）。
//
// 布局（2026-10-25 对光遇 ma75 实测解码，version=2）：
//
//	"PREF"
//	u32 version                       // 2
//	u32 ???                           // 观察值 26/27，含义未知，改写时原样保留
//	u32 2
//	u32 5
//	u32 5
//	u32 stringsBase                   // 字符串区起点（键名 + 字符串值混排，NUL 结尾）
//	之后到 stringsBase：8 字节一组的 [u32 键名相对偏移][u32 值]
//	  值 = 布尔 / 整数 / 浮点（IEEE 位）；对字符串键，值 = 指向字符串区的相对偏移
//
// 所以改数值键 = 就地改一个 u32；改字符串键 = 把值改成表里现成字符串的
// 偏移（表里没有就追加到文件尾——偏移都是相对 stringsBase 的，尾部追加安全）。
package prefs

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// 游戏设置里我们关心的键（键名就是文件里的 ASCII 明文）。
const (
	KeyFullscreen  = "kUserPreference_Fullscreen" // 0 = 窗口模式，实测 1 = 全屏
	KeyQualityFps  = "quality_fps"                // u32 帧率（实测 30）
	KeyQualityMode = "quality_mode"               // 字符串画质档位（实测 LowPower / Presentation）
)

const (
	magic       = "PREF"
	stringsBase = 24 // u32：字符串区起点
	entries     = 28 // 键值对区起点
	pairSize    = 8
)

func u32(b []byte, off int) uint32 {
	return binary.LittleEndian.Uint32(b[off:])
}

func putU32(b []byte, off int, v uint32) {
	binary.LittleEndian.PutUint32(b[off:], v)
}

// cstring 读 stringsBase 偏移系里的 NUL 结尾字符串。
func cstring(b []byte, off int) (string, error) {
	if off < 0 || off >= len(b) {
		return "", fmt.Errorf("字符串偏移越界: %d", off)
	}
	end := off
	for end < len(b) && b[end] != 0 {
		end++
	}
	return string(b[off:end]), nil
}

// findPair 找到键对应的值字段在文件里的偏移（拿到偏移就能就地改值）。
func findPair(data []byte, key string) (int, error) {
	if len(data) < entries+pairSize || string(data[:4]) != magic {
		return 0, errors.New("不是 PREF 格式")
	}
	base := int(u32(data, stringsBase))
	if base <= entries || base > len(data) {
		return 0, fmt.Errorf("字符串区起点异常: %d", base)
	}
	for pos := entries; pos+pairSize <= base; pos += pairSize {
		name, err := cstring(data, base+int(u32(data, pos)))
		if err != nil {
			continue
		}
		if name == key {
			return pos + 4, nil
		}
	}
	return 0, fmt.Errorf("键 %s 不存在", key)
}

// PatchValue 就地改数值键（布尔 / 整数 / 浮点位）。
func PatchValue(data []byte, key string, value uint32) ([]byte, error) {
	out := append([]byte(nil), data...)
	off, err := findPair(out, key)
	if err != nil {
		return nil, err
	}
	putU32(out, off, value)
	return out, nil
}

// ReadValue 读数值键的当前值。
func ReadValue(data []byte, key string) (uint32, error) {
	off, err := findPair(data, key)
	if err != nil {
		return 0, err
	}
	return u32(data, off), nil
}

// ReadString 读字符串键的当前值。
func ReadString(data []byte, key string) (string, error) {
	off, err := findPair(data, key)
	if err != nil {
		return "", err
	}
	base := int(u32(data, stringsBase))
	return cstring(data, base+int(u32(data, off)))
}

// PatchString 改字符串键的值：优先指向表里现成的同值字符串，
// 没有就把新值追加到文件尾（偏移相对 stringsBase，尾部追加安全）。
func PatchString(data []byte, key, value string) ([]byte, error) {
	out := append([]byte(nil), data...)
	off, err := findPair(out, key)
	if err != nil {
		return nil, err
	}
	base := int(u32(out, stringsBase))

	// 先扫现成的字符串
	for rel := 0; rel < len(out)-base; {
		s, err := cstring(out, base+rel)
		if err != nil {
			break
		}
		if s == value {
			putU32(out, off, uint32(rel))
			return out, nil
		}
		rel += len(s) + 1
	}

	// 表里没有：追加到文件尾
	newRel := len(out) - base
	out = append(out, []byte(value)...)
	out = append(out, 0)
	putU32(out, off, uint32(newRel))
	return out, nil
}

// addPair 在键值表末尾插入一条空记录（值先留 0），返回新文件和这条记录的偏移。
//
// 做法：在 stringsBase 处插 8 字节，stringsBase 相应 +8；键名追加到文件尾。
// **偏移都是相对 stringsBase 的**，所以字符串区整体后移不会影响已有条目——
// 这是"安全地加一个新键"的关键。
func addPair(data []byte, key string) (out []byte, pairOff int, err error) {
	if len(data) < entries || string(data[:4]) != magic {
		return nil, 0, errors.New("不是 PREF 格式")
	}
	base := int(u32(data, stringsBase))
	if base < entries || base > len(data) {
		return nil, 0, fmt.Errorf("字符串区起点异常: %d", base)
	}
	newBase := base + pairSize

	out = make([]byte, 0, len(data)+pairSize+len(key)+1)
	out = append(out, data[:base]...)            // 头 + 已有键值对
	out = append(out, make([]byte, pairSize)...) // 新记录占位
	out = append(out, data[base:]...)            // 字符串区整体后移
	putU32(out, stringsBase, uint32(newBase))

	// 键名追加到文件尾（相对新 base 的偏移）
	keyRel := len(out) - newBase
	out = append(out, []byte(key)...)
	out = append(out, 0)
	putU32(out, base, uint32(keyRel))
	putU32(out, base+4, 0)
	return out, base, nil
}

// EnsureValue 保证数值键存在：已有就改值，没有就插一条。
//
// 为什么需要：真实文件里这些键**不一定有**（引擎只写它当前有的设置）。
// 以前用 PatchValue，键不存在就返回错误，调用方只好整次预写放弃——
// 结果是"窗口化"也跟着不生效。现在缺键就地补上。
func EnsureValue(data []byte, key string, value uint32) ([]byte, error) {
	if off, err := findPair(data, key); err == nil {
		out := append([]byte(nil), data...)
		putU32(out, off, value)
		return out, nil
	}
	out, pairOff, err := addPair(data, key)
	if err != nil {
		return nil, err
	}
	putU32(out, pairOff+4, value)
	return out, nil
}

// EnsureString 保证字符串键存在：值指向字符串区（表里有同值就复用，没有就追加）。
func EnsureString(data []byte, key, value string) ([]byte, error) {
	if _, err := findPair(data, key); err == nil {
		return PatchString(data, key, value)
	}
	out, pairOff, err := addPair(data, key)
	if err != nil {
		return nil, err
	}
	base := int(u32(out, stringsBase))
	valRel := len(out) - base
	out = append(out, []byte(value)...)
	out = append(out, 0)
	putU32(out, pairOff+4, uint32(valRel))
	return out, nil
}

// ApplyMultiDefaults 按多开默认值改写设置：显示模式 + 可选画质 / 帧率。
// fullscreen=false 写窗口模式（网格排列的前提），true 写全屏（全屏堆叠用）。
// qualityMode 传空 = 跟随游戏；qualityFps <= 0 = 跟随游戏。
//
// 三项的权重不一样：
//   - 显示模式是**硬需求**（窗口排列的前提），写不进去就整体失败；
//   - 画质 / 帧率是加分项，缺键会补一条，补不上也不该连累显示模式。
//
// 2026-11-11 之前这里是"任何一项失败就整体放弃"，于是老模板（只有 34 个键、
// 没有 quality_*）上连全屏都写不进去，用户看到的是"窗口化也不生效"。
func ApplyMultiDefaults(data []byte, fullscreen bool, qualityMode string, qualityFps int) ([]byte, error) {
	mode := uint32(0) // 0 = 窗口模式，实测 1 = 全屏
	if fullscreen {
		mode = 1
	}
	out, err := EnsureValue(data, KeyFullscreen, mode)
	if err != nil {
		return nil, err
	}
	if qualityFps > 0 {
		if next, e := EnsureValue(out, KeyQualityFps, uint32(qualityFps)); e == nil {
			out = next
		}
	}
	if qualityMode != "" {
		if next, e := EnsureString(out, KeyQualityMode, qualityMode); e == nil {
			out = next
		}
	}
	return out, nil
}
