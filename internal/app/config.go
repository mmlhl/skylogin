package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config 是用户的本地设置。
type Config struct {
	SkyExe         string `json:"sky_exe"`                    // 手动指定光遇主程序路径
	MpayDeviceID   string `json:"mpay_device_id"`             // 本机在 MPay 登记的设备 id
	MpayDeviceKey  string `json:"mpay_device_key,omitempty"`  // 登记时服务器给的 key
	DeviceUniqueID string `json:"device_unique_id,omitempty"` // 登记用的唯一串（随机模式下保存）
	DeviceIDMode   string `json:"device_id_mode,omitempty"`   // fingerprint | random | custom

	// 按本机指纹申请过的那一个设备号，单独留一份底账。
	// 用户中途切到「手动/重新申请」再切回「按本机」时，直接恢复这个号，
	// 而不是又去服务器换一个新的。
	FingerprintDeviceID  string `json:"fingerprint_device_id,omitempty"`
	FingerprintDeviceKey string `json:"fingerprint_device_key,omitempty"`
	FingerprintUniqueID  string `json:"fingerprint_unique_id,omitempty"`
	CloseLauncher        *bool  `json:"close_launcher,omitempty"` // 启动前是否关闭发烧平台，默认 true

	// 多开镜像目录（第 2 个客户端从这里启动）。空 = 游戏所在盘的 \SkyLoginMirror。
	MirrorDir string `json:"mirror_dir,omitempty"`

	// 左栏宽度（像素）。用户能拖，拖完存这里；0 表示用默认值。
	RailWidth int `json:"rail_width,omitempty"`

	// 界面壳偏好：0=账号库 1=此刻 2=控制台 3=晨白（见前端 shellkit）。
	// WebView2 每次端口随机，localStorage 记不住，所以存后端。
	UiShell int `json:"ui_shell,omitempty"`

	// 多开默认设置：启动前写进每个客户端的 preferences.sav（internal/prefs）。
	// 画质用游戏认识的名字（实测见过 LowPower / Presentation），空 = 跟随游戏；
	// 帧率 0 = 跟随游戏。窗口模式恒为开（排列的前提）。
	MultiQualityMode string `json:"multi_quality_mode,omitempty"`
	MultiQualityFps  int    `json:"multi_quality_fps,omitempty"`

	// 鼠标仲裁：多开启动后的一段时间里，防止后台客户端自认持有鼠标
	// （进大厅后抢焦点、持续把光标拉回窗口中心）。分钟数 0 = 用默认 3。
	MouseArbiterEnabled bool `json:"mouse_arbiter_enabled,omitempty"`
	MouseArbiterMin     int  `json:"mouse_arbiter_min,omitempty"`

	// a50（发烧平台）上下文的设备号：签票走 a50 优先时用它。
	// MPay 的设备号按上下文发：本机指纹登记的号属于 ma75，a50 不认识，
	// 拿去签 a50 的票每张都会被当成新设备（登录设备列表一条条涨）。
	// 用同一个 unique_id 在 a50 再登记一遍即可长期复用。
	A50DeviceID  string `json:"a50_device_id,omitempty"`
	A50DeviceKey string `json:"a50_device_key,omitempty"`
}

// CloseLauncherEnabled 返回设置值，未设置时默认开启。
func (c Config) CloseLauncherEnabled() bool {
	if c.CloseLauncher == nil {
		return true
	}
	return *c.CloseLauncher
}

func configPath() string { return filepath.Join(dataDir(), "config.json") }

func loadConfig() Config {
	var c Config
	b, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveConfig(c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), b, 0o600)
}

// 左栏宽度的允许范围与默认值。
const (
	RailWidthDefault = 288
	RailWidthMin     = 220
	RailWidthMax     = 520
)

// saveRailWidth 保存用户拖出来的左栏宽度（越界会被夹回范围内）。
func saveRailWidth(w int) error {
	if w < RailWidthMin {
		w = RailWidthMin
	}
	if w > RailWidthMax {
		w = RailWidthMax
	}
	c := loadConfig()
	c.RailWidth = w
	return saveConfig(c)
}

// saveShell 保存界面壳偏好（越界回 0）。
func saveShell(shell int) error {
	if shell < 0 || shell > 9 {
		shell = 0
	}
	c := loadConfig()
	c.UiShell = shell
	return saveConfig(c)
}
