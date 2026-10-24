package win

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// StorageDeviceProperty / PropertyStandardQuery
const ioctlStorageQueryProperty = 0x2D1400

// DiskSerial 读取主硬盘（PhysicalDrive0）的序列号。
// 读不到时返回空串（比如没有管理员权限或虚拟磁盘）。
func DiskSerial() string {
	path, err := windows.UTF16PtrFromString(`\\.\PhysicalDrive0`)
	if err != nil {
		return ""
	}
	h, err := windows.CreateFile(path, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	query := make([]byte, 12) // STORAGE_PROPERTY_QUERY：PropertyId=0, QueryType=0
	out := make([]byte, 4096)
	var ret uint32
	if err := windows.DeviceIoControl(h, ioctlStorageQueryProperty,
		&query[0], uint32(len(query)), &out[0], uint32(len(out)), &ret, nil); err != nil {
		return ""
	}
	if ret <= 28 || int(ret) > len(out) {
		return ""
	}
	offset := binary.LittleEndian.Uint32(out[24:28])
	if offset == 0 || int(offset) >= int(ret) {
		return ""
	}
	raw := out[offset:ret]
	if i := strings.IndexByte(string(raw), 0); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(string(raw))
}

// MachineGUID 读取安装时生成的机器 GUID（重装系统才会变）。
func MachineGUID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
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

// DeviceFingerprint 把本机的几个稳定特征拼起来做 SHA-256，取前 32 位十六进制。
//
// 同一台机器（不重装系统、不换硬盘、不换网卡）算出来永远一样；
// 因此可以用它来当 MPay 登记设备时的 unique_id。
func DeviceFingerprint() string {
	parts := []string{
		DiskSerial(),
		MachineGUID(),
		primaryMacAddress(),
		hostname(),
		strings.Join([]string{os.Getenv("PROCESSOR_IDENTIFIER"), os.Getenv("NUMBER_OF_PROCESSORS")}, "|"),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "||")))
	return hex.EncodeToString(sum[:16])
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return ""
}

func primaryMacAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || len(ifc.HardwareAddr) < 6 {
			continue
		}
		return strings.ToUpper(ifc.HardwareAddr.String())
	}
	return ""
}
