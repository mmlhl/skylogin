package mpayhost

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// 无头调用 mpay.dll 的**语义化入口**。
//
// 底层还是 ProbeUILess：一次调用 = 在一个 OS 线程上初始化 DLL、执行一个动作、
// 等回调，然后卸载。这里只是把"动作 + 参数含义"包成能直接读懂的函数，
// 不再要求调用方记住 ActionUrs / ActionToken 这些内部常量。
//
// 为什么不做成"初始化一次、随便调多次"的 Session：
//   DLL 的窗口、消息循环、接口实例必须绑在同一个 OS 线程上，而且登录成功后会
//   自己起一堆异步线程往本地库里写东西。常驻实例一旦被 GC 或换线程调用就是
//   崩溃或静默失败。所以每次动作都重新初始化，由调用方在**子进程**里跑，
//   崩了只崩子进程（app 层的 headless.go 就是这么用的）。
//
// 参数约定（都在 UILessOptions 上）：
//
//	SkyDir/SkinZip/GameID/LogPath/DataPath —— 加载哪份 DLL、用哪个身份初始化
//	UID    —— 账号（手机号 / 邮箱 / 玩家 id）
//	Token  —— LoginByPassword 时是**明文密码**（内部用 DLL 公钥 RSA 加密后再传）
//	         LoginByToken 时是已有的 MPay token 文本
//	Ticket —— LoginByTicket 的票
//	Device —— 先 SetDeviceInfo：换票/续期必须和 token 配套
//	Platform —— 登录成功后顺手写平台记录（ad / ios / hm）
//	WaitSec —— 等回调的秒数，默认 15

// LoginByPassword 用账号密码无头登录（UILessUrsLogin）。
//
// 拿到的凭证身份由 GameID 决定，跟有没有窗口无关：
//
//	GameID = …-g-ma75 → g_i=游戏身份，能按渠道签发（推荐）
//	GameID = …-g-a50  → g_i=平台身份，只能换票/换 sid
//
// 明文密码只在内存里，用 DLL 回调的 RSA 公钥加密后才交给 DLL。
func LoginByPassword(o UILessOptions) Result {
	o.Action = ActionUrs
	return ProbeUILess(o)
}

// LoginByToken 用已有的 uid + token 登录（UILessTokenLogin）。
//
// 不重新输密码，适合"凭证还没过期、只想让 DLL 认一下"的场景。
func LoginByToken(o UILessOptions) Result {
	o.Action = ActionToken
	return ProbeUILess(o)
}

// LoginByTicket 直接拿一张票登录（TicketLogin）。
func LoginByTicket(o UILessOptions) Result {
	o.Action = ActionTicket
	return ProbeUILess(o)
}

// SetPlatform 给"当前用户"写平台记录（ad / ios / hm）。
//
// 注意这是**平台侧记录**，游戏不读它——真正让游戏免选区的是 uni_sauth 里的
// platform 声明（见 internal/app/channelplatform.go）。留着它是因为平台自己
// 登录后用得到，不能当免选区的解法。
func SetPlatform(o UILessOptions) Result {
	o.Action = ActionSetPlatform
	return ProbeUILess(o)
}

// ListAccounts 读 DLL 自己记住的账号列表。
func ListAccounts(o UILessOptions) Result {
	o.Action = ActionList
	return ProbeUILess(o)
}

// TokenIdentity 从 Result 的槽位里取出登录身份，方便调用方判断这份凭证能干什么。
//
//	Platform=true → g_i 是平台应用（a50），只能换票/换 sid
//	Platform=false → g_i 是游戏（ma75），可以按渠道签发
type Identity struct {
	Token    string
	UserID   string
	DeviceID string
	GameID   string
	Platform bool // true = 平台身份凭证
}

// Identity 解析 Result 里的槽位（slot0=用户 id、slot1=设备号、slot2=token）。
func (r Result) Identity(hostedGameID string) Identity {
	id := Identity{DeviceID: r.DeviceID, GameID: r.GameID}
	if len(r.Slots) > 0 {
		id.UserID = r.Slots[0]
	}
	if len(r.Slots) > 1 {
		id.DeviceID = r.Slots[1]
	}
	if len(r.Slots) > 2 {
		id.Token = r.Slots[2]
	}
	if id.Token != "" {
		id.Platform = tokenOwnerIs(id.Token, hostedGameID)
	}
	return id
}

// tokenOwnerIs 判断这份 token 是不是 hostedGameID 那个应用签发的。
//
// token 是 "1-" + base64(JSON)，JSON 里的 g_i 就是签发应用的长 id
// （例如 aecglf6ee4aaaarz 对应 …-g-a50）。用前缀匹配是因为长 id 形如
// "aecglf6ee4aaaarz-g-a50"，而 g_i 只有前半段。
func tokenOwnerIs(token, hostedGameID string) bool {
	g := tokenOwner(token)
	if g == "" || hostedGameID == "" {
		return false
	}
	return strings.HasPrefix(hostedGameID, g)
}

// tokenOwner 取 token 里的 g_i（签发应用 id），取不到返回空串。
func tokenOwner(token string) string {
	raw := strings.TrimPrefix(token, "1-")
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		if b, err = base64.RawStdEncoding.DecodeString(raw); err != nil {
			if b, err = base64.StdEncoding.DecodeString(raw); err != nil {
				return ""
			}
		}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	g, _ := m["g_i"].(string)
	return g
}
