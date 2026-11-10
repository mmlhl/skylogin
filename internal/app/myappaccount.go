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

	"skylogin/internal/channel"
	"skylogin/internal/device"
	"skylogin/internal/myapp"
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

// ── 应用宝：新框架（channel.Provider）的落库与续期 ──────────────────
//
// 与上面的 SaveMyAppAccount / TryMyAppVariants 是两条路：
// 那两个是老入口（DLL 扫码拿整包），下面这套是 channel 层扫码拿凭证后统一落库。
// 共用同一个 Account.MyappCred 字段，结构一致，互不影响。

// myappCredData 是 MyappCred 里存的 JSON，字段与 channel 层 Cred.Extra["cred"] 对齐。
type myappCredData struct {
	Platform  string `json:"platform"` // wechat / qq
	OpenID    string `json:"openid"`
	ATK       string `json:"atk"`
	PTK       string `json:"ptk,omitempty"`
	RTK       string `json:"rtk,omitempty"`
	PF        string `json:"pf,omitempty"`
	PFKey     string `json:"pfkey,omitempty"`
	ATKExpire int64  `json:"atk_expire,omitempty"`
	IssuedAt  int64  `json:"issued_at,omitempty"`
	NickName  string `json:"nickname,omitempty"`
	// LastCheck 是最后一次成功保活（qq_verify_login）的时间。
	// 用来观察"服务端是否在每次校验时滑动续期"——如果 60 天后 atk 还能用，
	// 说明能续，QQ 也能做成永久免密。
	LastCheck int64 `json:"last_check,omitempty"`
}

// myappExtraData 拼 uni_sauth 的 extra_data。
//
// 微信和 QQ 的三元组不一样，这是实测抓包确认的（见 .ai/04 踩坑记录）：
//
//	微信 login_type=8 session_id=hy_gameid session_type=wc_actoken
//	QQ   login_type=2 session_id=openid    session_type=kp_actoken
//
// 拿 QQ 的三元组去喂微信的 atk 会直接失败，且报错不会提到三元组，很难排查。
func myappExtraData(d myappCredData) string {
	loginType := 8
	sessionID := "hy_gameid"
	sessionType := "wc_actoken"
	if d.Platform == "qq" {
		loginType = 2
		sessionID = "openid"
		sessionType = "kp_actoken"
	}
	extra, _ := json.Marshal(map[string]any{
		"login_type":   loginType,
		"session_id":   sessionID,
		"session_type": sessionType,
		"openid":       d.OpenID,
		"openkey":      d.ATK,
		"pf":           d.PF,
		"pfkey":        d.PFKey,
		"zoneid":       "1",
	})
	return string(extra)
}

// finalizeMyapp 应用宝落库：先过一遍 uni_sauth，再组装 Account。
//
// uni_sauth 是"这份凭证真能换到光遇登录态"的唯一证据，光拿到 atk 不算数。
func finalizeMyapp(key string, cred channel.Cred) (Account, error) {
	raw := cred.Extra["cred"]
	var d myappCredData
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return Account{}, fmt.Errorf("应用宝登录材料损坏: %w", err)
	}
	if d.OpenID == "" || d.ATK == "" {
		return Account{}, fmt.Errorf("应用宝登录材料不完整（缺 openid / atk）")
	}
	if cred.UID != "" && d.OpenID != cred.UID {
		return Account{}, fmt.Errorf("应用宝登录的账号与凭证不一致，已取消保存")
	}

	if err := channelSAUTHWithExtra(key, d.OpenID, d.ATK, myappExtraData(d)); err != nil {
		// 网络不通时保留登录结果：凭证是刚扫码拿到的，启动时还会再验一次。
		// 让用户白扫一遍是最伤的。
		if channel.IsNetwork(err) {
			logf("应用宝凭证在线验证暂时失败（%v），保留本次登录结果", err)
		} else {
			return Account{}, err
		}
	}

	name := d.NickName
	if name == "" {
		name = "应用宝 " + tailOf(d.OpenID, 6)
	}
	a50dev, _ := ensureA50DeviceID()
	now := time.Now().Unix()
	return Account{
		Name:         name,
		NickName:     name,
		Username:     name,
		Channel:      key,
		UserID:       d.OpenID,
		PlayerID:     d.OpenID,
		GameAuthSign: d.ATK,
		DeviceID:     a50dev,
		Source:       "myapp",
		LoginMethod:  string(cred.Method),
		CredState:    "ok",
		LastCheck:    now,
		UpdatedAt:    now,
		MyappCred:    raw,
	}, nil
}


// myappRefreshLead 距过期不足这么多秒就提前续期，避免卡在临界点上启动失败。
//
// 必须小于微信 atk 的实际寿命（7200 秒）。取 30 分钟足够：
// 官方行为是 atk 未过期时 refresh 返回同一个 atk 但刷新有效期，
// 所以提前续不会有副作用。
const myappRefreshLead = 30 * 60

// ensureMyappCredential 应用宝启动前保活。
//
// 微信：用 refresh_token 换新 atk，rtk 每次都会滚新，能一直续下去。
// QQ  ：YSDK 侧根本没有续期接口（反编译确认），60 天到期只能重扫，
//       所以这里对 QQ 只在**确认过期**时才标失效。
func ensureMyappCredential(acc *Account) error {
	if acc.MyappCred == "" {
		// 没有续期材料：可能是导入时丢了。不能直接判死——
		// atk 本身可能还有效，交给后面的 uni_sauth 去验。
		return nil
	}
	var d myappCredData
	if err := json.Unmarshal([]byte(acc.MyappCred), &d); err != nil {
		expireChannelCredential(acc, "应用宝登录材料损坏", "myapp_expired",
			"应用宝登录已过期", "请重新扫码添加该账号")
		return fmt.Errorf("应用宝登录已过期，请重新扫码添加该账号")
	}

	now := time.Now().Unix()
	expired := d.ATKExpire > 0 && now >= d.ATKExpire

	// QQ：没有换 token 的接口，但 YSDK 自己有个定时任务 UserTask
	// （YSDKAutoLogin，默认每 30 分钟）拿本地 atk 重走 qq_verify_login。
	// 这里模拟它：每次启动校验一次。
	//
	// 注意不能放到下面"离到期还远就直接返回"之后——60 天内都算"还远"，
	// 放后面等于永不执行。失败也不影响启动（本地 atk 照样能用）。
	if d.Platform == "qq" {
		if !expired {
			if pf, pfKey, kerr := myapp.QQKeepAlive(d.ATK, d.OpenID, d.PTK); kerr == nil {
				if pf != "" {
					d.PF = pf
				}
				if pfKey != "" {
					d.PFKey = pfKey
				}
				d.LastCheck = now
				if raw, e := json.Marshal(d); e == nil {
					acc.MyappCred = string(raw)
					_ = saveAccount(*acc)
				}
				logf("应用宝 QQ 账号 %s：atk 保活成功（服务端是否顺延有效期待观察）", displayName(*acc))
			} else {
				logf("应用宝 QQ 账号 %s：保活未通过（%v），继续使用本地 atk", displayName(*acc), kerr)
			}
			return nil
		}
		// 确认已过期：QQ 没有续期手段，只能重扫
		expireChannelCredential(acc, "应用宝 QQ 登录已失效", "myapp_expired",
			"应用宝登录已过期", "QQ 登录约 60 天需要重新扫码")
		return fmt.Errorf("应用宝 QQ 登录已过期，请重新扫码")
	}

	// 微信：还有效且离到期还远 → 不必打服务器
	if !expired && (d.ATKExpire == 0 || now < d.ATKExpire-myappRefreshLead) {
		return nil
	}

	if d.RTK == "" {
		if expired {
			expireChannelCredential(acc, "应用宝微信登录已失效", "myapp_expired",
				"应用宝登录已过期", "请重新扫码添加该账号")
			return fmt.Errorf("应用宝微信登录已过期，请重新扫码")
		}
		return nil
	}

	res, err := myapp.RefreshWxToken(d.RTK)
	if err != nil {
		if expired {
			expireChannelCredential(acc, "应用宝微信登录已失效", "myapp_expired",
				"应用宝登录已过期", "请重新扫码添加该账号")
			return fmt.Errorf("应用宝微信登录已过期，请重新扫码")
		}
		logf("应用宝凭证刷新失败，继续使用本地缓存: %v", err)
		return nil
	}

	// 身份不能变：万一 openid 变了说明换号了，别覆盖原账号
	if res.OpenID != "" && d.OpenID != "" && res.OpenID != d.OpenID {
		expireChannelCredential(acc, "应用宝账号身份已变更", "myapp_expired",
			"应用宝登录已过期", "请重新扫码添加该账号")
		return fmt.Errorf("应用宝登录已过期，请重新扫码添加该账号")
	}

	d.ATK = res.ATK
	d.RTK = res.RTK
	d.ATKExpire = myapp.ExpireAtFrom(res.ExpiresIn)
	d.IssuedAt = now
	raw, _ := json.Marshal(d)

	acc.GameAuthSign = d.ATK
	acc.MyappCred = string(raw)
	acc.CredState = "ok"
	acc.LastCheck = now
	acc.UpdatedAt = now
	_ = saveAccount(*acc)
	logf("应用宝账号 %s：微信凭证已静默续期", displayName(*acc))
	return nil
}


// myappCredOf 从账号里解析应用宝凭证；没有或损坏时返回 false。
func myappCredOf(acc Account) (myappCredData, bool) {
	var d myappCredData
	if acc.MyappCred == "" {
		return d, false
	}
	if json.Unmarshal([]byte(acc.MyappCred), &d) != nil {
		return d, false
	}
	if d.ATK == "" || d.OpenID == "" {
		return d, false
	}
	return d, true
}

