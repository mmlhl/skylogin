package app

// 界面上的「刷新凭证」。
//
// 以前这个按钮对网易账号一律弹登录框，让用户再输一次账号密码——哪怕这个号
// 早就把密码存下来了。既然存了，就该直接重登；只有没存密码的号（扫码进来的、
// 官方登录窗进来的）才需要用户动手。
//
// 两种情况都走这里，返回值告诉界面这次是"静默重登成功"还是"得请用户输入"。

import (
	"fmt"
)

// ReloginAccount 用保存的密码给某个账号自动重登一次。
//
// 返回 errNoSavedPassword 表示这个号没存密码，调用方应该转成"弹输入框"。
func ReloginAccount(accountID string) (map[string]any, error) {
	list, err := loadAccounts()
	if err != nil {
		return nil, fmt.Errorf("读账号失败: %w", err)
	}
	acc, ok := findAccount(list, accountID)
	if !ok {
		return nil, fmt.Errorf("没找到这个账号")
	}
	if acc.Channel != "netease" {
		return nil, fmt.Errorf("这个渠道不支持自动重登")
	}
	if !hasSavedPassword(acc) {
		return nil, errNoSavedPassword
	}

	if _, err := autoRelogin(acc); err != nil {
		return nil, err
	}
	// 平台凭证另补一份：宿主资格只看它，缺了这个号就提供不了寄宿槽，
	// 住在它槽里的扫码号会跟着失效。
	canHost := ensureHostCred(acc)

	fresh, _ := findAccountByUserID(acc.UserID)
	setAccountCredState(acc.UserID, "ok", "")
	return map[string]any{
		"name":    displayName(fresh),
		"canHost": canHost,
	}, nil
}

// ShouldPromptForPassword 判断"刷新这个账号"要不要弹输入框。
//
// 存过密码的号直接重登，不用打扰用户；没存的（扫码 / 官方登录窗）只能让
// 用户自己输一次——这也是这些号以后能被自动续期的唯一办法。
func ShouldPromptForPassword(accountID string) bool {
	list, err := loadAccounts()
	if err != nil {
		return true
	}
	acc, ok := findAccount(list, accountID)
	if !ok {
		return true
	}
	return !hasSavedPassword(acc)
}
