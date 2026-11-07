package app

// 「添加账号」的登录入口清单。
//
// 以前这份清单硬编码在 web/src/components/ChannelPicker.vue 里：加一个渠道
// 要同时改 Go 和 Vue，前端还得知道"B站是网页登录、九游只能扫码"。
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
// Group 为 true 表示点进去还有二级（目前只有「网易账号」：扫码 / 账号密码）。
// OnlyRefresh 为 true 表示只在"刷新已有账号凭证"时出现，不进添加列表——
// 九游目前只有整包扫码一种方式，添加账号走"渠道服扫码"，不需要单独一行。
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

	// 网易账号：二级菜单，扫码 / 账号密码（官方 DLL 窗自带手机号登录）
	entries = append(entries, loginEntry{
		ID:      "netease",
		Action:  "netease",
		Label:   "网易账号",
		Hint:    "扫码 / 账号密码（含手机号）",
		Initial: "网",
		Group:   true,
		Methods: []loginMethod{
			{Kind: "channelscan", Label: "扫码登录", Hint: "用光遇手游 / 网易大神扫官方窗里的二维码"},
			{Kind: "password", Label: "账号密码登录", Hint: "网易官方登录窗：账号密码 / 手机号"},
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
