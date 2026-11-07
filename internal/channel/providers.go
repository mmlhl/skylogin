package channel

// 现有渠道到新契约的适配层。
//
// 这一层是"薄包装"：不搬任何逻辑，只把既有实现（InfoOf / LoginWindowOf /
// 各自的登录脚本）挂到 Provider 接口上。这样接口先立起来、行为一点不变，
// 后续再把某个渠道的流程真正迁进新框架时，其它渠道不受影响。

import (
	"context"

	"skylogin/internal/netsec"
)

// provider 通用实现：四个渠道的差异只在方法列表上。
type provider struct {
	key     string
	label   string
	methods []Method
}

func (p *provider) Key() string   { return p.key }
func (p *provider) Label() string { return p.label }

// Info 直接取既有的渠道身份参数，保证迁移期两套入口算出来的值完全一致。
// 注册表里只会放进已登记的渠道，所以这里拿不到值属于编码错误，直接 panic。
func (p *provider) Info() netsec.ChannelInfo {
	info, ok := InfoOf(p.key)
	if !ok {
		panic("channel: 渠道 " + p.key + " 没有登记 uni_sauth 参数")
	}
	return info
}

// Window 直接取既有的登录窗定义（标题、尺寸、注入脚本）。
func (p *provider) Window() LoginWindow { return LoginWindowOf(p.key) }

func (p *provider) Methods() []Method { return p.methods }

// webMethod 网页登录方式的占位实现。
//
// 真正的"起子进程开窗 → 轮询结果 → 落库"仍在 app/channellogin.go 里，
// 这里只负责对外声明"这个渠道有网页登录"，让界面能拿到方式清单。
type webMethod struct {
	label string
	hint  string
}

func (m webMethod) Kind() Kind    { return KindWeb }
func (m webMethod) Label() string { return m.label }
func (m webMethod) Hint() string  { return m.hint }

func (m webMethod) Available(context.Context) (bool, string) { return true, "" }

func (m webMethod) Login(context.Context, Progress) (Cred, error) {
	return Cred{}, ErrNotWired
}

// packageMethod 整包导入方式。
//
// 渠道服扫码拿到的 extra_unisdk_data 就是整包，落库走 app/channelaccount.go。
// 它没有"主动登录"的动作，所以 Login 不参与流程。
type packageMethod struct {
	label string
	hint  string
}

func (m packageMethod) Kind() Kind    { return KindPackage }
func (m packageMethod) Label() string { return m.label }
func (m packageMethod) Hint() string  { return m.hint }

func (m packageMethod) Available(context.Context) (bool, string) { return true, "" }

func (m packageMethod) Login(context.Context, Progress) (Cred, error) {
	return Cred{}, ErrNotWired
}

func init() {
	Register(&provider{
		key:   KeyBili,
		label: LabelOf(KeyBili),
		methods: []Method{webMethod{
			label: "B站账号",
			hint:  "在弹出的窗口里登录 B站账号",
		}},
	})
	Register(&provider{
		key:   KeyM4399,
		label: LabelOf(KeyM4399),
		methods: []Method{webMethod{
			label: "4399账号",
			hint:  "在弹出的窗口里登录 4399 账号",
		}},
	})
	Register(&provider{
		key:   KeyVivo,
		label: LabelOf(KeyVivo),
		methods: []Method{webMethod{
			label: "vivo账号",
			hint:  "在弹出的窗口里登录 vivo 账号",
		}},
	})
	Register(&provider{
		key:   KeyUC,
		label: LabelOf(KeyUC),
		methods: []Method{
			ucPassword{},
			packageMethod{
				label: "游戏内扫码",
				hint:  "打开游戏到「加好友」界面扫一次，凭证会写回这个账号",
			},
		},
	})
}
