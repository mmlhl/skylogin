package ucsdk

// 内置的协议常量。
//
// 这里只放「服务端定、客户端改不了」的值：两把公钥、游戏身份、请求里的
// 固定字段名。设备环境的构造不在这里，见 device.go。

// 九游的信封加密要用两把公钥，顺序不能弄反：
//
//	引导公钥 bootstrapPublicKey ──加密──> system.getSecurityKey
//	                        <──下发── 会话公钥（securityKey 字段）
//	会话公钥                    ──加密──> 其余所有业务请求
//
// 格式都是「版本号|base64(PKIX DER)」，版本当前是 5，公钥 512 位。

// bootstrapPublicKey 是「换取会话公钥」那一步专用的内置公钥。
//
// getSecurityKey 这条请求本身也要加密，而它用的是一把编译期常量；
// 拿到响应之后才换成新的会话公钥。若用会话公钥去加密这条请求，
// 服务端拿配对私钥解不开，只会回一个 c=-1，且不提示是哪个字段出错。
//
// 实测来源：算法日志里 system.getSecurityKey 的「加密密钥」字段。
// 两次独立冷启动取值完全一致，说明它是固定常量、不随会话变化。
const bootstrapPublicKey = "5|MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJBAKjnX9Dkb4gVT2PbXg9QLPUSmoOQndKra4wfGwub16R/zjdqLJgDo6DisetD4diIw/1gjdSU+6nCEaiK+5a9F/8CAwEAAQ=="

// builtinPublicKey 是上一次实测拿到的会话公钥，只作兜底缓存。
//
// 正常流程每次启动都会重新 getSecurityKey 并把它写进 Client；
// 只有在取公钥那一步失败时，才会退回这个值继续尝试。
// 服务端轮换后本值会失效，需要重新抓一次 getSecurityKey 的响应来更新。
const builtinPublicKey = "5|MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJBAMFxR3NxXU5Mf3U6wykuXYOU+OgXZgr/VyfNaXw4vOGImx0f7a9s9ASrHQAGsev8nqn7C5uLzTo8tiHWd5Cb/YsCAwEAAQ=="

// ClientInfo 是请求里的 client 段（设备环境）。
//
// 字段集合与顺序均按实测抓包（2026-11-08 的 getSecurityKey 明文）敲定：
// ve, os, fr, si, ci, ex, appId。
// 早期版本漏了 ci、多了 apiLevel/ssid、ex 还带了 werewolf 前缀，
// 服务端一律回 c=-1 且不提示是哪个字段。
type ClientInfo struct {
	Ve    string `json:"ve"`
	Os    string `json:"os"`
	Fr    string `json:"fr"`
	Si    string `json:"si"`
	Ci    string `json:"ci"`
	Ex    string `json:"ex"`
	AppID string `json:"appId"`
}

// BizClientInfo 是业务请求里的 client 段。
//
// 和 getSecurityKey 那条完全不是一套：
//   - 字段顺序 ex,fr,os,si,ve,mve,appId,apiLevel,ssid
//   - ex 带 werewolf: 前缀（取公钥那条没有）
//   - 多了 mve / apiLevel / ssid，没有 ci
//
// 混用不影响信封解密（信封只看 key/iv），但业务层会回 code=42「系统繁忙」。
type BizClientInfo struct {
	Ex       string `json:"ex"`
	Fr       string `json:"fr"`
	Os       string `json:"os"`
	Si       string `json:"si"`
	Ve       string `json:"ve"`
	Mve      string `json:"mve"`
	AppID    string `json:"appId"`
	APILevel int    `json:"apiLevel"`
	Ssid     string `json:"ssid"`
}

// SkyGame 是光遇九游包的 game 段参数。
//
// gameId 918836 与 channelId KM_75033 都是「光遇 × 九游」专属的，
// 换成别的游戏或渠道必须重新取值。
func SkyGame() GameInfo {
	return GameInfo{
		CPID:           0,
		GameID:         918836,
		ChannelID:      "KM_75033",
		APKChannelID:   "KM_75033",
		VersionCode:    421589,
		VersionName:    "0.16.7",
		BrandID:        "",
		FirstInstallAt: 1793894327227,
		PackType:       "net",
	}
}
