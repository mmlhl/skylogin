package channel

// 应用宝（myapp / YSDK）渠道接入。
//
// 两条扫码链路，都是纯 HTTP，不开窗、不需要浏览器：
//
//	微信：ysdk.qq.com/auth/wx_scan_code_login → 微信二维码
//	      long.open.weixin.qq.com 轮询 → ysdk.qq.com/auth/wx_verify_code 换 atk
//	QQ  ：openmobile.qq.com/oauth2.0/m_authorize → 登录页二维码
//	      xui.ptlogin2.qq.com 轮询 → ysdk.qq.com/cmd/QQCodeLogin 换 atk
//
// 两者最终拿到的都是「openid + atk + pf/pfKey」，拼进 uni_sauth 的 extra_data
// 就能换光遇登录态。区别只在 extra_data 的三元组（见 app/myappaccount.go）：
//
//	微信 login_type=8 session_id=hy_gameid session_type=wc_actoken
//	QQ   login_type=2 session_id=openid    session_type=kp_actoken
//
// 续期：微信有 refresh_token（30 天可滚动）→ 能永久续；
// QQ 的 YSDK 实现里根本没有续期请求（反编译 com.tencent.ysdkcore 确认），
// 60 天到期只能重新扫码。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"skylogin/internal/myapp"
)

// myappWxPlatform / myappQQPlatform 是凭证里的平台标记，落库与拼 extra_data 都用它。
const (
	myappWxPlatform = "wechat"
	myappQQPlatform = "qq"
)

// myappQRPollGap 是扫码轮询间隔。二维码有效期约 2 分钟，2 秒一次足够灵敏。
const myappQRPollGap = 2 * time.Second

// myappWxQR 微信扫码登录。
type myappWxQR struct{}

func (myappWxQR) Kind() Kind    { return KindQR }
func (myappWxQR) Label() string { return "微信扫码" }
func (myappWxQR) Hint() string  { return "用微信扫一下，登录应用宝账号" }

func (myappWxQR) Available(context.Context) (bool, string) { return true, "" }

func (myappWxQR) Login(ctx context.Context, p Progress) (Cred, error) {
	p.Phase("waiting", "正在申请微信二维码…")
	sess, err := myapp.CreateSession()
	if err != nil {
		return Cred{}, err
	}
	p.QR(sess.QRCodePNG, "")

	p.Phase("waiting", "请用微信扫码")
	code, err := pollWxQR(ctx, sess, p)
	if err != nil {
		return Cred{}, err
	}

	p.Phase("waiting", "正在换取应用宝凭证…")
	out, err := sess.Verify(code, "00000000")
	if err != nil {
		return Cred{}, err
	}
	cred, err := wxCredFromVerify(out)
	if err != nil {
		return Cred{}, err
	}
	return cred, nil
}

// pollWxQR 轮询微信扫码状态，直到拿到 wx_code。
func pollWxQR(ctx context.Context, sess *myapp.Session, p Progress) (string, error) {
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		state, code, err := sess.PollCtx(ctx)
		if err != nil {
			// 长轮询偶尔超时是正常的，记下来继续；连续失败才报错。
			lastErr = err
			time.Sleep(myappQRPollGap)
			continue
		}
		lastErr = nil
		switch state {
		case myapp.Scanned:
			if code != "" {
				return code, nil
			}
		case myapp.Canceled:
			return "", fmt.Errorf("已取消扫码")
		case myapp.Expired:
			return "", fmt.Errorf("二维码已失效，请重新获取")
		}
		time.Sleep(myappQRPollGap)
		if lastErr != nil {
			return "", lastErr
		}
	}
}

// wxCredFromVerify 把 wx_verify_code 的响应转成统一 Cred。
func wxCredFromVerify(out map[string]any) (Cred, error) {
	openid := jsonStr(out["openid"])
	atk := jsonStr(out["atk"])
	if openid == "" || atk == "" {
		return Cred{}, fmt.Errorf("微信校验响应里缺 openid / atk")
	}
	payload := map[string]any{
		"platform":    myappWxPlatform,
		"openid":      openid,
		"atk":         atk,
		"rtk":         jsonStr(out["rtk"]),
		"pf":          jsonStr(out["pf"]),
		"pfkey":       jsonStr(out["pfKey"]),
		"atk_expire":  int64(jsonNum(out["atk_expire"])),
		"issued_at":   time.Now().Unix(),
		"pay_token":   jsonStr(out["pay_token"]),
		"reg_channel": jsonStr(out["regChannel"]),
	}
	raw, _ := json.Marshal(payload)
	return Cred{
		UID:      openid,
		Secret:   atk,
		Nickname: myapp.WxNickname(atk, openid),
		Method:   KindQR,
		Extra: map[string]string{
			"platform": myappWxPlatform,
			"cred":     string(raw),
		},
	}, nil
}

// myappQQQR QQ 扫码登录。
type myappQQQR struct{}

func (myappQQQR) Kind() Kind    { return KindQR }
func (myappQQQR) Label() string { return "QQ 扫码" }
func (myappQQQR) Hint() string  { return "用手机 QQ 扫一下，登录应用宝账号" }

func (myappQQQR) Available(context.Context) (bool, string) { return true, "" }

func (myappQQQR) Login(ctx context.Context, p Progress) (Cred, error) {
	p.Phase("waiting", "正在申请 QQ 二维码…")
	sess, png, err := myapp.CreateQQSession()
	if err != nil {
		return Cred{}, err
	}
	p.QR(png, "")

	p.Phase("waiting", "请用手机 QQ 扫码")
	code, err := pollQQQR(ctx, sess)
	if err != nil {
		return Cred{}, err
	}

	p.Phase("waiting", "正在换取应用宝凭证…")
	c, err := myapp.QQVerify(code)
	if err != nil {
		return Cred{}, err
	}
	c.NickName = myapp.QQNickname(c.ATK, c.OpenID)

	payload := map[string]any{
		"platform":   myappQQPlatform,
		"openid":     c.OpenID,
		"atk":        c.ATK,
		"ptk":        c.PTK,
		"rtk":        c.RTK,
		"atk_expire": c.ExpireAt,
		"issued_at":  c.IssuedAt,
		"nickname":   c.NickName,
	}
	raw, _ := json.Marshal(payload)
	return Cred{
		UID:      c.OpenID,
		Secret:   c.ATK,
		Nickname: c.NickName,
		Method:   KindQR,
		Extra: map[string]string{
			"platform": myappQQPlatform,
			"cred":     string(raw),
		},
	}, nil
}

// pollQQQR 轮询 QQ 扫码状态，直到拿到 code。
func pollQQQR(ctx context.Context, sess *myapp.QQSession) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		state, code, err := sess.PollQQCtx(ctx)
		if err != nil {
			// 网络抖动继续等；二维码过期会由 state 报出来。
			time.Sleep(myappQRPollGap)
			continue
		}
		switch state {
		case myapp.Scanned:
			if code != "" {
				return code, nil
			}
		case myapp.Canceled:
			return "", fmt.Errorf("已取消扫码")
		case myapp.Expired:
			return "", fmt.Errorf("二维码已失效，请重新获取")
		}
		time.Sleep(myappQRPollGap)
	}
}

// jsonStr 安全取字符串。
func jsonStr(v any) string {
	s, _ := v.(string)
	return s
}

// jsonNum 安全取数字（JSON 解出来的都是 float64）。
func jsonNum(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		var f float64
		fmt.Sscanf(t, "%f", &f)
		return f
	}
	return 0
}

func init() {
	Register(&provider{
		key:   KeyMyapp,
		label: LabelOf(KeyMyapp),
		methods: []Method{
			myappWxQR{},
			myappQQQR{},
		},
	})
}

