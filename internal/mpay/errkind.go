package mpay

import (
	"errors"
	"fmt"

	"skylogin/internal/neterr"
)

// Kind 把"请求失败"分成几类，供调用方判断**能不能据此断定凭证失效**。
//
// 为什么必须分
//
// 2026-11-10 的用户日志里，110 次 "token 已失效" 全部来自 DNS 解析失败
// （lookup service.mkey.163.com: no such host），凭证本身是好的。
// 旧代码只看 err != nil 就判过期，后果是一串连锁反应：
// 界面弹刷新横幅 → 触发自动重登 → 重登后仍然解析不了 → 再判过期 → 再重登。
// 同设备反复重登会把该账号别的会话顶掉，比"暂时签不出票"严重得多。
type Kind int

const (
	// KindUnknown 说不清是哪一类，调用方按"暂时无法验证"处理。
	KindUnknown Kind = iota
	// KindNetwork 请求根本没到服务端：DNS 失败、连不上、超时、响应不是 JSON。
	// 这一类**不能**用来判断凭证死活。
	KindNetwork
	// KindAuth 服务端明确回了"这个凭证不认"（见 authCodes）。可以判过期。
	KindAuth
	// KindRejected 服务端明确拒绝，但不是凭证的问题（参数错、风控、渠道不符…）。
	KindRejected
)

func (k Kind) String() string {
	switch k {
	case KindNetwork:
		return "网络不可达"
	case KindAuth:
		return "凭证被服务端拒绝"
	case KindRejected:
		return "服务端拒绝（非凭证原因）"
	default:
		return "未知错误"
	}
}

// authCodes 是"服务端明确表示凭证失效"的错误码。
//
// 只放经过实测确认的码：
//
//	1306 用户登录失效 —— 拿不匹配的上下文签票、token 过期都会回它
//
// 特别注意**不要**把 6008 放进来。6008 出现在加载湾的
// third_party_account/ticket（"当前槽没有记住这个账号"），是"记住槽没命中"
// 的正常回退信号——每个没走过扫码写槽的账号都会拿到它，把它当失效
// 会把所有这类账号一次性误标成过期。
var authCodes = map[int]bool{
	1306: true,
}

// Classify 判断一个错误属于哪一类。
func Classify(err error) Kind {
	if err == nil {
		return KindUnknown
	}
	var te *TicketError
	if errors.As(err, &te) {
		if authCodes[te.Code] {
			return KindAuth
		}
		return KindRejected
	}
	if isNetworkErr(err) {
		return KindNetwork
	}
	return KindUnknown
}

// IsAuth 表示服务端明确说凭证失效。
func IsAuth(err error) bool { return Classify(err) == KindAuth }

// IsNetwork 表示这次失败没能问到服务端，结论不可用。
func IsNetwork(err error) bool { return Classify(err) == KindNetwork }

// isNetworkErr 识别"请求没到服务端"这一类错误。
//
// 判据交给 internal/neterr（channel 包用的是同一套）；这里只补一个
// mpay 特有的形态：服务端回了非 JSON，说明中间设备改写了响应，
// 同样属于"没正常问到"。
func isNetworkErr(err error) bool {
	var nj *NotJSONError
	if errors.As(err, &nj) {
		return true
	}
	return neterr.IsNetwork(err)
}

// NotJSONError 表示服务端回了 HTML / 空响应 / 被中间设备改写的内容。
//
// 这属于"没能正常问到服务端"，不能据此判断凭证。单独定一个类型，
// 是为了让 Classify 认得出它，而不是靠错误字符串做匹配。
type NotJSONError struct {
	Path string
	Body string
}

func (e *NotJSONError) Error() string {
	return fmt.Sprintf("%s 返回非 JSON: %s", e.Path, truncate(e.Body, 200))
}
