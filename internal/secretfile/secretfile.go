// Package secretfile 把落盘的凭证文件整体加密：DPAPI（Windows 数据保护
// API，按当前用户绑定）加密 + 魔数头。密钥由系统管理，本工具不保存
// 任何口令；换用户/换机器后文件不可解（这正是要的安全性）。
//
// 读侧兼容旧明文文件：没有魔数头就按明文原样返回，调用方解析成功后
// 顺手用加密格式重写（自动升级，用户无感）。
package secretfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// magic 加密文件的标识行。带换行，肉眼一眼能看出"这不是明文 JSON"。
var magic = []byte("SKYLOGIN-DPAPIv1\n")

// WriteFile 把 data 加密后写入 path（等价 os.WriteFile 的权限语义）。
// 布局：明文魔数头 + DPAPI 密文——头部明文才能在读取时区分格式。
func WriteFile(path string, data []byte, perm os.FileMode) error {
	enc, err := Protect(data)
	if err != nil {
		return fmt.Errorf("加密失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out := append(append([]byte{}, magic...), enc...)
	return os.WriteFile(path, out, perm)
}

// ReadFile 读取 path：加密文件解密后返回内容；旧明文文件原样返回
// （调用方解析成功后应尽快 WriteFile 升级成加密存储）。
func ReadFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) > len(magic) && string(raw[:len(magic)]) == string(magic) {
		plain, uerr := Unprotect(raw[len(magic):])
		if uerr != nil {
			return nil, fmt.Errorf("解密失败（换过用户或系统重装后旧数据不可读）: %w", uerr)
		}
		return plain, nil
	}
	return raw, nil
}

// IsEncrypted 报告文件当前是否已是加密格式（升级判断用）。
func IsEncrypted(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return len(raw) > len(magic) && string(raw[:len(magic)]) == string(magic)
}

// ErrNotEncrypted 供需要强制加密的场景判断。
var ErrNotEncrypted = errors.New("文件不是加密格式")
