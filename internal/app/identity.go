package app

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// 本文件是"账号行标识"的支撑代码。
//
// 原来的设计里一个 PlayerID 只能有一条记录（store.go 的 saveAccount 按
// PlayerID 去重）。但同一个网易账号可以同时有安卓区和 iOS 区两个角色，
// 两条记录共用一个 PlayerID —— 靠 RowID 才能区分。
//
// 约定：
//   - 登录流程保存的账号 RowID 为空（还是老键 PlayerID），行为与以前完全一致；
//   - 只有"复制成第二个区"（duplicateAccount）才会生成 RowID；
//   - accountKey 统一给出这条记录在存储/路由表里的键。

// newRowID 生成一条记录的行标识（16 位十六进制）。
func newRowID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// accountKey 返回账号在本地存储与路由表里的唯一标识。
func accountKey(a Account) string {
	if a.RowID != "" {
		return a.RowID
	}
	return a.PlayerID
}

// resolveAccountKey 把界面/API 传来的 id 解析成存储键。
// 兼容三种情况：传 RowID、传旧版 PlayerID（且只有一条）、传不存在的 id。
func resolveAccountKey(list []Account, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for i := range list {
		if list[i].RowID != "" && list[i].RowID == id {
			return list[i].RowID
		}
	}
	hit, n := "", 0
	for i := range list {
		if list[i].PlayerID == id {
			hit, n = accountKey(list[i]), n+1
		}
	}
	if n == 1 {
		return hit
	}
	if n > 1 {
		for i := range list {
			if list[i].PlayerID == id && list[i].RowID != "" {
				return list[i].RowID
			}
		}
		return hit
	}
	return id
}

// findAccount 按 id 找账号（id 可以是 RowID 或 PlayerID）。
func findAccount(list []Account, id string) (Account, bool) {
	key := resolveAccountKey(list, id)
	for i := range list {
		if accountKey(list[i]) == key {
			return list[i], true
		}
	}
	return Account{}, false
}

// loginTarget 返回"这次登录应该覆盖哪一条"的下标：只认 RowID 为空的主记录。
// 复制出来的副本永不参与覆盖，否则再登录一次就会把副本吃掉。
func loginTarget(list []Account, a Account) int {
	for i := range list {
		if list[i].RowID != "" {
			continue
		}
		if list[i].PlayerID != a.PlayerID {
			continue
		}
		if a.Channel != "" && list[i].Channel != "" && a.Channel != list[i].Channel {
			continue
		}
		return i
	}
	return -1
}

// cefChannelFor 返回这一行账号专属的 CEF profile 名。
//
// 游戏把"选择 iOS/安卓渠道"的选择记在浏览器 profile 里
// （%APPDATA%\Netease\Mpay\MWSlocalStorage\<cache_path_channel_name>）。
// 让同一条记录永远用同一个 channel，选区结果就能记住；
// 不同记录用不同 channel，于是可以一条安卓区、一条 iOS 区。
func cefChannelFor(key string) string {
	var b strings.Builder
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if name == "" {
		return ""
	}
	return "h5_row" + name
}

// regionLabel 把区标识翻成界面上的中文。
func regionLabel(region string) string {
	switch region {
	case "ios":
		return "iOS 区"
	case "android":
		return "安卓区"
	}
	return ""
}

// channelKeyForAccount 返回准备镜像时要用的 CEF channel 键（空 = 由镜像名派生）。
// 只有副本（RowID 非空）才固定 channel：主记录保持原行为，不打扰老用户。
func channelKeyForAccount(a Account) string {
	if a.RowID == "" {
		return ""
	}
	return accountKey(a)
}
