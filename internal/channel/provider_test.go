package channel

// 契约层的护栏测试。
//
// 阶段迁移期最怕的是"新接口和老实现算出来的东西不一样"，所以这里逐项对账：
// Provider 报的每个值都必须和既有的 InfoOf / LabelOf / LoginWindowOf 完全一致。

import (
	"testing"

	"skylogin/internal/netsec"
)

func mustInfo(t *testing.T, key string) netsec.ChannelInfo {
	t.Helper()
	info, ok := InfoOf(key)
	if !ok {
		t.Fatalf("%s 应当登记了 uni_sauth 参数", key)
	}
	return info
}

func TestRegistryCoversKnownChannels(t *testing.T) {
	for _, key := range []string{KeyBili, KeyM4399, KeyVivo, KeyUC} {
		if !Known(key) {
			t.Fatalf("%s 应当是已知渠道", key)
		}
		p, ok := Lookup(key)
		if !ok {
			t.Fatalf("渠道 %s 没有注册 Provider", key)
		}
		if p.Key() != key {
			t.Fatalf("Provider.Key() = %s，期望 %s", p.Key(), key)
		}
		if p.Label() != LabelOf(key) {
			t.Fatalf("%s 的 Label = %q，期望 %q", key, p.Label(), LabelOf(key))
		}
		if p.Info() != mustInfo(t, key) {
			t.Fatalf("%s 的 Info 与 InfoOf 不一致", key)
		}
		if p.Window().Title != LoginWindowOf(key).Title {
			t.Fatalf("%s 的登录窗标题与 LoginWindowOf 不一致", key)
		}
		if len(p.Methods()) == 0 {
			t.Fatalf("%s 没有声明任何登录方式", key)
		}
	}
}

func TestMethodKindsAreKnown(t *testing.T) {
	valid := map[Kind]bool{
		KindWeb: true, KindSMS: true, KindQR: true,
		KindSilent: true, KindPassword: true, KindPackage: true,
	}
	for _, p := range All() {
		for _, m := range p.Methods() {
			if !valid[m.Kind()] {
				t.Fatalf("%s 声明了未知的登录方式 %q", p.Key(), m.Kind())
			}
			if m.Label() == "" {
				t.Fatalf("%s 的方式 %q 缺少显示名", p.Key(), m.Kind())
			}
			if ok, _ := m.Available(nil); !ok {
				t.Fatalf("%s 的方式 %q 在无参数环境下应默认可用", p.Key(), m.Kind())
			}
		}
	}
}

func TestUnregisteredChannelIsNotKnown(t *testing.T) {
	if _, ok := Lookup("nonexistent"); ok {
		t.Fatal("未注册的渠道不应查到 Provider")
	}
	// 未知渠道必须明确返回 false，不能静默回落成华为
	if info, ok := InfoOf("nonexistent"); ok {
		t.Fatalf("未知渠道不应返回渠道参数，实际拿到 %+v", info)
	}
}
