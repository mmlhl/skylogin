package app

// 诊断：网易账号能不能也走 uni_sauth。
//
// 动机：渠道服的 SAUTH 是我们自己拼的，platform 直接写 "ad"，所以游戏那边
// 从来不问"你玩 iOS 还是安卓"。网易账号走的是 create_ticket，sauth 由服务端
// 生成（platform=pc），于是每次启动都要手点一次选择框。
//
// 这里只验证第一步：把平台字段改成 ad 之后，服务端还认不认这个 sessionid。
// 认了才有后面（拿票、交给游戏）。

import (
	"fmt"
	"path/filepath"

	"skylogin/internal/device"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

// ProbeNeteaseUnisdk 用网易账号的 token 拼一份"声明自己是安卓"的 SAUTH，发一次 uni_sauth。
func ProbeNeteaseUnisdk(accountID, platform string) error {
	if platform == "" {
		platform = "ad"
	}
	list, err := loadAccounts()
	if err != nil {
		return fmt.Errorf("读账号失败: %w", err)
	}
	acc, ok := findAccount(list, accountID)
	if !ok {
		return fmt.Errorf("没找到账号（%s）", accountID)
	}
	if acc.MpayToken == "" || acc.UserID == "" {
		return fmt.Errorf("%s 没有可用的 token", displayName(acc))
	}
	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		return fmt.Errorf("设备标识不可用: %w", derr)
	}

	custom := map[string]any{
		"login_channel":      "netease",
		"app_channel":        "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd",
		"platform":           platform,
		"source_platform":    platform,
		"sdk_version":        "4.21.0",
	}
	// 服务端对 netease 这条要求带 deviceid，拿安卓画像里的那个填上
	if and, aerr := device.LoadOrCreateAndroid(filepath.Join(dataDir(), "android.json")); aerr == nil {
		custom["deviceid"] = and.DeviceID
		custom["username"] = acc.Name
	}
	sauth := netsec.NewSAUTH(acc.UserID, acc.MpayToken, profile.ShortGameID, custom, dev)
	logf("[探针] uni_sauth：uid=%s login_channel=netease platform=%s", acc.UserID, platform)

	resp, err := netsec.PostSigned(sauth, profile.ShortGameID)
	if err != nil {
		return fmt.Errorf("uni_sauth 请求失败: %w", err)
	}
	code, status, msg := toStr(resp["code"]), toStr(resp["status"]), toStr(resp["msg"])
	fmt.Printf("code=%s status=%s msg=%s\n", code, status, msg)
	logf("[探针] uni_sauth 响应：code=%s status=%s msg=%s", code, status, msg)

	if raw, ok := resp["unisdk_login_json"]; ok {
		s := toStr(raw)
		fmt.Printf("拿到 unisdk_login_json（%d 字符）\n", len(s))
		logf("[探针] 拿到 unisdk_login_json（%d 字符）", len(s))
	} else {
		fmt.Println("没有 unisdk_login_json")
	}
	if code != "200" {
		return fmt.Errorf("服务端没接受（code=%s %s）", code, msg)
	}
	return nil
}
