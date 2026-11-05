package app

// 渠道整包探针（开发用）：拿一份 channel_login_slots.json，验证里面的整包
// 登录凭证还能不能换出 uni_sauth 登录态。
//
//	skylogin.exe --probe-channel-package [路径]
//
// 不传路径就用 %APPDATA%\SkyLogin\channel_login_slots.json（渠道服扫码留下的那份）。
// 结果写 %APPDATA%\SkyLogin\package_probe.log，同时把 confirm 表单的字段名打出来。
//
// 为什么要它：扫码一次不容易（要拿手机），而"这个整包到底能不能用"是纯服务端
// 行为，离线就能反复验，不用每次都开官方窗。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/mpayhost"
)

// ProbeChannelPackage 验证渠道整包。
func ProbeChannelPackage(path string) {
	if path == "" {
		path = filepath.Join(dataDir(), "channel_login_slots.json")
	}
	logPath := filepath.Join(dataDir(), "package_probe.log")
	log := func(format string, args ...any) { qrProbeLog(logPath, format, args...) }
	log("=== 渠道整包探针: %s ===", path)

	b, err := os.ReadFile(path)
	if err != nil {
		log("读不到文件: %v", err)
		return
	}
	var dump struct {
		Slots []string `json:"slots"`
	}
	if err := json.Unmarshal(b, &dump); err != nil {
		log("解析失败: %v", err)
		return
	}
	if len(dump.Slots) < 9 {
		log("槽位不够（%d 个）", len(dump.Slots))
		return
	}
	raw := extraUnisdkFromSlot(dump.Slots[8])
	if raw == "" {
		log("slot8 里没有 extra_unisdk_data")
		return
	}
	pkg, perr := parseChannelPackage(raw)
	if perr != nil {
		log("登录包解析失败: %v", perr)
		return
	}
	log("渠道=%s uid=%s session=%d 字符 access=%d 字符 sdk_version=%s gameid=%s",
		pkg.Channel, pkg.UID, len(pkg.Session), len(pkg.Access), pkg.SDKVersion, pkg.GameID)

	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		log("准备设备标识失败: %v", derr)
		return
	}
	acc := Account{
		Name: displayChannel(pkg.Channel), NickName: displayChannel(pkg.Channel),
		Channel: pkg.Channel, UserID: pkg.UID, PlayerID: pkg.UID,
		GameAuthSign: pkg.Session, AccessToken: pkg.Access,
		UnisdkData: raw,
	}
	start := time.Now()
	payload, resp, err := buildPayloadFn(acc, dev)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		log("uni_sauth / 组包失败（%dms）: %v  响应: %s", ms, err, redactJSON(resp))
		return
	}
	log("uni_sauth 通过（%dms）", ms)
	log("响应: %s", redactJSON(resp))
	// confirm 表单的字段名 + 长度：真发一次会创建会话，这里只看结构
	fields := make([]string, 0, len(payload.Body))
	for k, v := range payload.Body {
		fields = append(fields, fmt.Sprintf("%s(%d)", k, len(v)))
	}
	sortStrings(fields)
	log("confirm 表单字段: %v", fields)
	log("用户名: %s", payload.UserName)

	// 真正走一遍取票（建码 → 确认 → 取票）。能出票就说明"除了游戏认不认，
	// 别的都通了"；出不了票，问题在 confirm 这一步，不用去点启动。
	ticket, _, terr := newTicketSource().Login(payload.Body)
	if terr != nil {
		log("取票失败: %v", terr)
	} else if ticket == "" {
		log("取票没报错但票是空的")
	} else {
		log("取票成功: %s（%d 字符）", maskTicket(ticket), len(ticket))
	}

	// 验证通过就顺手按真实路径存一次：这样这份探针同时也是"导入已有扫码结果"
	// 的工具——拿旧 slots 文件就能把账号加回来，不用再扫一次。
	name, serr := saveChannelScannedAccount(mpayhost.Result{
		Channel:    pkg.Channel,
		UnisdkData: raw,
	})
	if serr != nil {
		log("存账号失败: %v", serr)
		return
	}
	log("账号已保存: %s（可以回界面点「启动光遇」验证）", name)
}

// extraUnisdkFromSlot 从 slot8 的 JSON 里取 extra_unisdk_data 原文。
func extraUnisdkFromSlot(slot8 string) string {
	var outer map[string]any
	if json.Unmarshal([]byte(slot8), &outer) != nil {
		return ""
	}
	s, _ := outer["extra_unisdk_data"].(string)
	return s
}
