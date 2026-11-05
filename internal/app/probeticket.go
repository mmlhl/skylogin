package app

// 给"TicketLogin"诊断用：签一张票出来交给它。
//
// 思路：应用宝账号在 PC 上卡住的是"客户端不认这个渠道"，但票本身是服务端签的、
// 有效的一次性票据。mpay.dll 导出了 TicketLogin(ticket, bool)，如果拿票直接
// 登录能过，就不用去解 mpay.db 了。
//
// 这个函数只做一件事：按账号 id 签一张票并返回，不启动游戏、不改任何状态。

import (
	"fmt"
	"path/filepath"

	"skylogin/internal/device"
)

// MintTicketForProbe 给指定账号签一张一次性票据（诊断用）。
// accountID 为空时挑第一条带渠道整包的账号。
func MintTicketForProbe(accountID string) (string, string, error) {
	list, err := loadAccounts()
	if err != nil || len(list) == 0 {
		return "", "", fmt.Errorf("还没有账号")
	}

	var acc Account
	found := false
	if accountID != "" {
		acc, found = findAccount(list, accountID)
	} else {
		for _, a := range list {
			if a.UnisdkData != "" {
				acc, found = a, true
				break
			}
		}
	}
	if !found {
		return "", "", fmt.Errorf("没找到可用的账号（%s）", accountID)
	}

	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		return "", "", fmt.Errorf("设备标识不可用: %w", derr)
	}
	p, _, perr := buildPayloadFn(acc, dev)
	if perr != nil {
		return "", "", fmt.Errorf("生成登录包失败: %w", perr)
	}
	ticket, _, terr := newTicketSource().Login(p.Body)
	if terr != nil {
		return "", "", fmt.Errorf("取票失败: %w", terr)
	}
	if ticket == "" {
		return "", "", fmt.Errorf("票是空的")
	}
	logf("诊断：已为 %s 签出票据（%d 字符）", displayName(acc), len(ticket))
	return ticket, displayName(acc), nil
}

// MintTicketWithCred 用指定凭证签票（诊断用，不落库、不改任何账号）。
//
// 场景：安卓端刚登录拿到的 atk 是最新的，而本地存的可能已经被顶掉
// （应用宝微信单设备有效），这时需要一个"用外部凭证直接签票"的入口。
func MintTicketWithCred(openid, atk, pf, pfKey string) (string, error) {
	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		return "", fmt.Errorf("设备标识不可用: %w", err)
	}
	pkg, perr := buildMyappPackage(myappCred{OpenID: openid, Atk: atk, Pf: pf, PfKey: pfKey}, dev)
	if perr != nil {
		return "", fmt.Errorf("拼整包失败: %w", perr)
	}
	acc := Account{
		Name: "临时", Channel: "myapp", RowKey: openid + " · 应用宝",
		UserID: openid, PlayerID: openid,
		GameAuthSign: atk, AccessToken: atk, UnisdkData: pkg, Source: "myapplogin",
	}
	p, _, herr := buildPayloadFn(acc, dev)
	if herr != nil {
		return "", fmt.Errorf("组登录包失败: %w", herr)
	}
	ticket, _, terr := newTicketSource().Login(p.Body)
	if terr != nil {
		return "", fmt.Errorf("取票失败: %w", terr)
	}
	logf("诊断：用外部凭证签出票据（%d 字符）", len(ticket))
	return ticket, nil
}
