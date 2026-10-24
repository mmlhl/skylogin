package profile

// 光遇（网易 PC 版）的固定参数。数值来自实测与云端配置，勿随意改动。
const (
	GameName       = "光·遇"
	ShortGameID    = "ma75"
	LongGameID     = "aecfs5n2y4aaaakd-g-ma75" // MPay 使用的长 id
	FeverAppID     = "63"                      // 发烧平台的游戏编号
	SkyExeName     = "sky.exe"
	LauncherArg    = "--start_from_launcher=1"
	QrcodeLoginURL = "https://sky.163.com/yqlj"
	UniSdkBaseURL  = "https://mgbsdk.matrix.netease.com"
	MpayBaseURL    = "https://service.mkey.163.com"

	// 光遇专用 uni_sauth 签名密钥（来自 cloudRes.data[ma75].log_key）
	LogKey = "lM8jwFDFbrbKABNJr3IAKZHyavfDQfHu"

	// 华为渠道
	HuaweiPackage = "com.netease.sky.huawei"
	HuaweiAppID   = "100685413"
	HuaweiChannel = "huawei"

	// MPay 客户端自身的版本号（与被登录游戏无关）
	MpayGV  = "157"
	MpayGvn = "1.5.80"
	MpayCv  = "a1.5.0"

	// 注入到 create_login / query 等接口的客户端版本号
	MpayAppCv = "a5.10.0"

	// uni_sauth 里声明的 SDK 版本
	SdkVersion = "6.1.0.301"
)

// McountAppKey 是 MPay 统计接口用的 app key（设备登记等接口需要）。
const McountAppKey = "EEkEEXLymcNjM42yLY3Bn6AO15aGy4yq"

// DefaultAppChannel 是建码时使用的网易 PC SDK 通道。
const DefaultAppChannel = "a50_sdk_cn"

// HostedGameID 是发烧平台托管 MPay 时用的长 game id（第五人格 a50）。
// 手机扫码建码时要用这套参数，和"谁来确认"配套：
//   - 自己确认（华为）：用光遇自己的 game_id
//   - 手机确认（光遇手游/网易大神）：用发烧平台那套（本值 + dst_jf_game_id=ma75）
const HostedGameID = "aecglf6ee4aaaarz-g-a50"

// HostedCv 是托管建码时上报的客户端版本号。
const HostedCv = "c0.0.0"
