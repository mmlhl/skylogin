package win

import (
	"syscall"
	"unsafe"
)

var (
	comdlg32dll      = syscall.NewLazyDLL("comdlg32.dll")
	procGetOpenFile  = comdlg32dll.NewProc("GetOpenFileNameW")
	ole32dll         = syscall.NewLazyDLL("ole32.dll")
	procCoInitialize = ole32dll.NewProc("CoInitializeEx")
)

const (
	ofnExplorer      = 0x00080000
	ofnFileMustExist = 0x00001000
	ofnPathMustExist = 0x00000800
	ofnNoChangeDir   = 0x00000008
)

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

// utf16z 把若干字符串拼成以 0 分隔、双 0 结尾的过滤器串。
func utf16z(parts ...string) []uint16 {
	var out []uint16
	for _, p := range parts {
		u, _ := syscall.UTF16FromString(p)
		out = append(out, u[:len(u)-1]...)
		out = append(out, 0)
	}
	out = append(out, 0)
	return out
}

// PickExeFile 弹出系统文件选择框，返回选中的路径；用户取消时返回空串。
func PickExeFile(title string) (string, error) {
	// 通用对话框需要 COM 初始化；失败也不影响使用
	procCoInitialize.Call(0, 2) // COINIT_APARTMENTTHREADED

	buf := make([]uint16, 1024)
	filter := utf16z("光遇主程序 (sky.exe)", "sky.exe", "程序 (*.exe)", "*.exe")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	defExt, _ := syscall.UTF16PtrFromString("exe")

	ofn := openFileName{
		lpstrFilter:  &filter[0],
		nFilterIndex: 1,
		lpstrFile:    &buf[0],
		nMaxFile:     uint32(len(buf)),
		lpstrTitle:   titlePtr,
		lpstrDefExt:  defExt,
		flags:        ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))

	r, _, _ := procGetOpenFile.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", nil // 取消
	}
	return syscall.UTF16ToString(buf), nil
}
