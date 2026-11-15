// Package prefs 读写 TGC 自有引擎的本地设置文件
// （data/ThatGameCompany/com.netease.sky/preferences.sav，"PREF" 格式）。
//
// 布局（2026-10-25 首次解码，2026-11-15 修正头部语义，version=2）：
//
//	偏移 0  "PREF"
//	偏移 4  u32 version               // 2
//	偏移 8  u32 boolCount             // 布尔条目数
//	偏移 12 u32 intCount              // 整数条目数
//	偏移 16 u32 floatCount            // 浮点条目数
//	偏移 20 u32 stringCount           // 字符串条目数
//	偏移 24 u32 stringsBase           // 字符串区起点（键名 + 字符串值混排，NUL 结尾）
//	偏移 28 起，到 stringsBase：8 字节一组 [u32 键名相对偏移][u32 值]
//	  值 = 布尔 / 整数 / 浮点（IEEE 位）；对字符串键，值 = 指向字符串区的相对偏移
//
// **条目按 bool → int → float → string 四段顺序排列**，段长由上面四个计数决定。
// 2026-11-15 实测一份 34 键的文件：24 + 1 + 5 + 4 = 34，正好等于键值对总数
// （24 个 bool、1 个 int[cloudmusic_login_reject]、5 个 float、4 个 string）。
//
// 所以：
//
//	改已存在的数值键 = 就地改一个 u32（不动结构，最安全）
//	改已存在的字符串键 = 把值改成表里现成字符串的偏移（没有就追加到文件尾）
//	**新增**一个键 = 插到它所属那一段的段末，并把对应计数 +1
//
// 最后一条以前写错过：当时把新键一律插在字符串区之前（即所有段之后）且
// 不改计数，于是槽位数比计数多——游戏按计数读，多出来的键读不到，
// 画质/帧率从来没真正生效过，严重时游戏判定文件损坏后整份重置。
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
	countsBase  = 8  // 四个"各段条目数"所在偏移（8/12/16/20）
	stringsBase = 24 // u32：字符串区起点
	entries     = 28 // 键值对区起点
	pairSize    = 8
)

// Kind 是条目的值类型。文件里条目按这个顺序分段存放，
// 头部的四个 u32 就是各段的条目数。
//
// 为什么必须分清：新键插错段、或插了不改计数，游戏按计数读就少读一条——
// 轻则这条设置不生效，重则整份文件被判损坏后重置。
type Kind int

const (
	KindBool   Kind = iota // 布尔：raw 0/1
	KindInt                // 整数
	KindFloat              // 浮点（IEEE 位）
	KindString             // 字符串：值为指向字符串区的相对偏移
	kindCount  = 4
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

// counts 读四个"各段条目数"。
func counts(data []byte) [kindCount]uint32 {
	var c [kindCount]uint32
	for i := 0; i < kindCount; i++ {
		c[i] = u32(data, countsBase+4*i)
	}
	return c
}

// sectionEnd 返回某一类条目那一段的**末尾**偏移（下一段的开头），
// 也就是插入新条目该落的位置：插在段末才不会打乱段内顺序。
func sectionEnd(data []byte, kind Kind) int {
	c := counts(data)
	off := entries
	for i := 0; i <= int(kind); i++ {
		off += pairSize * int(c[i])
	}
	return off
}

// Validate 检查头部计数和实际槽位是否对得上。
//
// 游戏写出来的文件一定自洽；不自洽说明这文件被谁改坏过（包括我们自己）。
// 启动前后各校验一次，日志里就能看出"是不是被重置了"。
func Validate(data []byte) error {
	if len(data) < entries || string(data[:4]) != magic {
		return errors.New("不是 PREF 格式")
	}
	base := int(u32(data, stringsBase))
	if base < entries || base > len(data) {
		return fmt.Errorf("字符串区起点异常: %d", base)
	}
	c := counts(data)
	used := 0
	for i := 0; i < kindCount; i++ {
		used += int(c[i])
	}
	if slots := (base - entries) / pairSize; slots != used {
		return fmt.Errorf("头部计数合计 %d ≠ 实际槽位 %d", used, slots)
	}
	return nil
}

// addPair 在某一类条目的**段末**插入一条空记录（值先留 0），并给该段计数 +1。
// 返回新文件，以及这条记录"值"字段的偏移。
//
// 做法：在这一段的末尾（也就是下一段的开头）插 8 字节，stringsBase 相应 +8，
// 键名追加到文件尾。**偏移都是相对 stringsBase 的**，所以条目区整体后移
// 不会影响已有条目的键名——这是"安全地加一个新键"的关键。
func addPair(data []byte, key string, kind Kind) (out []byte, valOff int, err error) {
	if err := Validate(data); err != nil {
		// 已经不自洽的文件不再往里加东西：越加越乱，而且调用方该知道这件事。
		return nil, 0, err
	}
	base := int(u32(data, stringsBase))
	c := counts(data)
	at := sectionEnd(data, kind)
	newBase := base + pairSize

	out = make([]byte, 0, len(data)+pairSize+len(key)+1)
	out = append(out, data[:at]...)              // 头 + 本段之前的条目
	out = append(out, make([]byte, pairSize)...) // 新记录占位（本段末尾）
	out = append(out, data[at:]...)              // 后面的段 + 字符串区整体后移
	putU32(out, stringsBase, uint32(newBase))
	putU32(out, countsBase+4*int(kind), c[kind]+1) // 计数必须跟着 +1

	// 键名追加到文件尾（相对新 base 的偏移）
	keyRel := len(out) - newBase
	out = append(out, []byte(key)...)
	out = append(out, 0)
	putU32(out, at, uint32(keyRel))
	putU32(out, at+4, 0)
	return out, at + 4, nil
}

// EnsureValue 保证数值键存在：已有就改值，没有就按 kind 插到对应段末。
//
// 为什么需要：真实文件里这些键**不一定有**（引擎只写它当前有的设置）。
// 以前用 PatchValue，键不存在就返回错误，调用方只好整次预写放弃——
// 结果是"窗口化"也跟着不生效。现在缺键就地补上。
func EnsureValue(data []byte, key string, kind Kind, value uint32) ([]byte, error) {
	if off, err := findPair(data, key); err == nil {
		out := append([]byte(nil), data...)
		putU32(out, off, value)
		return out, nil
	}
	out, valOff, err := addPair(data, key, kind)
	if err != nil {
		return nil, err
	}
	putU32(out, valOff, value)
	return out, nil
}

// EnsureString 保证字符串键存在：值指向字符串区（表里有同值就复用，没有就追加）。
func EnsureString(data []byte, key, value string) ([]byte, error) {
	if _, err := findPair(data, key); err == nil {
		return PatchString(data, key, value)
	}
	out, valOff, err := addPair(data, key, KindString)
	if err != nil {
		return nil, err
	}
	base := int(u32(out, stringsBase))
	valRel := len(out) - base
	out = append(out, []byte(value)...)
	out = append(out, 0)
	putU32(out, valOff, uint32(valRel))
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
	out, err := EnsureValue(data, KeyFullscreen, KindBool, mode)
	if err != nil {
		return nil, err
	}
	if qualityFps > 0 {
		if next, e := EnsureValue(out, KeyQualityFps, KindInt, uint32(qualityFps)); e == nil {
			out = next
		}
	}
	if qualityMode != "" {
		if next, e := EnsureString(out, KeyQualityMode, qualityMode); e == nil {
			out = next
		}
	}
	// 最后再校验一次：写坏的设置文件游戏会整份重置（连窗口化一起丢），
	// 宁可这次不写、也不留一份看起来能读的坏文件。
	if err := Validate(out); err != nil {
		return nil, fmt.Errorf("写入后结构不自洽: %w", err)
	}
	return out, nil
}
