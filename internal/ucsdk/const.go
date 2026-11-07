// Package ucsdk 九游（UC）渠道的登录协议实现。
//
// 只做协议，不碰界面、不碰存储、不碰流程编排——那些在 app 层。
// 这样协议层可以离线单测，也可以被别的用途复用。
//
// 三条边界：
//
//	加密信封    RSA 包裹随机 AES key/iv，正文 AES-CBC
//	请求形态    POST /ng/client/{service}?ver=0&df=adat&os=android
//	业务接口    unifiedAccount.*（短信登录）、ucid.*（会话续期）
//
// 注意：九游的第三方登录（微信/QQ/支付宝）走阿里 mtop，签名在 native 库里算，
// 本包不涉及；账号密码同样如此。这里只有「不依赖九游 App」的那条链。
package ucsdk

// 九游协议里用到的固定端点。分三个域名，混用会得到「非法请求」：
//
//	sdk.9game.cn            system.getSecurityKey、mcs.combine
//	sdk-account.9game.cn    unifiedAccount.*、ucid.*
//	sdknc.9game.cn          ucid.vip.*、支付相关
const (
	// HostSDK 下发 RSA 公钥与系统配置。
	HostSDK = "sdk.9game.cn"
	// HostAccount 账号相关：登录、续期、用户信息。
	HostAccount = "sdk-account.9game.cn"
	// HostNC 会员与支付相关。
	HostNC = "sdknc.9game.cn"

	// pathPrefix 所有业务接口的公共前缀。
	pathPrefix = "/ng/client/"
	// querySuffix 公共查询串，实测必须带。
	querySuffix = "?ver=0&df=adat&os=android"
)

// 服务名常量。命名与抓包一致，别改。
const (
	// ServiceSecurityKey 取服务端 RSA 公钥（任何业务接口之前必须先调）。
	ServiceSecurityKey = "system.getSecurityKey"
	// ServiceSiApply 申请风控 token；返回的 si 要带在后续每个请求的 client 段里。
	ServiceSiApply = "si.apply"
	// ServiceSendSms 发送短信验证码。
	ServiceSendSms = "unifiedAccount.sendSmsCode"
	// ServiceLoginBySms 用验证码登录；账号多于一个时返回 214 + 账号列表。
	ServiceLoginBySms = "unifiedAccount.loginBySmsCode"
	// ServiceLoginByPassword 用手机号 + 密码登录；同样可能返回 214。
	ServiceLoginByPassword = "unifiedAccount.loginByMobilePassword"
	// ServiceUserInfo 用 sid 取用户简要信息。
	ServiceUserInfo = "unifiedAccount.getUserSimpleInfo"
	// ServiceRefreshLogin 用 refreshToken 续期，换新 sid。
	ServiceRefreshLogin = "ucid.user.refreshLogin"
)

// 业务状态码。服务端把结果放在 {"state":{"code":…,"msg":…}} 里。
const (
	CodeOK = 1
	// CodeNeedAccount 表示这个手机号下有多个游戏账号，需要带 accountId 重发。
	// 抓包里是 214「请选择游戏账号」。
	CodeNeedAccount = 214
)
