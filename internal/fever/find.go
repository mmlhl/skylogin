package fever

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// 发烧平台的注册表根（GUI 安装版是 HKCU，个别渠道可能写 HKLM）
var regRoots = []struct {
	key  registry.Key
	path string
}{
	{registry.CURRENT_USER, `Software\FeverGames\FeverGamesInstaller`},
	{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\FeverGames\FeverGamesInstaller`},
	{registry.LOCAL_MACHINE, `SOFTWARE\FeverGames\FeverGamesInstaller`},
}

// FindSky 定位光遇主程序。
//
// 不内置任何安装路径：只查注册表里装机信息（发烧平台安装时会写），
// 加上"用户默认游戏目录"下的目录扫描。找不到就让用户自己选。
func FindSky() (string, error) {
	for _, r := range regRoots {
		if p := findInRegistry(r.key, r.path); p != "" {
			return p, nil
		}
	}
	if p := scanDefaultGameDir(); p != "" {
		return p, nil
	}
	return "", errors.New("没有找到光遇主程序，请手动选择 sky.exe 的位置")
}

// findInRegistry 在某个注册表根下枚举所有已安装游戏，挑出光遇。
func findInRegistry(key registry.Key, root string) string {
	k, err := registry.OpenKey(key, root+`\game`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return ""
	}
	names, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil {
		return ""
	}

	type candidate struct {
		path  string
		score int
	}
	var found []candidate

	for _, sub := range names {
		gk, err := registry.OpenKey(key, root+`\game\`+sub, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		startup, _, _ := gk.GetStringValue("StartupPath")
		running, _, _ := gk.GetStringValue("RunningProcessName")
		display, _, _ := gk.GetStringValue("DisplayName")
		dirs := make([]string, 0, 2)
		for _, n := range []string{"LastInstallPath", "InstallPath"} {
			if v, _, err := gk.GetStringValue(n); err == nil && v != "" {
				dirs = append(dirs, v)
			}
		}
		gk.Close()

		// 判断这个条目是不是光遇
		score := 0
		if strings.EqualFold(startup, "sky.exe") || strings.EqualFold(running, "sky.exe") {
			score += 4
		}
		if strings.Contains(display, "光·遇") || strings.Contains(display, "光遇") {
			score += 3
		}
		if score == 0 {
			continue // 不是光遇，跳过
		}

		exe := startup
		if exe == "" {
			exe = "sky.exe"
		}
		for _, dir := range dirs {
			dir = strings.ReplaceAll(dir, "/", `\`)
			p := filepath.Join(dir, exe)
			if isFile(p) {
				found = append(found, candidate{p, score})
			}
			// StartupPath 可能带子目录
			if v := filepath.Join(dir, filepath.Base(exe)); isFile(v) {
				found = append(found, candidate{v, score})
			}
		}
	}

	if len(found) == 0 {
		return ""
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	return found[0].path
}

// scanDefaultGameDir 用发烧平台自己的"默认游戏目录"设置，扫一层子目录找 sky.exe。
func scanDefaultGameDir() string {
	for _, r := range regRoots {
		k, err := registry.OpenKey(r.key, r.path+`\setting`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		base, _, err := k.GetStringValue("defaultGamePath")
		k.Close()
		if err != nil || base == "" {
			continue
		}
		base = strings.ReplaceAll(base, "/", `\`)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(base, e.Name(), "sky.exe")
			if isFile(p) {
				return p
			}
		}
	}
	return ""
}

func isFile(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
