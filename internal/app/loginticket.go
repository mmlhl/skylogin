package app

// login/ticket 兑换的共用入口：一张票换用户信息（含 MPay token）。
// 按几组已知的游戏上下文依次尝试，哪组能用用哪组；被拒的把完整响应写进日志。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

// exchangeSMSLoginTicket 拿短信登录的 AAAA 票换登录态，**优先换账号级凭证**。
//
// 两个兑换口换出的 token 等级不同（2026-10-29 抓包结论）：
//
//	mobile/finish → s=yd_…、sa=-9、没有 lt/od，账号级，长期有效（平台存的就是这种）
//	login/ticket  → s=[mp]-…、带 lt(登录时间) 和 odi(绑定设备)、sa=3，会话级，会过期
//
// 所以先试 finish，通了就用；不通再退回 login/ticket，保证登录本身不会失败。
// 这里只给短信票用：扫码那条路的票（加载湾发的）试 finish 会被吃掉登录态，
// 之后 login/ticket 直接 1306，**别拿它来试**（2026-11-05 实测）。
func exchangeSMSLoginTicket(c *mpay.Client, ticket, devID string) (map[string]any, error) {
	var errs []string
	// 短信票是 a50 签的，所以 a50 的两组上下文先生效。
	for _, v := range []struct{ name, long, short, ch, cv string }{
		{"我们的 SMS 上下文", profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", "c4.20.0"},
		{"平台原始参数", profile.HostedGameID, "a50", "netease.allysdk3rd", "c4.21.0"},
	} {
		out, err := c.FinishSMS(ticket, devID, v.long, v.short, v.ch, v.cv)
		if err == nil && userTokenOf(out) != "" {
			logf("票据换登录态：mobile/finish（%s）成功", v.name)
			return out, nil
		}
		errs = append(errs, v.name+": "+errText(err))
	}
	logf("mobile/finish 都失败（%s），退回 login/ticket", strings.Join(errs, "；"))
	out, err := loginTicketAuto(c, ticket, devID)
	if err != nil {
		return out, err
	}
	return out, nil
}

// userTokenOf 取响应里的 user.token。
func userTokenOf(out map[string]any) string {
	user, _ := out["user"].(map[string]any)
	if user == nil {
		return ""
	}
	return str(user["token"])
}

// errText 让 nil 错误也能安全拼进日志。
func errText(err error) string {
	if err == nil {
		return "(没拿到 user.token)"
	}
	return err.Error()
}

// accessTokenInUser 从换票响应的 user.pc_ext_info 里取 EXT_ACCESS_TOKEN。
//
// 结构：user.pc_ext_info.extra_unisdk_data 是一段**内层 JSON 字符串**，
// 里面 {"EXT_ACCESS_TOKEN":"…"}（10-24 抓包里是 yd_… 开头）。
func accessTokenInUser(user map[string]any) string {
	ext, _ := user["pc_ext_info"].(map[string]any)
	if ext == nil {
		return ""
	}
	raw, _ := ext["extra_unisdk_data"].(string)
	if raw == "" {
		return ""
	}
	var inner map[string]any
	if json.Unmarshal([]byte(raw), &inner) != nil {
		return ""
	}
	v, _ := inner["EXT_ACCESS_TOKEN"].(string)
	return v
}

// accessHead 已废弃：日志里不再输出 token 片段。

// probeAccessToken 只读探测：EXT_ACCESS_TOKEN 能不能当凭证用。
//
// delay 秒后开始，每 60 秒验一次，最多验 tries 次（tries<=0 只验一次）。
// 只写日志，不改任何账号数据。
func probeAccessToken(access, userID, dev string, tries int) {
	if access == "" || userID == "" || dev == "" {
		return
	}
	longID, shortID := ticketContextForToken(access)
	c := mpay.New()
	if user, err := c.GetUserInfo(access, userID, dev, longID, shortID); err == nil {
		logf("[探测] EXT_ACCESS_TOKEN 能查用户资料（%s）", mpay.DisplayNameOf(user))
	} else {
		logf("[探测] EXT_ACCESS_TOKEN 查资料失败: %v", err)
		return
	}
	if t, _, err := c.CreateTicketFor(access, userID, dev, longID, shortID); err == nil {
		logf("[探测] EXT_ACCESS_TOKEN 能签票: %s", maskTicket(t))
	} else {
		logf("[探测] EXT_ACCESS_TOKEN 签票失败: %v", err)
	}
	if tries <= 0 {
		return
	}
	go func() {
		for i := 1; i <= tries; i++ {
			time.Sleep(time.Minute)
			if _, err := c.GetUserInfo(access, userID, dev, longID, shortID); err != nil {
				logf("[寿命] EXT_ACCESS_TOKEN 第 %d 分钟已失效（%v）", i, err)
				return
			}
		}
		logf("[寿命] EXT_ACCESS_TOKEN %d 分钟后仍可用", tries)
	}()
}

// tokenKeyList 把 token 解出来的**字段名**列一下（只列名字，不列值）。
//
// 排查用：平台的登录 token 带 u_ki，游戏那份不带——靠这行一眼能看出差别。
func tokenKeyList(token string) string {
	raw := strings.TrimPrefix(token, "1-")
	b, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		b, err = base64.StdEncoding.DecodeString(raw)
	}
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// tokenGrade 判断一张 MPay token 是"账号级"还是"会话级"，只用于写日志。
//
// 账号级：{"s":"yd_…","sa":-9}，没有 lt/odi，长期有效；
// 也能是回调里那个裸的 EXT_ACCESS_TOKEN（yd_… 开头，会话里根本没有 lt/od）；
// 会话级：带 lt（登录时间）和 odi（绑定的设备），放一阵就失效（1306 用户登录失效）。
// 解析失败就返回空串，不影响任何流程。
func tokenGrade(token string) string {
	if strings.HasPrefix(token, "yd_") {
		return "账号级（EXT_ACCESS_TOKEN，长期有效）"
	}
	raw := strings.TrimPrefix(token, "1-")
	b, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		b, err = base64.StdEncoding.DecodeString(raw)
	}
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	_, hasLT := m["lt"]
	_, hasOD := m["odi"]
	if hasLT || hasOD {
		return "会话级（带 lt/od，会过期）"
	}
	return "账号级（长期有效）"
}

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

// redactSecrets 递归把响应里的凭证字段换成长度占位，其他字段原样保留。
//
// 需要脱敏的：MPay token（1-eyJ…）、EXT_ACCESS_TOKEN（yd_…/38 位 hex）、
// 以及各种 ticket/code —— 日志里只留长度，够排查又不泄露。
func redactSecrets(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			lower := strings.ToLower(k)
			if s, ok := val.(string); ok && s != "" {
				if lower == "token" || lower == "user_token" || strings.Contains(lower, "access_token") {
					out[k] = fmt.Sprintf("<%d 字符已隐藏>", len(s))
					continue
				}
				if lower == "ticket" || lower == "code" {
					out[k] = maskTicket(s)
					continue
				}
			}
			out[k] = redactSecrets(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactSecrets(val)
		}
		return out
	default:
		return v
	}
}

// compactJSON 把响应压成一行日志（截断）。
//
// 响应里的 user.token 是登录凭证，必须脱敏后再写日志。
func compactJSON(out map[string]any) string {
	if len(out) == 0 {
		return "(空响应)"
	}
	b, err := json.Marshal(redactSecrets(out))
	if err != nil {
		return "(响应无法序列化)"
	}
	const max = 600
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
