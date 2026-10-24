package device

// 设备指纹：SAUTH 和华为接口都会校验"同一个账号是否总在同一台设备上"，
// 所以这两份指纹要落盘复用，不能每次随机。

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ClientProfile 是 SAUTH 里 udid / sdklog 用到的那组参数。
type ClientProfile struct {
	DeviceModel string `json:"device_model"`
	OSName      string `json:"os_name"`
	OSVer       string `json:"os_ver"`
	Udid        string `json:"udid"`
	AppVer      string `json:"app_ver"`
	Imei        string `json:"imei"`
	CountryCode string `json:"country_code"`
	IsEmulator  int    `json:"is_emulator"`
	IsRoot      int    `json:"is_root"`
	Oaid        string `json:"oaid"`
}

// AndroidProfile 是华为 silent_token / getGameAuthSign 需要的手机信息。
type AndroidProfile struct {
	DeviceID       string `json:"deviceId"`
	DeviceUUID     string `json:"device_uuid"`
	Brand          string `json:"brand"`
	RomVersion     string `json:"romVersion"`
	AndroidVersion string `json:"androidVersion"`
	Manufacturer   string `json:"manufacturer"`
	PhoneType      string `json:"phoneType"`
	DeviceIDType   int    `json:"deviceIdType"`
}

// Udid16 是华为接口认的 device_id：uuid 去掉横线后取前 16 位。
func (p *AndroidProfile) Udid16() string {
	out := make([]byte, 0, 16)
	for i := 0; i < len(p.DeviceUUID) && len(out) < 16; i++ {
		if p.DeviceUUID[i] != '-' {
			out = append(out, p.DeviceUUID[i])
		}
	}
	return string(out)
}

// LoadOrCreateClient 读取已有指纹；没有或损坏就生成一份并存盘。
func LoadOrCreateClient(path string) (*ClientProfile, error) {
	if raw, err := os.ReadFile(path); err == nil {
		var p ClientProfile
		if json.Unmarshal(raw, &p) == nil && p.Udid != "" {
			return &p, nil
		}
	}
	p := &ClientProfile{
		DeviceModel: "M2102K1AC",
		OSName:      "android",
		OSVer:       "12",
		Udid:        randomHex(8),
		AppVer:      "157",
		Imei:        randomDigits(15),
		CountryCode: "CN",
		IsEmulator:  0,
		IsRoot:      0,
	}
	if err := writeJSON(path, p); err != nil {
		return nil, err
	}
	return p, nil
}

// LoadOrCreateAndroid 同上，给华为渠道用。
func LoadOrCreateAndroid(path string) (*AndroidProfile, error) {
	if raw, err := os.ReadFile(path); err == nil {
		var p AndroidProfile
		if json.Unmarshal(raw, &p) == nil && p.DeviceUUID != "" {
			return &p, nil
		}
	}
	brand := []string{"HUAWEI", "HONOR"}[randInt(2)]
	p := &AndroidProfile{
		DeviceID:       randomHex(32),
		DeviceUUID:     newUUID(),
		Brand:          brand,
		RomVersion:     "V417IR release-keys",
		AndroidVersion: "12",
		Manufacturer:   brand,
		PhoneType:      "SM-G9910",
		DeviceIDType:   4,
	}
	if err := writeJSON(path, p); err != nil {
		return nil, err
	}
	return p, nil
}

// writeJSON 存盘（目录不存在就建）。
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
