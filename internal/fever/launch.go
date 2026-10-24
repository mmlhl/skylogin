package fever

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// LauncherPID 返回占用 LHMW_FG_Main 的进程 ID（即发烧平台）。
func LauncherPID() (uint32, bool) {
	h, ok := Existing()
	if !ok {
		return 0, false
	}
	pid := WindowPID(h)
	return pid, pid != 0
}

// KillLauncher 结束发烧平台进程，让出 IPC 窗口。
// 只有确认占用者是发烧平台本体才会结束，避免误杀其它工具。
func KillLauncher() error {
	pid, ok := LauncherPID()
	if !ok {
		return nil
	}
	name := processName(pid)
	switch strings.ToLower(name) {
	case "fevergamesinstaller.exe", "fevergameslauncher.exe", "fevergamesweb.exe", "fevergamesupdate.exe":
		// ok
	default:
		return fmt.Errorf("IPC 窗口被 %s (pid=%d) 占用，请先手动关闭它再启动游戏", name, pid)
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return fmt.Errorf("打开发烧平台进程失败(pid=%d): %w", pid, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return fmt.Errorf("结束发烧平台失败(pid=%d): %w", pid, err)
	}
	for i := 0; i < 40; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, still := Existing(); !still {
			return nil
		}
	}
	return errors.New("发烧平台仍在运行，请手动退出后再试")
}

// processName 取进程可执行文件名。
func processName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "?"
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "?"
	}
	return filepath.Base(windows.UTF16ToString(buf[:size]))
}

// StartSky 启动光遇，返回进程句柄。
func StartSky(exe string) (*os.Process, error) {
	cmd := exec.Command(exe, "--start_from_launcher=1")
	cmd.Dir = filepath.Dir(exe)
	// 即便目标是控制台程序，也不要弹出窗口
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

// IsSkyRunning 检查光遇是否在运行。
//
// 这里必须用系统快照 API，不能调 tasklist：
// 界面每隔几秒轮询一次状态，一调外部命令就会闪一下黑窗口。
func IsSkyRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "sky.exe") {
			return true
		}
	}
	return false
}

// ProcessNameOf 返回进程可执行文件名（诊断用）。
func ProcessNameOf(pid uint32) string { return processName(pid) }

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040
	swShowNormal          = 1
)

var (
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

// StartSkyElevated 用 ShellExecuteExW 以管理员身份启动光遇（会弹 UAC）。
func StartSkyElevated(exe string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	args, _ := windows.UTF16PtrFromString("--start_from_launcher=1")
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))

	sei := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: args,
		lpDirectory:  dir,
		nShow:        swShowNormal,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r == 0 {
		return fmt.Errorf("ShellExecuteEx(runas) 失败: %v", err)
	}
	if sei.hProcess != 0 {
		windows.CloseHandle(windows.Handle(sei.hProcess))
	}
	return nil
}

// StartSkyAuto 先直接启动，管理员权限不足时自动走提权启动。
func StartSkyAuto(exe string) (elevated bool, err error) {
	if _, err := StartSky(exe); err == nil {
		return false, nil
	}
	if err := StartSkyElevated(exe); err != nil {
		return true, err
	}
	return true, nil
}

// CountProcess 数一下当前有多少个同名进程（诊断用）。
//
// 用来确认"多开时游戏是不是共用同一个 webview 浏览器进程"：
// webview_support_browser.exe 是游戏自带的 CEF 浏览器，如果两个客户端
// 只对应一个浏览器进程，就说明它是全机单实例，第二个客户端的
// mpay_ipc_... 域名解析不了。
func CountProcess(name string) int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)

	n := 0
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), name) {
			n++
		}
	}
	return n
}
