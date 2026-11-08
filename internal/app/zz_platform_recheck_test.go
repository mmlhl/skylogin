package app

// 本机真实对照探针：不写账号库，不写寄宿槽；仅显式开启时使用本机凭据。
// 原始响应保存在被忽略的 .ai 运行目录，终端只输出阶段、等级和错误码。
import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

func TestZZPlatformRecheck(t *testing.T) {
	dir := os.Getenv("PLATFORM_RECHECK_DIR")
	if dir == "" {
		t.Skip("未开启平台身份完整链路复核")
	}
	if os.Getenv("URS_USER") == "" || os.Getenv("URS_PASS") == "" {
		t.Fatal("缺少本机登录凭据")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal("建立实验目录失败")
	}
	write := func(name string, v any) {
		t.Helper()
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal("编码记录失败")
		}
		if os.WriteFile(filepath.Join(dir, name+".json"), b, 0600) != nil {
			t.Fatal("保存记录失败")
		}
	}
	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatal("读取客户端配置失败")
	}
	summary := map[string]any{}
	defer func() { write("summary", summary) }()
	clean := func(err error, acc Account) string {
		if err == nil {
			return ""
		}
		s := err.Error()
		for _, secret := range []string{os.Getenv("URS_USER"), os.Getenv("URS_PASS"), acc.MpayToken, acc.UserID, acc.DeviceID} {
			if secret != "" {
				s = strings.ReplaceAll(s, secret, "[已隐藏]")
			}
		}
		return s
	}
	chain := func(label string, acc Account, platformDev string) string {
		result := map[string]any{"gi": tokenGI(acc.MpayToken), "sa": tokenSaValue(acc.MpayToken), "lt": tokenLoginUnix(acc.MpayToken)}
		summary[label] = result
		t.Logf("%s: g_i=%s sa=%.0f", label, tokenGI(acc.MpayToken), tokenSaValue(acc.MpayToken))
		c := mpay.New()
		ownLong, ownShort := ticketContextForToken(acc.MpayToken)
		if _, err := c.GetUserInfo(acc.MpayToken, acc.UserID, acc.DeviceID, ownLong, ownShort); err != nil {
			result["userinfo"] = clean(err, acc)
			t.Log(label + ": 配套资料校验失败")
			return ""
		}
		result["userinfo"] = "ok"
		platform := os.Getenv("RECHECK_PLATFORM")
		if platform == "" {
			platform = "ad"
		}
		sauth := netsec.NewSAUTH(acc.UserID, acc.MpayToken, ownShort, map[string]any{"login_channel": "netease", "app_channel": "netease.allysdk3rd", "source_app_channel": "netease.allysdk3rd", "platform": platform, "source_platform": platform, "sdk_version": "4.21.0", "deviceid": acc.DeviceID, "username": os.Getenv("URS_USER")}, dev)
		auth, err := netsec.PostSigned(sauth, ownShort)
		write(label+"-sauth", map[string]any{"request": sauth, "response": auth})
		if err != nil {
			result["sauth_error"] = clean(err, acc)
			t.Log(label + ": uni_sauth 请求失败")
			return ""
		}
		result["sauth_code"] = auth["code"]
		t.Logf("%s: uni_sauth code=%v", label, auth["code"])
		if toStr(auth["code"]) != "200" {
			return ""
		}
		lp, err := netsec.NewLoginPayloadFromSAUTH(auth, sauth, acc.MpayToken, "0", strconv.FormatInt(time.Now().Unix(), 10), dev, netsec.NeteaseChannelInfo())
		if err != nil {
			result["payload"] = "failed"
			return ""
		}
		lp.Body["token"] = acc.MpayToken
		lp.Body["device_id"] = acc.DeviceID
		c.AppChannel = "netease"
		n := 0
		c.ProbeTrace = func(method, path string, request url.Values, response map[string]any) {
			n++
			write(fmt.Sprintf("%s-trace-%d", label, n), map[string]any{"method": method, "path": path, "request": request, "response": response})
			t.Logf("%s: %s response code=%v", label, path, response["code"])
		}
		uuid, _, err := c.CreateLoginHostedFull(platformDev, profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
		if err != nil {
			result["create_login"] = clean(err, acc)
			return ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		code, err := c.CompleteHostedQRCode(ctx, uuid, profile.HostedGameID, lp.Body)
		if err != nil {
			result["authorize_error"] = clean(err, acc)
			t.Logf("%s: 授权失败 %s", label, clean(err, acc))
			return ""
		}
		result["authorize"] = "ok"
		out, err := launcher.ExchangeTicket(code, platformDev)
		if err != nil || out == nil || out.Ticket == "" {
			result["exchange"] = "failed"
			return ""
		}
		result["exchange"] = "ok"
		write(label+"-ticket", out)
		t.Log(label + ": 完整授权与匿名换票成功")
		if os.Getenv("RECHECK_LAUNCH") == label {
			launchHeadlessProbeGame(t, out.Channel, out.Ticket)
		}
		return out.Ticket
	}
	// 先跑平台登录与派生链，后跑游戏登录，避免后一次密码登录先顶掉前一次会话。
	for _, kind := range []string{"a50", "ma75"} {
		if only := os.Getenv("RECHECK_ONLY"); only != "" && only != kind {
			continue
		}
		game := profile.HostedGameID
		if kind == "ma75" {
			game = profile.LongGameID
		}
		res, err := HeadlessPasswordLogin(os.Getenv("URS_USER"), os.Getenv("URS_PASS"), game)
		if err != nil {
			summary[kind+"-login"] = "failed"
			t.Log(kind + ": 无头登录失败（详情仅留本地）")
			write(kind+"-login-error", map[string]any{"error": err.Error()})
			continue
		}
		write(kind+"-login", res)
		acc := Account{Channel: "netease", UserID: res.UserID, PlayerID: res.UserID, Name: "复核账号", MpayToken: res.Token, DeviceID: res.DeviceID, Region: "android"}
		platformDev := res.DeviceID
		if kind == "ma75" {
			platformDev, err = ensureA50DeviceID()
			if err != nil {
				t.Fatal("平台设备不可用")
			}
		}
		chain(kind+"-native", acc, platformDev)
		// 使用每份新鲜原生凭证签票，在 ma75 上下文兑换；回读 user.id，不沿用未知账号。
		ownLong, ownShort := ticketContextForToken(res.Token)
		ticket, _, err := mpay.New().CreateTicketFor(res.Token, res.UserID, res.DeviceID, ownLong, ownShort)
		if err != nil {
			summary[kind+"-derive"] = "create_ticket failed"
			continue
		}
		derivedDevice := res.DeviceID
		if kind == "a50" && os.Getenv("RECHECK_GAME_DEVICE") == "1" {
			derivedDevice, err = ensureDeviceID()
			if err != nil {
				t.Fatal("游戏登记设备不可用")
			}
		}
		out, err := mpay.New().LoginTicket(ticket, derivedDevice, profile.LongGameID, profile.ShortGameID)
		if err != nil {
			summary[kind+"-derive"] = "login_ticket failed"
			continue
		}
		write(kind+"-derived", out)
		user, _ := out["user"].(map[string]any)
		token, _ := user["token"].(string)
		uid, _ := user["id"].(string)
		if token == "" || uid == "" {
			summary[kind+"-derive"] = "incomplete"
			continue
		}
		if uid != res.UserID {
			t.Fatal("派生凭证账号不一致")
		}
		sess := acc
		sess.MpayToken = token
		sess.UserID = uid
		sess.DeviceID = derivedDevice
		fields := tokenFields(token)
		result := map[string]any{"same_user": true, "odi_matches_original": fields["odi"] == derivedDevice, "od_matches_original": fields["od"] == derivedDevice, "exchange_device_is_original": derivedDevice == res.DeviceID}
		summary[kind+"-binding"] = result
		chain(kind+"-derived", sess, platformDev)
		_, verifyErr := mpay.New().GetUserInfo(sess.MpayToken, sess.UserID, sess.DeviceID, profile.LongGameID, profile.ShortGameID)
		summary[kind+"-derived-userinfo-after"] = verifyErr == nil
		// 原始凭证同参数阳性对照：排除兑换后原凭证或设备已失效。
		chain(kind+"-native-after", acc, platformDev)
	}
}
