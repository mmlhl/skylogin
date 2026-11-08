package app

// 保存"能自动重登"的凭据，并据此自动重新登录。
//
// 两种凭据的区别：
//
//	官方登录窗（--mpaylogin）：密码在网易自己的窗口里输入，我们从头到尾看不到明文，
//	                          所以这条路径**存不了密码**，只能存它换出来的凭证。
//	无头密码登录（--headless-login）：账号密码是我们自己收的，可以存下来，
//	                          凭证过期后无需用户再操作就能重登。
//
// 存的是 Windows DPAPI 加密后的 base64：绑定当前 Windows 用户，把 accounts.json
// 拷到别的机器上也解不开。账号名（SavedUser）不加密，因为界面要显示它。

import (
	"encoding/base64"
	"errors"
	"time"
	"fmt"

	"skylogin/internal/profile"
	"skylogin/internal/secretfile"
)

// errNoSavedPassword 表示这个账号没有存过密码，没法自动重登。
var errNoSavedPassword = errors.New("没有保存账号密码，无法自动重登")

// saveAccountPassword 把账号密码加密后写进账号记录。
func saveAccountPassword(target, user, pass string) error {
	if user == "" || pass == "" {
		return fmt.Errorf("账号或密码为空")
	}
	enc, err := secretfile.Protect([]byte(pass))
	if err != nil {
		return fmt.Errorf("加密密码失败: %w", err)
	}
	list, err := loadAccounts()
	if err != nil {
		return err
	}
	acc, ok := findAccount(list, target)
	if !ok {
		return fmt.Errorf("没找到账号（%s）", target)
	}
	acc.SavedUser = user
	acc.SavedPass = base64.StdEncoding.EncodeToString(enc)
	acc.UpdatedAt = time.Now().Unix()
	return saveAccount(acc)
}

// accountPassword 取回账号密码明文（只在内存里用）。
func accountPassword(acc Account) (user, pass string, err error) {
	if acc.SavedUser == "" || acc.SavedPass == "" {
		return "", "", errNoSavedPassword
	}
	raw, derr := base64.StdEncoding.DecodeString(acc.SavedPass)
	if derr != nil {
		return "", "", fmt.Errorf("密码字段损坏: %w", derr)
	}
	plain, uerr := secretfile.Unprotect(raw)
	if uerr != nil {
		return "", "", fmt.Errorf("解密密码失败（换了 Windows 用户？）: %w", uerr)
	}
	return acc.SavedUser, string(plain), nil
}

// hasSavedPassword 判断这个账号能不能自动重登。
func hasSavedPassword(acc Account) bool {
	return acc.SavedUser != "" && acc.SavedPass != ""
}

// autoRelogin 用保存的密码无头重登一次，成功后把新凭证写回账号。
//
// 用的是**游戏身份**（ma75）上下文：这样拿到的凭证既能启动游戏，也能按渠道签发，
// 一举两得。失败时不动账号里任何东西，只返回错误。
func autoRelogin(acc Account) (HeadlessResult, error) {
	user, pass, err := accountPassword(acc)
	if err != nil {
		return HeadlessResult{}, err
	}
	logf("%s：凭证不可用，用保存的账号密码自动重登", displayName(acc))
	res, err := HeadlessPasswordLogin(user, pass, profile.LongGameID)
	if err != nil {
		logf("%s：自动重登失败（%v）", displayName(acc), err)
		return res, err
	}
	if res.Platform {
		// 不该发生：上下文传的就是游戏身份。真发生了说明 DLL 行为变了，
		// 存进去也签不了渠道，直接报错别污染账号。
		return res, fmt.Errorf("自动重登拿到的是平台身份凭证，已放弃保存")
	}
	if err := saveHeadlessCred(acc.PlayerID, res); err != nil {
		return res, err
	}
	logf("%s：自动重登成功（%d 字符，可签发）", displayName(acc), len(res.Token))
	return res, nil
}
