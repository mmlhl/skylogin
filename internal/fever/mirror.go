package fever

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DefaultMirrorRoot 返回默认的多开镜像根目录：游戏所在盘的 \SkyLoginMirror。
//
// 不能放在游戏目录里（比如 D:\FeverApps\sky2）：发烧平台的自动探测会扫
// 默认游戏目录的子目录找 sky.exe，多出来的会被当成第二份游戏。
func DefaultMirrorRoot(skyExe string) string {
	vol := filepath.VolumeName(filepath.Dir(skyExe))
	if vol == "" {
		return ""
	}
	return filepath.Join(vol+string(filepath.Separator), "SkyLoginMirror")
}

// MirrorRoot 解析用户配置的镜像根目录，空值回退到默认。
func MirrorRoot(skyExe, configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return DefaultMirrorRoot(skyExe)
	}
	return filepath.Clean(configured)
}

// SameVolume 判断两个路径是否在同一磁盘卷（硬链接只能在同卷内建立）。
func SameVolume(a, b string) bool {
	return strings.EqualFold(filepath.VolumeName(a), filepath.VolumeName(b))
}

// DirLogicalSize 统计目录下所有文件的大小之和（只看元数据，很快）。
//
// 这是"逻辑大小"：硬链接时它不等于真实磁盘占用，用来给用户一个数量级参考。
func DirLogicalSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// MirrorInstall 为这次多开新建一份"看起来是另一份安装"的游戏目录。
//
// 为什么需要：游戏自带的 webview 浏览器（webview_support_browser.exe）是
// 全机单实例的，第二个客户端复用它就会解析不了自己的 mpay_ipc_... 域名
// （表现为"找不到服务器 IP"）。给它一份独立目录，浏览器进程/缓存才各用各的。
//
// 同卷内用硬链接（os.Link）：只有目录项是新的，GB 级资源不复制。
// 跨卷时硬链接会失败，退化成复制（很慢、很占空间，调用方应先提示用户）。
func MirrorInstall(skyExe, name, root string) (string, error) {
	src := filepath.Dir(skyExe)
	if root == "" {
		root = DefaultMirrorRoot(skyExe)
	}
	if root == "" {
		return "", fmt.Errorf("拿不到游戏所在盘符: %s", src)
	}
	dst := filepath.Join(root, name)

	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	if err := os.RemoveAll(dst); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}

	copied := 0
	err := filepath.Walk(src, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // 读不了的跳过，别让整个镜像失败
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if skipMirrorFile(info.Name()) {
			return nil
		}
		if err := os.Link(p, target); err == nil {
			return nil
		}
		copied++
		return copyFileRaw(p, target)
	})
	if err != nil {
		return dst, err
	}
	if copied > 0 {
		return dst, fmt.Errorf("镜像已建好，但有 %d 个文件是复制的（硬链接失败，可能跨盘）", copied)
	}
	return dst, nil
}

// skipMirrorFile 跳过不该镜像的文件：日志会被两个客户端抢写。
func skipMirrorFile(name string) bool {
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".log") || strings.HasSuffix(low, ".tlog") ||
		strings.HasSuffix(low, ".dmp")
}

// RemoveAllMirrors 删掉某个根目录下所有镜像目录。
func RemoveAllMirrors(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			removeDir(filepath.Join(root, e.Name()))
		}
	}
	_ = os.Remove(root) // 空了就顺手删掉
}

// removeDir 删目录，失败就重试几次。
//
// 游戏退出后，崩溃上报/浏览器子进程可能还短暂持有 DLL 的映像句柄，
// 这时 DeleteFile 会 Access denied。等一两秒通常就放开了。
func removeDir(dir string) bool {
	for i := 0; i < 6; i++ {
		if err := os.RemoveAll(dir); err == nil {
			if _, statErr := os.Stat(dir); statErr != nil {
				return true
			}
		}
		time.Sleep(700 * time.Millisecond)
	}
	return false
}

// MirrorRootExists 判断镜像根目录是否还在。
func MirrorRootExists(root string) bool {
	_, err := os.Stat(root)
	return err == nil
}

// MirrorsRemaining 数一下某份安装还剩几套镜像目录。
//
// 根目录故意保留（用户要求，也避免反复创建删除），所以清理完不能再用
// "根目录不存在"来判断，要看里面还有没有 <游戏目录名>- 开头的子目录。
func MirrorsRemaining(skyExe, root string) int {
	prefix := filepath.Base(filepath.Dir(skyExe)) + "-"
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			n++
		}
	}
	return n
}

// RemoveMirrors 删掉某份安装对应的镜像（其它游戏的不动）。
func RemoveMirrors(skyExe, root string) {
	src := filepath.Dir(skyExe)
	if root == "" {
		root = DefaultMirrorRoot(skyExe)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	prefix := filepath.Base(src) + "-"
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			removeDir(filepath.Join(root, e.Name()))
		}
	}
	_ = os.Remove(root)
}

// ProcessesFrom 返回从指定目录启动的 sky.exe 的 pid（用于判断镜像是否还在用）。
func ProcessesFrom(gameDir string) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var out []uint32
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "sky.exe") {
			continue
		}
		if p := procPath(uint32(entry.ProcessID)); p != "" && strings.HasPrefix(strings.ToLower(p), strings.ToLower(gameDir)) {
			out = append(out, entry.ProcessID)
		}
	}
	return out
}

var (
	modkernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	procQueryFullProcessImageNameW = modkernel32.NewProc("QueryFullProcessImageNameW")
)

// procPath 取进程的完整镜像路径（权限不足时返回空）。
func procPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

// HumanSize 把字节数变成 5.1 GB 这种形式。
func HumanSize(n int64) string {
	const gb = 1 << 30
	const mb = 1 << 20
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f GB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.0f MB", float64(n)/mb)
	default:
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	}
}

// WaitProcessExit 阻塞到指定进程退出（拿不到句柄就直接返回）。
func WaitProcessExit(pid uint32) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	windows.WaitForSingleObject(h, windows.INFINITE)
}

// copyFileRaw 是最朴素的整文件复制，只在硬链接不可用时兜底。
func copyFileRaw(src, dst string) error {
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

// ProcessAlive 判断进程是否还活着。
func ProcessAlive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, 0)
	return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
}

// KillProcessTree 结束一个进程及其所有子进程（镜像安装下的 webview 浏览器等）。
func KillProcessTree(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snap)

	children := map[uint32][]uint32{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		children[entry.ParentProcessID] = append(children[entry.ParentProcessID], entry.ProcessID)
	}

	var kill func(uint32)
	kill = func(p uint32) {
		for _, c := range children[p] {
			kill(c)
		}
		if h, e := windows.OpenProcess(windows.PROCESS_TERMINATE, false, p); e == nil {
			_ = windows.TerminateProcess(h, 1)
			windows.CloseHandle(h)
		}
	}
	kill(pid)
	// 主进程一定杀（递归里已经包含，这里兜底）
	if h, e := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid); e == nil {
		_ = windows.TerminateProcess(h, 1)
		windows.CloseHandle(h)
	}
	return nil
}
