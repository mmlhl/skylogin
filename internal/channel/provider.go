package channel

// 渠道登录的统一契约。
//
// 一个渠道要接进来，只需要回答三个问题：
//
//	我是谁            Key / Label
//	我的凭证怎么算     Info（uni_sauth 的四个渠道参数）
//	我支持哪些登录方式 Methods（每一项对应一种拿凭证的手段）
//
// 登录流程本身（开窗、轮询、落库、校验、续期）不在这里，由 app 层统一跑。
// 这样加渠道 = 加一个文件 + 注册一行，流程代码永远不动。
//
// 本文件只放契约，不放实现。现有四个渠道的适配在 providers.go。

import (
	"context"

	"skylogin/internal/netsec"
)

// Kind 是登录方式的类别。界面按类别渲染，不按渠道名判断。
type Kind string

const (
	// KindWeb 网页登录：开一个窗口加载官方登录页，注入脚本把结果抓回来。
	KindWeb Kind = "web"
	// KindSMS 手机号 + 短信验证码，可能还需要选账号。
	KindSMS Kind = "sms"
	// KindQR 出二维码，用户在手机上扫。
	KindQR Kind = "qr"
	// KindSilent 一键免密：依赖设备上已登录的同厂 App。
	KindSilent Kind = "silent"
	// KindPassword 账号密码。
	KindPassword Kind = "password"
	// KindPackage 整包导入：把现成拼好的登录包原样收下。
	KindPackage Kind = "package"
)

// Cred 是一次登录的产物，也是唯一跨层传递的东西。
//
// 对 uni_sauth 而言只有两个值有意义：UID → sdkuid，Secret → sessionid。
// 各渠道的对应关系见 .ai/11 第十节的映射表（九游的 sdkuid 固定是 "0"）。
type Cred struct {
	UID      string
	Secret   string
	Extra    map[string]string // refreshToken / accountId / serviceTicket 这类附带材料
	Nickname string
	Method   Kind
}

// Field 是 Ask 里的一个输入项。
type Field struct {
	Name        string
	Label       string
	Type        string // text | password | tel | code
	Placeholder string
	MaxLen      int
}

// Option 是 Ask 里的一个选项（选账号、选服务器）。
type Option struct {
	Value string
	Label string
}

// Slice 是一次"请用户输入"的请求。
//
// 有了它，短信登录的三步（手机号 → 验证码 → 选账号）不需要每种方式各写一套窗口：
// 后端发一个 Slice，前端按 Kind 渲染表单或列表，结果回填给 Login。
type Slice struct {
	Kind    string // form | choice | confirm
	Title   string
	Fields  []Field
	Options []Option
}

// Progress 是登录过程中的回报通道：进度文案、二维码、需要用户输入。
type Progress interface {
	Phase(phase, message string)
	QR(png []byte, url string)
	Ask(ctx context.Context, s Slice) (map[string]string, error)
}

// Method 一种登录方式。Login 只负责拿到凭证，落库与校验由上层统一做。
type Method interface {
	Kind() Kind
	Label() string
	Hint() string
	// Available 报告运行期可用性，比如"需要已登录的九游 App"。
	// 不可用时界面显示原因而不是直接隐藏——用户需要知道为什么。
	Available(ctx context.Context) (ok bool, reason string)
	Login(ctx context.Context, p Progress) (Cred, error)
}

// Provider 一个渠道。
//
// Methods 返回的顺序就是界面上的顺序；没有的方式直接不返回，
// 界面自然不显示，前端不需要知道"这个渠道有没有微信登录"。
type Provider interface {
	Key() string
	Label() string
	// Info 是 uni_sauth 里的渠道身份参数（login_channel / sdk_version 等）。
	Info() netsec.ChannelInfo
	// Window 是该渠道网页登录窗的外观与注入脚本。
	Window() LoginWindow
	Methods() []Method
}
