package app

import (
	"errors"
	"path/filepath"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/huawei"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
)

// refreshAccount 用已保存的 ST 重新签发 access_token 与 gameAuthSign。
// ST 是长期凭证，每次启动前刷新一次可以避免 gameAuthSign 过期。
func refreshAccount(acc Account) (Account, error) {
	if acc.ServiceToken == "" {
		return acc, errors.New("账号没有保存 ST，需要重新扫码")
	}
	dev, err := device.LoadOrCreateAndroid(filepath.Join(dataDir(), "huawei_device.json"))
	if err != nil {
		return acc, err
	}

	var tok map[string]any
	if err := retry(3, 800*time.Millisecond, func() error {
		var e error
		tok, e = huawei.GetAccessToken(acc.ServiceToken, protocol.Get().HuaweiPackage, protocol.Get().HuaweiAppID, dev.Udid16())
		return e
	}); err != nil {
		return acc, err
	}
	accessToken, _ := tok["access_token"].(string)
	if accessToken == "" {
		return acc, errors.New("ST 可能已过期（silent_token 未返回 access_token），请重新扫码")
	}

	var sign map[string]any
	if err := retry(3, 800*time.Millisecond, func() error {
		var e error
		sign, e = huawei.GetGameSession(accessToken, profile.HuaweiAppID, dev.PhoneType, dev)
		return e
	}); err != nil {
		return acc, err
	}
	playerID := toStr(sign["playerId"])
	gameAuthSign := toStr(sign["gameAuthSign"])
	ts := toStr(sign["ts"])
	if playerID == "" || gameAuthSign == "" {
		return acc, errors.New("刷新失败：getGameAuthSign 缺少字段")
	}

	acc.AccessToken = accessToken
	acc.PlayerID = playerID
	acc.GameAuthSign = gameAuthSign
	acc.Ts = ts
	acc.PlayerLevel = toStr(sign["playerLevel"])
	if oid := toStr(sign["openId"]); oid != "" {
		acc.OpenID = oid
	}
	acc.UpdatedAt = time.Now().Unix()
	_ = saveAccount(acc)
	return acc, nil
}
