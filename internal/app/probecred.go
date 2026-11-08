package app

// 诊断探针的辅助：给 mpayhost 探针提供账号库里网易账号的真实凭证。
// 只给 cmd/skylogin 的探针用，不进任何正式流程。

import (
	"fmt"

	"skylogin/internal/profile"
	"time"
)

// NetEaseCredForProbe 按名字/键找网易账号，返回 (uid, token, device, 是否找到)。
func NetEaseCredForProbe(accountID string) (uid, token, device string, ok bool) {
	list, err := loadAccounts()
	if err != nil {
		return "", "", "", false
	}
	acc, found := findAccount(list, accountID)
	if !found || acc.Channel != "netease" || acc.MpayToken == "" {
		return "", "", "", false
	}
	return acc.UserID, acc.MpayToken, acc.DeviceID, true
}

// SaveA50Token 把 a50 凭证存进指定账号（URS 探针登录成功后调用）。
// a50Dev 是 DLL 这次登录实际用的设备号（token 绑定它，换 sid 必须配套）。
// 账号不在库里时自动创建（探针模式：正式流程由添加账号逻辑建号）。
func SaveA50Token(accountID, a50Token, a50Dev, account string) error {
	list, err := loadAccounts()
	if err != nil {
		return err
	}
	acc, found := findAccount(list, accountID)
	if !found {
		if account == "" {
			return fmt.Errorf("没找到账号（%s）", accountID)
		}
		acc = Account{
			Name:        account,
			Username:    account,
			Channel:     "netease",
			UserID:      accountID,
			PlayerID:    accountID,
			Source:      "ursprobe",
			LoginMethod: "password",
			CredState:   "ok",
			UpdatedAt:   time.Now().Unix(),
		}
		logf("探针：账号不在库里，已新建记录 %s", account)
	}
	if gi := tokenGI(a50Token); gi != profile.HostedGameID && gi != "aecglf6ee4aaaarz" {
		return fmt.Errorf("回调 token 不是 a50 签发的（g_i=%s），不存", gi)
	}
	acc.A50Token = a50Token
	if a50Dev != "" {
		acc.A50DeviceID = a50Dev
		if acc.DeviceID == "" {
			acc.DeviceID = a50Dev
		}
	}
	if acc.MpayToken == "" {
		// 没有游戏侧凭证时先用 a50 token 顶着（同为 MPay token，
		// create_ticket 在 a50 上下文可直接用）
		acc.MpayToken = a50Token
	}
	if err := saveAccount(acc); err != nil {
		return err
	}
	logf("%s：已存入 a50 凭证（自寄宿启用，dev=%s）", displayName(acc), a50Dev)
	return nil
}
