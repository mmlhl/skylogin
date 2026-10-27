package win

import (
	"syscall"
	"unsafe"
)

var (
	procCreateMutexW  = kernel32dll.NewProc("CreateMutexW")
	procFindWindowW   = user32dll.NewProc("FindWindowW")
	procSetForeground = user32dll.NewProc("SetForegroundWindow")
	procShowWindow    = user32dll.NewProc("ShowWindow")
	procIsIconic      = user32dll.NewProc("IsIconic")
)

const (
	errorAlreadyExists = 183
	swRestore          = 9
)

var mutexHandle uintptr

// TryAcquireSingleInstance 创建一个命名互斥量。
// 返回 false 表示已有实例在运行（此时应激活旧窗口并退出）。
//
// 注意：判断"已存在"必须用 Call 自己返回的 errno。
// 单独再调一次 GetLastError 是不行的——两次调用之间 Go 运行时可能已经
// 做了别的系统调用，把 last error 冲掉了（这里踩过坑）。
func TryAcquireSingleInstance(name string) bool {
	namePtr, _ := syscall.UTF16PtrFromString(name)
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		return true // 创建失败就不拦，宁可多开也不要打不开
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		return false
	}
	mutexHandle = h
	return true
}

// FocusWindowByTitle 找到标题匹配的窗口并置前；成功返回 true。
func FocusWindowByTitle(title string) bool {
	t, _ := syscall.UTF16PtrFromString(title)
	h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(t)))
	if h == 0 {
		return false
	}
	if iconic, _, _ := procIsIconic.Call(h); iconic != 0 {
		procShowWindow.Call(h, swRestore)
	}
	procSetForeground.Call(h)
	return true
}
