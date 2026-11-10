package app

import (
	"sort"
	"sync"
	"time"
)

// 待处理的账号提示（目前用于凭证过期）。
//
// 为什么要按账号分开存：过期是**账号级**的事实，但横幅以前只有一条全局的，
// 多个号先后过期时后一个会盖掉前一个——用户只看到最后一个，还以为只有
// 一个号有问题（2026-11-11 反馈"刷新凭证没说哪个账号"）。
//
// 现在一个账号一条，界面按发生时间排成清单：单条照旧一行说清，
// 多条先给数量、点开是逐条清单。
type hintItem struct {
	Kind      string
	Title     string
	Message   string
	Account   string // 展示名
	AccountID string // 定位用（PlayerID / UserID），界面按它精确回跳
	Since     time.Time
}

var (
	hintMu sync.Mutex
	hints  = map[string]hintItem{}
)

// hintKey 一个账号只留一条提示；没有账号 ID 的提示（极少）按 kind 占位。
func hintKey(accountID, kind string) string {
	if accountID != "" {
		return "acct:" + accountID
	}
	return "kind:" + kind
}

// setAuthHint 记下一条需要用户处理的提示。
//
// 同一个账号重复报同一件事时只更新内容、保留首次发生的时间：横幅是按时间
// 排的，每报一次就往后挪会让清单顺序一直跳。
func setAuthHint(kind, title, message, account, accountID string) {
	hintMu.Lock()
	defer hintMu.Unlock()
	k := hintKey(accountID, kind)
	it := hintItem{Kind: kind, Title: title, Message: message, Account: account, AccountID: accountID}
	if old, ok := hints[k]; ok {
		it.Since = old.Since
	} else {
		it.Since = time.Now()
	}
	hints[k] = it
}

// clearAuthHint 清掉**所有**提示（扫码 / 登录成功这类"整体重来"的时机）。
//
// 注意：只是某个号恢复可用时不要用它，用 clearAuthHintFor——否则会把
// 别的号还挂着的"凭证过期"一起抹掉。
func clearAuthHint() {
	hintMu.Lock()
	defer hintMu.Unlock()
	hints = map[string]hintItem{}
}

// clearAuthHintFor 只撤掉指定账号的提示。
func clearAuthHintFor(accountID string) {
	if accountID == "" {
		return
	}
	hintMu.Lock()
	defer hintMu.Unlock()
	delete(hints, hintKey(accountID, ""))
}

// clearAuthHintForAccount 按账号的两种 id 各清一次（调用方不一定知道手里是哪个）。
func clearAuthHintForAccount(acc Account) {
	clearAuthHintFor(acc.UserID)
	clearAuthHintFor(acc.PlayerID)
}

// authHintView 给界面用的结构；没有提示时返回空 map。
//
// 除了 items 清单，还保留 kind/message/account/accountId 这几个"第一条"的字段：
// 万一还有只认单条的地方，它至少能显示最早的那条。
func authHintView() map[string]any {
	hintMu.Lock()
	items := make([]hintItem, 0, len(hints))
	for _, it := range hints {
		items = append(items, it)
	}
	hintMu.Unlock()
	if len(items) == 0 {
		return map[string]any{}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Since.Equal(items[j].Since) {
			return items[i].Account < items[j].Account // 同一秒内按名字，保证顺序稳定
		}
		return items[i].Since.Before(items[j].Since)
	})
	list := make([]map[string]any, 0, len(items))
	for _, it := range items {
		list = append(list, map[string]any{
			"kind":      it.Kind,
			"title":     it.Title,
			"message":   it.Message,
			"account":   it.Account,
			"accountId": it.AccountID,
		})
	}
	first := items[0]
	return map[string]any{
		"kind":      first.Kind,
		"title":     first.Title,
		"message":   first.Message,
		"account":   first.Account,
		"accountId": first.AccountID,
		"count":     len(items),
		"items":     list,
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
