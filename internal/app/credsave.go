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
	"fmt"
	"strings"
	"time"

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

// clearSavedPassword 忘掉存下来的账号密码。
//
// 什么时候该清：登录时服务端明确说密码不对。留着它只会让每次自动重登
// 都拿同一个错密码去打接口——既白跑一轮，又可能触发风控。
// 用 saveList 而不是 saveAccount：后者会连带做凭证同步、设当前账号，
// 而这里只想改两个字段。
func clearSavedPassword(target string) error {
	list, err := loadAccounts()
	if err != nil {
		return err
	}
	acc, ok := findAccount(list, target)
	if !ok {
		return fmt.Errorf("没找到账号（%s）", target)
	}
	if !hasSavedPassword(acc) {
		return nil
	}
	acc.SavedUser = ""
	acc.SavedPass = ""
	acc.UpdatedAt = time.Now().Unix()
	return saveList(list)
}

// clearPasswordIfMatches 登录失败后调用：只有当"这次输的密码"和"存下来的密码"
// 完全一样时，才认定存的那份也错了并清掉它。
//
// 区分两种情况很重要：
//   - 存的密码过期（用户在别处改了密码）→ 清掉，别再自动重试；
//   - 用户手输错了一次 → 旧的密码可能还是对的，清掉等于白丢一份能自动续期的凭据。
func clearPasswordIfMatches(loginUser, password string) {
	if loginUser == "" || password == "" {
		return
	}
	list, err := loadAccounts()
	if err != nil {
		return
	}
	for i := range list {
		a := list[i]
		if a.SavedUser != loginUser || !hasSavedPassword(a) {
			continue
		}
		if _, saved, perr := accountPassword(a); perr != nil || saved != password {
			return
		}
		if cerr := clearSavedPassword(a.PlayerID); cerr == nil {
			logf("%s：保存的密码登录失败，已清掉，下次刷新需要重新输入", displayName(a))
		}
		return
	}
}

// looksLikeWrongPassword 判断无头登录失败是不是"账号密码不对"。
//
// DLL 的原话不固定，见过 decrypt fail / 账号或密码错误 / password 这几种。
// 宁可漏判也不要误判：误判会把一份好密码删掉，用户下次还得重新输一遍。
func looksLikeWrongPassword(msg string) bool {
	m := strings.ToLower(msg)
	for _, kw := range []string{"密码", "password", "decrypt fail"} {
		if strings.Contains(m, kw) {
			return true
		}
	}
	return false
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
		// 存的那份密码已经不对了（多半是在别处改过密码）：清掉它，
		// 否则每轮凭证巡检都会拿同一个错密码去撞接口。
		if looksLikeWrongPassword(err.Error()) {
			if cerr := clearSavedPassword(acc.PlayerID); cerr == nil {
				logf("%s：保存的密码已失效，已清掉，请重新输入一次", displayName(acc))
			}
		}
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
