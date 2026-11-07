package device

// 手机档案的存储：按「渠道 + 账号」绑定一台随机真机，落盘复用。
//
// 机型与标识本身由 internal/phone 生成（渠道无关），这里只负责持久化和
// 分配策略。之所以按账号而不是按本机绑定：同一个工具往往要登好几个号，
// 若所有号共用一台"手机"，服务端看到的就是一台设备反复切号，
// 比"每台设备只登一个号"可疑得多。
//
// 存储沿用与其它档案一致的做法：一份 JSON、加密落盘。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"skylogin/internal/phone"
	"skylogin/internal/secretfile"
)

// phoneStoreMu 串行化读写：登录可能被并行触发，同一份文件不能两边同时改。
var phoneStoreMu sync.Mutex

// phoneStorePathFn 便于测试替换成临时文件。
var phoneStorePathFn = defaultPhoneStorePath

func defaultPhoneStorePath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "SkyLogin", "phone_devices.json")
}

// LoadOrCreatePhone 取某个渠道下某个账号绑定的设备；没有就随机挑一台并立即落盘。
//
// scope 区分渠道（"uc"、"huawei"…），key 一般是账号标识（手机号 / accountId）。
// 落盘时机是"分配"而不是"登录成功"：一次登录失败（密码打错）不该导致下次
// 换一台手机——那反而更像异常。
func LoadOrCreatePhone(scope, key string) (*phone.Device, error) {
	phoneStoreMu.Lock()
	defer phoneStoreMu.Unlock()

	path := phoneStorePathFn()
	all := map[string]phone.Device{}
	if raw, err := secretfile.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &all)
	}

	id := scope + "/" + key
	if d, ok := all[id]; ok && d.Valid() {
		return &d, nil
	}

	d := phone.Random()
	all[id] = d
	if err := writeJSON(path, all); err != nil {
		// 存不下也要能登录，只是下次会换一台
		return &d, err
	}
	return &d, nil
}

// LoadOrCreateUC 是 LoadOrCreatePhone 的九游快捷方式。
func LoadOrCreateUC(account string) (*phone.Device, error) {
	return LoadOrCreatePhone("uc", account)
}
