package app

// 官方登录窗的调试参数（设置页「开发者调试」）。
//
// 以前靠 %APPDATA%\SkyLogin 下的 .on 开关文件切换：
//
//	mpay_dll_platform.on —— 加载平台目录的 mpay.dll（4.21）
//	mpay_game.on         —— 登录窗用 ma75 初始化（二维码变成光遇手游能扫的那种）
//	mpay_platform_db.on  —— 用平台凭证库副本并跳过清理登录态
//
// 文件开关的问题：测试时建了又删，事后根本分不清当时用的是哪套（2026-11-05 就踩过）。
// 现在挪到配置里，界面上看得见、改得动、跑完还记得。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"skylogin/internal/fever"
	"skylogin/internal/profile"
)

// MpayDebugConfig 是设置页里那几个下拉框的值。
type MpayDebugConfig struct {
	DLL        string `json:"dll"`        // game | platform
	Game       string `json:"game"`       // a50 | ma75
	SkipLogout bool   `json:"skipLogout"` // 打开窗口前不清旧登录态
	DataPath   string `json:"dataPath"`   // 自定义数据目录（空 = 用默认）
}

// mpayDebug 读当前配置，空值填成默认。
//
// 默认用**平台目录**那份（4.21）：平台自己登录用的就是它，登录后的表现
// （包括会不会弹“选择平台”页）与平台行为一致。要回游戏目录那份，在
// 设置页的开发者调试里显式选“游戏目录”。
func mpayDebug() MpayDebugConfig {
	c := loadConfig()
	d := MpayDebugConfig{
		DLL:        c.MpayDebugDLL,
		Game:       c.MpayDebugGame,
		SkipLogout: c.MpayDebugSkipLogout,
		DataPath:   c.MpayDebugDataPath,
	}
	if d.DLL != "game" && d.DLL != "platform" {
		d.DLL = "platform"
	}
	if d.Game != "a50" {
		d.Game = "ma75"
	}
	return d
}

// SaveMpayDebug 保存调试参数（设置页调用）。
func SaveMpayDebug(d MpayDebugConfig) error {
	if d.DLL != "game" && d.DLL != "platform" {
		return fmt.Errorf("未知的 DLL 选择: %s", d.DLL)
	}
	if d.Game != "a50" && d.Game != "ma75" {
		return fmt.Errorf("未知的 game id: %s", d.Game)
	}
	if d.DataPath != "" {
		if _, err := os.Stat(d.DataPath); err != nil {
			return fmt.Errorf("数据目录不存在: %s", d.DataPath)
		}
	}
	c := loadConfig()
	c.MpayDebugDLL = d.DLL
	c.MpayDebugGame = d.Game
	c.MpayDebugSkipLogout = d.SkipLogout
	c.MpayDebugDataPath = d.DataPath
	return saveConfig(c)
}

// mpayDebugDLLDir 按调试参数决定加载哪份 mpay.dll，返回目录。
//
//	game     → 游戏目录那份（4.20，默认）
//	platform → 平台目录那份（4.21），路径从皮肤所在目录推
func mpayDebugDLLDir(skyDir, skinPath string) (string, string) {
	d := mpayDebug()
	if d.DLL != "platform" {
		return skyDir, "游戏目录"
	}
	platformDir := filepath.Dir(skinPath)
	if platformDir == "" {
		return skyDir, "游戏目录（没找到平台目录）"
	}
	if _, err := os.Stat(filepath.Join(platformDir, "mpay.dll")); err != nil {
		return skyDir, "游戏目录（平台目录没有 mpay.dll）"
	}
	return platformDir, "平台目录"
}

// mpayDebugGameID 决定用哪个 game id 初始化登录窗。
//
//	ma75 → 游戏上下文（**正常登录固定用它**）：回调凭证 g_i=aecfs5n2y4aaaakd、
//	        sa=-9，能按渠道签发；窗口里的二维码也能用光遇手游扫。
//	a50  → 平台上下文：回调凭证 g_i=aecglf6ee4aaaarz（平台身份），
//	        uni_sauth 会回 501，选渠道永远不生效。
//
// 2026-11-08 两次实测（都记录在 05/06 号文档）：
//
//	平台目录 mpay.dll 4.21 + init ma75 + 账号密码 → g_i=ma75 sa=-9 ✅
//	同一份 DLL        + init a50 + 账号密码 → g_i=a50  sa=-9 ❌（501）
//
// 结论：**凭证身份完全由 init 的 game_id 决定**，跟有没有窗口、用哪份 DLL 无关。
// 所以这里不再读设置页那个下拉——那个值一旦被存成 a50，正常登录就会拿到
// 不能签发的平台凭证，账号再也选不了区（用户实测踩过）。要复现 a50 行为，
// 只能显式设环境变量 SKYLOGIN_MPAY_GAME=a50，日常路径不受影响。
func mpayDebugGameID() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SKYLOGIN_MPAY_GAME")), "a50") {
		return profile.HostedGameID
	}
	return profile.LongGameID
}

// findSkin 找官方登录窗要用的皮肤资源（发烧平台安装目录里的 skin.zip）。
func findSkin() string {
	return fever.FindMpaySkin()
}
