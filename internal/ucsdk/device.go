package ucsdk

// 九游怎么把一台手机"翻译"成它要的上报格式。
//
// 机型、android_id、OAID 这些事实由 internal/phone 提供（与渠道无关）；
// 这里只负责九游专有的部分：
//
//   - ex 指纹串的字段顺序与拼接方式
//   - 五个摘要字段的算法（mikasa / sola / ackerman / kaisa / uya）
//   - utdid：阿里体系里的设备标识，九游上报要用，其它渠道未必需要
//
// 这些算法来自 SDK 里的 DeviceHashParam，并用抓包反推验证过：
// MD5("ffffffffffffffff") 正好等于抓包里的 mikasa 与 sola，
// MD5(抓包的 csid) 等于 kaisa，MD5(抓包的 utdid) 等于 uya。

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"

	"skylogin/internal/phone"
)

// Device 是设备档案的类型别名，方便调用方只引 ucsdk 一个包。
type Device = phone.Device

// 空值占位：SDK 读取 MAC / IMEI / IMSI 失败时用它顶上，再对整串取摘要。
//
// 这不是我们编的常量，是 SDK 自己的行为；md5("ffffffffffffffff")
// 就是抓包里 mikasa 与 sola 的值，所以它必须原样保留，不能"优化"成空串。
const absentPlaceholder = "ffffffffffffffff"

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

// utdidFor 给一台设备派生固定的 utdid（24 位字母数字）。
//
// 阿里系的 utdid 是设备级标识，一台机器一个、常年不变；这里没有真机可取，
// 就用设备自己的 android_id 摘要出来，保证"同一台设备永远同一个 utdid"。
// 直接随机也能用，但那样每次重装就换一个，反而不像真机。
func utdidFor(d phone.Device) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	sum := sha256.Sum256([]byte("utdid:" + d.AndroidID))
	b := make([]byte, 24)
	for i := range b {
		b[i] = alphabet[int(sum[i])%len(alphabet)]
	}
	return string(b)
}

// fingerprint 拼出客户端上报的 ex 串。
//
// crc 非空时加 werewolf 前缀（业务阶段），为空时保持引导阶段的形态。
// 两个阶段的差别只有三处：werewolf 前缀、bssid、ssid 与 oaId 的取值，
// 其余字段与顺序完全一致。
func fingerprint(d phone.Device, csid, crc string) string {
	biz := crc != ""
	bssid, ssid, oaID := "", "", ""
	if biz {
		// 业务阶段的 bssid 是一个本地管理位 MAC（02:..），真机在无权限时也报它；
		// ssid 只有拿不到时才会是 <unknown ssid>，与这里一致。
		bssid, ssid, oaID = "02:00:00:00:00:00", "<unknown ssid>", d.OAID
	}

	utdid := utdidFor(d)
	parts := []string{
		"orient:L",
		"sola:" + digest(""),
		"csid:" + csid,
		"netType:-2",
		"bssid:" + bssid,
		"utdid:" + utdid,
		"net_id:-1",
		"imsi:",
		"osId:" + d.InstallUUID,
		"resY:" + itoa(d.ScreenShort),
		"resX:" + itoa(d.ScreenLong),
		"ssid:" + ssid,
		"mac:",
		"kaisa:" + digest(csid),
		"ackerman:" + digest(d.AndroidID),
		"imei:",
		"mikasa:" + digest(""),
		"model:" + d.Model,
		"mobi:",
		"net:wifi",
		"uya:" + digest(utdid),
		"oaId:" + oaID,
	}
	body := strings.Join(parts, "|")
	if biz {
		return "werewolf:" + crc + "|" + body
	}
	return body
}

// itoa 是给分辨率用的最小整数转换，避免为一个数字引 strconv。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// newCSID 生成一次会话的 csid（v4 UUID 形态）。
//
// csid 是「客户端会话 ID」：每次登录重新生成，服务端据此把同一次会话里的
// 若干请求串起来。它不落盘，所以重启程序就换一个，和真机一致。
func newCSID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// RandomDevice 从机型库里随机挑一台（转发到 phone 包，保持老调用点可用）。
func RandomDevice() Device { return phone.Random() }
