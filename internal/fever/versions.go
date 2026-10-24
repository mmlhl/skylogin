package fever

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"skylogin/internal/win"
)

// 这些参数都跟着官方更新走，不要写死。
const (
	fallbackClientCV   = "c4.20.0"
	fallbackStartupArg = "--start_from_launcher=1"
)

// ClientCV 返回 MPay 客户端版本号，形如 "c4.20.0"。
//
// 它就是游戏目录里 mpay.dll 的资源版本前三段（实机是 4.20.0.532 → c4.20.0）。
// 游戏更新后这里会自动跟着变，不需要改代码。
func ClientCV(skyExe string) string {
	if skyExe == "" {
		return fallbackClientCV
	}
	dll := filepath.Join(filepath.Dir(skyExe), "mpay.dll")
	major, minor, patch, ok := win.FileVersion(dll)
	if !ok {
		return fallbackClientCV
	}
	return fmt.Sprintf("c%d.%d.%d", major, minor, patch)
}

// GameVersion 返回 MPay 上报用的 gv（取自 mpay.dll 的完整版本）。
func GameVersion(skyExe string) string {
	if skyExe == "" {
		return "0.0.0.0"
	}
	dll := filepath.Join(filepath.Dir(skyExe), "mpay.dll")
	major, minor, patch, ok := win.FileVersion(dll)
	if !ok {
		return "0.0.0.0"
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

// SystemVersion 返回形如 "10.0.19045" 的系统版本（跟游戏上报的一致）。
func SystemVersion() string {
	major, minor, build := windows.RtlGetNtVersionNumbers()
	if major == 0 {
		return "10.0.19045"
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, build&0xFFFF)
}

// StartupArgs 从发烧平台注册表里读启动参数（游戏参数变了也能跟上）。
func StartupArgs() string {
	for _, r := range regRoots {
		k, err := registry.OpenKey(r.key, r.path+`\game`, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subs, err := k.ReadSubKeyNames(-1)
		k.Close()
		if err != nil {
			continue
		}
		for _, sub := range subs {
			gk, err := registry.OpenKey(r.key, r.path+`\game\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			startup, _, _ := gk.GetStringValue("StartupPath")
			params, _, _ := gk.GetStringValue("StartupParams")
			gk.Close()
			if strings.EqualFold(startup, "sky.exe") && strings.TrimSpace(params) != "" {
				return strings.TrimSpace(params)
			}
		}
	}
	return fallbackStartupArg
}
