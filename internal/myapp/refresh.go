package myapp

// 微信凭证续期。
//
// 微信的 refresh_token 有效期 30 天，每次换新都会把有效期重新拉满，
// 所以只要用户每隔一阵启动一次游戏，账号就能一直续下去 —— 这是应用宝
// 微信链路能"永久免密"的根本原因。
//
// 对比 QQ：YSDK 的 QQ 模块里根本没有续期请求（反编译 com.tencent.ysdkcore
// 确认：local/auto/wakeup 三种"自动登录"全是拿本地 atk 重走 qq_verify_login），
// 60 天到期只能重新扫码。
//
// 接口只需要 appid + refresh_token，**不需要 client_secret**，
// 这是微信和 QQ 互联最大的设计差异。

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"
)

// wxRefreshURL 是微信官方的 refresh_token 端点。
const wxRefreshURL = "https://api.weixin.qq.com/sns/oauth2/refresh_token"

// WxRefreshResult 是一次续期的结果。
type WxRefreshResult struct {
	ATK       string
	RTK       string
	ExpiresIn int64
	OpenID    string
}

// RefreshWxToken 用 refresh_token 换新的 access_token。
//
// 官方行为：atk 未过期时返回同一个 atk 但刷新有效期；已过期时返回新 atk。
// 两种情况都算续期成功。
func RefreshWxToken(rtk string) (*WxRefreshResult, error) {
	if rtk == "" {
		return nil, fmt.Errorf("没有 refresh_token，无法续期")
	}
	q := url.Values{}
	q.Set("appid", WxAppID)
	q.Set("grant_type", "refresh_token")
	q.Set("refresh_token", rtk)

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		OpenID       string `json:"openid"`
		ErrCode      int    `json:"errcode"`
		ErrMsg       string `json:"errmsg"`
	}
	if err := getJSON(wxRefreshURL+"?"+q.Encode(), &out); err != nil {
		return nil, fmt.Errorf("微信凭证续期失败: %w", err)
	}
	if out.ErrCode != 0 {
		return nil, fmt.Errorf("微信凭证续期失败（errcode=%d %s）", out.ErrCode, out.ErrMsg)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("微信凭证续期未返回 access_token")
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 7200
	}
	res := &WxRefreshResult{
		ATK:       out.AccessToken,
		RTK:       out.RefreshToken,
		ExpiresIn: out.ExpiresIn,
		OpenID:    out.OpenID,
	}
	if res.RTK == "" {
		res.RTK = rtk
	}
	return res, nil
}

// WxAuthValid 用 sns/auth 查一次 access_token 是否还有效。
func WxAuthValid(atk, openID string) bool {
	var out struct {
		ErrCode int `json:"errcode"`
	}
	u := "https://api.weixin.qq.com/sns/auth?access_token=" +
		url.QueryEscape(atk) + "&openid=" + url.QueryEscape(openID)
	if err := getJSON(u, &out); err != nil {
		return false
	}
	return out.ErrCode == 0
}

// ExpireAtFrom 按 expires_in 算绝对过期时间（unix 秒）。
func ExpireAtFrom(expiresIn int64) int64 {
	if expiresIn <= 0 {
		return 0
	}
	return time.Now().Unix() + expiresIn
}

// atoiSafe 容错解析（expiresIn 有时是字符串）。
func atoiSafe(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// drainClose 读完并关闭响应体，保证连接能复用。
func drainClose(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(rc, 1<<16))
	_ = rc.Close()
}
