package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"skylogin/internal/fever"
	"skylogin/internal/protocol"
)

// Version 是版本号，发布构建时用链接参数注入：
//
//	go build -ldflags "-X skylogin/internal/app.Version=1.2.0 ..."
//
// 不注入时用下面这个默认值（本地开发用）。
var Version = "1.0.0"

// RepoURL 是项目地址。
const RepoURL = "https://github.com/mmlhl/skylogin"

// settingsView 给界面用的设置快照。
func settingsView() map[string]any {
	c := loadConfig()
	exe := c.SkyExe
	auto := false
	if !fileExists(exe) {
		if p, err := findSkyFn(); err == nil {
			exe = p
			auto = c.SkyExe == ""
		}
	}
	found := fileExists(exe)
	return map[string]any{
		"skyExe":           exe,
		"skyFound":         found,
		"skyAuto":          auto,
		"deviceId":         deviceIDForUI(),
		"deviceIdSet":      true,
		"deviceIdSource":   deviceIDSource(),
		"deviceInfo":       deviceInfoForUI(),
		"closeLauncher":    c.CloseLauncherEnabled(),
		"railWidth":        c.RailWidth,
		"mirrorDir":        fever.MirrorRoot(exe, c.MirrorDir),
		"mirrorDefault":    fever.DefaultMirrorRoot(exe),
		"mirrorSameVolume": fever.SameVolume(fever.MirrorRoot(exe, c.MirrorDir), filepath.Dir(exe)),
		"mirrorSizeText":   fever.HumanSize(fever.DirLogicalSize(filepath.Dir(exe))),
		"multiQualityMode": c.MultiQualityMode,
		"multiQualityFps":  c.MultiQualityFps,
		"mouseArbiter":     c.MouseArbiterEnabled,
		"mouseArbiterMin":  c.MouseArbiterMin,
		"dataDir":          dataDir(),
		"version":          Version,
		"repo":             RepoURL,
		"goVersion":        runtime.Version(),
		"protocol":         protocolSource(),
	}
}

// applySettings 保存界面上改动的设置；传 nil 表示不改这一项。
func applySettings(skyExe, deviceID, mirrorDir *string, closeLauncher *bool, multiQuality *string, multiFps *int, mouseArbiter *bool, arbiterMin *int) error {
	c := loadConfig()
	if skyExe != nil {
		p := *skyExe
		if p != "" {
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("这个路径不存在: %s", p)
			}
			if filepath.Base(p) != "sky.exe" {
				return errors.New("请选择 sky.exe（光遇主程序）")
			}
		}
		c.SkyExe = p
	}
	if deviceID != nil {
		c.MpayDeviceID = *deviceID
	}
	if mirrorDir != nil {
		v := strings.TrimSpace(*mirrorDir)
		if v == "" {
			c.MirrorDir = "" // 空 = 回到默认（游戏盘根目录）
		} else {
			sky := c.SkyExe
			if sky == "" {
				sky = resolveSkyPath()
			}
			if sky == "" {
				return errors.New("还没设置游戏路径，请先选择 sky.exe")
			}
			// 硬链接只能在同一磁盘内建立；跨盘会退化成复制（多占好几 GB）
			if !fever.SameVolume(v, filepath.Dir(sky)) {
				return fmt.Errorf("多开目录必须和游戏在同一个磁盘（游戏在 %s）", filepath.VolumeName(filepath.Dir(sky)))
			}
			// 只接受空目录或还不存在的目录，避免动到用户已有的文件
			if entries, err := os.ReadDir(v); err == nil && len(entries) > 0 {
				return errors.New("多开目录必须是空文件夹（里面已有文件的话请换一个）")
			}
			c.MirrorDir = v
		}
	}
	if closeLauncher != nil {
		c.CloseLauncher = closeLauncher
	}
	if multiQuality != nil {
		c.MultiQualityMode = strings.TrimSpace(*multiQuality)
	}
	if mouseArbiter != nil {
		c.MouseArbiterEnabled = *mouseArbiter
	}
	if arbiterMin != nil {
		c.MouseArbiterMin = *arbiterMin
	}
	if multiFps != nil {
		if *multiFps < 0 {
			*multiFps = 0
		}
		c.MultiQualityFps = *multiFps
	}
	return saveConfig(c)
}

// cleanupOptions 是"清理工具数据"里勾选的项。
type cleanupOptions struct {
	Accounts bool `json:"accounts"`
	Cache    bool `json:"cache"`
	Logs     bool `json:"logs"`
}

// cleanupData 按勾选删除本地数据，返回删掉的条目说明。
func cleanupData(o cleanupOptions) ([]string, error) {
	if !o.Accounts && !o.Cache && !o.Logs {
		return nil, errors.New("没有选择要清理的内容")
	}
	var done []string
	if o.Accounts {
		for _, f := range []string{"accounts.json", "current.txt", "last_scan.json", "sdk_device.json", "huawei_device.json"} {
			if err := os.Remove(filepath.Join(dataDir(), f)); err == nil {
				done = append(done, f)
			}
		}
		_ = os.Remove(configPath()) // 里面的 device_id 也一并清掉
		done = append(done, "config.json")
	}
	if o.Cache {
		dir := filepath.Join(dataDir(), "webview")
		if _, err := os.Stat(dir); err == nil {
			if err := os.RemoveAll(dir); err != nil {
				return done, fmt.Errorf("清理缓存失败: %w", err)
			}
			done = append(done, "webview 缓存")
		}
	}
	if o.Logs {
		if err := os.Remove(filepath.Join(dataDir(), "log.txt")); err == nil {
			done = append(done, "log.txt")
		}
	}
	return done, nil
}

// openLogFile 在资源管理器里定位日志文件；没有日志就打开数据目录。
func openLogFile() error {
	logPath := filepath.Join(dataDir(), "log.txt")
	cmd := exec.Command("explorer.exe")
	if _, err := os.Stat(logPath); err == nil {
		cmd = exec.Command("explorer.exe", "/select,"+logPath)
	} else {
		cmd = exec.Command("explorer.exe", dataDir())
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// revealInExplorer 在资源管理器里选中一个文件。
func revealInExplorer(path string) error {
	cmd := exec.Command("explorer.exe", "/select,"+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// fileExists 判断路径是不是一个存在的文件。
func fileExists(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// protocolSource 说明协议参数的来源，方便排查"为什么参数不对"。
func protocolSource() string {
	p := protocol.Get()
	if p.UpdatedAt != "" {
		return "远程配置 " + p.UpdatedAt
	}
	if fileExists(filepath.Join(dataDir(), "protocol.json")) {
		return "本地缓存"
	}
	return "内置默认"
}

// mirrorLogicalSize 缓存游戏目录的逻辑大小：/api/state 每秒轮询一次，
// 每次去遍历几千个文件太浪费。
var (
	mirrorSizeMu   sync.Mutex
	mirrorSizePath string
	mirrorSizeVal  int64
)

func mirrorLogicalSize(skyExe string) int64 {
	dir := filepath.Dir(skyExe)
	mirrorSizeMu.Lock()
	defer mirrorSizeMu.Unlock()
	if mirrorSizePath == dir && mirrorSizeVal > 0 {
		return mirrorSizeVal
	}
	mirrorSizeVal = fever.DirLogicalSize(dir)
	mirrorSizePath = dir
	return mirrorSizeVal
}

// buildTimeText 返回 exe 的构建时间（就是文件修改时间）。
//
// 界面上显示出来，用户一眼能确认自己跑的是不是刚构建的那个版本——
// 之前换 exe 时踩过"以为重启了、其实还是旧进程"的坑。
func buildTimeText() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	st, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	return st.ModTime().Format("01-02 15:04")
}
