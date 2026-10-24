package win

import (
	"syscall"
	"unsafe"
)

var (
	versiondll         = syscall.NewLazyDLL("version.dll")
	procGetVerInfoSize = versiondll.NewProc("GetFileVersionInfoSizeW")
	procGetVerInfo     = versiondll.NewProc("GetFileVersionInfoW")
	procVerQueryValue  = versiondll.NewProc("VerQueryValueW")
)

// FileVersion 读取 exe/dll 的资源版本号（取 FileVersion 的前三段）。
func FileVersion(path string) (major, minor, patch uint16, ok bool) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, 0, false
	}
	var ignored uint32
	size, _, _ := procGetVerInfoSize.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&ignored)))
	if size == 0 {
		return 0, 0, 0, false
	}
	buf := make([]byte, size)
	if r, _, _ := procGetVerInfo.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(size), uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return 0, 0, 0, false
	}

	root, _ := syscall.UTF16PtrFromString(`\`)
	var vptr unsafe.Pointer
	var vlen uint32
	if r, _, _ := procVerQueryValue.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&vptr)), uintptr(unsafe.Pointer(&vlen))); r == 0 || vlen == 0 {
		return 0, 0, 0, false
	}
	// VS_FIXEDFILEINFO：offset 8 起为 FileVersionMS，12 起为 FileVersionLS
	base := (*[16]uint32)(vptr)
	ms := base[2]
	ls := base[3]
	return uint16(ms >> 16), uint16(ms & 0xFFFF), uint16(ls >> 16), true
}
