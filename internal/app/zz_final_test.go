package app

// 自寄宿实验：默认跳过，显式开启后才访问服务端。
// SELFHOST_FINAL=1 PROBE_ACCOUNT=<账号键> PROBE_SIGN_KEY=<已确认的a50密钥>
// PROBE_PLATFORM=ad|ios|hm|pc；平台仅作为实验变量，不代表服务端一定接受。
// 若槽中已有其它账号，需显式设置 PROBE_ALLOW_REPLACE=1 才允许覆盖。

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
)

func TestZZFinal(t *testing.T) {
	if os.Getenv("SELFHOST_FINAL") != "1" {
		t.Skip("未开启 a50 自寄宿实验")
	}
	runSelfHostProbe(t, true)
}

// runSelfHostProbe 为两个实验入口提供同一条完整链，避免各自漏掉步骤。
// 探针不保存返回的凭证，不启动游戏，也不把未通过的阶段报成成功。
func runSelfHostProbe(t *testing.T, useA50 bool) {
	t.Helper()
	key := os.Getenv("PROBE_ACCOUNT")
	if key == "" {
		t.Fatal("必须设置 PROBE_ACCOUNT，不能默认选择某个真实账号")
	}
	platform := os.Getenv("PROBE_PLATFORM")
	if platform == "" {
		t.Fatal("必须显式设置 PROBE_PLATFORM（ad/ios/hm/pc）以记录实验变量")
	}
	if platform != "ad" && platform != "ios" && platform != "hm" && platform != "pc" {
		t.Fatal("PROBE_PLATFORM 不合法")
	}
	signingKey := os.Getenv("PROBE_SIGN_KEY")
	if useA50 && signingKey == "" {
		t.Fatal("缺少已确认的 a50 签名密钥 PROBE_SIGN_KEY；不再默认使用 ma75 密钥")
	}
	if !useA50 && signingKey == "" {
		signingKey = protocol.Get().LogKey
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal("读取账号列表失败")
	}
	acc, ok := findAccount(list, key)
	if !ok || acc.Channel != "netease" {
		t.Fatal("找不到指定网易账号")
	}
	token, devID := acc.MpayToken, acc.DeviceID
	authGame, authLong := profile.ShortGameID, profile.LongGameID
	if useA50 {
		token, devID = acc.A50Token, acc.A50DeviceID
		authGame, authLong = "a50", profile.HostedGameID
	}
	if token == "" || devID == "" {
		t.Fatal("所选应用上下文缺少配套 token/device；不会跨应用猜测或覆盖凭证")
	}
	if tokenGI(token) != authLong[:len(authLong)-len("-g-"+authGame)] {
		t.Fatal("凭证签发应用与实验上下文不一致")
	}

	host := acc
	if hostKey := os.Getenv("PROBE_HOST_ACCOUNT"); hostKey != "" {
		var found bool
		host, found = findAccount(list, hostKey)
		if !found || host.Channel != "netease" {
			t.Fatal("找不到指定宿主账号")
		}
	}
	hostToken, hostDevice := host.A50Token, host.A50DeviceID
	if hostToken == "" {
		hostToken, hostDevice = host.MpayToken, host.DeviceID
	}
	if hostToken == "" || hostDevice == "" || tokenGI(hostToken) != "aecglf6ee4aaaarz" {
		t.Fatal("宿主缺少 a50 上下文的配套凭证；不尝试用 ma75 凭证冒充平台态")
	}
	t.Logf("实验变量：授权应用=%s，声明平台=%s，目标与宿主相同=%v", authGame, platform, host.UserID == acc.UserID)
	sid, err := launcher.Login(host.UserID, hostToken, hostDevice)
	if err != nil || sid == "" {
		t.Fatal("阶段1：宿主平台登录失败；未取得 sid")
	}
	before, err := launcher.SavedAccount(sid)
	if err != nil {
		t.Fatal("阶段2：回读槽位失败")
	}
	if before != "" && before != acc.UserID && os.Getenv("PROBE_ALLOW_REPLACE") != "1" {
		t.Fatal("阶段2：槽中已有其它账号；未设置 PROBE_ALLOW_REPLACE=1，不执行覆盖授权")
	}
	t.Logf("阶段2：槽位检查通过，原槽是否为目标=%v", before == acc.UserID)

	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatal("读取授权设备配置失败")
	}
	custom := map[string]any{
		"login_channel": "netease", "app_channel": "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd", "platform": platform,
		"source_platform": platform, "sdk_version": "4.21.0",
		"deviceid": devID, "username": acc.Name,
	}
	sauth := netsec.NewSAUTH(acc.UserID, token, authGame, custom, dev)
	authResp, err := netsec.PostSignedWithKey(sauth, authGame, signingKey)
	if err != nil {
		t.Fatal("阶段3：uni_sauth 请求失败")
	}
	if code := toStr(authResp["code"]); code != "200" {
		t.Fatalf("阶段3：uni_sauth 被拒绝，code=%s；尚未验证后续授权，不能据此判定只缺密钥", code)
	}
	lp, err := netsec.NewLoginPayloadFromSAUTH(authResp, sauth, token, "0", strconv.FormatInt(time.Now().Unix(), 10), dev, netsec.NeteaseChannelInfo())
	if err != nil {
		t.Fatal("阶段4：授权组包失败")
	}
	c := mpay.New()
	c.AppChannel = "netease"
	uuid, _, err := c.CreateLoginHostedFull(hostDevice, profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
	if err != nil {
		t.Fatal("阶段5：托管建码失败")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	code, err := c.CompleteHostedQRCode(ctx, uuid, profile.HostedGameID, lp.Body)
	if err != nil {
		t.Fatalf("阶段6：%v", err)
	}
	t.Log("阶段6：已从轮询结果取得授权码（不记录授权码正文）")
	r, err := launcher.ExchangeTicketWithSid(code, hostDevice, sid)
	if err != nil || r == nil || r.Ticket == "" {
		t.Fatal("阶段7：授权码换槽位票失败")
	}
	after, err := launcher.SavedAccount(sid)
	if err != nil || after != acc.UserID {
		t.Fatal("阶段8：换票后槽位回读未匹配目标账号，不能宣称写槽成功")
	}
	r, err = launcher.TicketForAccount(acc.UserID, hostDevice, sid)
	if err != nil || r == nil || r.Ticket == "" {
		t.Fatal("阶段9：回读虽匹配，但按账号再次出票失败")
	}
	t.Log("阶段9：槽位回读和再次出票均成功；仍需游戏实测验证是否记住所选平台")
}
