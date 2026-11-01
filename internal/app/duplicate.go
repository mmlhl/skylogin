package app

import (
	"errors"
	"strconv"
	"strings"
)

// 副本：同一条账号记录的另一份本地拷贝（典型用法：在另一个区再玩一个）。
//
// 为什么需要：同一个网易账号在游戏里的键（PlayerID = 网易通行证 id）只有
// 一个，存储去重按 PlayerID 的话直接"再添加一次"只会覆盖原记录。
// 副本通过 CopyKey（"PlayerID-2" 这样的后缀键）与原始行并存。
//
// 与原始行的关系：
//   - 凭证（token）复制自原行；原行刷新凭证时同 PlayerID 的副本一起更新；
//   - 副本启动永远走镜像目录——只有镜像里装了浏览器壳，能把它的 CEF
//     channel 改成专属名字，浏览器侧状态（登录会话等）互不干扰。

// duplicateAccount 复制 id 指定的账号记录。name 为用户在弹窗里输入的
// 副本名（空 = 自动取「原显示名 · 副本N」）；返回新记录的键
// （"PlayerID-2" 这样的副本键）。
func duplicateAccount(id, name string) (string, error) {
	list, err := loadAccounts()
	if err != nil {
		return "", errors.New("还没有账号")
	}
	src, ok := findAccount(list, id)
	if !ok {
		return "", errors.New("没有找到该账号")
	}

	copyKey := nextCopyKey(list, src.PlayerID)

	cp := src
	cp.CopyKey = copyKey
	cp.Region = ""
	// 副本不继承别名（那是用户给原行起的）；用户没起名就按序号自动补
	base := displayName(src)
	name = strings.TrimSpace(name)
	if name == "" {
		existing := 0
		for i := range list {
			if list[i].PlayerID == src.PlayerID && list[i].CopyKey != "" {
				existing++
			}
		}
		name = base + " · 副本" + strconv.Itoa(existing+1)
	}
	if len([]rune(name)) > 24 {
		name = string([]rune(name)[:24])
	}
	cp.Name = name

	list = append(list, cp)
	if err := saveList(list); err != nil {
		return "", err
	}
	if err := setCurrent(copyKey); err != nil {
		logf("创建副本后设为当前失败: %v", err)
	}
	logf("已创建副本：%s → %s（键=%s）", base, cp.Name, copyKey)
	return copyKey, nil
}
