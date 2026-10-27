package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"skylogin/internal/fever"
	"skylogin/internal/mpay"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
	"skylogin/internal/win"
)

// 实测得到的"游戏自身 MPay"参数（ma75 自己的长 id，不是 a50 托管那套）
const (
	gameAppChannel = "a50_sdk_cn"
	gameAppCv      = "c4.20.0"
	gameGv         = "0.0.0.0"
	gameSv         = "10.0.19045"
	gameArch       = "win_x64"
)

// 设备身份的三种来源
const (
	modeFingerprint = "fingerprint" // 按本机硬件算出来（同机永远一样）
	modeRandom      = "random"      // 随机生成一个（换新身份）
	modeCustom      = "custom"      // 用户手动填的设备 id
)

var (
	deviceOnce sync.Once
	deviceID   string
	deviceErr  error

	// 登记设备要串行化：并发的两次 register 会各拿一个新设备号，
	// 而且短时间内多次申请会被服务器判定为"账号存在安全风险"。
	deviceMu sync.Mutex
)

// ensureDeviceID 返回可用的设备标识。
//
// MPay 只接受它自己发出去的设备号，而首次启动游戏时客户端会走一步
// POST /mpay/games/{game_id}/devices 登记一台新设备（请求里不带 device_id，
// 服务器回一个 id + key）。本地按同样的规则算一遍：
//
//   - unique_id 默认用"本机硬件指纹"算出来（同一台机器永远一样）
//   - 需要换身份时用随机 unique_id 重新登记
func ensureDeviceID() (string, error) {
	deviceOnce.Do(func() {
		c := loadConfig()

		// 老配置没有底账：如果当前号就是按本机申请的，补记一份，
		// 这样切到手动/随机后还能切回来。
		if c.FingerprintDeviceID == "" && c.MpayDeviceID != "" && c.DeviceUniqueID == win.DeviceFingerprint() {
			c.FingerprintDeviceID = c.MpayDeviceID
			c.FingerprintDeviceKey = c.MpayDeviceKey
			c.FingerprintUniqueID = c.DeviceUniqueID
			if err := saveConfig(c); err != nil {
				logf("补记按本机设备号失败: %v", err)
			}
		}

		if v := os.Getenv("SKYLOGIN_DEVICE_ID"); v != "" {
			deviceID = v
			return
		}
		if c.MpayDeviceID != "" {
			deviceID = c.MpayDeviceID
			return
		}

		id, err := registerDevice(&c, c.DeviceIDMode)
		if err != nil {
			logf("登记设备失败: %v", err)
			deviceErr = fmt.Errorf("设备登记失败（可能没联网），请在设置里重试或手动填写设备号: %w", err)
			return
		}
		deviceID = id
	})
	return deviceID, deviceErr
}

// ensureA50DeviceID 返回在 a50（发烧平台）上下文登记过的设备号。
//
// 为什么需要：设备号是按上下文发的——本机指纹登记的号属于 ma75（游戏），
// a50 不认识。签票走 a50 优先后（见 start.go mintTicket），拿着 ma75 的
// 设备号签 a50 的票，平台每张票都当成一台新设备，账号安全页里的
// "登录设备"就会一条条涨。用同一个 unique_id 在 a50 再登记一遍，
// 服务器就会把它认成同一台设备，之后长期复用。
func ensureA50DeviceID() (string, error) {
	if c := loadConfig(); c.A50DeviceID != "" {
		return c.A50DeviceID, nil
	}
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if c := loadConfig(); c.A50DeviceID != "" {
		return c.A50DeviceID, nil
	}

	c := loadConfig()
	unique := c.DeviceUniqueID
	if unique == "" {
		unique = win.DeviceFingerprint()
	}
	if unique == "" {
		unique = randomHex(16)
	}
	logf("正在登记 a50 设备（与 ma75 同一指纹）…")
	id, key, err := mpay.New().RegisterDevice(a50Long, a50Short, mpay.DeviceInfo{
		Name:       hostName(),
		Mac:        primaryMac(),
		Resolution: win.ScreenResolution(),
		UniqueID:   unique,
		ProcessID:  os.Getpid(),
	})
	if err != nil {
		return "", err
	}
	c.A50DeviceID = id
	c.A50DeviceKey = key
	if err := saveConfig(c); err != nil {
		logf("保存 a50 设备号失败: %v", err)
	}
	logf("已登记 a50 设备: %s", id)
	return id, nil
}

// registerDevice 用指定模式生成 unique_id 并注册，成功后写回 config。
func registerDevice(c *Config, mode string) (string, error) {
	if mode != modeRandom && mode != modeFingerprint {
		mode = modeFingerprint
	}

	unique := c.DeviceUniqueID
	if mode == modeRandom {
		unique = randomHex(16)
	} else if mode == modeFingerprint {
		unique = win.DeviceFingerprint()
	}
	if unique == "" {
		unique = randomHex(16)
		mode = modeRandom
	}

	logf("正在登记设备（身份来源: %s）…", mode)
	id, key, err := mpay.New().RegisterDevice(protocol.Get().LongGameID, profile.ShortGameID, mpay.DeviceInfo{
		Name:       hostName(),
		Mac:        primaryMac(),
		Resolution: win.ScreenResolution(),
		UniqueID:   unique,
		ProcessID:  os.Getpid(),
	})
	if err != nil {
		return "", err
	}

	c.MpayDeviceID = id
	c.MpayDeviceKey = key
	c.DeviceUniqueID = unique
	c.DeviceIDMode = mode

	// 按本机申请到的号额外记一份底账，切走再切回来时用它恢复
	if mode == modeFingerprint {
		c.FingerprintDeviceID = id
		c.FingerprintDeviceKey = key
		c.FingerprintUniqueID = unique
	}

	if err := saveConfig(*c); err != nil {
		logf("保存设备信息失败: %v", err)
	}

	deviceOnce = sync.Once{}
	deviceID = id
	deviceErr = nil
	logf("已登记设备: %s（%s）", id, mode)
	return id, nil
}

// reregisterDevice 供界面上的"重新登记"使用：丢掉旧号，按指定模式再登记一次。
func reregisterDevice(mode string) (string, error) {
	deviceMu.Lock()
	defer deviceMu.Unlock()

	c := loadConfig()

	// 「按本机」是幂等的：
	//   1) 当前号就是按本机指纹申请的 → 直接复用
	//   2) 之前按本机申请过、后来切到手动/随机 → 从底账里恢复，不再换号
	if mode == modeFingerprint || mode == "" {
		fp := win.DeviceFingerprint()

		if c.MpayDeviceID != "" && c.DeviceUniqueID == fp && (c.DeviceIDMode == modeFingerprint || c.DeviceIDMode == "") {
			logf("设备号已按本机指纹申请过，保持不变: %s", c.MpayDeviceID)
			return c.MpayDeviceID, nil
		}

		if c.FingerprintDeviceID != "" && c.FingerprintUniqueID == fp {
			c.MpayDeviceID = c.FingerprintDeviceID
			c.MpayDeviceKey = c.FingerprintDeviceKey
			c.DeviceUniqueID = fp
			c.DeviceIDMode = modeFingerprint
			if err := saveConfig(c); err != nil {
				return "", err
			}
			logf("已恢复按本机申请过的设备号: %s", c.MpayDeviceID)
			deviceID = c.MpayDeviceID
			return c.MpayDeviceID, nil
		}
	}

	c.MpayDeviceID = ""
	c.MpayDeviceKey = ""
	c.DeviceUniqueID = ""
	if err := saveConfig(c); err != nil {
		return "", err
	}
	deviceOnce = sync.Once{}
	deviceID = ""
	return registerDevice(&c, mode)
}

// deviceIDForUI 取当前标识（界面展示用，可能触发一次登记）。
func deviceIDForUI() string {
	id, _ := ensureDeviceID()
	return id
}

// deviceIDSource 告诉界面这个号是算出来的、随机的、手填的，还是内置兜底。
func deviceIDSource() string {
	if os.Getenv("SKYLOGIN_DEVICE_ID") != "" {
		return modeCustom
	}
	c := loadConfig()
	if c.MpayDeviceID != "" {
		if c.DeviceIDMode == "" {
			return modeFingerprint
		}
		return c.DeviceIDMode
	}
	return "none" // 还没登记（或登记失败），界面上按默认处理
}

// deviceInfoForUI 给设置面板展示设备身份的细节。
func deviceInfoForUI() map[string]any {
	c := loadConfig()
	return map[string]any{
		"fingerprint": win.DeviceFingerprint(),
		"uniqueId":    c.DeviceUniqueID,
		"mode":        c.DeviceIDMode,
		"hasKey":      c.MpayDeviceKey != "",
	}
}

// setCustomDeviceID 让用户手动指定设备号（高级用法）。
// setCustomDeviceID 手动指定设备号。
//
// 支持两种写法：
//
// \tamdemo0000000000-d                  只给设备号
// \tamdemo0000000000-d:627130666a75...  设备号 + key（从别的机器或备份里拿到的完整信息）
//
// key 目前只保存、不参与请求，但存下来以后要签名时能直接用上。
func setCustomDeviceID(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("设备标识不能为空")
	}

	id, key := raw, ""
	if i := strings.IndexAny(raw, ":："); i > 0 {
		id = strings.TrimSpace(raw[:i])
		key = strings.TrimSpace(raw[i+len(raw[i:i+1]):])
	}
	if id == "" {
		return fmt.Errorf("设备标识不能为空")
	}

	// 服务器只认自己发过的设备号（编造的建码会直接 400 / 1003），
	// 先探一次，避免用户填完以为生效、结果启动时才失败。
	if err := probeDeviceID(id); err != nil {
		return err
	}

	c := loadConfig()
	c.MpayDeviceID = id
	if key != "" {
		c.MpayDeviceKey = key
	}
	c.DeviceIDMode = modeCustom
	if err := saveConfig(c); err != nil {
		return err
	}
	deviceOnce = sync.Once{}
	deviceID = id
	return nil
}

func hostName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "DESKTOP-PC"
}

// primaryMac 返回第一块物理网卡的 MAC，格式 8C-32-23-14-06-2F。
func primaryMac() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "00-00-00-00-00-00"
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || len(ifc.HardwareAddr) < 6 {
			continue
		}
		return strings.ReplaceAll(strings.ToUpper(ifc.HardwareAddr.String()), ":", "-")
	}
	return "00-00-00-00-00-00"
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%032x", os.Getpid())
	}
	return hex.EncodeToString(b)
}

// ticketParams 返回跟随游戏更新自动变化的协议参数。
//
// cv 取游戏目录里 mpay.dll 的资源版本（实机 4.20.0.532 → c4.20.0），
// 游戏一更新就自动跟着变。gv 保持 0.0.0.0 —— 抓包里游戏自己发的就是这个值，
// 改成 DLL 版本反而会让票据不被接受。sv 取系统版本号。
func ticketParams() (cv, gv, sv string) {
	exe := resolveSkyPath()
	return fever.ClientCV(exe), gameGv, fever.SystemVersion()
}

// hostedTicket 是默认的取票实现：建码 → 扫码 → 确认 → 取票据。
type hostedTicket struct{}

func (hostedTicket) Login(payload map[string]string) (string, map[string]any, error) {
	devID, err := ensureDeviceID()
	if err != nil && devID == "" {
		return "", nil, err
	}
	cv, gv, sv := ticketParams()
	logf("协议参数: cv=%s gv=%s sv=%s", cv, gv, sv)
	c := mpay.New()
	return c.LoginGame(devID, payload, protocol.Get().LongGameID, profile.ShortGameID,
		protocol.Get().AppChannel, cv, gv, sv, gameArch)
}

// probeDeviceID 用一次建码请求验证设备号是否被服务器认可。
//
// 实测：服务器发过的号返回 200（带 uuid），编造的号返回 400 / 1003。
// 建码只是生成一个二维码会话，不会登记设备，也没有副作用。
func probeDeviceID(id string) error {
	_, data, err := mpay.New().CreateLoginHosted(id, profile.HostedGameID, profile.ShortGameID, profile.HuaweiChannel, profile.HostedCv)
	if err != nil {
		code := ""
		if c, ok := data["code"].(float64); ok {
			code = fmt.Sprint(int(c))
		}
		if code != "" {
			return fmt.Errorf("服务器不认这个设备号（%s）：只能填服务器发过的，随手编的不能用", code)
		}
		return fmt.Errorf("设备号校验失败: %w", err)
	}
	return nil
}
