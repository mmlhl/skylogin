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
	Mu        sync.Mutex
	Active    bool
	Kind      string
	Title     string
	Message   string
	Account   string // 展示用（用户名 / 手机号）
	AccountID string // 定位用（PlayerID）；横幅的"一键刷新"按它找账号
	Since     time.Time
}

var currentHint authHint

// setAuthHint 记下一条需要用户处理的提示。
// account 是展示名，accountID 用于界面精确回跳到要刷新的账号。
func setAuthHint(kind, title, message, account, accountID string) {
	currentHint.Mu.Lock()
	defer currentHint.Mu.Unlock()
	currentHint.Active = true
	currentHint.Kind = kind
	currentHint.Title = title
	currentHint.Message = message
	currentHint.Account = account
	currentHint.AccountID = accountID
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
		"kind":      currentHint.Kind,
		"title":     currentHint.Title,
		"message":   currentHint.Message,
		"account":   currentHint.Account,
		"accountId": currentHint.AccountID,
	}
}

// launchAlert 一次性启动事件弹窗（如客户端闪退）。带递增 id，
// 前端记住已展示的 id 就不会重复弹；内容只保留最近一条。
var (
	launchAlertMu  sync.Mutex
	launchAlertVal map[string]any
	launchAlertSeq int64
)

// setLaunchAlert 推一条启动事件弹窗（覆盖上一条）。
func setLaunchAlert(title, message string) {
	launchAlertMu.Lock()
	defer launchAlertMu.Unlock()
	launchAlertSeq++
	launchAlertVal = map[string]any{
		"id":      launchAlertSeq,
		"title":   title,
		"message": message,
	}
	logf("启动事件：%s — %s", title, message)
}

// launchAlertView 给 /api/state 的弹窗字段（nil = 没有新事件）。
func launchAlertView() map[string]any {
	launchAlertMu.Lock()
	defer launchAlertMu.Unlock()
	return launchAlertVal
}
