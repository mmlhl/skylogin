package app

// 账号的"所选渠道"（区）设置。
//
// 只有网易官方账号能选区：游戏启动时的 iOS / 安卓 选择由 uni_sauth 的
// platform 声明决定（见 channelplatform.go），渠道服账号的渠道由渠道 SDK
// 决定，我们不能也不该替它声明。
//
// 存储用 Account.Region，值沿用界面上已有的 "android" / "ios" / "harmony"，
// 空 = 还没选（保持老行为：进游戏后自己选）。

import (
	"errors"
	"fmt"
	"strings"
)

// normalizeRegion 校验并归一化渠道值。空串表示"先不选"。
func normalizeRegion(region string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "":
		return "", nil
	case "android":
		return "android", nil
	case "ios":
		return "ios", nil
	case "harmony":
		return "harmony", nil
	}
	return "", fmt.Errorf("不认识的渠道：%s", region)
}

// setAccountRegion 设置某条账号记录的所选渠道。
//
// 原始行和副本各自独立：副本可以在自己的副本里选另一个区，互不影响
// （这正是副本存在的意义）。
func setAccountRegion(id, region string) error {
	norm, err := normalizeRegion(region)
	if err != nil {
		return err
	}
	list, err := loadAccounts()
	if err != nil {
		return errors.New("还没有账号")
	}
	for i := range list {
		if accountKey(list[i]) != id {
			continue
		}
		if list[i].Channel != "netease" {
			return errors.New("只有网易官方账号可以选择渠道")
		}
		if list[i].Region == norm {
			return nil // 没变化，不写盘
		}
		list[i].Region = norm
		if err := saveList(list); err != nil {
			return err
		}
		if norm == "" {
			logf("%s：已改为「先不选」，下次启动由游戏自己问渠道", displayName(list[i]))
		} else {
			logf("%s：所选渠道已改为 %s（下次启动按此签发）", displayName(list[i]), regionLabel(norm))
		}
		return nil
	}
	// 回退：调用方可能只拿到网易账号 id（登录回调里的 userId），
	// 而那行记录的键是 RowKey / CopyKey。按 UserID 找第一条网易账号。
	for i := range list {
		if list[i].UserID != id || list[i].Channel != "netease" {
			continue
		}
		if list[i].Region == norm {
			return nil
		}
		list[i].Region = norm
		if err := saveList(list); err != nil {
			return err
		}
		logf("%s：所选渠道已改为 %s（下次启动按此签发）", displayName(list[i]), regionOrNone(norm))
		return nil
	}
	return errors.New("没有找到该账号")
}

// regionOrNone 把渠道值转成日志用词（空串显示为“未选”）。
func regionOrNone(region string) string {
	if l := regionLabel(region); l != "" {
		return l
	}
	return "未选"
}
