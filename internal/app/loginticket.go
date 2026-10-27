package app

// login/ticket 兑换的共用入口：一张票换用户信息（含 MPay token）。
// 按几组已知的游戏上下文依次尝试，哪组能用用哪组；被拒的把完整响应写进日志。

import (
	"encoding/json"

	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

// loginTicketAuto 按上下文变体链尝试 login/ticket，返回第一个成功的响应。
// 全部被拒时返回最后一个错误（日志里已有每组的完整响应）。
func loginTicketAuto(c *mpay.Client, ticket, devID string) (map[string]any, error) {
	type variant struct {
		name  string
		long  string // game_id
		short string // jf_game_id
	}
	variants := []variant{
		{"a50托管 jf=a50", profile.HostedGameID, "a50"},
		{"a50托管 jf=ma75", profile.HostedGameID, profile.ShortGameID},
		{"光遇本体 jf=ma75", profile.LongGameID, profile.ShortGameID},
	}
	var lastOut map[string]any
	var lastErr error
	for _, v := range variants {
		out, err := c.LoginTicket(ticket, devID, v.long, v.short)
		if err == nil {
			logf("login/ticket 上下文「%s」通过", v.name)
			return out, nil
		}
		lastOut, lastErr = out, err
		logf("login/ticket 上下文「%s」被拒: %v 响应: %s", v.name, err, compactJSON(out))
	}
	return lastOut, lastErr
}

// compactJSON 把响应压成一行日志（截断）。
func compactJSON(out map[string]any) string {
	if len(out) == 0 {
		return "(空响应)"
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "(响应无法序列化)"
	}
	const max = 600
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
