package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/huawei"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
)

// refreshAccount 用已保存的 ST 重新签发 access_token 与 gameAuthSign。
// ST 是长期凭证，每次启动前刷新一次可以避免 gameAuthSign 过期。
//
// 渠道服扫码存的账号（没有 ST，只有 accessToken）走另一条：直接拿 accessToken
// 换新的 gameAuthSign。华为的 ts 只认 10 分钟（实测报错原文：
// "param [ts:xxx] should between [a, a+600000]"），所以每次启动都必须刷。
func refreshAccount(acc Account) (Account, error) {
	if acc.ServiceToken == "" {
		// 华为渠道服扫码的号：只有 accessToken 一把钥匙，每次启动前重签
		// （ts 只认 10 分钟）。带整包的渠道（vivo 这类）不走这里——它们
		// 的凭证就是整包本身，没有单独的重签接口。
		if acc.AccessToken != "" && acc.PlayerID != "" && acc.Source == "channelscan" &&
			acc.UnisdkData == "" && (acc.Channel == "" || acc.Channel == "huawei") {
			return refreshChannelScannedAccount(acc)
		}
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

// refreshChannelScannedAccount 用渠道服扫码存下来的 accessToken 换新的 gameAuthSign。
//
// 这条路没有 ST（扫的是渠道服客户端，拿不到华为的长期凭证），
// 所以只能靠 accessToken；它一旦过期，这个号就得重新扫。
func refreshChannelScannedAccount(acc Account) (Account, error) {
	dev, err := device.LoadOrCreateAndroid(filepath.Join(dataDir(), "android.json"))
	if err != nil {
		return acc, err
	}
	var sign map[string]any
	if err := retry(3, 800*time.Millisecond, func() error {
		var e error
		sign, e = huawei.GetGameSession(acc.AccessToken, profile.HuaweiAppID, dev.PhoneType, dev)
		return e
	}); err != nil {
		return acc, fmt.Errorf("渠道凭证刷新失败（accessToken 可能过期了，需要重新扫码）: %w", err)
	}
	playerID := toStr(sign["playerId"])
	gameAuthSign := toStr(sign["gameAuthSign"])
	ts := toStr(sign["ts"])
	if playerID == "" || gameAuthSign == "" {
		return acc, errors.New("刷新失败：getGameAuthSign 缺少字段")
	}
	acc.PlayerID = playerID
	acc.GameAuthSign = gameAuthSign
	acc.Ts = ts
	acc.PlayerLevel = toStr(sign["playerLevel"])
	if oid := toStr(sign["openId"]); oid != "" {
		acc.OpenID = oid
	}
	acc.UpdatedAt = time.Now().Unix()
	_ = saveAccount(acc)
	logf("%s：渠道凭证已刷新（ts 换新）", displayName(acc))
	return acc, nil
}
