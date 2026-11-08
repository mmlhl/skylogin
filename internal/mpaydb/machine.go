// 本机标识：put_account 的参数之一。
//
// mpay.db 的 User::Save 变换会把机器标识哈希进去（变换本身在 wasm 里），
// 宿主只负责取到 OS 层的标识字符串。取法和参考实现一致：
// 主硬盘（PhysicalDrive0）序列号优先，取不到退回注册表 MachineGuid。
package mpaydb

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// MachineIdentifier 返回本机标识字符串（硬盘序列号或 MachineGuid）。
func MachineIdentifier() string {
	if s := diskSerial(); s != "" {
		return s
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`,
		registry.READ|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	return v
}

// diskSerial 用 IOCTL_STORAGE_QUERY_PROPERTY 读 PhysicalDrive0 的序列号。
// 失败返回空串（常见于权限不够，调用方自行退回 MachineGuid）。
func diskSerial() string {
	const ioctlStorageQueryProperty = 0x2D1400 // 2954240

	path, err := windows.UTF16PtrFromString(`\\.\PhysicalDrive0`)
	if err != nil {
		return ""
	}
	handle, err := windows.CreateFile(path, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)

	// STORAGE_PROPERTY_QUERY：PropertyId=0(StorageDeviceProperty), QueryType=0
	query := make([]byte, 12)
	// 字段布局：PropertyId(4) QueryType(4) AdditionalParameters(4)——全零即可
	out := make([]byte, 4096)
	var returned uint32
	err = windows.DeviceIoControl(handle, ioctlStorageQueryProperty,
		&query[0], uint32(len(query)),
		&out[0], uint32(len(out)), &returned, nil)
	if err != nil {
		return ""
	}
	// 返回结构：STORAGE_DEVICE_DESCRIPTOR，offset 24 处是 SerialNumberOffset
	if len(out) < 28 {
		return ""
	}
	offset := int(out[24]) | int(out[25])<<8 | int(out[26])<<16 | int(out[27])<<24
	if offset <= 0 || offset >= len(out) || returned <= uint32(offset) {
		return ""
	}
	serial := string(out[offset:min(int(returned), len(out))])
	if i := indexZero(serial); i >= 0 {
		serial = serial[:i]
	}
	serial = strings.TrimSpace(serial)
	if len(serial) > 64 { // 序列号不会很长，防御性截断
		serial = serial[:64]
	}
	return serial
}

func indexZero(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return i
		}
	}
	return -1
}

var _ = fmt.Sprintf
var _ = unsafe.Pointer(nil)
