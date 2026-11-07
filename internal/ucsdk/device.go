package ucsdk

// 九游上报的「设备环境」怎么造。
//
// 这里的取值不是随手编的，全部来自真机抓包 + 服务端行为验证：
//
//   - 机型、系统版本、分辨率、API Level 必须自洽（fr 串里的版本号和 apiLevel 对不上，
//     等于自己承认是拼出来的）
//   - 五个摘要字段的算法来自 SDK 里的 DeviceHashParam：
//     mikasa=MD5(MAC)、sola=MD5(IMEI)、ackerman=MD5(android_id)、kaisa=MD5(csid)、uya=MD5(utdid)
//     其中 MAC/IMEI 在 Android 10+ 读不到，SDK 会拿 "ffffffffffffffff" 占位再摘要，
//     所以 mikasa 与 sola 恒等，且全网设备一致——它们不携带任何身份信息
//   - 真正区分设备的是 android_id（进 ackerman）和 utdid（进 uya）
//
// 生命周期：
//
//	账号级  一台真机的全部参数，某个账号第一次登录时随机挑一台，之后一直用它
//	会话级  csid（客户端会话 ID），每次登录重新生成，kaisa 由它摘要而来
//	服务端  werewolf 前缀（getSecurityKey 下发）、si（si.apply 下发）
//
// 为什么要绑定到账号而不是绑定到本机：
// 同一个工具可能被用来登多个号，如果所有号共用一台"手机"，服务端看到的
// 就是一台设备反复切号——这比每台设备只登一个号可疑得多。

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// 空值占位：SDK 读取 MAC / IMEI / IMSI 失败时用它顶上，再对整串取摘要。
//
// 这不是我们编的常量，是 SDK 自己的行为；md5("ffffffffffffffff")
// 就是抓包里 mikasa 与 sola 的值，所以它必须原样保留，不能"优化"成空串。
const absentPlaceholder = "ffffffffffffffff"

// Device 是一台"假想真机"的完整档案。
//
// 字段都按真机语义命名，没有照抄任何实现的字段名。
type Device struct {
	// 机型与系统（必须互相匹配，见 catalog）
	Model     string `json:"model"`      // Build.MODEL，如 PJZ110
	Vendor    string `json:"vendor"`     // Build.BRAND，如 OnePlus
	OSRelease string `json:"os_release"` // 系统大版本，如 16
	APILevel  int    `json:"api_level"`  // 对应 API，如 36

	// 屏幕：横屏下的长边与短边（抓包里 resX 是长边、resY 是短边）
	ScreenLong  int `json:"screen_long"`
	ScreenShort int `json:"screen_short"`

	// 设备标识。真机上前两个是"这台设备"级别的，第三个是安装级，
	// 第四个是广告标识。这里统一在分配设备时生成，之后不再变。
	HardwareID string `json:"hardware_id"` // 16 位 hex，对应 android_id
	InstallID  string `json:"install_id"`  // UUID，对应上报里的 osId
	TraceID    string `json:"trace_id"`    // 24 位字母数字，对应 utdid
	AdvertID   string `json:"advert_id"`   // 64 位 hex，对应 oaId

	// InstalledAt 是"光遇装在这台设备上"的时间，毫秒。
	// 真机上它一旦确定就不再变，所以跟着设备档案走。
	InstalledAt int64 `json:"installed_at"`
}

// Model 目录：都是真实在售/在用的机型，参数彼此匹配。
//
// 为什么要列目录而不是纯随机拼：机型、系统版本、分辨率、API 之间是强关联的，
// 随便组合（比如 PJZ110 + Android 12）会立刻露馅。
var deviceCatalog = []Device{
	{Model: "PJZ110", Vendor: "OnePlus", OSRelease: "16", APILevel: 36, ScreenLong: 3008, ScreenShort: 1440},
	{Model: "SM-S928B", Vendor: "samsung", OSRelease: "15", APILevel: 35, ScreenLong: 3120, ScreenShort: 1440},
	{Model: "ALN-AL00", Vendor: "HUAWEI", OSRelease: "14", APILevel: 34, ScreenLong: 2720, ScreenShort: 1260},
	{Model: "23127PN0CC", Vendor: "Xiaomi", OSRelease: "14", APILevel: 34, ScreenLong: 2670, ScreenShort: 1200},
	{Model: "V2324A", Vendor: "vivo", OSRelease: "14", APILevel: 34, ScreenLong: 2800, ScreenShort: 1260},
	{Model: "PJD110", Vendor: "OnePlus", OSRelease: "15", APILevel: 35, ScreenLong: 3168, ScreenShort: 1440},
}

// RandomDevice 从目录里随机挑一台真机，并把设备级标识一次性生成好。
//
// 调用方应当把结果落盘并按账号复用；每次登录都重新挑等于每次都换手机。
func RandomDevice() Device {
	return newDeviceFrom(deviceCatalog[rand.Intn(len(deviceCatalog))])
}

// newDeviceFrom 在目录条目上补齐标识，得到一台完整的设备。
func newDeviceFrom(t Device) Device {
	d := t
	d.HardwareID = randomHexLower(8) // 16 位 hex，真机 android_id 就是 16 位
	d.InstallID = randomUUID()
	d.TraceID = randomAlnum(24)
	d.AdvertID = randomHexUpper(32) // 64 位 hex
	d.InstalledAt = time.Now().Add(-time.Duration(2+rand.Intn(118)) * 24 * time.Hour).UnixMilli()
	return d
}

// Valid 判断一份档案是否完整（落盘后读回来要能直接用）。
func (d Device) Valid() bool {
	return d.Model != "" && d.Vendor != "" && d.APILevel > 0 &&
		d.HardwareID != "" && d.InstallID != "" && d.TraceID != ""
}

// frame 拼出上报里的 fr 串，格式实测为「API Level-36 - OnePlus PJZ110-16」。
func (d Device) frame() string {
	return fmt.Sprintf("API Level-%d - %s %s-%s", d.APILevel, d.Vendor, d.Model, d.OSRelease)
}

// appVersion 是九游 SDK 的版本号，实测 9.8.12.0；换 SDK 版本时这里要跟着改。
const appVersion = "9.8.12.0"

// digest 是上报里五个摘要字段统一的算法：MD5 后转大写 hex。
//
// 传空值时必须先替换成 absentPlaceholder——SDK 就是这么干的，
// 所以 mikasa / sola 永远等于同一个值。
func digest(s string) string {
	if s == "" {
		s = absentPlaceholder
	}
	sum := md5.Sum([]byte(s))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// fingerprint 拼出客户端上报的 ex 串。
//
// crc 非空时加 werewolf 前缀（业务阶段），为空时保持引导阶段的形态。
// 两个阶段的差别只有三处：werewolf 前缀、bssid、ssid 与 oaId 的取值，
// 其余字段与顺序完全一致。
func (d Device) fingerprint(csid, crc string) string {
	biz := crc != ""
	bssid, ssid, oaID := "", "", ""
	if biz {
		// 业务阶段的 bssid 是一个本地管理位 MAC（02:..），真机在无权限时也报它；
		// ssid 只有拿不到时才会是 <unknown ssid>，与这里一致。
		bssid, ssid, oaID = "02:00:00:00:00:00", "<unknown ssid>", d.AdvertID
	}

	parts := []string{
		"orient:L",
		"sola:" + digest(""),
		"csid:" + csid,
		"netType:-2",
		"bssid:" + bssid,
		"utdid:" + d.TraceID,
		"net_id:-1",
		"imsi:",
		"osId:" + d.InstallID,
		fmt.Sprintf("resY:%d", d.ScreenShort),
		fmt.Sprintf("resX:%d", d.ScreenLong),
		"ssid:" + ssid,
		"mac:",
		"kaisa:" + digest(csid),
		"ackerman:" + digest(d.HardwareID),
		"imei:",
		"mikasa:" + digest(""),
		"model:" + d.Model,
		"mobi:",
		"net:wifi",
		"uya:" + digest(d.TraceID),
		"oaId:" + oaID,
	}
	body := strings.Join(parts, "|")
	if biz {
		return "werewolf:" + crc + "|" + body
	}
	return body
}

// —— 随机源 ——
//
// 这几个只用于生成设备标识，不需要密码学强度，但必须避免同一台机器
// 反复生成出同一个值，所以用 math/rand 的全局源（已按时间播种）。

const letterDigits = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randomAlnum(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterDigits[rand.Intn(len(letterDigits))]
	}
	return string(b)
}

func randomHexLower(nBytes int) string {
	b := make([]byte, nBytes)
	for i := range b {
		b[i] = "0123456789abcdef"[rand.Intn(16)]
	}
	return string(b)
}

func randomHexUpper(nBytes int) string {
	return strings.ToUpper(randomHexLower(nBytes))
}

// randomUUID 生成 v4 UUID，形态与真机上报的 csid / osId 一致。
func randomUUID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
