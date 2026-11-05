package app

// 渠道服凭证寿命探测。
//
// 渠道服扫码的账号只有 accessToken 一把钥匙，它一过期这个号就得重扫。
// 但 accessToken 到底能活多久，只能靠时间观察——所以留一个后台探测模式：
//
//	skylogin.exe --probe-channel-token
//
// 每次用 accessToken 调一次华为的 getGameAuthSign：
//   - 成功 → 记一行 ok
//   - 失败 → 记一行 fail，并带上错误
// 结果追加到 %APPDATA%\SkyLogin\channel_token_probe.log，方便事后看失效时刻。

import (
	"os"
	"path/filepath"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/huawei"
	"skylogin/internal/profile"
)

// ProbeChannelToken 探测所有渠道服扫码账号的 accessToken 还有没有效。
func ProbeChannelToken() {
	path := filepath.Join(dataDir(), "channel_token_probe.log")
	logLine := func(s string) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + "  " + s + "\n")
	}

	list, err := loadAccounts()
	if err != nil {
		logLine("读取账号失败: " + err.Error())
		return
	}
	dev, derr := device.LoadOrCreateAndroid(filepath.Join(dataDir(), "android.json"))
	if derr != nil {
		logLine("准备设备失败: " + derr.Error())
		return
	}

	found := false
	for _, a := range list {
		if a.Source != "channelscan" || a.AccessToken == "" || a.UnisdkData != "" {
			continue
		}
		found = true
		sign, serr := huawei.GetGameSession(a.AccessToken, profile.HuaweiAppID, dev.PhoneType, dev)
		if serr != nil {
			logLine("fail  " + displayName(a) + "  accessToken 已失效: " + serr.Error())
			continue
		}
		// 接口 200 不代表还能用：token 半失效时返回里没有 ts / gameAuthSign，
		// 这时候必须记 fail，不然日志会骗人（2026-11-05 22:11 就这样漏了一次）。
		if toStr(sign["ts"]) == "" || toStr(sign["gameAuthSign"]) == "" {
			logLine("fail  " + displayName(a) + "  accessToken 已半失效（返回里没有 ts/gameAuthSign）")
			continue
		}
		logLine("ok    " + displayName(a) + "  仍可用，新 ts=" + toStr(sign["ts"]))
	}
	if !found {
		logLine("skip  没有渠道服扫码的账号")
	}
}
