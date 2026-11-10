package app

// 渠道服扫码存账号。
//
// 场景：官方登录窗用游戏身份（game id = ma75）初始化时，二维码不只网易大神能扫，
// **渠道服客户端**（华为、vivo 这类）也能扫。扫完 DLL 回调里 slot7 就是渠道名，
// 后面带的东西分两种：
//
//	华为     slot8 → extra_unisdk_data → extra_data → sdk_info { openid, accessToken }
//	         → 拿 accessToken 走华为接口换 gameAuthSign（ts 只有 10 分钟，每次启动前重签）
//
//	vivo     slot8 → extra_unisdk_data → SAUTH_JSON / SAUTH_STR
//	         → 已经是拼好的渠道登录包，原样存下来交给 uni_sauth 就行
//
// 第二种是 2026-11-05 实测出来的：vivo 没有 sdk_info，但整包比 sdk_info 还全
// （sessionid、access_token、udid、sdk_version 都在里面）。所以这里按"有没有
// accessToken"分两条路，以后再来新渠道（小米/OPPO/B站…）也走整包那条，不用改代码。

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"skylogin/internal/channel"
	"skylogin/internal/device"
	"skylogin/internal/huawei"
	"skylogin/internal/mpayhost"
	"skylogin/internal/profile"
)

// saveChannelScannedAccount 把渠道服扫码的结果存成账号；返回展示用的名字。
//
// 渠道差异全部收在 scanChannels 注册表里（见 scanchen.go），这里只按
// Kind 分两条路：要再换一次的（华为 sdk_info）和直接能用的（整包）。
func saveChannelScannedAccount(res mpayhost.Result) (string, error) {
	if res.Channel == "" || res.Channel == "netease" {
		return "", fmt.Errorf("回调里没有渠道信息，没法当渠道账号保存")
	}
	ch, known := lookupScanChannel(res.Channel)
	if !known {
		return "", fmt.Errorf("暂不支持 %s 渠道的游戏内扫码", res.Channel)
	}

	// 华为要的是安卓设备画像（GetGameSession 按手机型号签名）；
	// 整包那条走的是 PC 客户端画像（和 B站/4399/vivo 网页登录同一份）。
	androidDev, aerr := device.LoadOrCreateAndroid(filepath.Join(dataDir(), "android.json"))
	if aerr != nil {
		return "", fmt.Errorf("准备设备信息失败: %w", aerr)
	}
	clientDev, cerr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if cerr != nil {
		return "", fmt.Errorf("准备设备标识失败: %w", cerr)
	}

	switch ch.Kind {
	case kindSDKInfo:
		if res.AccessToken == "" {
			return "", fmt.Errorf("%s 的回调里没有 accessToken，没法换登录凭证", ch.Label)
		}
		return saveSDKInfoAccount(res, ch, androidDev)
	case kindPackage:
		if res.UnisdkData == "" {
			return "", fmt.Errorf("%s 的回调里没有整包凭证", ch.Label)
		}
		return savePackageChannelAccount(res, ch, clientDev)
	}
	return "", fmt.Errorf("渠道 %s 的凭证形态未定义", ch.Label)
}

// saveSDKInfoAccount 华为这条路：sdk_info 的 accessToken → gameAuthSign。
func saveSDKInfoAccount(res mpayhost.Result, ch scanChannel, dev *device.AndroidProfile) (string, error) {
	sign, err := huawei.GetGameSession(res.AccessToken, profile.HuaweiAppID, dev.PhoneType, dev)
	if err != nil {
		return "", fmt.Errorf("用渠道凭证换 gameAuthSign 失败: %w", err)
	}
	playerID := toStr(sign["playerId"])
	gameAuthSign := toStr(sign["gameAuthSign"])
	ts := toStr(sign["ts"])
	if playerID == "" || gameAuthSign == "" {
		return "", fmt.Errorf("换回来的登录凭证不完整: %v", sign)
	}

	name := ch.Label + " " + tailOf(playerID, 8)
	acc := Account{
		// Channel 留空 = 华为（和网页扫码登录存出来的记录一致）
		Name:         name,
		NickName:     name,
		AccessToken:  res.AccessToken,
		PlayerID:     playerID,
		PlayerLevel:  toStr(sign["playerLevel"]),
		GameAuthSign: gameAuthSign,
		Ts:           ts,
		OpenID:       toStr(sign["openId"]),
		Source:       "channelscan",
		LoginMethod:  "channelscan",
		UpdatedAt:    time.Now().Unix(),
	}
	if acc.OpenID == "" {
		acc.OpenID = res.OpenID
	}
	if err := saveAccount(acc); err != nil {
		return "", fmt.Errorf("保存账号失败: %w", err)
	}
	logf("渠道服扫码已保存：%s（playerId=%s）", name, playerID)
	return name, nil
}

// savePackageChannelAccount vivo 这类：整包登录凭证。
func savePackageChannelAccount(res mpayhost.Result, ch scanChannel, dev *device.ClientProfile) (string, error) {
	pkg, err := parseChannelPackage(res.UnisdkData)
	if err != nil {
		return "", fmt.Errorf("%s 渠道的回调里没有可用的凭证: %w", ch.Label, err)
	}
	if pkg.UID == "" || pkg.Session == "" {
		return "", fmt.Errorf("%s 渠道的登录包不完整（uid / session 缺失）", ch.Label)
	}

	name := ch.Label + " " + tailOf(pkg.UID, 8) + " · 扫码"
	acc := Account{
		Name:     name,
		NickName: name,
		// 渠道名统一成账号文件里的 key（nearme_vivo → vivo），但**不复用**
		// 网页登录那一行：同一个玩家的两种登录方式各占一行，靠 RowKey 区分，
		// 否则两行 id 相同会互相覆盖、界面上点一个另一个跟着亮。
		Channel:      channelKeyOf(pkg.Channel),
		RowKey:       pkg.UID + " · 扫码",
		UserID:       pkg.UID,
		PlayerID:     pkg.UID,
		GameAuthSign: pkg.Session,
		AccessToken:  pkg.Access,
		UnisdkData:   res.UnisdkData,
		Source:       "channelscan",
		LoginMethod:  "channelscan",
		UpdatedAt:    time.Now().Unix(),
	}
	// 落库前先真发一次 uni_sauth：整包能不能换出登录态，只有试了才知道。
	// 失败就别存，免得列表里多一个点了就报错的账号。
	// 但网络不通时保留：凭证是刚扫出来的，重扫一遍也还是它。
	if _, _, verr := buildPayloadFn(acc, dev); verr != nil {
		if channel.IsNetwork(verr) {
			logf("%s 渠道凭证在线验证暂时失败（%v），保留本次扫码结果", ch.Label, verr)
		} else {
			return "", fmt.Errorf("%s 渠道凭证验证失败（%v）", ch.Label, verr)
		}
	}

	if err := saveScannedRow(acc, pkg.UID, pkg.Channel); err != nil {
		return "", fmt.Errorf("保存账号失败: %w", err)
	}
	logf("渠道服扫码已保存：%s（channel=%s uid=%s）", acc.Name, pkg.Channel, pkg.UID)
	return acc.Name, nil
}

// saveScannedRow 保存"扫码"这一行：只覆盖自己那一行，不动同玩家的其它行。
//
// 还要兼容老数据：早期版本没给扫码行加 RowKey，那种行（同 PlayerID、有整包、
// 没有 RowKey）就地升级成本行，不要另起一条。
func saveScannedRow(acc Account, playerID, rawChannel string) error {
	list, _ := loadAccounts()
	for i := range list {
		same := list[i].CopyKey == "" &&
			(list[i].RowKey == acc.RowKey ||
				(list[i].RowKey == "" && list[i].PlayerID == playerID && list[i].UnisdkData != ""))
		if !same {
			continue
		}
		acc = mergeCredMeta(acc, list[i])
		// 沿用原来的名字，但加个"· 扫码"后缀：同一玩家的网页登录行名字一样，
		// 不加就又在列表里分不清了。另外早期没做渠道名映射时存的是
		// "myapp B7A96EA3 · 扫码" 这种原始名，遇到就换成展示名。
		if !strings.HasPrefix(list[i].Name, rawChannel+" ") {
			acc.Name, acc.NickName = list[i].Name, list[i].NickName
		}
		if !strings.HasSuffix(acc.Name, " · 扫码") {
			acc.Name += " · 扫码"
			acc.NickName = acc.Name
		}
		list[i] = acc
		return saveList(list)
	}
	acc.fillMeta()
	return saveList(append(list, acc))
}

// tailOf 取 id 的末 n 位用于展示（完整 id 太长）。
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
