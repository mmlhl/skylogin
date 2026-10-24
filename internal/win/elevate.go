package win

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32dll          = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = shell32dll.NewProc("ShellExecuteExW")
	elevateOnce         sync.Once
)

type shellExecuteInfoW struct {
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

const seeMaskNoCloseProcess = 0x00000040

// IsElevated 判断当前进程是否具备管理员权限。
func IsElevated() bool {
	t, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer t.Close()
	return t.IsElevated()
}

// relaunchElevated 以管理员身份重启自身（相同的命令行参数），随后退出当前进程。
func relaunchElevated() error {
	var err error
	elevateOnce.Do(func() {
		exe, e := os.Executable()
		if e != nil {
			err = e
			return
		}
		verb, _ := windows.UTF16PtrFromString("runas")
		file, _ := windows.UTF16PtrFromString(exe)
		var argLine string
		for i, a := range os.Args[1:] {
			if i > 0 {
				argLine += " "
			}
			if len(a) > 0 && (a[0] == '-' || a[0] == '/') {
				argLine += a
			} else {
				argLine += `"` + a + `"`
			}
		}
		params, _ := windows.UTF16PtrFromString(argLine)
		cwd, _ := os.Getwd()
		dir, _ := windows.UTF16PtrFromString(cwd)

		sei := shellExecuteInfoW{
			fMask:        seeMaskNoCloseProcess,
			lpVerb:       verb,
			lpFile:       file,
			lpParameters: params,
			lpDirectory:  dir,
			nShow:        1,
		}
		sei.cbSize = uint32(unsafe.Sizeof(sei))
		r, _, e := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
		if r == 0 {
			err = fmt.Errorf("提权启动失败: %v", e)
			return
		}
		if sei.hProcess != 0 {
			windows.CloseHandle(windows.Handle(sei.hProcess))
		}
	})
	return err
}

// RequireElevation 保证当前进程以管理员身份运行；不是就重启自身并退出。
func RequireElevation() {
	if IsElevated() {
		return
	}
	fmt.Println("光遇本体要求管理员权限（Sky.exe 的清单是 requireAdministrator），正在提权…")
	if err := relaunchElevated(); err != nil {
		fmt.Fprintln(os.Stderr, "提权失败:", err)
		fmt.Fprintln(os.Stderr, "请右键 skylogin.exe → 以管理员身份运行")
		os.Exit(1)
	}
	os.Exit(0)
}
