package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"skylogin/internal/profile"
)

// Current 是当前生效的协议参数。
//
// 三层覆盖，从下往上：
//  1. 内置默认值（profile 里的常量，来自实测）
//  2. 本地缓存（上次成功拉取的远程配置）
//  3. 远程配置（维护者发布的 JSON；网易改参数时不用重新发版）
//
// 只有下面列出的字段能被覆盖，未知键一律忽略——避免把用户配置写坏。
type Current struct {
	// 网易
	LogKey     string `json:"log_key"`        // uni_sauth 签名密钥
	LongGameID string `json:"long_game_id"`   // MPay 的长 game id
	AppChannel string `json:"app_channel"`    // 建码用的通道
	SdkVersion string `json:"sdk_version"`    // uni_sauth 里声明的版本
	MpayAppCv  string `json:"mpay_app_cv"`    // 设备登记用的客户端版本
	MpayKey    string `json:"mcount_app_key"` // mcount_app_key

	// 华为
	HuaweiPackage string `json:"huawei_package"`
	HuaweiAppID   string `json:"huawei_app_id"`

	// 元信息（只读，用来判断是否需要更新）
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Default 返回内置默认值。
func Default() Current {
	return Current{
		LogKey:        profile.LogKey,
		LongGameID:    profile.LongGameID,
		AppChannel:    "a50_sdk_cn",
		SdkVersion:    profile.SdkVersion,
		MpayAppCv:     profile.MpayAppCv,
		MpayKey:       profile.McountAppKey,
		HuaweiPackage: profile.HuaweiPackage,
		HuaweiAppID:   profile.HuaweiAppID,
	}
}

var (
	active    = Default()
	cachePath string
)

// Init 载入本地缓存。dataDir 是 %APPDATA%\SkyLogin。
// 远程地址为空时不联网（离线优先，行为可预期）。
func Init(dataDir, remoteURL string) {
	cachePath = filepath.Join(dataDir, "protocol.json")
	if b, err := os.ReadFile(cachePath); err == nil {
		var fromCache Current
		if json.Unmarshal(b, &fromCache) == nil {
			active = merge(active, fromCache)
		}
	}
	if remoteURL != "" {
		if remote, err := fetch(remoteURL); err == nil {
			active = merge(active, remote)
			save(remote) // 只缓存远程拉到的部分
		}
	}
}

// Get 返回当前生效的协议参数。
func Get() Current { return active }

// merge 只接受非空字段，空值表示"这一项不覆盖"。
func merge(base, over Current) Current {
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&base.LogKey, over.LogKey)
	set(&base.LongGameID, over.LongGameID)
	set(&base.AppChannel, over.AppChannel)
	set(&base.SdkVersion, over.SdkVersion)
	set(&base.MpayAppCv, over.MpayAppCv)
	set(&base.MpayKey, over.MpayKey)
	set(&base.HuaweiPackage, over.HuaweiPackage)
	set(&base.HuaweiAppID, over.HuaweiAppID)
	if over.UpdatedAt != "" {
		base.UpdatedAt = over.UpdatedAt
	}
	return base
}

func save(v Current) {
	if cachePath == "" {
		return
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(cachePath, b, 0o600)
}

func fetch(url string) (Current, error) {
	client := &httpClient{timeout: 6 * time.Second}
	var out Current
	if err := client.getJSON(url, &out); err != nil {
		return out, err
	}
	return out, nil
}
