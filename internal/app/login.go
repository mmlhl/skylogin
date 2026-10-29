package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/huawei"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
	"skylogin/internal/secretfile"
)

// performLogin 完成 华为扫码结果 → ST → access_token → gameAuthSign → 保存账号。
func performLogin(sess *huawei.Client, scan *huawei.ScanReply, log func(string)) (Account, error) {
	say := func(s string) {
		if log != nil {
			log(s)
		}
	}
	dev, err := device.LoadOrCreateAndroid(filepath.Join(dataDir(), "huawei_device.json"))
	if err != nil {
		return Account{}, err
	}
	// 先把扫码结果落盘：换 ST 失败时可以据此排查/重试，不用重新扫
	record := sess.Snapshot(scan)
	if rec, err := json.MarshalIndent(record, "", "  "); err == nil {
		_ = secretfile.WriteFile(filepath.Join(dataDir(), "last_scan.json"), rec, 0o600)
	}

	logf("扫码成功 userID=%s account=%s accountType=%s siteID=%s qrCode=%s", scan.UserID, scan.UserAccount, scan.AccountType, scan.SiteID, record.QrCode)
	if record.RawJSON != "" {
		logf("轮询原文: %s", truncate(record.RawJSON, 400))
	}
	say("正在换取 ST…")
	st, xmlResp, err := sess.ExchangeSTFallback(scan, record.QrCode, dev.DeviceUUID, logf)
	if err != nil {
		logf("换 ST 失败: %v", err)
		return Account{}, fmt.Errorf("换 ST 失败: %w", err)
	}
	logf("ST 获取成功（昵称 %s）", huawei.AccountName(xmlResp))
	nick := huawei.AccountName(xmlResp)

	say("正在为光遇签发 access_token…")
	tok, err := huawei.GetAccessToken(st, protocol.Get().HuaweiPackage, protocol.Get().HuaweiAppID, dev.Udid16())
	if err != nil {
		return Account{}, err
	}
	accessToken, _ := tok["access_token"].(string)
	if accessToken == "" {
		return Account{}, fmt.Errorf("silent_token 未返回 access_token: %v", tok)
	}

	say("正在获取 gameAuthSign…")
	sign, err := huawei.GetGameSession(accessToken, profile.HuaweiAppID, dev.PhoneType, dev)
	if err != nil {
		return Account{}, err
	}
	playerID := toStr(sign["playerId"])
	gameAuthSign := toStr(sign["gameAuthSign"])
	ts := toStr(sign["ts"])
	if playerID == "" || gameAuthSign == "" {
		return Account{}, fmt.Errorf("getGameAuthSign 缺少字段: %v", sign)
	}

	acc := Account{
		Name:         nick,
		NickName:     nick,
		ServiceToken: st,
		AccessToken:  accessToken,
		PlayerID:     playerID,
		PlayerLevel:  toStr(sign["playerLevel"]),
		GameAuthSign: gameAuthSign,
		Ts:           ts,
		OpenID:       toStr(sign["openId"]),
		UpdatedAt:    time.Now().Unix(),
	}
	if err := saveAccount(acc); err != nil {
		return Account{}, err
	}
	say("账号已保存: " + playerID)
	return acc, nil
}

// accountsView 供 UI 展示，带上"当前账号"标记。
func accountsView() []map[string]any {
	list, _ := loadAccounts()
	cur, _ := currentID()
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		v := accountView(a)
		v["current"] = a.PlayerID == cur
		out = append(out, v)
	}
	return out
}
