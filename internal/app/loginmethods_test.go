package app

// 登录入口清单的护栏测试。
//
// 这份清单现在是界面的唯一来源：少一项，界面上就少一个入口，用户会直接发现
// "怎么没法加 B 站账号了"。所以把该有的项钉死在测试里。

import (
	"testing"

	"skylogin/internal/channel"
)

func findEntry(entries []loginEntry, id string) (loginEntry, bool) {
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return loginEntry{}, false
}

func TestLoginEntriesCoverAllAddOptions(t *testing.T) {
	entries := LoginEntries()

	// 添加账号时该看到的六个入口（id 就是渠道 Key，网页渠道三个 + 华为 + 扫码 + 网易）
	for _, id := range []string{channel.KeyBili, channel.KeyM4399, channel.KeyVivo, "huawei", "netease-channel", "netease"} {
		e, ok := findEntry(entries, id)
		if !ok {
			t.Fatalf("入口 %s 缺失", id)
		}
		if e.OnlyRefresh {
			t.Fatalf("入口 %s 不该标成只在刷新时出现", id)
		}
		if len(e.Methods) == 0 {
			t.Fatalf("入口 %s 没有声明任何登录方式", id)
		}
		if e.Label == "" || e.Initial == "" {
			t.Fatalf("入口 %s 缺少显示名或图标字", id)
		}
	}
}

func TestNeteaseIsGrouped(t *testing.T) {
	e, ok := findEntry(LoginEntries(), "netease")
	if !ok {
		t.Fatal("网易账号入口缺失")
	}
	if !e.Group {
		t.Fatal("网易账号应当是二级分组")
	}
	if len(e.Methods) < 2 {
		t.Fatalf("网易账号应当有扫码与账号密码等方式，实际 %d 种", len(e.Methods))
	}
}

// 网易账号的扫码必须走纯 Go 建码（kind=scan），不能退回官方登录窗。
//
// 原因：只有自己建码才拿得到 login_info.code，才能带平台会话去加载湾换票，
// 服务端才会把这个号写进记住槽。官方窗那条路 DLL 内部就换完票了，没有 code，
// 自动寄宿也就无从谈起。渠道服扫码（netease-channel）仍走官方窗，别一起改。
func TestNeteaseScanUsesGoFlow(t *testing.T) {
	entries := LoginEntries()

	netease, ok := findEntry(entries, "netease")
	if !ok {
		t.Fatal("网易账号入口缺失")
	}
	var scanKind string
	for _, m := range netease.Methods {
		if m.Kind == "scan" || m.Kind == "channelscan" {
			scanKind = m.Kind
		}
	}
	if scanKind != "scan" {
		t.Fatalf("网易账号扫码应走纯 Go 建码（kind=scan），实际是 %q", scanKind)
	}

	channelEntry, ok := findEntry(entries, "netease-channel")
	if !ok {
		t.Fatal("渠道服扫码入口缺失")
	}
	if len(channelEntry.Methods) == 0 || channelEntry.Methods[0].Kind != "channelscan" {
		t.Fatal("渠道服扫码必须保持官方登录窗（kind=channelscan）")
	}
}

func TestUCEntryOffersPassword(t *testing.T) {
	entries := LoginEntries()
	e, ok := findEntry(entries, "uc_platform")
	if !ok {
		t.Fatal("九游入口缺失")
	}
	// 九游现在有纯 Go 协议的「手机号 + 密码」，应该在「添加账号」里直接可选
	if e.OnlyRefresh {
		t.Fatal("九游已有账号密码登录，应当出现在添加账号列表里")
	}
	var hasPassword bool
	for _, m := range e.Methods {
		if m.Kind == "password" {
			hasPassword = true
		}
	}
	if !hasPassword {
		t.Fatal("九游必须声明 password 方式（手机号 + 密码）")
	}
}

func TestLoginMethodsWrapperShape(t *testing.T) {
	m := LoginChannels()
	if _, ok := m["entries"]; !ok {
		t.Fatal("LoginChannels 必须返回 entries 字段")
	}
}

// 网易账号的手机验证码登录必须出现在清单里。
//
// 后端 sms.go 早就通了（ma75 上下文走 get_sms / verify_sms / mobile/finish，
// 换出账号级凭证 sa=-99），前端 SMSLoginDialog 与 App.vue 的 case "sms" 也现成。
// 唯独清单漏了这一项，界面就少一个入口——而这类缺失不会有任何报错，只能靠测试钉住。
func TestNeteaseOffersSMS(t *testing.T) {
	e, ok := findEntry(LoginEntries(), "netease")
	if !ok {
		t.Fatal("网易账号入口缺失")
	}
	var hasSMS bool
	for _, m := range e.Methods {
		if m.Kind == "sms" {
			hasSMS = true
		}
	}
	if !hasSMS {
		t.Fatal("网易账号缺少手机验证码登录（kind=sms），界面上不会有这个入口")
	}
}
