package myapp

// QQ 扫码登录（纯 HTTP，不依赖浏览器）。
//
// 链路：
//
//	① GET  openmobile.qq.com/oauth2.0/m_authorize
//	        → 返回一段 JS，var src 里就是 xlogin 地址（含服务端生成的 h5sig）
//	② GET  xui.ptlogin2.qq.com/cgi-bin/xlogin
//	        → 种下会话 cookie（pt_login_sig / pt_local_token / uikey …）
//	③ GET  xui.ptlogin2.qq.com/ssl/ptqrshow
//	        → 二维码 PNG + Set-Cookie: qrsig
//	④ 轮询 xui.ptlogin2.qq.com/ssl/ptqrlogin
//	        → ptuiCB('0','0','http://auth://tauth.qq.com/?code=…',…)
//	⑤ POST ysdk.qq.com/cmd/QQCodeLogin（HMAC 签名）
//	        → atk / ptk / rtk / openid，60 天有效
//
// 最关键的一点：④ 必须带 pt_openlogin_data，它的值是 ② 那个 xlogin 地址的
// 整个 query string（再整体 URL 编码一次）。服务端从这里读到
// redirect_uri=auth://tauth.qq.com/，才会把回调生成成带 code 的地址；
// 少了它服务端不报错，只是静默降级成
// http://ptlogin4.openmobile.qq.com/check_sig?…&s_url=http://connect.qq.com
// —— 而那个域名在全球 DNS 里都是 0.0.0.1，等于死路。
//
// 另一个坑：qrsig 必须留在 cookiejar 里，不能手动设 Cookie 头。
// 手动设会盖掉 pt_login_sig / pt_local_token，服务端认不出会话。

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// ptAppID 是 QQ 互联第三方登录的固定 appid（不是光遇的 offerid）。
	ptAppID = "716027609"
	// ptDaid 是 QQ 登录的 daid，抓包确认是 381（不是 383）。
	ptDaid = "381"
	// qqU1 是登录成功后的跳转目标，与官方客户端一致。
	qqU1 = "http://connect.qq.com"

	// codeLoginKey 是 QQCodeLogin 的 HMAC 密钥。
	//
	// 出处：YSDK 插件包（com.tencent.ysdkcore）里的云端游戏登录模块，
	// 与微信扫码共用同一套 ysdk 签名体系。
	codeLoginKey = "yyb@cloud_game:CQ8FA#"

	// qqUserAgent 必须是浏览器 UA：Go 默认 UA 时 m_authorize 不回跳转脚本。
	qqUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36 Edg/142.0.0.0"

	qqAuthorizeURL = "https://openmobile.qq.com/oauth2.0/m_authorize"
	qqLoginURL     = "https://ysdk.qq.com/cmd/QQCodeLogin"
	qqVerifyURL    = "https://ysdk.qq.com/auth/qq_verify_login"
)

// qqSession 是一次 QQ 扫码会话。
type qqSession struct {
	client    *http.Client
	xloginURL string
	qrsig     string
}

// QQSession 是对外的会话句柄（字段不导出，避免调用方拼错）。
type QQSession struct{ s *qqSession }

// qqHTTPClient 建一个带 cookie 罐的客户端。
//
// 二维码有效期 2 分钟、轮询是短连接，20 秒超时足够；
// 关键是必须有 cookiejar —— 会话全靠 cookie 串起来。
func qqHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 20 * time.Second}
}

// reQQSrc 匹配 m_authorize 页面里的 var src = "…"。
var reQQSrc = regexp.MustCompile(`var\s+src\s*=\s*"([^"]+)"`)

// reJSEscape 匹配 JS 里的 \x3A 这类转义。
var reJSEscape = regexp.MustCompile(`\\x[0-9A-Fa-f]{2}`)

// rePtuiCB 匹配 ptuiCB('0','0','<url>','0','<msg>','<nick>','')。
var rePtuiCB = regexp.MustCompile(`ptuiCB\('(\d+)','(\d+)','([^']*)'`)

// reQQCode 从回调地址里抠出 32 位授权码。
var reQQCode = regexp.MustCompile(`[?&]code=([0-9A-Fa-f]{16,})`)

// extractQQSrc 从 m_authorize 的 HTML 里抠出 xlogin 地址，并还原 JS 转义。
func extractQQSrc(html string) string {
	m := reQQSrc.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return reJSEscape.ReplaceAllStringFunc(m[1], func(s string) string {
		v, err := strconv.ParseUint(s[2:], 16, 8)
		if err != nil {
			return s
		}
		return string(byte(v))
	})
}

// hash33 算 ptqrtoken：e = (e<<5) + c，最后 & 0x7FFFFFFF。
func hash33(s string) int {
	e := 0
	for _, c := range s {
		e += (e << 5) + int(c)
	}
	return 2147483647 & e
}

// CreateQQSession 申请一张 QQ 扫码二维码，返回会话与 PNG 图片。
func CreateQQSession() (*QQSession, []byte, error) {
	c := qqHTTPClient()

	// ① m_authorize：不回 302，而是给一段 JS 做跳转，xlogin 地址就在里面。
	ma := qqAuthorizeURL + "?" + url.Values{
		"client_id":     {OfferID},
		"scope":         {"all"},
		"redirect_uri":  {"auth://tauth.qq.com/"},
		"style":         {"qr"},
		"response_type": {"code"},
	}.Encode()

	noRedirect := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodGet, ma, nil)
	req.Header.Set("User-Agent", qqUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := noRedirect.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("QQ 授权页打不开: %w", err)
	}
	html, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	xloginURL := extractQQSrc(string(html))
	if xloginURL == "" {
		return nil, nil, fmt.Errorf("QQ 授权页里没找到跳转地址（HTTP %d，%d 字节）",
			resp.StatusCode, len(html))
	}

	// ② xlogin：建立会话，后面所有请求都靠这几个 cookie。
	xr, err := c.Get(xloginURL)
	if err != nil {
		return nil, nil, fmt.Errorf("QQ 登录会话建立失败: %w", err)
	}
	xr.Body.Close()

	// ③ ptqrshow：拿二维码图片。Referer 必须是 xlogin 地址，服务端会校验。
	qrURL := "https://xui.ptlogin2.qq.com/ssl/ptqrshow?" + url.Values{
		"s":          {"8"},
		"e":          {"0"},
		"appid":      {ptAppID},
		"type":       {"0"},
		"t":          {fmt.Sprintf("%.17f", float64(time.Now().UnixNano()%1000000)/1000000)},
		"u1":         {qqU1},
		"daid":       {ptDaid},
		"pt_3rd_aid": {OfferID},
	}.Encode()

	qrReq, _ := http.NewRequest(http.MethodGet, qrURL, nil)
	qrReq.Header.Set("Referer", xloginURL)
	qrReq.Header.Set("User-Agent", qqUserAgent)
	qrResp, err := c.Do(qrReq)
	if err != nil {
		return nil, nil, fmt.Errorf("获取 QQ 二维码失败: %w", err)
	}
	png, _ := io.ReadAll(io.LimitReader(qrResp.Body, 1<<20))
	qrResp.Body.Close()
	if len(png) == 0 {
		return nil, nil, fmt.Errorf("QQ 二维码内容为空（HTTP %d）", qrResp.StatusCode)
	}

	// qrsig 由 ptqrshow 通过 Set-Cookie 下发，jar 已经收下了；
	// 这里再显式读一次，用于算 ptqrtoken。
	qrsig := ""
	if pu, perr := url.Parse(qrURL); perr == nil {
		for _, ck := range c.Jar.Cookies(pu) {
			if ck.Name == "qrsig" {
				qrsig = ck.Value
			}
		}
	}
	if qrsig == "" {
		return nil, nil, fmt.Errorf("QQ 二维码没有下发 qrsig")
	}

	return &QQSession{s: &qqSession{client: c, xloginURL: xloginURL, qrsig: qrsig}}, png, nil
}

// PollQQ 查一次扫码状态。
//
// 返回的 state 与微信那套一致（Waiting / Scanned / Canceled / Expired），
// Scanned 时 code 就是 QQ 互联的授权码。
//
// ptqrlogin 的状态码：65 二维码过期、66 未扫、67 已扫待确认、0 成功。
func (q *QQSession) PollQQ() (ScanState, string, error) {
	if q == nil || q.s == nil {
		return Waiting, "", fmt.Errorf("QQ 扫码会话已失效")
	}
	s := q.s

	// pt_openlogin_data 是整条链路的关键：把 xlogin 的 query string
	// 原样打包传过去，服务端才知道 redirect_uri 是 auth://tauth.qq.com/。
	openLoginData := ""
	if pu, err := url.Parse(s.xloginURL); err == nil {
		openLoginData = pu.RawQuery
	}

	pollURL := "https://xui.ptlogin2.qq.com/ssl/ptqrlogin?" + url.Values{
		"u1":                {qqU1},
		"from_ui":           {"1"},
		"type":              {"1"},
		"ptlang":            {"2052"},
		"ptqrtoken":         {strconv.Itoa(hash33(s.qrsig))},
		"daid":              {ptDaid},
		"aid":               {ptAppID},
		"pt_3rd_aid":        {OfferID},
		"pt_openlogin_data": {openLoginData},
		"device":            {"2"},
		"ptopt":             {"1"},
		"pt_uistyle":        {"35"},
		"jsver":             {"3ea7b82c"},
		"r":                 {fmt.Sprintf("%.16f", float64(time.Now().UnixNano()%1000000)/1000000)},
	}.Encode()

	req, _ := http.NewRequest(http.MethodGet, pollURL, nil)
	req.Header.Set("Referer", s.xloginURL)
	req.Header.Set("User-Agent", qqUserAgent)
	// 浏览器里这是页面 JS 发的 XHR，会带这几个头；缺了不影响，带上更贴近真实。
	req.Header.Set("Origin", "https://xui.ptlogin2.qq.com")
	req.Header.Set("Accept", "*/*")

	resp, err := s.client.Do(req)
	if err != nil {
		return Waiting, "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	resp.Body.Close()

	m := rePtuiCB.FindStringSubmatch(string(body))
	if m == nil {
		// 没有 ptuiCB 一般是会话过期，按"等待"处理让上层继续轮询。
		return Waiting, "", nil
	}

	switch m[1] {
	case "0":
		// 成功：第三个参数就是回调地址，正常形态是
		// http://auth://tauth.qq.com/?code=<32位hex>&ret=0&pf=…&pfkey=…
		if cm := reQQCode.FindStringSubmatch(m[3]); cm != nil {
			return Scanned, cm[1], nil
		}
		return Waiting, "", fmt.Errorf("扫码成功但回调里没有 code：%s", truncate(m[3], 160))
	case "65":
		return Expired, "", nil
	case "67":
		// 已扫待确认：当成"还没好"，继续等
		return Waiting, "", nil
	}
	return Waiting, "", nil
}

// QQCredential 是 QQCodeLogin 换回来的凭证。
type QQCredential struct {
	OpenID    string `json:"openid"`
	ATK       string `json:"atk"`
	PTK       string `json:"ptk"`
	RTK       string `json:"rtk"`
	ExpireAt  int64  `json:"atk_expire"`
	IssuedAt  int64  `json:"token_issued_at"`
	NickName  string `json:"nickname,omitempty"`
	Platform  string `json:"platform"`
	ExpiresIn int64  `json:"expires_in"`
}

// QQVerify 用 code 换 QQ 的登录凭证。
//
// 签名：HMAC-SHA256(key=codeLoginKey, "POST\n/cmd/QQCodeLogin\njson\nysdk\n<ts>\n<body>")。
// 注意 expiresIn 是**字符串**，直接按数字解析会失败。
func QQVerify(code string) (*QQCredential, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"appID":"%s","loginCode":"%s"}`, OfferID, code)
	signStr := "POST\n/cmd/QQCodeLogin\njson\nysdk\n" + ts + "\n" + body

	mac := hmac.New(sha256.New, []byte(codeLoginKey))
	mac.Write([]byte(signStr))
	digest := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest(http.MethodPost, qqLoginURL, strings.NewReader(body))
	req.Header.Set("Content-Type", "json")
	req.Header.Set("Auth-Secret-ID", "ysdk")
	req.Header.Set("Auth-Secret-Digest", digest)
	req.Header.Set("Auth-Request-Time", ts)

	c := qqHTTPClient()
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("QQ 凭证换取失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))

	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Ret          int    `json:"ret"`
			OpenID       string `json:"openID"`
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			PayToken     string `json:"payToken"`
			ExpiresIn    string `json:"expiresIn"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("QQ 凭证响应不是 JSON: %s", truncate(string(raw), 200))
	}
	if out.Code != 0 || out.Data.Ret != 0 || out.Data.AccessToken == "" {
		return nil, fmt.Errorf("QQ 凭证换取失败（code=%d ret=%d msg=%s）",
			out.Code, out.Data.Ret, truncate(string(raw), 200))
	}

	exp, _ := strconv.ParseInt(out.Data.ExpiresIn, 10, 64)
	now := time.Now().Unix()
	cred := &QQCredential{
		OpenID:    out.Data.OpenID,
		ATK:       out.Data.AccessToken,
		PTK:       out.Data.PayToken,
		RTK:       out.Data.RefreshToken,
		ExpiresIn: exp,
		IssuedAt:  now,
		Platform:  "qq",
	}
	if exp > 0 {
		cred.ExpireAt = now + exp
	}
	return cred, nil
}

// QQNickname 用 atk 拉 QQ 昵称（拉不到就空着，不影响登录）。
func QQNickname(atk, openID string) string {
	c := qqHTTPClient()
	u := "https://graph.qq.com/user/get_user_info?" + url.Values{
		"access_token":       {atk},
		"oauth_consumer_key": {OfferID},
		"openid":             {openID},
	}.Encode()
	resp, err := c.Get(u)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var out struct {
		Nickname string `json:"nickname"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return ""
	}
	return out.Nickname
}

// PollQQCtx 带 ctx 的 QQ 轮询（同 PollQQ，但取消能立刻断开）。
func (q *QQSession) PollQQCtx(ctx context.Context) (ScanState, string, error) {
	if q == nil || q.s == nil {
		return Waiting, "", fmt.Errorf("QQ 扫码会话已失效")
	}
	s := q.s
	openLoginData := ""
	if pu, err := url.Parse(s.xloginURL); err == nil {
		openLoginData = pu.RawQuery
	}
	pollURL := "https://xui.ptlogin2.qq.com/ssl/ptqrlogin?" + url.Values{
		"u1":                {qqU1},
		"from_ui":           {"1"},
		"type":              {"1"},
		"ptlang":            {"2052"},
		"ptqrtoken":         {strconv.Itoa(hash33(s.qrsig))},
		"daid":              {ptDaid},
		"aid":               {ptAppID},
		"pt_3rd_aid":        {OfferID},
		"pt_openlogin_data": {openLoginData},
		"device":            {"2"},
		"ptopt":             {"1"},
		"pt_uistyle":        {"35"},
		"jsver":             {"3ea7b82c"},
		"r":                 {fmt.Sprintf("%.16f", float64(time.Now().UnixNano()%1000000)/1000000)},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
	if err != nil {
		return Waiting, "", err
	}
	req.Header.Set("Referer", s.xloginURL)
	req.Header.Set("User-Agent", qqUserAgent)
	req.Header.Set("Origin", "https://xui.ptlogin2.qq.com")
	req.Header.Set("Accept", "*/*")

	resp, err := s.client.Do(req)
	if err != nil {
		return Waiting, "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	resp.Body.Close()

	m := rePtuiCB.FindStringSubmatch(string(body))
	if m == nil {
		return Waiting, "", nil
	}
	switch m[1] {
	case "0":
		if cm := reQQCode.FindStringSubmatch(m[3]); cm != nil {
			return Scanned, cm[1], nil
		}
		return Waiting, "", fmt.Errorf("扫码成功但回调里没有 code：%s", truncate(m[3], 160))
	case "65":
		return Expired, "", nil
	}
	return Waiting, "", nil
}


// QQKeepAlive 用当前 atk 走一次 qq_verify_login。
//
// 对应 YSDK 里的定时任务 UserTask（任务名 YSDKAutoLogin，默认每 30 分钟一次）：
//
//	public void run() {
//	    if (!SwitchManager.enabledAutoRefreshLogin()) return;  // 默认 true
//	    UserApi.getInstance().autoLogin();                      // → QQUserCheckRequest
//	}
//
// 注意它**不换 token**，只是拿本地 atk 重新校验一次。所以「自动刷新登录 token」
// 刷的很可能是 token 的**有效期**（服务端滑动续期），而不是 token 本身。
// 如果这个猜测成立，定期调用就能让 atk 永不过期，QQ 也就不用 60 天重扫。
//
// 参数照抄真机抓包：少一个就可能回 11000 missing essential parameters。
// 注意签名的时间戳是**毫秒**（微信那套是秒），别搞混。
func QQKeepAlive(atk, openID, ptk string) (pf, pfKey string, err error) {
	if atk == "" || openID == "" {
		return "", "", fmt.Errorf("缺少 atk / openid，无法保活")
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	q := url.Values{
		"channel":             {"10095285"},
		"offerid":             {OfferID},
		"atk":                 {atk},
		"ptk":                 {ptk},
		"openid":              {openID},
		"loginType":           {"0"},
		"loginplatform":       {"1"},
		"platform":            {"desktop_m_qq_m"},
		"isCheckATK":          {"1"},
		"isCheckPTK":          {"1"},
		"version":             {Version},
		"appid":               {OfferID},
		"qq_appid":            {OfferID},
		"channel_id":          {"10095285"},
		"source_channel":      {"10095285"},
		"regChannel":          {"00000000"},
		"client_hope_switch":  {"1"},
		"anti_hope_switch":    {"1"},
		"visitor_switch":      {"-1"},
		"yyb_install":         {"1"},
		"wx_appid":            {WxAppID},
		"app_version":         {"0.16.7"},
		"orientation":         {"2"},
		"yyb_vercode":         {"9292130"},
		"resolution":          {"3008*1440"},
		"app_name":            {"光·遇"},
		"yyb_vername":         {"9.2.9"},
		"ysdk_plugin_version": {"102"},
		"pay_os":              {"36"},
		"is_cloud_env":        {"0"},
		"yyb_app":             {"应用宝"},
		"sk":                  {"default"},
		"timestamp":           {ts},
		"sig":                 {signature(ts)},
	}.Encode()

	c := qqHTTPClient()
	resp, err := c.Get(qqVerifyURL + "?" + q)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))

	var out struct {
		Ret   int    `json:"ret"`
		Msg   string `json:"msg"`
		PF    string `json:"pf"`
		PFKey string `json:"pfKey"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return "", "", fmt.Errorf("校验响应不是 JSON: %s", truncate(string(raw), 200))
	}
	if out.Ret != 0 {
		return "", "", fmt.Errorf("校验未通过（ret=%d msg=%s）", out.Ret, out.Msg)
	}
	return out.PF, out.PFKey, nil
}

