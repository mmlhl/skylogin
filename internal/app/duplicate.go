package app

import (
	"errors"
	"strings"
)

// 复制账号成"第二条记录"。
//
// 背景：同一个网易账号可以同时有安卓区和 iOS 区两个角色，两边数据独立，
// 但账号键（PlayerID = 网易通行证 id）是同一个。storage 去重按 PlayerID，
// 所以直接"再添加一次"只会覆盖原记录（见 store.go 的 saveAccount）。
//
// 这里的做法：复制一行、发一个新 RowID，让两条记录并存；
// 两条记录各自绑定独立的 CEF profile（identity.go 的 cefChannelFor），
// 于是各自记住自己在游戏里选的区。副本永远走镜像目录（clients.go
// 的 chooseInstall），因为只有镜像里才装了改 channel 的浏览器壳。

// duplicateAccount 把 id 指定的账号复制一份，region 是 android / ios / 空。
// 返回新记录的键（RowID）。
func duplicateAccount(id, region string) (string, error) {
	region = strings.ToLower(strings.TrimSpace(region))
	switch region {
	case "", "android", "ios":
	default:
		return "", errors.New("区只能是 android 或 ios")
	}

	list, err := loadAccounts()
	if err != nil {
		return "", errors.New("还没有账号")
	}
	src, ok := findAccount(list, id)
	if !ok {
		return "", errors.New("没有找到该账号")
	}
	// 已经有副本了就不再加第二条，避免点两下出来三条。
	for _, a := range list {
		if a.RowID != "" && a.PlayerID == src.PlayerID && a.Channel == src.Channel {
			return "", errors.New("这个账号已经有一条副本了，先删掉再复制")
		}
	}

	rowID := newRowID()
	if rowID == "" {
		return "", errors.New("生成记录标识失败")
	}

	cp := src
	cp.RowID = rowID
	cp.Region = region
	// 副本不要继承别名（别名是用户给主记录起的），改用"原显示名 + 区"。
	base := displayName(src)
	if l := regionLabel(region); l != "" {
		cp.Name = base + " · " + l
	} else {
		cp.Name = base + " · 第二区"
	}
	if len([]rune(cp.Name)) > 24 {
		cp.Name = string([]rune(cp.Name)[:24])
	}

	list = append(list, cp)
	if err := saveList(list); err != nil {
		return "", err
	}
	if err := setCurrent(rowID); err != nil {
		logf("复制账号后设为当前失败: %v", err)
	}
	logf("已复制账号 %s → 新记录 %s（区=%s）", base, rowID, cp.Region)
	return rowID, nil
}

// dropAllDuplicates 清掉所有副本（设置里的"清理"用得上）。
func dropAllDuplicates() (int, error) {
	list, err := loadAccounts()
	if err != nil {
		return 0, err
	}
	out := make([]Account, 0, len(list))
	for _, a := range list {
		if a.RowID == "" {
			out = append(out, a)
		}
	}
	n := len(list) - len(out)
	if n == 0 {
		return 0, nil
	}
	if err := saveList(out); err != nil {
		return 0, err
	}
	return n, nil
}
