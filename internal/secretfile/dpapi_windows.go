package secretfile

// DPAPI（CryptProtectData / CryptUnprotectData）的纯 syscall 实现。
// 选它而不是自己管密钥：密钥由 Windows 按当前用户派生与管理，
// 代码零密钥、无 CGO（目标机器没有 gcc 是硬约束）。
// 代价：文件只有"同一台机器的同一个 Windows 用户"能解——对凭证
// 存储恰恰是正确的行为。

import (
	"syscall"
	"unsafe"
)

var (
	crypt32               = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	kernel32Local          = syscall.NewLazyDLL("kernel32.dll")
	procLocalFree          = kernel32Local.NewProc("LocalFree")
)

// CRYPTPROTECT_UI_FORBIDDEN：无界面服务环境必须（弹 UI 会卡死）。
const cryptProtectUIForbidden = 0x1

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func blobFromBytes(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

// Protect 用 DPAPI 加密（当前用户作用域）。
func Protect(data []byte) ([]byte, error) {
	in := blobFromBytes(data)
	var out dataBlob
	r, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(in)), 0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	res := make([]byte, out.cbData)
	copy(res, unsafe.Slice(out.pbData, out.cbData))
	return res, nil
}

// Unprotect 解开 Protect 的输出。
func Unprotect(data []byte) ([]byte, error) {
	in := blobFromBytes(data)
	var out dataBlob
	r, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(in)), 0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	res := make([]byte, out.cbData)
	copy(res, unsafe.Slice(out.pbData, out.cbData))
	return res, nil
}
