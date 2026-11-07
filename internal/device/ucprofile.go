package device

// 九游的设备绑定：一个账号一台"手机"，第一次登录时分配，之后一直用它。
//
// 为什么不按本机分配：同一个工具往往要登好几个号，若所有号共用一台设备，
// 在服务端看来就是"一台手机反复切号"，这比"每台手机登一个号"可疑得多。
// 所以绑定粒度是账号，而不是安装。
//
// 存储沿用与其它档案一致的做法：一份 JSON、加密落盘。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"skylogin/internal/secretfile"
	"skylogin/internal/ucsdk"
)

// ucStoreMu 串行化读写：登录可能被并行触发，同一份文件不能两边同时改。
var ucStoreMu sync.Mutex

// ucStorePathFn 便于测试替换成临时文件。
var ucStorePathFn = defaultUCStorePath

func defaultUCStorePath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "SkyLogin", "uc_devices.json")
}

// LoadOrCreateUC 取某个账号绑定的设备；没有就随机挑一台并立即落盘。
//
// 落盘时机是"分配"而不是"登录成功"：一次登录失败（密码打错）不应该
// 导致下次换一台手机——那反而更像异常。同一个账号始终对应同一台设备。
func LoadOrCreateUC(account string) (*ucsdk.Device, error) {
	ucStoreMu.Lock()
	defer ucStoreMu.Unlock()

	path := ucStorePathFn()
	all := map[string]ucsdk.Device{}
	if raw, err := secretfile.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &all)
	}

	if d, ok := all[account]; ok && d.Valid() {
		return &d, nil
	}

	d := ucsdk.RandomDevice()
	all[account] = d
	if err := writeJSON(path, all); err != nil {
		// 存不下也要能登录，只是下次会换一台
		return &d, err
	}
	return &d, nil
}
