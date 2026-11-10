package app

// 账号级的"游戏设置"：画质 / 帧率 / 显示方式（窗口还是全屏）。
//
// 三条链路，别再各写各的：
//
//	保存   saveGameSettings（store.go）—— 只改这一行的三个字段
//	单开   applySingleLaunchPrefs —— 直接写游戏安装目录的 preferences.sav
//	多开   makeIndependentPrefs（clients.go）—— 写镜像里那份
//
// 为什么单开也要写：以前只有多开（建镜像）时才预写，于是界面上的画质/帧率
// 对单开完全没有作用——用户改完点启动，游戏还是老样子，看起来就像"没保存"。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"skylogin/internal/prefs"
)

// 游戏认识的档位名（2026-10-29 逐档实测）。写进别的字符串引擎会当无效值。
var (
	validQualityModes = map[string]bool{
		"": true, "LowPower": true, "Default": true, "HighQuality": true, "Presentation": true,
	}
	validQualityFps = map[int]bool{0: true, 30: true, 60: true, 120: true}
	validDisplays   = map[string]bool{"": true, "windowed": true, "fullscreen": true}
)

// accountProfileDir 返回某个账号的本地 profile 目录（备份游戏设置用）。
func accountProfileDir(key string) string {
	return filepath.Join(dataDir(), "profiles", safeFileName(key))
}

// safeFileName 把账号键变成能当目录名的字符串（键里有 "-" 之类的也留着）。
func safeFileName(s string) string {
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// backupOnce 把文件备份到 dst（dst 已存在就不覆盖）。
//
// 只备份第一次见到的原件：那是"游戏自己写的"那份，之后每次启动我们都会
// 改写它，再备份就只是备份自己的产物了。用户想还原时从这里拿。
func backupOnce(src, dst string) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		logf("游戏设置备份失败: %v", err)
		return
	}
	logf("游戏设置原件已备份: %s", dst)
}

// applySingleLaunchPrefs 单开前把账号的"游戏设置"写进游戏自己的 preferences.sav。
//
// 只写用户明确设过的项：三项都空（跟随游戏）时一个字节都不动。
// 改之前先把原件备份到 %APPDATA%\SkyLogin\profiles\<账号>\preferences.sav.orig。
func applySingleLaunchPrefs(acc Account, gameDir string) {
	mode, fps, display := acc.QualityMode, acc.QualityFps, acc.DisplayMode
	if mode == "" && fps <= 0 && display == "" {
		return // 全跟随游戏
	}
	p := prefsPathOf(gameDir)
	b, err := os.ReadFile(p)
	if err != nil {
		// 游戏还没在这个目录跑过（没有 data 目录）：等它自己建，别硬造一份。
		logf("单开游戏设置：还没有 %s，跳过（游戏第一次运行后会生成）", p)
		return
	}
	backupOnce(p, filepath.Join(accountProfileDir(accountKey(acc)), "preferences.sav.orig"))

	fullscreen := display == "fullscreen"
	if display == "" {
		// 没设显示方式：保留游戏当前的值，只改画质 / 帧率。
		cur, cerr := prefs.ReadValue(b, prefs.KeyFullscreen)
		if cerr == nil {
			fullscreen = cur != 0
		}
	}
	out, perr := prefs.ApplyMultiDefaults(b, fullscreen, mode, fps)
	if perr != nil {
		logf("单开游戏设置预写失败（%v），本次按游戏原设置启动", perr)
		return
	}
	if werr := os.WriteFile(p, out, 0o644); werr != nil {
		logf("单开游戏设置写入失败: %v", werr)
		return
	}
	extra := "显示跟随游戏"
	if display == "windowed" {
		extra = "窗口"
	} else if display == "fullscreen" {
		extra = "全屏"
	}
	if mode != "" {
		extra += "，画质 " + mode
	}
	if fps > 0 {
		extra += fmt.Sprintf("，帧率 %d", fps)
	}
	logf("单开游戏设置已写入（%s）", extra)
	logPrefsState("单开预写回读", gameDir)
}
