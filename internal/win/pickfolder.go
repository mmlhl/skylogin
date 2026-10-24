package win

import (
	"syscall"
	"unsafe"
)

var (
	procSHBrowseFolder  = shell32dll.NewProc("SHBrowseForFolderW")
	procSHGetPathFromID = shell32dll.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree   = ole32dll.NewProc("CoTaskMemFree")
)

type browseInfo struct {
	hwndOwner      uintptr
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

const (
	bifReturnOnlyFSDirs = 0x00000001
	bifNewDialogStyle   = 0x00000040
)

// PickFolder 弹出"选择文件夹"对话框；用户取消时返回空串。
func PickFolder(title string) (string, error) {
	procCoInitialize.Call(0, 2) // COINIT_APARTMENTTHREADED

	disp := make([]uint16, 260)
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	bi := browseInfo{
		pszDisplayName: &disp[0],
		lpszTitle:      titlePtr,
		ulFlags:        bifReturnOnlyFSDirs | bifNewDialogStyle,
	}
	pidl, _, _ := procSHBrowseFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", nil // 取消
	}
	defer procCoTaskMemFree.Call(pidl)

	buf := make([]uint16, 1024)
	ok, _, _ := procSHGetPathFromID.Call(pidl, uintptr(unsafe.Pointer(&buf[0])))
	if ok == 0 {
		return "", nil
	}
	return syscall.UTF16ToString(buf), nil
}
