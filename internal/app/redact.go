package app

// 日志脱敏。
//
// 为什么必须有：日志是给用户回报问题用的，会原样贴到群里或发给别人。
// 2026-11-10 那份反馈日志里，MPay token 以完整明文出现了 27 次
// （每次请求失败的 url.Error 都会把整条 URL 打出来），还有一处网易账号
// 邮箱。token 是账号级凭证，拿到就能签票——等于把号交出去。
//
// 原则：**保留足够区分、不保留可用**。
//   - token / access_key / sign 只留前几位，排查时还能看出"是不是同一个"
//   - 邮箱保留首字符和域名
//   - 玩家 ID、设备号不脱敏：它们是排查必需的定位信息，且单独无法登录
//
// 处理放在 logf 这个唯一出口上，避免以后新增日志点又漏掉。

import (
	"regexp"
	"strings"
)

var (
	// token=1-FAKE… （MPay token，base64 + 可能带 URL 编码）
	reTokenParam = regexp.MustCompile(`(token=)([^\s&"')]+)`)
	// access_key= / accesskey= （B站等渠道）
	reAccessKeyParam = regexp.MustCompile(`(access_?key=)([^\s&"')]+)`)
	// sign=<hex> （渠道接口签名）
	reSignParam = regexp.MustCompile(`(\bsign=)([0-9a-fA-F]{8,})`)
	// 邮箱：只留首字符和域名
	reEmail = regexp.MustCompile(`([A-Za-z0-9._%+\-])[A-Za-z0-9._%+\-]*@([A-Za-z0-9.\-]+\.[A-Za-z]{2,})`)
)

// redactLogLine 把日志行里的凭证与隐私打码。
func redactLogLine(s string) string {
	if s == "" {
		return s
	}
	// 绝大多数日志不含这些关键字，先做一次廉价判断，避免每行都跑正则
	if !strings.Contains(s, "token=") && !strings.Contains(s, "key=") &&
		!strings.Contains(s, "sign=") && !strings.Contains(s, "@") {
		return s
	}
	s = maskParam(s, reTokenParam, 6)
	s = maskParam(s, reAccessKeyParam, 4)
	s = maskParam(s, reSignParam, 6)
	s = reEmail.ReplaceAllString(s, "$1***@$2")
	return s
}

// maskParam 把正则第一组（参数名）保留，第二组（值）截成前 keep 位 + 省略号。
func maskParam(s string, re *regexp.Regexp, keep int) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		v := sub[2]
		if len(v) <= keep {
			// 值本来就短，整个抹掉，免得"前 keep 位"等于原文
			return sub[1] + "…"
		}
		return sub[1] + v[:keep] + "…"
	})
}
