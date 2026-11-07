package phone

// 随机真机档案：机型库 + 设备标识生成，**与渠道无关**。
//
// 为什么单独拆出来：机型、系统版本、分辨率、android_id、OAID 这些东西
// 不归某个渠道所有——九游上报设备指纹要用它，华为的 deviceId/机型要用它，
// 以后接别的渠道也一样。渠道层只负责"怎么把这台手机翻译成对方要的格式"。
//
// 这个包只依赖标准库，不 import 任何内部包，所以谁都可以引。

import (
	"fmt"
	"math/rand"
	"time"
)

// Device 是一台"假想真机"的档案。
//
// 所有字段都在分配设备时一次性确定，之后不再变——真机就是这样：
// 一台手机买回来，型号、分辨率、android_id 都是固定的。
type Device struct {
	// 机型与系统（必须互相匹配，见 Catalog）
	Model     string `json:"model"`      // Build.MODEL，如 PJZ110
	Vendor    string `json:"vendor"`     // Build.BRAND，如 OnePlus
	OSRelease string `json:"os_release"` // 系统大版本，如 16
	APILevel  int    `json:"api_level"`  // 对应 API Level，如 36

	// 屏幕：长边与短边（横屏上报时 resX 是长边、resY 是短边）
	ScreenLong  int `json:"screen_long"`
	ScreenShort int `json:"screen_short"`

	// 设备标识
	AndroidID   string `json:"android_id"`   // 16 位 hex，等于 Build 里的 android_id
	InstallUUID string `json:"install_uuid"` // v4 UUID，安装级标识
	OAID        string `json:"oaid"`         // 64 位 hex；空表示这台机器取不到广告标识

	// InstalledAt 是"游戏装在这台设备上"的时间，毫秒。
	InstalledAt int64 `json:"installed_at"`

	// NoOAID 记录"这台机器本来就没有 OAID"，生成时就不再编一个。
	NoOAID bool `json:"no_oaid,omitempty"`
}

// Catalog 是机型库：真实在售/在用的机器，参数彼此匹配。
//
// 每一行都保证三件事：厂商与机型对得上、系统版本与 API Level 对得上、
// 分辨率是这台机器的真实值（长边在前）。加机型只改这张表，
// phone_test.go 会校验新加的行是否自洽。
var Catalog = []Device{
	// —— 一加 / OPPO ——
	{Model: "PJZ110", Vendor: "OnePlus", OSRelease: "16", APILevel: 36, ScreenLong: 3008, ScreenShort: 1440},
	{Model: "PJD110", Vendor: "OnePlus", OSRelease: "15", APILevel: 35, ScreenLong: 3168, ScreenShort: 1440},
	{Model: "PHK110", Vendor: "OnePlus", OSRelease: "14", APILevel: 34, ScreenLong: 2772, ScreenShort: 1240},
	{Model: "PHY110", Vendor: "OPPO", OSRelease: "14", APILevel: 34, ScreenLong: 3168, ScreenShort: 1440},
	{Model: "PGEM10", Vendor: "OPPO", OSRelease: "13", APILevel: 33, ScreenLong: 2772, ScreenShort: 1240},

	// —— 小米 / 红米 ——
	{Model: "23127PN0CC", Vendor: "Xiaomi", OSRelease: "14", APILevel: 34, ScreenLong: 2670, ScreenShort: 1200},
	{Model: "23116PN5BC", Vendor: "Xiaomi", OSRelease: "14", APILevel: 34, ScreenLong: 3200, ScreenShort: 1440},
	{Model: "2211133C", Vendor: "Xiaomi", OSRelease: "13", APILevel: 33, ScreenLong: 2400, ScreenShort: 1080},
	{Model: "2312DRA50C", Vendor: "Redmi", OSRelease: "14", APILevel: 34, ScreenLong: 2712, ScreenShort: 1220},
	{Model: "23013RK75C", Vendor: "Redmi", OSRelease: "13", APILevel: 33, ScreenLong: 3200, ScreenShort: 1440},
	{Model: "23049RAD8C", Vendor: "Redmi", OSRelease: "13", APILevel: 33, ScreenLong: 2400, ScreenShort: 1080, NoOAID: true},

	// —— 华为 / 荣耀 ——
	{Model: "ALN-AL00", Vendor: "HUAWEI", OSRelease: "14", APILevel: 34, ScreenLong: 2720, ScreenShort: 1260},
	{Model: "PGT-AN20", Vendor: "HUAWEI", OSRelease: "13", APILevel: 33, ScreenLong: 2700, ScreenShort: 1220},
	{Model: "NOH-AN00", Vendor: "HUAWEI", OSRelease: "12", APILevel: 31, ScreenLong: 2772, ScreenShort: 1344},
	{Model: "ELP-AN00", Vendor: "HUAWEI", OSRelease: "13", APILevel: 33, ScreenLong: 2412, ScreenShort: 1084, NoOAID: true},
	{Model: "RKY-AN10", Vendor: "HONOR", OSRelease: "14", APILevel: 34, ScreenLong: 2664, ScreenShort: 1200},
	{Model: "BVL-N49", Vendor: "HONOR", OSRelease: "13", APILevel: 33, ScreenLong: 2848, ScreenShort: 1260},

	// —— vivo / iQOO ——
	{Model: "V2324A", Vendor: "vivo", OSRelease: "14", APILevel: 34, ScreenLong: 2800, ScreenShort: 1260},
	{Model: "V2309A", Vendor: "vivo", OSRelease: "14", APILevel: 34, ScreenLong: 2800, ScreenShort: 1260},
	{Model: "V2227A", Vendor: "vivo", OSRelease: "13", APILevel: 33, ScreenLong: 3200, ScreenShort: 1440},
	{Model: "V2302A", Vendor: "iQOO", OSRelease: "13", APILevel: 33, ScreenLong: 3200, ScreenShort: 1440},
	{Model: "V2241A", Vendor: "iQOO", OSRelease: "13", APILevel: 33, ScreenLong: 2800, ScreenShort: 1260},

	// —— 三星 / realme / 魅族 ——
	{Model: "SM-S928B", Vendor: "samsung", OSRelease: "15", APILevel: 35, ScreenLong: 3120, ScreenShort: 1440},
	{Model: "SM-S918B", Vendor: "samsung", OSRelease: "13", APILevel: 33, ScreenLong: 3088, ScreenShort: 1440},
	{Model: "SM-S921B", Vendor: "samsung", OSRelease: "14", APILevel: 34, ScreenLong: 2340, ScreenShort: 1080},
	{Model: "RMX3820", Vendor: "realme", OSRelease: "13", APILevel: 33, ScreenLong: 2772, ScreenShort: 1240},
	{Model: "M2391", Vendor: "Meizu", OSRelease: "14", APILevel: 34, ScreenLong: 2400, ScreenShort: 1080},
}

// Random 从机型库里随机挑一台，并把设备标识一次性生成好。
//
// 调用方应当把结果按账号落盘复用：每次登录重新挑，等于每次都换手机。
func Random() Device {
	return New(Catalog[rand.Intn(len(Catalog))])
}

// RandomFrom 按条件挑一台（比如"只要华为系"）。
//
// 没有匹配项时退回整个库随机，避免调用方因为过滤条件写错而拿到零值。
func RandomFrom(match func(Device) bool) Device {
	var pool []Device
	for _, d := range Catalog {
		if match(d) {
			pool = append(pool, d)
		}
	}
	if len(pool) == 0 {
		return Random()
	}
	return New(pool[rand.Intn(len(pool))])
}

// New 在一条机型条目上补齐标识，得到一台完整的设备。
//
// 传进来的条目只用于确定机型参数；标识一律重新生成，不会被沿用。
func New(spec Device) Device {
	d := spec
	d.AndroidID = randomHexLower(16)
	d.InstallUUID = randomUUID()
	if !d.NoOAID {
		d.OAID = randomHexUpper(64)
	}
	d.InstalledAt = time.Now().Add(-time.Duration(2+rand.Intn(118)) * 24 * time.Hour).UnixMilli()
	return d
}

// Valid 判断一份档案是否完整、可用（落盘读回来时要用）。
//
// 长度也要校验：早期版本生成过 8 位 hex 的 android_id，那种档案要作废重发。
func (d Device) Valid() bool {
	return d.Model != "" && d.Vendor != "" && d.APILevel > 0 &&
		len(d.AndroidID) == 16 && d.InstallUUID != "" && d.OSRelease != ""
}

// Frame 返回形如「API Level-36 - OnePlus PJZ110-16」的描述串。
//
// 这是九游上报里的 fr 字段格式，其它渠道用不上可以不管；放这里是因为
// 它只依赖机型参数，属于"设备怎么描述自己"，不属于某个渠道的协议。
func (d Device) Frame() string {
	return fmt.Sprintf("API Level-%d - %s %s-%s", d.APILevel, d.Vendor, d.Model, d.OSRelease)
}

// —— 随机源 ——

const letterDigits = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randomAlnum(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterDigits[rand.Intn(len(letterDigits))]
	}
	return string(b)
}

// RandomAlnum 生成 n 位字母数字混合串（utdid 这类标识用得上）。
func RandomAlnum(n int) string { return randomAlnum(n) }

func randomHexLower(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[rand.Intn(16)]
	}
	return string(b)
}

func randomHexUpper(n int) string {
	const hexChars = "0123456789ABCDEF"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[rand.Intn(16)]
	}
	return string(b)
}

// randomUUID 生成 v4 UUID。
func randomUUID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
