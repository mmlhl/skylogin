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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"skylogin/internal/fever"
)

const (
	shimBrowserName = "webview_support_browser.exe"
	shimRealName    = "webview_support_browser.real.exe"
	shimMarkerName  = ".shim-backup"
	// shimChannelFileName 是镜像里预置的"这条记录专属 channel"。
	// 有它就用它（同一个账号行永远同一个 profile，选区才记得住），
	// 没有就按镜像目录名派生（多开的临时实例）。
	shimChannelFileName = ".shim-channel"
)

// readShimChannel 读镜像里预置的 channel；格式不对或没有就返回空。
func readShimChannel(cefDir string) string {
	b, err := os.ReadFile(filepath.Join(cefDir, shimChannelFileName))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if s == "" || len(s) > 64 {
		return ""
	}
	for _, r := range s {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return ""
		}
	}
	return s
}

// writeShimChannelHint 把账号行专属 channel 写进镜像，给浏览器壳读。
func writeShimChannelHint(mirrorRoot, key string) {
	if key == "" {
		return
	}
	ch := cefChannelFor(key)
	if ch == "" {
		return
	}
	cefDir := filepath.Join(mirrorRoot, "webviewsupport.cef904430")
	if _, err := os.Stat(cefDir); err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(cefDir, shimChannelFileName), []byte(ch), 0o644); err != nil {
		logf("浏览器壳：写 channel 提示失败: %v", err)
		return
	}
	logf("浏览器壳：%s 固定使用 channel=%s", filepath.Base(mirrorRoot), ch)
}

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

	channel := readShimChannel(dir)
	if channel == "" {
		channel = shimChannel(filepath.Base(filepath.Dir(dir)))
	}
	debug := cefDebugEnabled()
	args, note := rewriteChannel(os.Args[1:], channel, debug)
	logf("浏览器壳：镜像 %s → channel=%s%s", filepath.Base(dir), channel, note)

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
// 返回改写后的参数，以及一句"改成了什么"（给日志用）。
// debug=true 时同时打开 CEF 的本地调试端口（排查用，见 cefDebugEnabled）。
//
// 参数形态：<Mpay_IPC_...> <base64 json> [其它开关]；认不出来的原样保留。
func rewriteChannel(args []string, channel string, debug bool) ([]string, string) {
	out := make([]string, 0, len(args))
	note := ""
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
		orig, _ := ext["cache_path_channel_name"].(string)
		next := pickChannel(orig, channel)
		if next != orig {
			note = fmt.Sprintf("（原 %s）", orig)
		}
		ext["cache_path_channel_name"] = next
		blob, _ := json.Marshal(ext)
		cfg["configs.configs_extension_json"] = string(blob)
		if debug {
			// 端口按**最终 profile 名**派生：一个客户端会起好几个浏览器，
			// 按登录 channel 算的话它们会抢同一个端口。
			port := debugPortFor(next)
			cfg["configs.remote_debugging_port"] = port
			note += fmt.Sprintf("（调试端口 %d）", port)
		}

		encoded, err := json.Marshal(cfg)
		if err != nil {
			out = append(out, a)
			continue
		}
		out = append(out, base64.StdEncoding.EncodeToString(encoded))
	}
	return out, note
}

// accountChannelPrefix 是游戏给"每行账号"分配的登录 profile 前缀。
// 实测：登录/选渠道页用 h5_copy<账号 id>，一行一份。
const accountChannelPrefix = "h5_copy"

// pickChannel 决定这条 base64 配置该用哪个 profile 名。
//
// 为什么不能一律换成 channel：一个客户端会先后拉起**三种**浏览器（实测）：
//
//	h5_copy<账号 id>  登录 / 选渠道     —— 本来就是一行一份，换成 channel 正合适
//	h5_payment        支付 / 精灵
//	ngwebview         活动页
//	gmbridge          游戏桥
//
// 后三种是所有实例共用的固定 profile。以前也一并换成 channel，于是它们和
// 登录页挤进同一个 profile 目录，彼此之间也互相挤——CEF 的 profile 是独占的，
// 后起的那个拿不到锁，表现就是"点开活动，浏览器直接报 DNS_PROBE_FINISHED_NXDOMAIN"。
//
// 所以除登录 profile 之外，一律**保留类别、只接账号键**：同类不串、跨类不撞。
func pickChannel(orig, channel string) string {
	if orig == "" || orig == channel {
		return channel
	}
	if strings.HasPrefix(orig, accountChannelPrefix) {
		return channel
	}
	suffix := strings.TrimPrefix(channel, "h5_")
	if suffix == "" {
		return channel
	}
	return orig + "_" + suffix
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

// cefDebugEnabled 判断要不要给浏览器开本地调试端口。
//
// 排查"活动页/选渠道页打不开"时最难受的是只知道一个 NXDOMAIN，看不到
// 真实 URL、也看不到是哪个请求挂了。把 %APPDATA%\SkyLogin\cef-debug 建出来，
// 壳就会给每个浏览器开一个按 channel 派生的 127.0.0.1 端口，外部脚本用
// CDP 就能直接读到页面标题、URL 和网络失败原因，不用再猜。
//
// 默认关闭：这是排查用的后门，不该常驻开着。
func cefDebugEnabled() bool {
	_, err := os.Stat(filepath.Join(dataDir(), "cef-debug"))
	return err == nil
}

// debugPortFor 从 channel 派生一个稳定的本地端口（多开之间不会撞）。
func debugPortFor(channel string) int {
	var h uint32 = 2166136261 // FNV-1a
	for i := 0; i < len(channel); i++ {
		h ^= uint32(channel[i])
		h *= 16777619
	}
	return 9400 + int(h%400)
}

// installWebviewShim 在镜像里把浏览器换成本程序的硬链接。
//
// 真身改名 .real.exe（这就是备份）；我们的 exe 以硬链接方式放进去。
// 硬防护：只允许改我们自己建的镜像目录，绝不碰游戏本体。
func installWebviewShim(mirrorRoot string) error {
	if !isOurMirror(mirrorRoot) {
		logf("浏览器壳：%s 不是镜像目录，拒绝修改", mirrorRoot)
		return fmt.Errorf("拒绝修改非镜像目录: %s", mirrorRoot)
	}
	cefDir := filepath.Join(mirrorRoot, "webviewsupport.cef904430")
	browser := filepath.Join(cefDir, shimBrowserName)
	if _, err := os.Stat(browser); err != nil {
		return nil // 这个版本没有浏览器组件，跳过
	}
	src, err := shimSource(mirrorRoot)
	if err != nil {
		return err
	}
	realExe := filepath.Join(cefDir, shimRealName)
	if _, err := os.Stat(realExe); err != nil {
		// 改名就是备份：真身留在同目录，随时可以还原
		if err := os.Rename(browser, realExe); err != nil {
			return err
		}
		logf("浏览器壳：已备份真身 %s", shimRealName)
	}
	_ = os.WriteFile(filepath.Join(cefDir, shimMarkerName), []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644)
	_ = os.Remove(browser)
	if err := os.Link(src, browser); err != nil {
		if err := copyFile(src, browser); err != nil {
			return err
		}
	}
	return nil
}

// shimTemplateName 是镜像根目录下的"壳模板"。同卷硬链接不了本程序时，
// 先在这里放一份，之后每套镜像都从模板硬链接，不再逐份复制。
const shimTemplateName = ".sky-shim.exe"

// shimSource 返回可以用来硬链接的壳本体。
//
// 为什么需要模板：os.Executable() 和镜像不在同一卷时（工具装在 C:、
// 游戏和多开目录在 D:/E:），os.Link 必然失败，旧代码退化成 copyFile ——
// 每套镜像复制一份约 30MB 的本程序，11 开就是 300MB+ 的写入，还都是
// 刚建好镜像、最该快的时候。模板放在镜像根目录（与镜像同卷），
// 第一套复制一次，其余全部硬链接。
func shimSource(mirrorRoot string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if fever.SameVolume(self, mirrorRoot) {
		return self, nil
	}
	selfInfo, err := os.Stat(self)
	if err != nil {
		return "", err
	}
	tmpl := filepath.Join(mirrorRoot, shimTemplateName)
	if ti, err := os.Stat(tmpl); err == nil && ti.Size() == selfInfo.Size() && !ti.ModTime().Before(selfInfo.ModTime()) {
		return tmpl, nil
	}
	if err := os.MkdirAll(mirrorRoot, 0o755); err != nil {
		return "", err
	}
	_ = os.Remove(tmpl)
	if err := copyFile(self, tmpl); err != nil {
		return "", err
	}
	return tmpl, nil
}

// restoreWebviewShim 把镜像里的浏览器还原成真身（删壳、改回原名）。
//
// 客户端全部退出后、启动器退出前、下次启动清理残留时都会调。
// 镜像本来就要删，还原是为了"即使删不掉也不留一个被改过的浏览器"。
func restoreWebviewShim(mirrorRoot string) {
	if !isOurMirror(mirrorRoot) {
		return
	}
	cefDir := filepath.Join(mirrorRoot, "webviewsupport.cef904430")
	browser := filepath.Join(cefDir, shimBrowserName)
	realExe := filepath.Join(cefDir, shimRealName)
	if _, err := os.Stat(realExe); err != nil {
		return // 没有备份，说明没装过壳
	}
	if _, err := os.Stat(browser); err == nil {
		for i := 0; i < 20; i++ { // 游戏可能还没完全退，等它释放文件
			if err := os.Remove(browser); err == nil {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	if _, err := os.Stat(browser); err == nil {
		return // 删不掉就别动真身，免得两个都没了
	}
	if err := os.Rename(realExe, browser); err == nil {
		_ = os.Remove(filepath.Join(cefDir, shimMarkerName))
	}
}

// isOurMirror 判断目录是不是我们建的镜像（名字形如 sky-<数字>-<数字>）。
func isOurMirror(dir string) bool {
	parts := strings.Split(filepath.Base(dir), "-")
	if len(parts) != 3 || parts[0] == "" {
		return false
	}
	for _, p := range parts[1:] {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
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
