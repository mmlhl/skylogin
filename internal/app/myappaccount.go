package app

// 把应用宝（YSDK）微信登录拿到的凭证拼成"网易认的整包"，再走一次 uni_sauth。
//
// 为什么能这么拼
//
//	之前用游戏码 + 应用宝 App 扫码时，DLL 回调里给的就是同一套东西：
//	  extra_data = {login_type:7, session_id:"openid", session_type:"kp_actoken",
//	                openid, openkey, pf, pfkey, zoneid:"1"}
//	其中 openkey 就是 YSDK 的 atk。所以自己登录拿到的 openid + atk + pf + pfKey
//	可以原样填进同一个结构，剩下的字段（gameid / login_channel / sdk_version…）
//	照抄扫码那份。
//
// 唯一不确定的是 SAUTH 顶层的 sessionid：扫码那次是 SDK 自己生成的 32 位十六进制。
// 这里先用 atk 顶上（服务端要求非空），跑一遍看能不能过。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/profile"
)

// myappCred 是 YSDK 校验接口返回的凭证。
type myappCred struct {
	OpenID string
	Atk    string
	Pf     string
	PfKey  string
	RTK    string
}

// credFromVerify 从 wx_verify_code 的响应里取凭证。
func credFromVerify(out map[string]any) (myappCred, error) {
	c := myappCred{
		OpenID: toStr(out["openid"]),
		Atk:    toStr(out["atk"]),
		Pf:     toStr(out["pf"]),
		PfKey:  toStr(out["pfKey"]),
		RTK:    toStr(out["rtk"]),
	}
	if c.OpenID == "" || c.Atk == "" {
		return c, fmt.Errorf("校验响应里缺 openid / atk")
	}
	return c, nil
}

// myappVariant 是一组"整包怎么拼"的尝试参数。
type myappVariant struct {
	name        string
	sessionid   string // SAUTH 顶层 sessionid（微信/QQ 都是 atk）
	loginType   int    // extra_data.login_type：微信 8、QQ 2
	sessionID   string // extra_data.session_id：微信 hy_gameid、QQ openid
	sessionType string // extra_data.session_type：微信 wc_actoken、QQ kp_actoken
	openkey     string // extra_data.openkey（= atk）
}

// buildMyappPackage 拼出 extra_unisdk_data（和 DLL 回调里那个同构）。
func buildMyappPackage(c myappCred, dev *device.ClientProfile) (string, error) {
	return buildMyappPackageV(c, dev, myappVariant{
		sessionid: c.Atk, loginType: 8, sessionID: "hy_gameid",
		sessionType: "wc_actoken", openkey: c.Atk,
	})
}

// buildMyappPackageV 按指定变体拼整包。
func buildMyappPackageV(c myappCred, dev *device.ClientProfile, v myappVariant) (string, error) {
	session := v.sessionid
	if session == "" {
		session = c.Atk
	}
	extra, err := json.Marshal(map[string]any{
		"login_type":   v.loginType,
		"session_id":   v.sessionID,
		"session_type": v.sessionType,
		"openid":       c.OpenID,
		"openkey":      v.openkey,
		"pf":           c.Pf,
		"pfkey":        c.PfKey,
		"zoneid":       "1",
	})
	if err != nil {
		return "", err
	}

	sauth := map[string]any{
		"gameid":             profile.ShortGameID,
		"login_channel":      "myapp",
		"app_channel":        "myapp",
		"platform":           "ad",
		"sdkuid":             c.OpenID,
		"sessionid":          session,
		"sdk_version":        "2.2.2",
		"is_unisdk_guest":    0,
		"source_platform":    "pc",
		"source_app_channel": "myapp",
		"udid":               dev.Udid,
		"ip":                 "",
		"aim_info":           `{"tz":"+0800","tzid":"Asia/Shanghai","country":"CN"}`,
		"extra_data":         string(extra),
		"get_access_token":   "1",
	}
	raw, err := json.Marshal(sauth)
	if err != nil {
		return "", err
	}
	// 外层和 DLL 给的一样：{"SAUTH_JSON": "<base64>"}
	wrap, err := json.Marshal(map[string]string{
		"SAUTH_JSON": base64.StdEncoding.EncodeToString(raw),
	})
	if err != nil {
		return "", err
	}
	return string(wrap), nil
}

// SaveMyAppAccount 校验凭证 → 存成账号。返回展示名。
func SaveMyAppAccount(out map[string]any) (string, error) {
	cred, err := credFromVerify(out)
	if err != nil {
		return "", err
	}
	dev, derr := device.LoadOrCreateClient(dataDir() + "/sdk_device.json")
	if derr != nil {
		return "", fmt.Errorf("准备设备标识失败: %w", derr)
	}
	pkg, err := buildMyappPackage(cred, dev)
	if err != nil {
		return "", err
	}

	acc := Account{
		Name:         "应用宝 " + tailOf(cred.OpenID, 6),
		NickName:     "应用宝 " + tailOf(cred.OpenID, 6),
		Channel:      "myapp",
		RowKey:       cred.OpenID + " · 应用宝",
		UserID:       cred.OpenID,
		PlayerID:     cred.OpenID,
		GameAuthSign: cred.Atk,
		AccessToken:  cred.Atk,
		UnisdkData:   pkg,
		Source:       "myapplogin",
		LoginMethod:  "wechat",
		UpdatedAt:    time.Now().Unix(),
	}

	// 先真发一次 uni_sauth：能换出登录态才存
	if _, _, verr := buildPayloadFn(acc, dev); verr != nil {
		return "", fmt.Errorf("应用宝凭证换登录态失败（%v）", verr)
	}
	if err := saveScannedRow(acc, cred.OpenID, "myapp"); err != nil {
		return "", fmt.Errorf("保存账号失败: %w", err)
	}
	logf("应用宝微信登录已保存：%s（openid=%s）", acc.Name, cred.OpenID)
	return acc.Name, nil
}

// TryMyAppVariants 用同一份 YSDK 凭证试几种"整包拼法"，返回第一个能过 uni_sauth 的。
//
// 扫码那次 DLL 给的 sessionid 是 SDK 自己生成的 32 位十六进制，我们复现不出来，
// 所以把能想到的组合都试一遍：sessionid 用 atk / rtk / openid，
// openkey 用 atk / rtk，session_type 用 kp_actoken / kp_accesstoken。
func TryMyAppVariants(c myappCred, dev *device.ClientProfile, log func(string, ...any)) (Account, bool) {
	// 微信和 QQ 的 extra_data 三元组不一样（安卓客户端实测抓包确认）：
	//   微信：login_type=8, session_id=hy_gameid,     session_type=wc_actoken
	//   QQ  ：login_type=2, session_id=openid,        session_type=kp_actoken
	// 之前六种组合全挂，就是因为拿 QQ 的三元组去喂微信的 atk。
	variants := []myappVariant{
		{"微信三元组(hy_gameid/wc_actoken) + atk", c.Atk, 8, "hy_gameid", "wc_actoken", c.Atk},
		{"微信三元组，sessionid 用 openid", c.OpenID, 8, "hy_gameid", "wc_actoken", c.Atk},
		{"QQ 三元组(openid/kp_actoken) + atk", c.Atk, 2, "openid", "kp_actoken", c.Atk},
		{"微信三元组，openkey 用 rtk", c.Atk, 8, "hy_gameid", "wc_actoken", c.RTK},
	}
	for _, v := range variants {
		pkg, err := buildMyappPackageV(c, dev, v)
		if err != nil {
			continue
		}
		acc := Account{
			Name: "应用宝 " + tailOf(c.OpenID, 6), NickName: "应用宝 " + tailOf(c.OpenID, 6),
			Channel: "myapp", RowKey: c.OpenID + " · 应用宝",
			UserID: c.OpenID, PlayerID: c.OpenID,
			GameAuthSign: c.Atk, AccessToken: c.Atk, UnisdkData: pkg,
			Source: "myapplogin", LoginMethod: "wechat", UpdatedAt: time.Now().Unix(),
		}
		_, resp, verr := buildPayloadFn(acc, dev)
		if verr == nil {
			log("  变体[%s] 通过 ✓", v.name)
			return acc, true
		}
		log("  变体[%s] 失败: %s", v.name, cut(redactJSON(resp), 200))
	}
	return Account{}, false
}
