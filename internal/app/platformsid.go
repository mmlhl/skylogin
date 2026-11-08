package app

// 加载湾会话（sid）的获取与缓存。
//
// 为什么需要：平台自己扫码换票时带着它的登录会话（sid），服务端据此把"扫码的
// 这个账号"写进记住列表；我们匿名调同一个接口，服务端就不记
// （2026-11-05 抓包对比：参数一模一样，唯一差别是 sid；实测匿名换票后记住列表为空）。
// 所以换票前先拿一个 sid——用任意一个 token 还有效的账号去换就行。

import (
	"fmt"
	"sync"
	"time"

	"skylogin/internal/launcher"
)

const platformSidTTL = 20 * time.Minute

var (
	sidMu   sync.Mutex
	sidVal  string
	sidTime time.Time
)

// rememberedTicket 用"服务端记住的授权"给这个账号直接换一张票（不需要 token）。
//
// 能成立，是因为扫码换票时我们带了平台会话 sid，服务端把那个账号写进了
// 这个 sid 对应的"记住槽"（2026-11-05 抓包对比 + 实测）。
//
// 槽是按 sid（平台用户）分的，所以在续期时要把所有能换 sid 的账号都试一遍——
// 命中的那个槽里可能正好是这个号。全失败就当没发生过（调用方退回提示重扫）。
func rememberedTicket(acc Account) (string, error) {
	list, err := loadAccounts()
	if err != nil {
		return "", err
	}
	var lastErr error
	for i := range list {
		src := list[i]
		if src.Channel != "netease" || src.MpayToken == "" || src.UserID == "" {
			continue
		}
		sid, lerr := launcher.Login(src.UserID, src.MpayToken, src.DeviceID)
		if lerr != nil || sid == "" {
			continue
		}
		r, terr := launcher.TicketForAccount(acc.UserID, acc.DeviceID, sid)
		if terr == nil {
			logf("免扫续期：用 %s 的平台会话命中了记住槽", displayName(src))
			return r.Ticket, nil
		}
		lastErr = terr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的平台会话")
	}
	return "", lastErr
}

// selfHostTicket 只尝试用账号自己的平台会话，为槽里已经授权的自己出票。
// 它没有扫码确认或写槽步骤；6008 只表示当前槽授权不可用，不能证明不能自寄宿。
// 该实验函数未接入主流程，自寄宿写入应由独立的完整授权探针验证。
func selfHostTicket(acc Account) (string, error) {
	if acc.A50Token == "" || acc.UserID == "" {
		return "", fmt.Errorf("没有 a50 凭证")
	}
	a50Dev := acc.A50DeviceID
	if a50Dev == "" {
		a50Dev = acc.DeviceID
	}
	sid, err := launcher.Login(acc.UserID, acc.A50Token, a50Dev)
	if err != nil || sid == "" {
		return "", fmt.Errorf("a50 凭证换会话失败: %w", err)
	}
	hosted, herr := launcher.SavedAccount(sid)
	if herr == nil {
		if hosted == "" {
			logf("自寄宿出票检查：自己的槽为空；本函数不会写槽")
		} else if hosted != acc.UserID {
			logf("自寄宿出票检查：槽中是另一账号（%s）；本函数不会覆盖", hosted)
		}
	}
	r, terr := launcher.TicketForAccount(acc.UserID, acc.DeviceID, sid)
	if terr != nil {
		return "", fmt.Errorf("自寄宿出票失败: %w", terr)
	}
	return r.Ticket, nil
}

// pickScanSid 给"扫码换票"挑一个 sid：优先用**空槽**的会话。
//
// 记住槽是按 sid 分的，用空槽写就不会把别的账号顶掉；都满了就用第一个（覆盖）。
func pickScanSid() string {
	list, err := loadAccounts()
	if err != nil {
		return ""
	}
	var fallback string
	for i := range list {
		a := list[i]
		if a.Channel != "netease" || a.MpayToken == "" || a.UserID == "" {
			continue
		}
		sid, lerr := launcher.Login(a.UserID, a.MpayToken, a.DeviceID)
		if lerr != nil || sid == "" {
			continue
		}
		cur, serr := launcher.SavedAccount(sid)
		if serr == nil && cur == "" {
			logf("本次扫码换票用一个空槽的会话（%s）", displayName(a))
			return sid
		}
		if fallback == "" {
			fallback = sid
			logf("所有槽都占着，本次扫码换票会用 %s 的槽（会顶掉它原来记的号）", displayName(a))
		}
	}
	return fallback
}

// platformSid 返回一个可用的加载湾会话；拿不到就返回空串（调用方按匿名处理）。
//
// 诊断开关 scan_sid2.on：改用"第二个"可用账号去换 sid。
// 服务端的记住槽是绑在 sid（平台用户）上的，换一个 sid 就是换一个槽，
// 用它验证"多个账号各自记住、互不顶掉"。
func platformSid() string {
	sidMu.Lock()
	defer sidMu.Unlock()
	if sidVal != "" && time.Since(sidTime) < platformSidTTL {
		return sidVal
	}
	sidVal = pickScanSid()
	sidTime = time.Now()
	if sidVal == "" {
		logf("没有账号能换到加载湾会话，本次扫码仍按匿名方式换票")
	}
	return sidVal
}
