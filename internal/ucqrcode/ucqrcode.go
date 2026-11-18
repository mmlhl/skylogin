// Package ucqrcode 实现九游（阿里 ieu-member）网页扫码登录的协议部分。
//
// 这套接口跑在阿里的 mtop 网关上，跟 internal/ucsdk 那套「UC SDK 加密信封」
// 完全是两条链路：
//
//	ucsdk    sdk-account.9game.cn/ng/client/{service}   RSA+AES 信封，拿 sid
//	ucqrcode cscore.9game.cn/h5/{api}/1.0/             mtop 签名，拿门户登录态
//
// 本包只做后者：建码 → 轮询 → 拿到登录态（cookie）。
//
// 三个实测要点：
//
//	签名   sign = md5(token & t & appKey & data)，token 取 _m_h5_tk 下划线前半段
//	取token 首次请求不带 token，服务端回 FAIL_SYS_TOKEN_EMPTY 并下发 _m_h5_tk
//	二维码 服务端直接给 PNG 的 base64，客户端不用自己画
package ucqrcode

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"skylogin/internal/netdns"
)

// 网关与固定参数，全部来自实测抓包（2026-11-13，九游云游戏页的登录弹窗）。
const (
	// GatewayHost mtop 网关。九游自己的网关，不是 h5api.m.taobao.com。
	GatewayHost = "cscore.9game.cn"
	// AppKey 九游 ieu-member 的 mtop 应用号，固定值。
	AppKey = "12574478"
	// BizID / AppCode / SceneCode 决定登录页的形态与登录后的归属。
	BizID     = "jiuyou"
	AppCode   = "JIUYOU_PC"
	SceneCode = "web_login_config_cloud_game_pc"

	// APIStartQRCode 申请一个扫码会话，返回二维码图片与 qrToken。
	APIStartQRCode = "mtop.ieu.member.passport.login.qr.startQRCodeLogin"
	// APIQueryLoginStatus 用 qrToken 轮询状态。
	APIQueryLoginStatus = "mtop.ieu.member.passport.login.qr.queryLoginStatus"
	// APIAutoLoginByCode 扫码确认后，用轮询拿到的 vcode 完成登录。
	//
	// 2026-11-14 修正：真实服务名是驼峰（登录页 JS 里写的就是驼峰），
	// 旧的全小写名一直回 FAIL_SYS_SERVICE_FAULT。
	APIAutoLoginByCode = "mtop.ieu.member.passport.login.autoLoginByCode"

	// DefaultUA 与服务端看到的浏览器一致即可，实测不校验具体版本。
	DefaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36"
	// Referer 必须指向登录页，服务端用它判断来源场景。
	Referer = "https://member.9game.cn/pages/login/pcCustom" +
		"?bizId=jiuyou&appCode=JIUYOU_PC&sceneCode=web_login_config_cloud_game_pc"
)

// 扫码状态。服务端只回一个数字，含义按实测排列。
//
// 注意 4 是两义的：手机确认完成后回 4，二维码自然过期也回 4。
// 两者只能靠「有没有下发登录态」区分，见 Client.LoginSessionID。
const (
	StatusWaiting   = 1 // 还没人扫
	StatusScanned   = 2 // 手机扫到了，等确认
	StatusConfirmed = 3 // 确认中（实测少见）
	StatusDone      = 4 // 已完成 / 已过期，看有没有 ieu_member_sid
	StatusCanceled  = 5 // 手机上取消了
)

// Client 是扫码登录客户端。一个 Client 对应一次登录会话，
// cookie 由它自己持有（_m_h5_tk 就存在 cookie 里）。
type Client struct {
	HTTP *http.Client
	UA   string

	// UMIDToken 是阿里 WebUMID 生成的风控设备标识。
	//
	// 建码阶段服务端接受「未加载」的占位值，但轮询阶段必须与建码时同一个真值，
	// 否则后端查不到 qrToken 对应记录，直接抛 FAIL_SYS_SERVICE_FAULT。
	// 留空则使用占位值（只能建码，轮询会失败）。
	UMIDToken string

	// 建码时拿到的会话标识，轮询要用
	QRToken string
	// QRImage 是服务端给的 PNG 原始字节
	QRImage []byte
	// Verbose 打印每次请求的 URL 与 body，排错用
	Verbose bool
	// LastSetCookie 记录收到过的 Set-Cookie 原文（排查登录态用）
	LastSetCookie []string

	// AppCodeOverride / ClientTypeOverride 建码时伪装客户端身份（实验用）。
	//
	// 2026-11-14 发现：票的"国籍"由建码请求的 appCode 决定，与谁调
	// autoLoginByCode 无关（换票时伪装身份无效，sessionInfo 原样返回）。
	// 若建码就用 JIUYOU_ANDROID_APP 申请，App 扫码后签出来的票
	// 或许是 Android 上下文，能被 loginByServiceTicket 兑换。
	AppCodeOverride    string
	ClientTypeOverride string

	// SessionInfo 是 autoLoginByCode 成功后服务端下发的完整登录态。
	//
	// 2026-11-14 实测：登录态不走 Set-Cookie，而是放在响应 JSON 的
	// sessionInfo 里（cookies 数组 + sessionId + refreshToken），
	// 由页面 JS 的 setCookie() 负责写浏览器。纯 HTTP 客户端要自己落。
	SessionInfo *SessionInfo
}

// SessionInfo 是扫码登录成功后的门户会话材料。
type SessionInfo struct {
	SessionID    string              `json:"sessionId"`
	RefreshToken string              `json:"refreshToken"`
	Cookies      []SessionCookie     `json:"cookies"`
	Domains      []string            `json:"domains"`
	UserBasic    map[string]any      `json:"userBasicInfo"`
}

// SessionCookie 是 sessionInfo.cookies 里的一项。
type SessionCookie struct {
	KeyName string `json:"keyName"`
	Value   string `json:"value"`
}

// SID 返回门户会话票据（ieu_member_sid / ng_session_id，pt109265… 前缀）。
func (s *SessionInfo) SID() string {
	for _, ck := range s.Cookies {
		if ck.KeyName == "ieu_member_sid" || ck.KeyName == "ng_session_id" {
			return ck.Value
		}
	}
	return s.SessionID
}

// ApplyCookies 把 sessionInfo 里的 cookie 写进客户端 jar，
// 让后续请求带上登录态（域按服务端给的 domains）。
func (c *Client) ApplyCookies() {
	if c.SessionInfo == nil {
		return
	}
	domains := c.SessionInfo.Domains
	if len(domains) == 0 {
		domains = []string{".9game.cn"}
	}
	for _, ck := range c.SessionInfo.Cookies {
		for _, dom := range domains {
			u, err := url.Parse("https://" + strings.TrimPrefix(dom, ".") + "/")
			if err != nil {
				continue
			}
			c.HTTP.Jar.SetCookies(u, []*http.Cookie{{
				Name:   ck.KeyName,
				Value:  ck.Value,
				Domain: dom,
				Path:   "/",
			}})
		}
	}
}

// New 创建客户端。
func New() *Client {
	jar, _ := cookiejar.New(nil)
	// 强制 HTTP/1.1：浏览器（以及这个网关）都是 1.1 语义，
	// 走 h2 时服务端对表单 body 的处理会出岔子（实测轮询直接 500）。
	// 用 Wrap 而不是 NewTransport：默认 Transport 允许 h2，这里必须保留强制 1.1 的设置。
	tr := netdns.Wrap(&http.Transport{
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	})
	return &Client{
		HTTP: &http.Client{Timeout: 20 * time.Second, Jar: jar, Transport: tr},
		UA:   DefaultUA,
	}
}

// clientUser / clientDevice / clientScene 三个段的字段顺序按抓包固定。
// mtop 侧对 JSON 顺序不敏感（实测改序也能过），但保持一致便于逐字对比。
type clientUser struct {
	SessionID  string `json:"sessionId"`
	LocalID    string `json:"localId"`
	PassportID string `json:"passportId"`
}

type clientDevice struct {
	OS            string `json:"os"`
	Umid          string `json:"umid"`
	Utdid         string `json:"utdid"`
	AppVer        string `json:"appVer"`
	IP            string `json:"ip"`
	IDFA          string `json:"idfa"`
	UserAgent     string `json:"userAgent"`
	ClientBizID   string `json:"clientBizId"`
	UmidToken     string `json:"umidToken"`
	DeviceID      string `json:"deviceId"`
	OSVer         string `json:"osVer"`
	ClientAppCode string `json:"clientAppCode"`
	ClientType    string `json:"clientType"`
	Port          string `json:"port"`
	Refer         string `json:"refer"`
	PkgName       string `json:"pkgName"`
	ClientFlag    int    `json:"clientFlag"`
	SDKVer        string `json:"sdkVer"`
	AppKey        string `json:"appKey"`
	Model         string `json:"model"`
	Brand         string `json:"brand"`
	OAID          string `json:"oaid"`
}

type clientScene struct {
	GameID       any    `json:"gameId"`
	SceneCode    string `json:"sceneCode"`
	ClientCaller string `json:"clientCaller"`
	BizID        string `json:"bizId"`
	Channel      string `json:"channel"`
	AppCode      string `json:"appCode"`
	ExtInfo      string `json:"extInfo"`
}

type requestDTO struct {
	ClientUser   clientUser   `json:"clientUser"`
	ClientDevice clientDevice `json:"clientDevice"`
	ClientScene  clientScene  `json:"clientScene"`
	// QRToken 在 requestDTO 的顶层，不在 clientScene 里。
	// 放错层级不会报参数错，只会回 FAIL_SYS_SERVICE_FAULT（后端查不到码直接抛异常）。
	QRToken string `json:"qrToken,omitempty"`
}

// baseDTO 组装建码/轮询共用的那部分参数。
func (c *Client) baseDTO() requestDTO {
	umid := c.UMIDToken
	if umid == "" {
		// 实测服务端接受这个「未加载」的占位值，足够建码
		umid = "defaultToken1_um_not_loaded@@" + Referer + "@@" + fmt.Sprint(time.Now().UnixMilli())
	}
	appCode := AppCode
	clientType := "WEB"
	refer := Referer
	if c.AppCodeOverride != "" {
		appCode = c.AppCodeOverride
	}
	if c.ClientTypeOverride != "" {
		clientType = c.ClientTypeOverride
	}
	if appCode != AppCode {
		// refer 也要跟 appCode 配套，照 App 抓包的 refer 形状拼
		refer = "https://member.9game.cn/pages/login/pcCustom?bizId=" + BizID + "&appCode=" + appCode
	}
	return requestDTO{
		ClientUser: clientUser{},
		ClientDevice: clientDevice{
			UserAgent:     c.UA,
			ClientBizID:   BizID,
			ClientAppCode: appCode,
			ClientType:    clientType,
			Refer:         refer,
			UmidToken:     umid,
		},
		ClientScene: clientScene{
			SceneCode:    SceneCode,
			ClientCaller: "member-frontend",
			BizID:        BizID,
			AppCode:      appCode,
			ExtInfo:      `{"INNER_KEY_AUTH_SCENE":"","ugSid":""}`,
		},
	}
}

// StartQRCode 申请二维码，结果写进 c.QRToken 与 c.QRImage。
func (c *Client) StartQRCode() error {
	body, err := c.call(APIStartQRCode, c.baseDTO())
	if err != nil {
		return err
	}
	var d struct {
		QRCodeBase64 string `json:"qrCodeBase64"`
		QRToken      string `json:"qrToken"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return fmt.Errorf("建码响应解析失败: %w", err)
	}
	if d.QRToken == "" || d.QRCodeBase64 == "" {
		return fmt.Errorf("建码没返回二维码: %s", trim(string(body), 300))
	}
	png, err := decodeDataURL(d.QRCodeBase64)
	if err != nil {
		return err
	}
	c.QRToken = d.QRToken
	c.QRImage = png
	return nil
}

// QueryStatus 轮询一次，返回状态码。
func (c *Client) QueryStatus() (int, error) {
	st, _, err := c.QueryStatusDetail()
	return st, err
}

// QueryStatusDetail 轮询一次，返回状态码和完整 data 段。
//
// 手机确认后拿到的不是「3」，而是：
//
//	{"status":0,"vcode":"…","connectScene":"JIUYOU_ANDROID_APP_JIUYOU_PC"}
//
// vcode 要再用 autologinbycode 换登录态。
func (c *Client) QueryStatusDetail() (int, map[string]any, error) {
	dto := c.baseDTO()
	dto.QRToken = c.QRToken
	body, err := c.call(APIQueryLoginStatus, dto)
	if err != nil {
		return 0, nil, err
	}
	var d struct {
		Status       int    `json:"status"`
		VCode        string `json:"vcode"`
		ConnectScene string `json:"connectScene"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return 0, nil, fmt.Errorf("状态响应解析失败: %w", err)
	}
	return d.Status, map[string]any{"vcode": d.VCode, "connectScene": d.ConnectScene}, nil
}

// AutoLoginByCode 用轮询拿到的 vcode 完成登录，成功后 cookie 里会有 ieu_member_sid。
func (c *Client) AutoLoginByCode(vcode, connectScene string) error {
	_, err := c.AutoLoginByCodeOpts(vcode, connectScene, false)
	return err
}

// AutoLoginByCodeOpts 与 AutoLoginByCode 相同，但可以指定是否把 qrToken
// 一起塞进 requestDTO 顶层。
//
// 为什么要这个开关：`.ai/12` 坑 1 记的是「qrToken 必须在 requestDTO 顶层」，
// 那条是在轮询接口上确认的。换票接口是否也要求 qrToken，还没有定论——
// 2026-11-14 实测不带 qrToken 会回 FAIL_SYS_SERVICE_FAULT，所以两种都留一手。
func (c *Client) AutoLoginByCodeOpts(vcode, connectScene string, withQRToken bool) (json.RawMessage, error) {
	extra := map[string]any{"vcode": vcode}
	if connectScene != "" {
		extra["connectScene"] = connectScene
	}
	if withQRToken && c.QRToken != "" {
		extra["qrToken"] = c.QRToken
	}
	return c.CallWithExtra(APIAutoLoginByCode, extra)
}

// AutoLoginByCodeV2 按登录页 JS 的真实实现换登录态（2026-11-14 从
// tmp/pcCustom.js 里挖出来的）：
//
//	api      = mtop.ieu.member.passport.login.autoLoginByCode（驼峰）
//	参数     = {connectCode: <vcode>, loginToken: ""}
//	clientScene.sceneCode = connectScene（不是建码时的那个 scene）
//
// vcode 形如 nt_security_v_… 时，响应里会带 nextUrl：那是阿里的安全验证页，
// 页面把它塞进隐藏 iframe 加载，验完回传 loginToken，再用 loginToken 重发
// 一次本接口。这里返回完整 data，把 nextUrl / loginToken 交给调用方处理。
func (c *Client) AutoLoginByCodeV2(vcode, connectScene, loginToken string) (map[string]any, json.RawMessage, error) {
	return c.AutoLoginByCodeDev(vcode, connectScene, loginToken, nil)
}

// AutoLoginByCodeDev 同 AutoLoginByCodeV2，但允许整体改写 clientDevice 段。
//
// 用途（2026-11-14）：门户票兑 SDK sid 回 160，怀疑服务端按「票据签发环境」
// 绑定。把换票请求的 clientDevice 伪装成九游 App 的身份
// （clientType=ANDROID / clientAppCode=JIUYOU_ANDROID_APP / pkgName=…），
// 看签出来的票能不能被 Android 游戏端兑换。字段取自抓包里 App 的
// scanQRCode 请求（.ai/12 第十节）。
func (c *Client) AutoLoginByCodeDev(vcode, connectScene, loginToken string, dev map[string]any) (map[string]any, json.RawMessage, error) {
	extra := map[string]any{
		"connectCode": vcode,
		"loginToken":  loginToken,
	}
	// 这个调用的 clientScene.sceneCode 要用 connectScene
	m := map[string]any{}
	b, err := marshalNoEscape(c.baseDTO())
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, nil, err
	}
	if cs, ok := m["clientScene"].(map[string]any); ok {
		cs["sceneCode"] = connectScene
	}
	if len(dev) > 0 {
		if cd, ok := m["clientDevice"].(map[string]any); ok {
			for k, v := range dev {
				cd[k] = v
			}
		}
	}
	for k, v := range extra {
		m[k] = v
	}
	inner, err := marshalNoEscape(m)
	if err != nil {
		return nil, nil, err
	}
	outer, err := marshalNoEscape(map[string]string{"requestDTO": string(inner)})
	if err != nil {
		return nil, nil, err
	}
	raw, err := c.send(APIAutoLoginByCode, string(outer))
	if err != nil {
		return nil, raw, err
	}
	var d struct {
		Data       map[string]any `json:"data"`
		Raw        json.RawMessage `json:"-"`
		Session    *SessionInfo   `json:"sessionInfo"`
	}
	if e := json.Unmarshal(raw, &d); e != nil {
		return nil, raw, fmt.Errorf("换票响应解析失败: %w（%s）", e, trim(string(raw), 200))
	}
	// 2026-11-14 实测：sessionInfo 不在 data 里，直接挂在响应顶层
	if d.Session != nil {
		c.SessionInfo = d.Session
		c.ApplyCookies()
	}
	return d.Data, raw, nil
}

// FetchSecurityIframe 抓安全验证页的原文（调试用：看它要求什么）。
func (c *Client) FetchSecurityIframe(nextURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", nextURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UA)
	req.Header.Set("Referer", Referer)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// WaitLogin 轮询到手机确认（或过期/取消/超时）。
//
// 服务端大约 2 秒一次就够（页面自己也是 2 秒一轮）。
// 查询出错不中断：偶发的 token 失效下一轮会自愈，连续失败才会体现在超时里。
func (c *Client) WaitLogin(timeout time.Duration, onStatus func(int), onErr func(error)) (int, error) {
	deadline := time.Now().Add(timeout)
	last := 0
	for time.Now().Before(deadline) {
		st, err := c.QueryStatus()
		if err != nil {
			if onErr != nil {
				onErr(err)
			}
			time.Sleep(2 * time.Second)
			continue
		}
		if st != last && onStatus != nil {
			onStatus(st)
		}
		last = st
		if st == StatusConfirmed || st == StatusDone || st == StatusCanceled {
			return st, nil
		}
		time.Sleep(2 * time.Second)
	}
	return last, fmt.Errorf("等待扫码超时")
}

// CallWithExtra 调一个 mtop 接口，在标准 DTO 之外再挂几个字段（探针用）。
//
// 例：CallWithExtra(APIAutoLogin, map[string]any{"vcode": "…"})
func (c *Client) CallWithExtra(api string, extra map[string]any) (json.RawMessage, error) {
	m := map[string]any{}
	b, err := marshalNoEscape(c.baseDTO())
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		m[k] = v
	}
	inner, err := marshalNoEscape(m)
	if err != nil {
		return nil, err
	}
	outer, err := marshalNoEscape(map[string]string{"requestDTO": string(inner)})
	if err != nil {
		return nil, err
	}
	return c.send(api, string(outer))
}

// send 用给定的 data 原文发一次请求（含 token 获取与失败重试）。
func (c *Client) send(api, data string) (json.RawMessage, error) {
	if c.token() == "" {
		_, _ = c.do(api, data, "")
	}
	tk := c.token()
	if tk == "" {
		return nil, fmt.Errorf("拿不到 _m_h5_tk")
	}
	body, err := c.do(api, data, tk)
	if err != nil {
		return nil, err
	}
	var env struct {
		Ret  []string        `json:"ret"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("响应不是 JSON: %s", trim(string(body), 200))
	}
	if len(env.Ret) > 0 && !strings.HasPrefix(env.Ret[0], "SUCCESS") {
		return nil, fmt.Errorf("%s 失败: %s", api, env.Ret[0])
	}
	var wrap struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(env.Data, &wrap); err != nil {
		return nil, fmt.Errorf("data 段解析失败: %s", trim(string(env.Data), 200))
	}
	if wrap.Code != "" && wrap.Code != "SUCCESS" {
		return nil, fmt.Errorf("%s 返回 %s: %s", api, wrap.Code, wrap.Msg)
	}
	return wrap.Data, nil
}

// LoginSessionID 返回登录态里的 ieu 会话票据（pt109265… 形态）。
//
// 扫码确认成功后，服务端会在 .9game.cn 下种三样东西：
//
//	ieu_member_sid = pt109265…    ← 通行证会话票据，等于 ng_session_id
//	ieu_member_uid = 1001…        ← 九游账号 ID（门户 localId）
//	ieu_member_passport_id = 288…
//
// 空字符串表示还没登录成功。
func (c *Client) LoginSessionID() string {
	if c.SessionInfo != nil {
		if s := c.SessionInfo.SID(); s != "" {
			return s
		}
	}
	ck := c.Cookies()
	if v := ck["ieu_member_sid"]; v != "" {
		return v
	}
	return ck["ng_session_id"]
}

// LoginUID 返回九游账号 ID（门户 localId）。
func (c *Client) LoginUID() string { return c.Cookies()["ieu_member_uid"] }

// Cookies 返回当前会话在 .9game.cn 下的 cookie 快照。
// 登录成功后 ieu_member_sid / ng_session_id / ieu_member_uid 会出现在这里。
func (c *Client) Cookies() map[string]string {
	u, _ := url.Parse("https://" + GatewayHost + "/")
	out := map[string]string{}
	for _, ck := range c.HTTP.Jar.Cookies(u) {
		out[ck.Name] = ck.Value
	}
	return out
}

// call 发一次 mtop 请求，返回 data 段。
func (c *Client) call(api string, dto requestDTO) (json.RawMessage, error) {
	// 注意：必须关掉 Go 默认的 HTML 转义。
	// json.Marshal 会把 & 写成 \u0026，而 refer 字段里全是 &，
	// 服务端解出来就变成 "...bizId=jiuyou\u0026appCode=..."。
	// 建码阶段它不校验这个字段，轮询阶段会因此抛 FAIL_SYS_SERVICE_FAULT。
	inner, err := marshalNoEscape(dto)
	if err != nil {
		return nil, err
	}
	// 外层再套一层 requestDTO 字符串，这是 ieu-member 自己的约定
	outer, err := marshalNoEscape(map[string]string{"requestDTO": string(inner)})
	if err != nil {
		return nil, err
	}
	data := string(outer)
	return c.send(api, data)
}

// do 发一次带签名的 POST。
//
// 两个实测细节：
//   - URL 里 path 段用全小写（startqrcodelogin），api 参数用驼峰（startQRCodeLogin）
//   - timeout / preventFallback 必须带，缺了轮询会回 FAIL_SYS_SERVICE_FAULT
func (c *Client) do(api, data, token string) ([]byte, error) {
	t := fmt.Sprint(time.Now().UnixMilli())
	sum := md5.Sum([]byte(token + "&" + t + "&" + AppKey + "&" + data))
	sign := hex.EncodeToString(sum[:])

	u := fmt.Sprintf("https://%s/h5/%s/1.0/?jsv=2.7.2&appKey=%s&t=%s&sign=%s&api=%s"+
		"&type=originaljson&dataType=json&v=1.0&timeout=15000&preventFallback=true",
		GatewayHost, strings.ToLower(api), AppKey, t, sign, api)
	form := "data=" + encodeURIComponent(data)
	if c.Verbose {
		fmt.Printf("[ucqrcode] %s\n  url=%s\n  body=%s\n", api, u, form)
	}
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(form))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", c.UA)
	req.Header.Set("Referer", Referer)
	req.Header.Set("Origin", "https://member.9game.cn")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// 登录态（ieu_member_sid 等）随 Set-Cookie 下发，留一份原文便于排查
	if sc := resp.Header.Values("Set-Cookie"); len(sc) > 0 {
		c.LastSetCookie = append(c.LastSetCookie, sc...)
	}
	return io.ReadAll(resp.Body)
}

// FetchLoginPage 用同一个会话访问一次九游首页/登录页。
//
// 实测：扫码确认（status=4）之后，ieu_member_sid 并不会随轮询响应的
// Set-Cookie 一起下来，而是需要再访问一次页面才会被种上。
func (c *Client) FetchLoginPage(u string) error {
	if u == "" {
		u = "https://www.9game.cn/"
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UA)
	req.Header.Set("Referer", Referer)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if sc := resp.Header.Values("Set-Cookie"); len(sc) > 0 {
		c.LastSetCookie = append(c.LastSetCookie, sc...)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// token 从 cookie 里取 _m_h5_tk 的下划线前半段。
func (c *Client) token() string {
	for _, ck := range c.HTTP.Jar.Cookies(mustURL("https://" + GatewayHost + "/")) {
		if ck.Name == "_m_h5_tk" {
			if i := strings.Index(ck.Value, "_"); i > 0 {
				return ck.Value[:i]
			}
		}
	}
	return ""
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }

// encodeURIComponent 与浏览器的 encodeURIComponent 一致。
//
// 不能用 url.QueryEscape：它把空格写成 +、把 ()'!* 也编码了，
// 而服务端按「原始 body」对 data 做一致性校验，编码形式不同就会被判成故障请求
// （实测表现是 FAIL_SYS_SERVICE_FAULT，且只在轮询接口上出现）。
func encodeURIComponent(s string) string {
	const keep = "-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteByte(ch)
		case strings.IndexByte(keep, ch) >= 0:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// marshalNoEscape 序列化 JSON 但不做 HTML 转义（& < > 保持原样）。
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// decodeDataURL 把 data:image/png;base64,… 还原成字节。
func decodeDataURL(s string) ([]byte, error) {
	i := strings.Index(s, "base64,")
	if i < 0 {
		return nil, fmt.Errorf("二维码不是 base64 data URL")
	}
	return base64.StdEncoding.DecodeString(s[i+7:])
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
