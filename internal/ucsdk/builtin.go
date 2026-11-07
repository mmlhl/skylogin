package ucsdk

// 内置的实测参数。
//
// 这些都是「服务端定、客户端无法协商」的值，只能从真机抓包得到。
// 集中放一处，轮换时只改这里。

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

// SkyClient 返回光遇九游包的 client 段（仅用于取公钥那一步）。
//
// si 是风控会话 token，会随会话变而且会过期。实测留空不影响取公钥与业务请求，
// 所以这里留空；若将来真的被风控拦住，再考虑接 si.apply 动态取。
func SkyClient() ClientInfo {
	return ClientInfo{
		Ve:    "9.8.12.0",
		Os:    "android",
		Fr:    "API Level-36 - OnePlus PJZ110-16",
		Si:    "",
		Ci:    "6447c470f7716382a74697d5d456baa0",
		Ex:    skyFingerprint,
		AppID: "17",
	}
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

// SkyBizClient 返回光遇九游包业务请求的 client 段。
func SkyBizClient() BizClientInfo {
	return BizClientInfo{
		Ex:       skyBizFingerprint,
		Fr:       "API Level-36 - OnePlus PJZ110-16",
		Os:       "android",
		Si:       "",
		Ve:       "9.8.12.0",
		Mve:      "9.8.12.0",
		AppID:    "17",
		APILevel: 36,
		Ssid:     "<unknown ssid>",
	}
}

// skyBizFingerprint 是业务请求上报的设备指纹，带 werewolf: 前缀。
//
// 前缀后面那串每次会话都不同（疑似会话级摘要），这里是某次实测值。
const skyBizFingerprint = "werewolf:c8d8bc68f2d9c99cc5505355ddc63bd0|orient:L|" +
	"sola:527848B310B793E41E0616F38EE85059|csid:a7814273-c4e3-4393-87c7-84b189aca457|" +
	"netType:-2|bssid:02:00:00:00:00:00|utdid:au9pNRh097QDAONuTga4IWu6|net_id:-1|imsi:|" +
	"osId:cfae1414-a7c3-455f-9a84-31772646f4cf|resY:1440|resX:3008|ssid:<unknown ssid>|mac:|" +
	"kaisa:3C3852CBBD0679364F806DBB912CC463|ackerman:528E9051B436D4B69DDEDE060A07CC88|" +
	"imei:|mikasa:527848B310B793E41E0616F38EE85059|model:PJZ110|mobi:|net:wifi|" +
	"uya:D0585CA2DDE614276EC2F6B7B69FDC0B3|" +
	"oaId:13DE77E2083543938062A2C29A49EBC8C1def4d861ce501b55a66979d62f10fcf"

// skyFingerprint 是 getSecurityKey 阶段上报的设备指纹串。
//
// 注意：它以 orient:L 开头，没有 werewolf 前缀——那是登录阶段才出现的。
// 各段含义：mikasa/sola/ackerman/kaisa/uya 都是 MD5 值，其余是网络与屏幕参数。
const skyFingerprint = "orient:L|sola:527848B310B793E41E0616F38EE85059|" +
	"csid:dbc2dc5f-db92-4b75-8b91-d37627086f15|netType:-2|bssid:|utdid:au9dFUCsGgUDAHxJW8nb3dgF|net_id:-1|imsi:|" +
	"osId:ef63be7a-4026-4dd9-b3fe-f64978ec2cd7|resY:1440|resX:3008|ssid:|mac:|" +
	"kaisa:D394BE0BFC408B3B469C5AB79CCDD700|ackerman:4E8C22FCF8979613A28058AA5D25BC63|" +
	"imei:|mikasa:527848B310B793E41E0616F38EE85059|model:PJZ110|mobi:|net:wifi|" +
	"uya:1CE15E03389148CC6527EF3E7028658A|oaId:"

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
