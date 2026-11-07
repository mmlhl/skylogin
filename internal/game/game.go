package game

// 游戏档案：把"某个游戏"的全部固定参数收在一处。
//
// 现在的 internal/profile 是一整包常量，写死了光遇（ma75）。接第二款游戏时
// 那份常量无法复用，所以这里先立一个结构化的档案模型：
//
//	游戏自己的一份参数        ID / 长 id / log_key / exe / 启动参数
//	游戏 × 渠道的交叉参数     每个游戏在每个渠道的包名、appid、sdk 版本都不同
//
// 交叉参数为什么必须按"游戏 × 渠道"存：九游的 app_id 在光遇是 918836，
// 在别的游戏是 800051；华为的 cp_id / api_key 每个游戏一套。
// 这些值在云端渠道表里有全量，不能抄成常量。
//
// 迁移期约定：本包先只提供数据与查询，不接管 profile 包。
// 两者的一致性由 game_test.go 守着，防止数据漂移。

// ChannelParams 是某个游戏在某个渠道下的接入参数。
type ChannelParams struct {
	// Package 是该游戏在该渠道的安装包名（android 侧习惯），用于渠道侧识别。
	Package string
	// AppID / SDKVer 是渠道 SDK 的标识与版本，参与渠道请求与 uni_sauth。
	AppID  string
	SDKVer string
	// Extra 放各渠道特有的零散参数（bili_game_id、cp_id、product_id 等）。
	Extra map[string]string
}

// Profile 一款游戏的档案。
type Profile struct {
	// ID 是短 game id（ma75），LongID 是 MPay 用的长 id。
	ID     string
	Name   string
	LongID string

	// LogKey 是 uni_sauth 的签名密钥，按游戏不同。
	LogKey string

	// Exe 与 LauncherArg 描述怎么把游戏拉起来。
	Exe         string
	LauncherArg string

	// SdkVersion 是 uni_sauth 里声明的 SDK 版本。
	SdkVersion string

	// Channels 按渠道 Key 索引该游戏的渠道参数。
	Channels map[string]ChannelParams
}

// Channel 取某渠道在本游戏下的参数；没有配置时返回零值与 false。
func (p *Profile) Channel(key string) (ChannelParams, bool) {
	c, ok := p.Channels[key]
	return c, ok
}

// 当前生效的档案。目前只有光遇一款，后续按启动参数或配置切换。
var current = Sky()

// Current 返回当前游戏档案。
func Current() *Profile { return current }

// Sky 光遇的档案。数值来自实测与云端渠道表，勿随意改动。
func Sky() *Profile {
	return &Profile{
		ID:          "ma75",
		Name:        "光·遇",
		LongID:      "aecfs5n2y4aaaakd-g-ma75",
		LogKey:      "lM8jwFDFbrbKABNJr3IAKZHyavfDQfHu",
		Exe:         "sky.exe",
		LauncherArg: "--start_from_launcher=1",
		SdkVersion:  "6.1.0.301",
		Channels: map[string]ChannelParams{
			// 华为：cp_id / client_id / api_key 等长参数放 Extra，
			// 迁移时从云端渠道表读取，不在这里堆。
			"huawei":      {Package: "com.netease.sky.huawei", AppID: "100685413", SDKVer: "6.1.0.301"},
			"nearme_vivo": {Package: "com.netease.sky.vivo", SDKVer: "4.7.2.0"},
			"bilibili_sdk": {
				Package: "com.netease.sky",
				SDKVer:  "5.9.6",
				Extra:   map[string]string{"bili_game_id": "833", "app_key": "h9Ejat5tFh81cq8"},
			},
			// 九游：app_id 是"游戏 × 渠道"差异最典型的一处
			"uc_platform": {Package: "com.netease.sky.aligames", AppID: "918836", SDKVer: "9.7.12.0"},
		},
	}
}
