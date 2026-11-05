package app

import (
	"strconv"
	"strings"
)

// 账号"行"标识。
//
// 一个网易账号（PlayerID）可以存多条本地记录（典型：同一个号建一个副本
// 在另一个区玩）。键规则：原始行键 = PlayerID；副本键 = PlayerID + "-2"
// 起的递增后缀（"xxx-2"、"xxx-3"…），人眼能直接看出它是谁的副本。
// 老数据没有这个字段，一律当作原始行，无需迁移。
//
// 约定：
//   - 登录流程保存的账号 CopyKey 为空，覆盖目标 = 同 PlayerID 的原始行；
//   - 副本（CopyKey 非空）只覆盖自己那一行，再登录也不会被吃掉。

// accountKey 返回账号在本地存储与路由表里的唯一标识。
func accountKey(a Account) string {
	if a.CopyKey != "" {
		return a.CopyKey
	}
	// 同一个玩家的不同来源各占一行（典型：vivo 网页登录 + 渠道服扫码）。
	// 两行 PlayerID 一样，只靠这个行键区分，否则会互相覆盖、界面联动。
	if a.RowKey != "" {
		return a.RowKey
	}
	return a.PlayerID
}

// nextCopyKey 给出 basePlayerID 的下一个空闲副本键（"-2"、"-3"…）。
func nextCopyKey(list []Account, basePlayerID string) string {
	for n := 2; ; n++ {
		key := basePlayerID + "-" + strconv.Itoa(n)
		taken := false
		for i := range list {
			if list[i].CopyKey == key {
				taken = true
				break
			}
		}
		if !taken {
			return key
		}
	}
}

// resolveAccountKey 把界面/API 传来的 id 解析成存储键。
// id 可以是行键（RowKey）、副本键（"xxx-2"）或原始行的 PlayerID。
func resolveAccountKey(list []Account, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for i := range list {
		if list[i].CopyKey == id {
			return id
		}
	}
	// 行键优先于 PlayerID：同一玩家可能有两行（网页登录 + 扫码）
	for i := range list {
		if list[i].RowKey == id {
			return id
		}
	}
	for i := range list {
		if list[i].CopyKey == "" && list[i].PlayerID == id {
			return id
		}
	}
	return id
}

// findAccount 按 id 找账号（id 可以是副本键或 PlayerID）。
func findAccount(list []Account, id string) (Account, bool) {
	key := resolveAccountKey(list, id)
	for i := range list {
		if accountKey(list[i]) == key {
			return list[i], true
		}
	}
	return Account{}, false
}

// loginTarget 返回"这次登录应该覆盖哪一条"的下标：只认原始行
// （CopyKey 为空），副本永不参与覆盖，否则再登录一次就把副本吃掉了。
func loginTarget(list []Account, a Account) int {
	for i := range list {
		if list[i].CopyKey != "" {
			continue
		}
		// 带行键的是"同一玩家的另一种来源"（网页登录 / 扫码各一行），
		// 普通登录不该覆盖它。
		if list[i].RowKey != "" && list[i].RowKey != a.RowKey {
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
// 游戏把浏览器侧状态（登录会话、选过的渠道页等）记在 profile 目录里，
// 目录名来自浏览器壳的 channel。让同一条记录永远用同一个 channel，
// 它的浏览器侧状态就能跨启动保留；不同记录用不同 channel，互不干扰。
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
	return "h5_copy" + name
}

// channelKeyForAccount 返回准备镜像时要用的 CEF channel 键（空 = 由镜像名派生）。
// 只有副本才固定 channel：原始行保持原行为，不打扰老用户。
func channelKeyForAccount(a Account) string {
	if a.CopyKey == "" {
		return ""
	}
	return accountKey(a)
}

// regionLabel 把区标注翻成界面上的中文。
func regionLabel(region string) string {
	switch region {
	case "ios":
		return "iOS 区"
	case "android":
		return "安卓区"
	}
	return ""
}
