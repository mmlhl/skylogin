package app

// 账号级"游戏设置"（画质 / 帧率 / 显示方式）的回归测试。
//
// 背景（2026-11-11 用户反馈）：在界面里设了画质，关掉窗口再打开又变回去了。
// 原因不是没保存逻辑写错，而是**保存接口根本没注册**——前端 POST
// /api/accounts/quality，后端没有这个路由，请求落到单页兜底返回 HTML，
// 前端解析失败抛错且没人 catch，于是静默失败。

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"skylogin/internal/prefs"
)

func TestSaveGameSettings(t *testing.T) {
	dir := t.TempDir()
	oldDir := dataDirFn
	dataDirFn = func() string { return dir }
	defer func() { dataDirFn = oldDir }()

	now := time.Now().Unix()
	if err := saveList([]Account{
		{Channel: "netease", PlayerID: "1001", UserID: "1001", NickName: "甲", CredState: "ok", UpdatedAt: now},
		{Channel: "netease", PlayerID: "2002", UserID: "2002", NickName: "乙", CredState: "ok", UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	if err := setCurrent("1001"); err != nil {
		t.Fatal(err)
	}

	if err := saveGameSettings("2002", "Presentation", 120, "windowed"); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.UserID != "2002" {
			continue
		}
		if a.QualityMode != "Presentation" || a.QualityFps != 120 || a.DisplayMode != "windowed" {
			t.Fatalf("字段没存上: mode=%q fps=%d display=%q", a.QualityMode, a.QualityFps, a.DisplayMode)
		}
	}
	// 改画质不该顺手把"当前账号"挪到被改的那个号上（saveAccount 会 setCurrent，
	// 所以这里专门走 saveList 的定点更新）
	if b, err := os.ReadFile(currentPath()); err == nil && string(b) != "1001" {
		t.Fatalf("保存画质把当前账号改了：%q", string(b))
	}

	// 非法值必须拒绝：写进引擎不认的档位名，游戏启动时会当无效值处理
	if err := saveGameSettings("2002", "Ultra", 120, ""); err == nil {
		t.Fatal("不认识的画质档位应该被拒绝")
	}
	if err := saveGameSettings("2002", "Default", 45, ""); err == nil {
		t.Fatal("不支持的帧率应该被拒绝")
	}
	if err := saveGameSettings("2002", "Default", 60, "borderless"); err == nil {
		t.Fatal("不认识的显示方式应该被拒绝")
	}
	if err := saveGameSettings("不存在", "Default", 60, ""); err == nil {
		t.Fatal("找不到账号应该报错")
	}

	// 清空 = 回到"跟随游戏"
	if err := saveGameSettings("2002", "", 0, ""); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	list, _ = loadAccounts()
	for _, a := range list {
		if a.UserID == "2002" && (a.QualityMode != "" || a.QualityFps != 0 || a.DisplayMode != "") {
			t.Fatalf("清空后应全部跟随游戏: %+v", a)
		}
	}
}

// buildPref 合成一份最小 PREF 文件（三个键），用于验证单开预写。
// 结构与 internal/prefs 的实测样本一致：头部 + 键值对 + 字符串区。
func buildPref(t *testing.T) []byte {
	t.Helper()
	names := []string{prefs.KeyFullscreen, prefs.KeyQualityFps, prefs.KeyQualityMode}
	blob := []byte{}
	rel := map[string]int{}
	for _, s := range append([]string{"LowPower"}, names...) {
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
	for _, name := range names {
		w(uint32(rel[name]))
		switch name {
		case prefs.KeyQualityMode:
			w(uint32(rel["LowPower"]))
		case prefs.KeyQualityFps:
			w(30)
		default:
			w(1) // 初始全屏
		}
	}
	return append(b, blob...)
}

// TestApplySingleLaunchPrefs 单开前要真的把设置写进游戏目录，
// 并且"没设的项"不能被动到。
func TestApplySingleLaunchPrefs(t *testing.T) {
	dir := t.TempDir()
	oldDir := dataDirFn
	dataDirFn = func() string { return dir }
	defer func() { dataDirFn = oldDir }()

	gameDir := filepath.Join(dir, "game")
	p := prefsPathOf(gameDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buildPref(t), 0o644); err != nil {
		t.Fatal(err)
	}

	// 三项都设了：全屏 + 最高 + 120
	acc := Account{
		Channel: "netease", PlayerID: "2002", UserID: "2002",
		QualityMode: "Presentation", QualityFps: 120, DisplayMode: "fullscreen",
	}
	applySingleLaunchPrefs(acc, gameDir)

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if fs, _ := prefs.ReadValue(b, prefs.KeyFullscreen); fs != 1 {
		t.Fatalf("全屏没写进去: %d", fs)
	}
	if qf, _ := prefs.ReadValue(b, prefs.KeyQualityFps); qf != 120 {
		t.Fatalf("帧率没写进去: %d", qf)
	}
	if qm, _ := prefs.ReadString(b, prefs.KeyQualityMode); qm != "Presentation" {
		t.Fatalf("画质没写进去: %q", qm)
	}
	// 原件要留一份，方便用户还原
	backup := filepath.Join(accountProfileDir(accountKey(acc)), "preferences.sav.orig")
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("没有备份原件: %v", err)
	}

	// 只设画质/帧率、显示留空：不能把游戏的窗口状态改掉
	applySingleLaunchPrefs(Account{
		Channel: "netease", PlayerID: "2003", UserID: "2003",
		QualityMode: "LowPower", QualityFps: 30,
	}, gameDir)
	b, _ = os.ReadFile(p)
	if fs, _ := prefs.ReadValue(b, prefs.KeyFullscreen); fs != 1 {
		t.Fatalf("没设显示方式却改了全屏状态: %d", fs)
	}
	if qm, _ := prefs.ReadString(b, prefs.KeyQualityMode); qm != "LowPower" {
		t.Fatalf("画质没改成 LowPower: %q", qm)
	}

	// 三项全空 = 跟随游戏：一个字节都不能动
	before, _ := os.ReadFile(p)
	applySingleLaunchPrefs(Account{Channel: "netease", PlayerID: "2004", UserID: "2004"}, gameDir)
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("跟随游戏时不该动文件")
	}
}
