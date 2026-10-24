package fever

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// FindMpaySkin 定位发烧平台自带的 skin.zip。
//
// 官方 mpay.dll 的登录窗不是自绘资源，必须由外部喂一份皮肤包（zip）才会创建窗口。
// 这份包跟着发烧平台一起安装，所以从"卸载表里的安装位置"去找，
// 不内置任何路径，也不把网易的资源塞进仓库。
func FindMpaySkin() string {
	root := launcherRoot()
	if root == "" {
		return ""
	}
	// 优先用当前版本目录（注册表里 upgrade\installerVersion）
	if ver := installerVersion(); ver != "" {
		if p := filepath.Join(root, ver, "skin.zip"); isFile(p) {
			return p
		}
	}
	// 退而求其次：扫一层版本目录，取版本号最大的
	entries, err := os.ReadDir(root)
	if err == nil {
		var versions []string
		for _, e := range entries {
			if e.IsDir() && isFile(filepath.Join(root, e.Name(), "skin.zip")) {
				versions = append(versions, e.Name())
			}
		}
		if len(versions) > 0 {
			sort.Slice(versions, func(i, j int) bool { return versionLess(versions[i], versions[j]) })
			return filepath.Join(root, versions[len(versions)-1], "skin.zip")
		}
	}
	if p := filepath.Join(root, "skin.zip"); isFile(p) {
		return p
	}
	return ""
}

// launcherRoot 从卸载表里读发烧平台的安装目录。
func launcherRoot() string {
	type root struct {
		key  registry.Key
		path string
	}
	roots := []root{
		{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	}
	for _, r := range roots {
		k, err := registry.OpenKey(r.key, r.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, err := k.ReadSubKeyNames(-1)
		k.Close()
		if err != nil {
			continue
		}
		for _, sub := range names {
			sk, err := registry.OpenKey(r.key, r.path+`\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			display, _, _ := sk.GetStringValue("DisplayName")
			loc, _, _ := sk.GetStringValue("InstallLocation")
			sk.Close()
			if loc == "" {
				continue
			}
			if strings.Contains(display, "发烧游戏") || strings.Contains(display, "FeverGames") {
				return strings.TrimRight(strings.ReplaceAll(loc, "/", `\`), `\`)
			}
		}
	}
	return ""
}

// installerVersion 读平台自己记录的当前版本号（用于拼 skin.zip 的路径）。
func installerVersion() string {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\FeverGames\FeverGamesInstaller\upgrade`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("installerVersion")
	if err != nil {
		return ""
	}
	return v
}

// versionLess 按点分数字比较版本号（1.18.9 < 1.18.45）。
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		return atoiSafe(pa[i]) < atoiSafe(pb[i])
	}
	return len(pa) < len(pb)
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
