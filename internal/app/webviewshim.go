package app

// 浏览器壳（webview shim）。
//
// 游戏的 CEF 浏览器是"一个 profile 只能有一个进程"，profile 目录是
// %APPDATA%\Netease\Mpay\MWSlocalStorage\<cache_path_channel_name>。
// 所有客户端共用同一个 channel，于是只有第一个客户端能真正起浏览器；
// 后面的都附到它身上，它们各自的 mpay_ipc_<pid>_... 解析不了——
// 表现就是官服"选择 iOS/安卓渠道"那个网页报 ERR_NAME_NOT_RESOLVED。
//
// 做法：镜像目录里把 webview_support_browser.exe 换成本程序的硬链接
// （真身改名 .real.exe）。游戏拉起"浏览器"时其实拉起我们，我们把
// 命令行里 base64 配置的 channel 改成镜像专属的名字，再启动真身。
// 这样每个客户端都有独立 profile → 各起一个浏览器进程。

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	shimBrowserName = "webview_support_browser.exe"
	shimRealName    = "webview_support_browser.real.exe"
)

// IsWebviewShim 判断当前进程是不是被游戏当成浏览器拉起来的。
func IsWebviewShim() bool {
	return strings.EqualFold(filepath.Base(os.Args[0]), shimBrowserName)
}

// RunWebviewShim 改写参数并启动真正的浏览器，等它退出后返回退出码。
func RunWebviewShim() int {
	self, err := os.Executable()
	if err != nil {
		return 1
	}
	dir := filepath.Dir(self)
	realExe := filepath.Join(dir, shimRealName)
	if _, err := os.Stat(realExe); err != nil {
		logf("浏览器壳：真身不存在（%s），直接退出", realExe)
		return 1
	}

	channel := shimChannel(filepath.Base(filepath.Dir(dir)))
	args := rewriteChannel(os.Args[1:], channel)
	logf("浏览器壳：镜像 %s → channel=%s", filepath.Base(dir), channel)

	cmd := exec.Command(realExe, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		logf("浏览器壳：启动真身失败: %v", err)
		return 1
	}
	err = cmd.Wait()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}

// shimChannel 用镜像目录名派生一个稳定、合法的 channel 名。
//
// 注意传进来的是镜像根目录名（sky-<毫秒>-<序号>），不能是 webviewsupport.*，
// 否则所有镜像会得到同一个 channel，又回到共用一个 profile。
func shimChannel(mirrorName string) string {
	var b strings.Builder
	for _, r := range mirrorName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if name == "" {
		name = "webview"
	}
	return "h5_" + name
}

// rewriteChannel 把参数里那个 base64 配置解出来，换掉 cache_path_channel_name。
//
// 参数形态：<Mpay_IPC_...> <base64 json> [其它开关]；认不出来的原样保留。
func rewriteChannel(args []string, channel string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			out = append(out, a)
			continue
		}
		cfg, ok := decodeWebviewConfig(a)
		if !ok {
			out = append(out, a)
			continue
		}
		ext := map[string]any{}
		if raw, ok := cfg["configs.configs_extension_json"].(string); ok {
			_ = json.Unmarshal([]byte(raw), &ext)
		}
		ext["cache_path_channel_name"] = channel
		blob, _ := json.Marshal(ext)
		cfg["configs.configs_extension_json"] = string(blob)

		encoded, err := json.Marshal(cfg)
		if err != nil {
			out = append(out, a)
			continue
		}
		out = append(out, base64.StdEncoding.EncodeToString(encoded))
	}
	return out
}

// decodeWebviewConfig 试解 base64 → JSON，并确认是浏览器的配置。
func decodeWebviewConfig(s string) (map[string]any, bool) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, false
	}
	if _, ok := cfg["webview_type"]; !ok {
		return nil, false
	}
	return cfg, true
}

// installWebviewShim 在镜像里把浏览器换成本程序的硬链接。
//
// 真身改名 .real.exe；我们的 exe 以硬链接方式放进去（同盘零占用）。
func installWebviewShim(mirrorRoot string) error {
	cefDir := filepath.Join(mirrorRoot, "webviewsupport.cef904430")
	browser := filepath.Join(cefDir, shimBrowserName)
	if _, err := os.Stat(browser); err != nil {
		return nil // 这个版本没有浏览器组件，跳过
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	realExe := filepath.Join(cefDir, shimRealName)
	if _, err := os.Stat(realExe); err != nil {
		if err := os.Rename(browser, realExe); err != nil {
			return err
		}
	}
	_ = os.Remove(browser)
	if err := os.Link(self, browser); err != nil {
		if err := copyFile(self, browser); err != nil {
			return err
		}
	}
	return nil
}

// copyFile 整文件复制（硬链接不可用时兜底）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
