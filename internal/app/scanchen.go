package app

// 渠道服「游戏内扫码」的渠道注册表。
//
// 这里只登记差异，不写流程。扫码的整体流程（开官方登录窗 → 等回调 → 落库）
// 在 channellogin/mpaylogin 里，对每个渠道都一样；真正不同的只有四件事：
//
//	1. 服务端在回调 slot7 里给这个渠道起的名字（Key）
//	2. 界面上怎么称呼它（Label）
//	3. 凭证在回调里的形态（Kind：sdk_info 还是整包）
//	4. 凭证大概能活多久（Lifespan，只用于提示用户）
//
// 以后要接新渠道（小米 / OPPO / TapTap / 荣耀…），只要在 scanChannels 里
// 加一行，其它代码都不用动。
//
// 关于"整包"：vivo 之后我们发现，非华为渠道大多不走 sdk_info，而是直接给
// 一份拼好的登录包（extra_unisdk_data → SAUTH_JSON），原样存下来就能用。
// 所以 Kind 只有两种，加渠道时先看它给的是哪一种。

// scanKind 说明凭证在回调里的形态。
type scanKind int

const (
	// kindSDKInfo：slot8 里有 sdk_info（openid + accessToken），
	// 还要再调一次渠道接口换出游戏登录凭证。目前只有华为是这样。
	kindSDKInfo scanKind = iota
	// kindPackage：slot8 里是拼好的整包（extra_unisdk_data），直接能用。
	kindPackage
)

// scanChannel 一个渠道在"游戏内扫码"里需要知道的全部差异。
type scanChannel struct {
	// Key 是服务端在回调 slot7 里返回的渠道名（存账号时也会归一成这个名字）。
	Key string
	// Label 是界面上的叫法。
	Label string
	// Kind 决定凭证怎么解析。
	Kind scanKind
	// Lifespan 是凭证大概能活多久，写进日志和界面提示。
	Lifespan string
	// Supported 为 false 表示这条路能加到账号、但进不了游戏（应用宝就是）。
	Supported bool
	// Note 是给用户的一句话说明。
	Note string
}

// scanChannels 是"游戏内扫码"支持的渠道清单。
//
// 加新渠道时：先做一次扫码，看回调 slot7 是什么、slot8 里是 sdk_info 还是整包，
// 然后在这里加一行。
var scanChannels = []scanChannel{
	{
		Key: "huawei", Label: "华为", Kind: kindSDKInfo, Supported: true,
		Lifespan: "accessToken 约 1 小时（启动前会自动重签）",
		Note:     "在游戏「加好友」界面的扫码入口扫",
	},
	{
		Key: "nearme_vivo", Label: "vivo", Kind: kindPackage, Supported: true,
		Lifespan: "会话约 1 小时",
		Note:     "在游戏「加好友」界面的扫码入口扫",
	},
	{
		Key: "bilibili_sdk", Label: "B站", Kind: kindPackage, Supported: true,
		Lifespan: "较久",
	},
	{
		Key: "4399com", Label: "4399", Kind: kindPackage, Supported: true,
		Lifespan: "较久",
	},
	{
		Key: "uc_platform", Label: "九游", Kind: kindPackage, Supported: true,
		Lifespan: "较久",
	},
	{
		Key: "myapp", Label: "应用宝", Kind: kindPackage, Supported: false,
		Lifespan: "2 小时，且同一微信只能在一台设备上有效",
		Note:     "PC 版客户端没有这个渠道的换票实现，能加到账号但进不了游戏",
	},
}

// lookupScanChannel 按服务端渠道名找登记项；不认识的返回零值 + false。
func lookupScanChannel(key string) (scanChannel, bool) {
	for _, c := range scanChannels {
		if c.Key == key {
			return c, true
		}
	}
	return scanChannel{}, false
}

// labelOrKey 取渠道的显示名，没登记的按原样显示。
func labelOrKey(key string) string {
	if c, ok := lookupScanChannel(key); ok {
		return c.Label
	}
	if key == "" {
		return "渠道服"
	}
	return key
}

// scanChannelHelp 给界面用的一段说明：支持哪些渠道、各自注意什么。
func scanChannelHelp() string {
	var parts []string
	for _, c := range scanChannels {
		if !c.Supported {
			continue
		}
		parts = append(parts, c.Label)
	}
	return "支持 " + joinChinese(parts) + "。打开游戏到「加好友」界面点扫码，用对应渠道的 App 扫窗口里的二维码。华为 / vivo 的凭证只有约 1 小时，过期后重新扫一次即可。"
}

// joinChinese 用顿号拼接。
func joinChinese(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += "、"
		}
		out += s
	}
	return out
}

// ChannelScanInfo 给界面用的「渠道服扫码」说明。
//
// 界面只读这一处，所以以后加渠道时改 scanChannels 就够了，不用再改前端文案。
func ChannelScanInfo() map[string]any {
	list := make([]map[string]any, 0, len(scanChannels))
	for _, c := range scanChannels {
		list = append(list, map[string]any{
			"key":       c.Key,
			"label":     c.Label,
			"supported": c.Supported,
			"lifespan":  c.Lifespan,
			"note":      c.Note,
		})
	}
	return map[string]any{
		"help":     scanChannelHelp(),
		"channels": list,
	}
}
