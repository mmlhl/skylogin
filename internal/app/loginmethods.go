package app

// 「添加账号」的登录入口清单。
//
// 以前这份清单硬编码在 web/src/components/ChannelPicker.vue 里：加一个渠道
// 要同时改 Go 和 Vue，前端还得知道"B站是网页登录、九游只能扫码"（当时的情况）。
// 现在 Go 是唯一来源，前端只负责渲染：
//
//	后端多一项 → 界面多一行
//	后端少一项 → 界面自动消失
//
// 每一步都在往"渠道自己声明能力"的方向走：清单里的渠道项，label / hint
// 来自 channel.Provider 的 Methods()，只有网易和华为这种还没迁进渠道包的
// 特殊入口才在下面手工补。

import (
	"skylogin/internal/channel"
)

// loginMethod 一种登录方式，前端按 kind 决定点下去打开哪个界面。
type loginMethod struct {
	Kind  string `json:"kind"`  // web | qr | sms | password | channelscan | package
	Label string `json:"label"` // 「扫码登录」
	Hint  string `json:"hint"`  // 一句话副标题
}

// loginEntry 「添加账号」列表里的一项。
//
// Group 为 true 表示点进去还有二级（目前只有「网易账号」：扫码 / 账号密码 / 手机验证码）。
// OnlyRefresh 为 true 表示只在"刷新已有账号凭证"时出现，不进添加列表。
type loginEntry struct {
	ID          string        `json:"id"`
	Action      string        `json:"action"`  // 前端调接口时用的标识：bilibili / m4399 / vivo / huawei / netease
	Label       string        `json:"label"`   // 「B站账号」
	Hint        string        `json:"hint"`    // 副标题
	Initial     string        `json:"initial"` // 图标上的字
	Group       bool          `json:"group"`
	OnlyRefresh bool          `json:"onlyRefresh"`
	Methods     []loginMethod `json:"methods"`
}

// LoginEntries 生成「添加账号」的入口清单。
//
// 顺序就是界面顺序；渠道服扫码的说明文字来自 scanchen.go 的注册表，
// 所以那边加渠道时这里不用动。
func LoginEntries() []loginEntry {
	entries := make([]loginEntry, 0, 8)

	// 已注册进渠道包的三个网页登录渠道：文案直接取渠道自己声明的
	for _, item := range []struct {
		key     string
		action  string
		initial string
	}{
		{channel.KeyBili, "bilibili", "B"},
		{channel.KeyM4399, "m4399", "4"},
		{channel.KeyVivo, "vivo", "V"},
	} {
		p, ok := channel.Lookup(item.key)
		if !ok {
			continue
		}
		methods := make([]loginMethod, 0, len(p.Methods()))
		for _, m := range p.Methods() {
			methods = append(methods, loginMethod{
				Kind:  string(m.Kind()),
				Label: m.Label(),
				Hint:  m.Hint(),
			})
		}
		if len(methods) == 0 {
			continue
		}
		entries = append(entries, loginEntry{
			ID:      item.key,
			Action:  item.action,
			Label:   p.Label() + "账号",
			Hint:    methods[0].Hint,
			Initial: item.initial,
			Methods: methods,
		})
	}

	// 华为：扫码登录还没迁进渠道包（走 huawei 包 + WebView 扫码），先手工补
	entries = append(entries, loginEntry{
		ID:      "huawei",
		Action:  "huawei",
		Label:   "华为账号",
		Hint:    "用华为手机扫码",
		Initial: "华",
		Methods: []loginMethod{{Kind: "qr", Label: "华为账号", Hint: "用华为手机扫码"}},
	})

	// 渠道服扫码：开的是官方登录窗，扫完按 slot7 存成对应渠道账号
	entries = append(entries, loginEntry{
		ID:      "netease-channel",
		Action:  "netease",
		Label:   "渠道服扫码",
		Hint:    scanChannelHelp(),
		Initial: "渠",
		Methods: []loginMethod{{Kind: "channelscan", Label: "渠道服扫码", Hint: scanChannelHelp()}},
	})

	// 网易账号：二级菜单，扫码 / 账号密码 / 手机号验证码
	//
	// 这里的扫码走**纯 Go 建码**（Kind=scan，见 scan.go），不用官方登录窗：
	//   - 只有自己建码才拿得到 login_info.code，才能带平台会话去加载湾换票，
	//     服务端才会把这个号写进记住槽（过期后免扫出票）；
	//   - 官方窗那条路 DLL 内部就把换票做完了，我们手上没有 code，补不了写槽。
	// 渠道服扫码仍然走官方窗（Kind=channelscan）：它要的是 DLL 回调里的渠道整包，
	// 纯 Go 扫码给不出那种形态。
	//
	// 账号密码和手机号验证码都走本工具自己的输入框，不弹官方登录窗：
	//   - 官方窗的密码我们看不到、存不下来，换不出续期要用的账号级凭证；
	//   - 自己登录还能顺手落一份平台（a50）凭证，这个号才当得了寄宿宿主。
	// 两条路的区别只在换票方式：密码走 users/login（密码加密在 DLL 里，由子进程
	// 无头完成），验证码走 get_sms / verify_sms / mobile/finish（纯 HTTP，见 sms.go）。
	entries = append(entries, loginEntry{
		ID:      "netease",
		Action:  "netease",
		Label:   "网易账号",
		Hint:    "扫码 / 账号密码 / 手机验证码",
		Initial: "网",
		Group:   true,
		Methods: []loginMethod{
			{Kind: "scan", Label: "扫码登录", Hint: "用光遇手游 / 网易大神扫码，过期后免扫"},
			{Kind: "password", Label: "账号密码登录", Hint: "网易账号或手机号 + 密码"},
			{Kind: "sms", Label: "手机验证码登录", Hint: "输入手机号收短信，不用记密码"},
		},
	})

	// 九游：手机号 + 密码是纯 Go 协议直连，可以直接在「添加账号」里用；
	// 另外保留「游戏内扫码」作为备选。
	if p, ok := channel.Lookup(channel.KeyUC); ok && len(p.Methods()) > 0 {
		methods := make([]loginMethod, 0, len(p.Methods()))
		for _, m := range p.Methods() {
			methods = append(methods, loginMethod{Kind: string(m.Kind()), Label: m.Label(), Hint: m.Hint()})
		}
		entries = append(entries, loginEntry{
			ID:      channel.KeyUC,
			Action:  channel.KeyUC,
			Label:   p.Label() + "账号",
			Hint:    methods[0].Hint,
			Initial: "九",
			// 有两种方式，先让用户选，不要点了就直接走第一个
			Group:   true,
			Methods: methods,
		})
	}

	return entries
}

// LoginChannels 给界面用的入口清单（包一层 map，避免前端依赖 Go 结构体字段名）。
func LoginChannels() map[string]any {
	return map[string]any{"entries": LoginEntries()}
}
