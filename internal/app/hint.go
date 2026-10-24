package app

import (
	"sync"
	"time"
)

// authHint 是"需要用户处理"的账号提示（目前用于网易凭证过期）。
//
// 这类问题不该只躺在日志里——玩家看不到日志，只会觉得"点了没反应"。
// 记在内存里，由界面读取并显示成可点击的提示。
type authHint struct {
	Mu      sync.Mutex
	Active  bool
	Kind    string // 目前只有 "netease_expired"
	Title   string
	Message string
	Account string
	Since   time.Time
}

var currentHint authHint

// setAuthHint 记下一条需要用户处理的提示。
func setAuthHint(kind, title, message, account string) {
	currentHint.Mu.Lock()
	defer currentHint.Mu.Unlock()
	currentHint.Active = true
	currentHint.Kind = kind
	currentHint.Title = title
	currentHint.Message = message
	currentHint.Account = account
	currentHint.Since = time.Now()
}

// clearAuthHint 清掉提示（扫码成功、登录成功时调用）。
func clearAuthHint() {
	currentHint.Mu.Lock()
	defer currentHint.Mu.Unlock()
	currentHint.Active = false
	currentHint.Kind = ""
	currentHint.Title = ""
	currentHint.Message = ""
	currentHint.Account = ""
}

// authHintView 给界面用的结构；没有提示时返回空 map。
func authHintView() map[string]any {
	currentHint.Mu.Lock()
	defer currentHint.Mu.Unlock()
	if !currentHint.Active {
		return map[string]any{}
	}
	return map[string]any{
		"kind":    currentHint.Kind,
		"title":   currentHint.Title,
		"message": currentHint.Message,
		"account": currentHint.Account,
	}
}
