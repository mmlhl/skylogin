// QQ 扫码验证（网页版）：本地起一个页面，自动显示最新二维码并走完整个流程。
//
// 为什么不用命令行版：二维码有效期很短，从"生成"到"人拿起手机扫"中间
// 耽误一分钟就废了。这里让服务端自己轮换二维码，页面上的图永远是新鲜的，
// 随时打开都能扫。
//
// 用法：go run qqweb.go   然后浏览器打开 http://127.0.0.1:8899
package main

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	qqAppID      = "1109425751" // 光遇应用宝版的 QQ appid（抓包确认）
	ptAppID      = "716027609"  // QQ 互联第三方登录固定 appid
	ptDaid       = "381"        // 抓包确认：381（不是 383）
	codeLoginKey = "yyb@cloud_game:CQ8FA#"
	sURL         = "http://connect.qq.com"
	defaultAddr = "127.0.0.1:8899"
)

var (
	// 带 cookie 的客户端：xlogin 建立的会话要带到 ptqrshow / ptqrlogin
	client *http.Client

	mu     sync.Mutex
	png    []byte
	status = "启动中…"
	trace  []string
	genAt  time.Time
)

func init() {
	jar, _ := cookiejar.New(nil)
	client = &http.Client{Jar: jar, Timeout: 40 * time.Second}
}

func setStatus(s string) {
	mu.Lock()
	status = s
	mu.Unlock()
}

func addTrace(s string) {
	mu.Lock()
	trace = append(trace, time.Now().Format("15:04:05")+"  "+s)
	if len(trace) > 60 {
		trace = trace[len(trace)-60:]
	}
	mu.Unlock()
	fmt.Println(s)
}

func main() {
	addr := defaultAddr
	flag.StringVar(&addr, "addr", defaultAddr, "本地监听地址（浏览器打开它扫码）")
	flag.Parse()

	go sessionLoop()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pageTpl.Execute(w, nil)
	})
	http.HandleFunc("/qr.png", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		b := png
		mu.Unlock()
		if len(b) == 0 {
			http.Error(w, "还没有二维码", 503)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	http.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": status,
			"trace":  trace,
			"genAt":  genAt.Format("15:04:05"),
		})
	})

	fmt.Println("打开 http://" + addr + " 扫码")
	_ = http.ListenAndServe(addr, nil)
}

// sessionLoop 不断轮换二维码，直到某次扫码成功并走完流程。
func sessionLoop() {
	for attempt := 1; attempt <= 20; attempt++ {
		addTrace(fmt.Sprintf("—— 第 %d 次申请二维码 ——", attempt))
		qrsig, img, err := newQR()
		if err != nil {
			addTrace("申请二维码失败: " + err.Error())
			time.Sleep(3 * time.Second)
			continue
		}
		mu.Lock()
		png, genAt = img, time.Now()
		mu.Unlock()
		setStatus("等待扫码（二维码每 80 秒自动更换）")

		redirect, ok := waitScan(qrsig)
		if !ok {
			continue // 过期，换一张
		}

		setStatus("扫码成功，正在换 code…")
		code, err := followForCode(redirect)
		if err != nil {
			addTrace("换 code 失败: " + err.Error())
			setStatus("换 code 失败，见下方日志")
			return
		}
		addTrace("code 已拿到: " + truncate(code, 12) + "…")

		setStatus("正在用 QQCodeLogin 换 token…")
		atk, openid, rtk, expires, ptk, err := qqCodeLogin(code)
		if err != nil {
			addTrace("✗ QQCodeLogin 失败: " + err.Error())
			addTrace("→ 结论：ptlogin2 的 code 不能直接喂给这个接口，需要走 m_authorize")
			setStatus("QQCodeLogin 失败 —— 纯 HTTP 方案不成立")
			return
		}
		addTrace(fmt.Sprintf("✓ QQCodeLogin 成功  atk=%s  openid=%s  有效期 %.1f 天",
			truncate(atk, 12), openid, float64(expires)/86400))
		addTrace("rtk=" + truncate(rtk, 16))

		setStatus("正在用 atk 走 qq_verify_login…")
		if err := qqVerifyLogin(atk, openid, ptk); err != nil {
			addTrace("✗ qq_verify_login 失败: " + err.Error())
			addTrace("→ 结论：token 拿到了但服务端不认（appid 归属问题）")
			setStatus("qq_verify_login 失败")
			return
		}
		addTrace("✓✓ qq_verify_login 通过 —— 纯 HTTP 方案成立，不需要浏览器")
		setStatus("全部通过：纯 HTTP 方案成立")
		return
	}
	setStatus("多次尝试都过期，可能缺 h5sig")
}

// newQR 申请一张二维码。
//
// 必须走三跳，缺一不可：
//  ① m_authorize 重定向到 xlogin，Location 里带着服务端生成的 h5sig
//  ② 请求那个 xlogin 地址，建立登录会话（cookie）
//  ③ 再申请二维码，服务端才知道这张码属于哪个会话
//
// 之前跳过 ①② 直接打 ptqrshow，扫了就失效——因为没有会话可绑。
func newQR() (string, []byte, error) {
	// ① 拿重定向地址（不跟随，只要 Location）
	ma := "https://openmobile.qq.com/oauth2.0/m_authorize?" + url.Values{
		"client_id":     {qqAppID},
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
	req0, _ := http.NewRequest("GET", ma, nil)
	// 必须伪装成浏览器：UA 是 Go 默认值时服务端不重定向，直接回登录页 HTML
	req0.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req0.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp0, err := noRedirect.Do(req0)
	if err != nil {
		return "", nil, fmt.Errorf("m_authorize: %w", err)
	}
	html, _ := io.ReadAll(resp0.Body)
	resp0.Body.Close()

	// m_authorize 不回 302，而是给一段 JS 做跳转；xlogin 的完整地址
	// （含服务端生成的 h5sig）就硬编码在 var src 里，直接抠出来即可。
	xloginURL := extractSrc(string(html))
	if xloginURL == "" {
		return "", nil, fmt.Errorf("m_authorize 页面里没找到跳转地址（HTTP %d，%d 字节）",
			resp0.StatusCode, len(html))
	}
	addTrace("① 提取到 xlogin 地址（含 h5sig）")

	// ② 走一次 xlogin，把会话 cookie 种下
	resp1, err := client.Get(xloginURL)
	if err != nil {
		return "", nil, fmt.Errorf("xlogin: %w", err)
	}
	resp1.Body.Close()
	cks := client.Jar.Cookies(mustURL(xloginURL))
	names := make([]string, 0, len(cks))
	for _, c := range cks {
		names = append(names, c.Name)
	}
	addTrace(fmt.Sprintf("② xlogin 完成（HTTP %d，cookie %d 个：%s）",
		resp1.StatusCode, len(cks), strings.Join(names, ",")))
	addTrace("   xlogin URL: " + xloginURL)

	// ③ 申请二维码（带 Referer：浏览器发这个请求时也带着 xlogin 地址）
	qrURL := "https://xui.ptlogin2.qq.com/ssl/ptqrshow?" + url.Values{
		"s":          {"8"},
		"e":          {"0"},
		"appid":      {ptAppID},
		"type":       {"0"},
		"t":          {fmt.Sprintf("%.17f", rand.Float64())},
		"u1":         {sURL},
		"daid":       {ptDaid},
		"pt_3rd_aid": {qqAppID},
	}.Encode()

	reqQR, _ := http.NewRequest("GET", qrURL, nil)
	reqQR.Header.Set("Referer", xloginURL)
	reqQR.Header.Set("User-Agent", uaString)
	resp, err := client.Do(reqQR)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	img, _ := io.ReadAll(resp.Body)
	sig := ""
	for _, c := range resp.Cookies() {
		if c.Name == "qrsig" {
			sig = c.Value
		}
	}
	if sig == "" {
		return "", nil, fmt.Errorf("没拿到 qrsig")
	}
	if len(img) < 100 || img[0] != 0x89 {
		return "", nil, fmt.Errorf("返回的不是 PNG（%d 字节）", len(img))
	}
	// 轮询也要带同一个 Referer
	curReferer = xloginURL
	addTrace("③ 二维码已就绪")
	return sig, img, nil
}

// uaString 用浏览器的 UA：服务端会按 UA 判断是不是真浏览器。
const uaString = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36 Edg/142.0.0.0"

// curReferer 保存当前二维码对应的 xlogin 地址，轮询时要带上。
var curReferer string

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }

// extractSrc 从 m_authorize 的 HTML 里抠出 var src 的地址，并还原 JS 转义。
//
// 页面里写的是 "\x3A\x2F\x2F" 这种转义（:、/、&、%），
// 不还原就直接请求会得到一堆 %5Cx3A。
func extractSrc(html string) string {
	m := regexp.MustCompile(`var\s+src\s*=\s*"([^"]+)"`).FindStringSubmatch(html)
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

var reJSEscape = regexp.MustCompile(`\\x[0-9A-Fa-f]{2}`)

// waitScan 轮询扫码状态，返回扫码成功后的跳转地址。
//
// 参数照抄浏览器真实请求：域名是 xui.ptlogin2.qq.com/ssl/ptqrlogin，
// 不是 ptqrlogin.qq.com——域名错了永远收不到"已扫"。
func waitScan(qrsig string) (string, bool) {
	// 浏览器实测的 ptqrlogin 参数。最关键的是 pt_openlogin_data：
	// 它把整个 xlogin 的 query string 原样打包传过去，服务端从这里读到
	// redirect_uri=auth://tauth.qq.com/，才会把回调地址生成为带 code 的
	// auth://tauth.qq.com/?code=…；少了它服务端不知道回调目标，只能降级成
	// check_sig 地址（那个域名不可达，等于死路）。
	openLoginData := ""
	if pu, err := url.Parse(curReferer); err == nil {
		openLoginData = pu.RawQuery
	}
	pollURL := "https://xui.ptlogin2.qq.com/ssl/ptqrlogin?" + url.Values{
		"u1":               {sURL},
		"from_ui":          {"1"},
		"type":             {"1"},
		"ptlang":           {"2052"},
		"ptqrtoken":        {strconv.Itoa(hash33(qrsig))},
		"daid":             {ptDaid},
		"aid":              {ptAppID},
		"pt_3rd_aid":       {qqAppID},
		"pt_openlogin_data": {openLoginData},
		"device":           {"2"},
		"ptopt":            {"1"},
		"pt_uistyle":       {"35"},
		"jsver":            {"3ea7b82c"},
		"r":                {fmt.Sprintf("%.16f", rand.Float64())},
	}.Encode()
	addTrace(fmt.Sprintf("   轮询参数含 pt_openlogin_data（%d 字节）", len(openLoginData)))

	for i := 0; i < 40; i++ {
		time.Sleep(2 * time.Second)
		req, _ := http.NewRequest("GET", pollURL, nil)
		// 注意：qrsig 已经塞进 cookiejar（见 newQR），这里不能再手动设 Cookie 头——
		// 手动设会覆盖 jar 里的 pt_login_sig / pt_local_token，服务端认不出会话，
		// 就会把回调降级成 check_sig 地址（那个域名不可达）。这是踩过的坑。
		req.Header.Set("Referer", curReferer)
		req.Header.Set("User-Agent", uaString)
		// 浏览器里 ptqrlogin 是页面 JS 发的 XHR，会带 Origin / Accept 这些头。
		// 少了它们服务端会当成普通请求，回调降级成 check_sig。
		req.Header.Set("Origin", "https://xui.ptlogin2.qq.com")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		r, err := client.Do(req)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body := string(b)

		switch ptuiCode(body) {
		case "65": // 过期
			addTrace("二维码过期，换一张")
			return "", false
		case "66": // 未扫
			continue
		case "67": // 已扫待确认
			setStatus("已扫码，请在手机上点「确认登录」")
			continue
		case "0":
			u := ptuiURL(body)
			addTrace("扫码确认成功，原始响应: " + body)
			// 首选：服务端直接在回调地址里给了 code（浏览器实测就是这个形态）
			if code := extractCode(u); code != "" {
				addTrace("✓ 回调里直接带 code: " + truncate(code, 12) + "…")
				if atk, openid, rtk, exp, ptk, err := qqCodeLogin(code); err == nil {
					addTrace(fmt.Sprintf("✓ code 换 token 成功！atk=%s openid=%s %.1f 天",
						truncate(atk, 12), openid, float64(exp)/86400))
					addTrace("rtk=" + truncate(rtk, 16))
					addTrace("rtk=" + truncate(rtk, 16))
					setStatus("登录成功")
					finish(atk, openid, ptk)
					return "", false
				} else {
					addTrace("  QQCodeLogin 失败: " + truncate(err.Error(), 140))
				}
			}
			ptsigx := extractPtsigx(u)
			if ptsigx != "" {
				addTrace("ptsigx: " + truncate(ptsigx, 24) + "…")
				// 兜底：ptsigx 直接喂 QQCodeLogin（历史上验证过不行，留作探测）
				addTrace("尝试：ptsigx 直接喂 QQCodeLogin")
				if atk, openid, _, exp, ptk, err := qqCodeLogin(ptsigx); err == nil {
					addTrace(fmt.Sprintf("✓ 直接成功！atk=%s openid=%s %.1f 天",
						truncate(atk, 12), openid, float64(exp)/86400))
					setStatus("ptsigx 直接换 token 成功")
					finish(atk, openid, ptk)
					return "", false
				} else {
					addTrace("  失败: " + truncate(err.Error(), 120))
				}
			}
			addTrace("尝试：走 check_sig")
			return u, true
		default:
			// 打出来才知道服务端在说什么（常见：66 未扫、67 已扫待确认、65 失效）
			if i%5 == 0 {
				addTrace("轮询返回: " + truncate(body, 120))
			}
		}
	}
	return "", false
}

func hash33(s string) int {
	e := 0
	for _, c := range s {
		e += (e << 5) + int(c)
	}
	return 2147483647 & e
}

var rePtui = regexp.MustCompile(`ptuiCB\('(\d+)','(\d+)','([^']*)'`)

func ptuiCode(body string) string {
	if m := rePtui.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return ""
}

func ptuiURL(body string) string {
	if m := rePtui.FindStringSubmatch(body); m != nil {
		return m[3]
	}
	return ""
}

func followForCode(raw string) (string, error) {
	// check_sig 的原域名 ptlogin4.openmobile.qq.com 在全球 DNS 里都是占位地址
	// 0.0.0.1（权威记录就是它，不是污染），直连必然失败。
	// 同一个 check_sig 服务还有别的入口域名，按「最可能对」的顺序逐个试。
	hosts := []string{"ptlogin4.qq.com", "ptlogin2.qq.com"}
	var lastErr error
	for _, h := range hosts {
		try := strings.Replace(raw, "ptlogin4.openmobile.qq.com", h, 1)
		try = strings.Replace(try, "http://", "https://", 1)
		addTrace("check_sig 走 " + h + ": " + truncate(try, 130))
		code, err := followRedirectsForCode(try)
		if err == nil && code != "" {
			addTrace("✓ " + h + " 拿到了 code=" + truncate(code, 10) + "…")
			return code, nil
		}
		lastErr = err
		if err != nil {
			addTrace("  " + h + " 失败: " + truncate(err.Error(), 150))
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("所有入口域名都没拿到 code")
	}
	return "", lastErr
}

// followRedirectsForCode 跟着跳转找 code。check_sig 成功时会 302 到
// auth://tauth.qq.com/?code=xxx —— 这个 Location 不是 http 开头，
// 不能按相对路径处理，要直接从里面抠 code。
func followRedirectsForCode(raw string) (string, error) {
	reCode := regexp.MustCompile(`[?&]code=([0-9A-Fa-f]{16,})`)
	cur := raw
	for i := 0; i < 6; i++ {
		u, err := url.Parse(cur)
		if err != nil {
			return "", err
		}
		if c := u.Query().Get("code"); c != "" {
			return c, nil
		}
		req, _ := http.NewRequest("GET", cur, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
		r, err := client.Do(req)
		if err != nil {
			return "", err
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()

		loc := r.Header.Get("Location")
		if m := reCode.FindStringSubmatch(loc); m != nil {
			return m[1], nil
		}
		if m := reCode.FindStringSubmatch(string(b)); m != nil {
			return m[1], nil
		}
		if loc == "" {
			return "", fmt.Errorf("HTTP %d，无 Location：%s", r.StatusCode, truncate(string(b), 160))
		}
		if strings.HasPrefix(loc, "auth://") {
			return "", fmt.Errorf("跳到 auth:// 但没带 code：%s", truncate(loc, 140))
		}
		if !strings.HasPrefix(loc, "http") {
			base := *u
			base.Path = loc
			loc = base.String()
		}
		cur = loc
	}
	return "", fmt.Errorf("跳转次数过多")
}

func qqCodeLogin(code string) (atk, openid, rtk string, expires int, ptk string, err error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := fmt.Sprintf(`{"appID":"%s","loginCode":"%s"}`, qqAppID, code)
	signStr := "POST\n/cmd/QQCodeLogin\njson\nysdk\n" + ts + "\n" + body
	mac := hmac.New(sha256.New, []byte(codeLoginKey))
	mac.Write([]byte(signStr))
	digest := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest("POST", "https://ysdk.qq.com/cmd/QQCodeLogin?", strings.NewReader(body))
	req.Header.Set("Content-Type", "json")
	req.Header.Set("Auth-Secret-ID", "ysdk")
	req.Header.Set("Auth-Secret-Digest", digest)
	req.Header.Set("Auth-Request-Time", ts)

	r, err := client.Do(req)
	if err != nil {
		return "", "", "", 0, "", err
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)

	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Ret          int    `json:"ret"`
			AccessToken  string `json:"accessToken"`
			OpenID       string `json:"openID"`
			RefreshToken string `json:"refreshToken"`
			PayToken     string `json:"payToken"`
			ExpiresIn    string `json:"expiresIn"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", "", "", 0, "", fmt.Errorf("响应不是 JSON: %s", truncate(string(b), 200))
	}
	if out.Code != 0 {
		return "", "", "", 0, "", fmt.Errorf("code=%d msg=%s body=%s", out.Code, out.Msg, truncate(string(b), 240))
	}
	if out.Data.Ret != 0 || out.Data.AccessToken == "" {
		return "", "", "", 0, "", fmt.Errorf("data.ret=%d body=%s", out.Data.Ret, truncate(string(b), 240))
	}
	exp, _ := strconv.Atoi(out.Data.ExpiresIn)
	return out.Data.AccessToken, out.Data.OpenID, out.Data.RefreshToken, exp, out.Data.PayToken, nil
}

func qqVerifyLogin(atk, openid, ptk string) error {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// 参数照抄真机请求：缺一个就回 11000 missing essential parameters
	q := url.Values{
		"channel":             {"10095285"},
		"offerid":             {qqAppID},
		"atk":                 {atk},
		"ptk":                 {ptk},
		"openid":              {openid},
		"loginType":           {"0"},
		"loginplatform":       {"1"},
		"platform":            {"desktop_m_qq_m"},
		"isCheckATK":          {"1"},
		"isCheckPTK":          {"1"},
		"version":             {"2.2.2"},
		"appid":               {qqAppID},
		"qq_appid":            {qqAppID},
		"channel_id":          {"10095285"},
		"source_channel":      {"10095285"},
		"regChannel":          {"00000000"},
		"client_hope_switch":  {"1"},
		"anti_hope_switch":    {"1"},
		"visitor_switch":      {"-1"},
		"yyb_install":         {"1"},
		"wx_appid":            {"wx365db603f56bf4da"},
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
		"sig":                 {md5Hex(ts + "5C2F##3[6$^(68#%#D3E96;]35q#FB46")},
	}
	r, err := client.Get("https://ysdk.qq.com/auth/qq_verify_login?" + q.Encode())
	if err != nil {
		return err
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return fmt.Errorf("响应不是 JSON: %s", truncate(string(b), 200))
	}
	if ret, _ := out["ret"].(float64); ret != 0 {
		return fmt.Errorf("ret=%v msg=%v body=%s", out["ret"], out["msg"], truncate(string(b), 240))
	}
	return nil
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", sum)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var pageTpl = template.Must(template.New("p").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>QQ 扫码验证</title>
<style>
body{font:14px/1.6 system-ui,'Microsoft YaHei',sans-serif;background:#14120c;color:#e8e2d4;
     display:flex;justify-content:center;padding:28px;margin:0}
.wrap{width:760px}
h1{font-size:17px;margin:0 0 6px}
.tip{color:#8d8677;font-size:12px;margin-bottom:16px}
.card{background:#1c1a14;border:1px solid #2e2a20;border-radius:10px;padding:18px;display:flex;gap:20px}
img{width:220px;height:220px;background:#fff;border-radius:6px;image-rendering:pixelated}
.right{flex:1;min-width:0}
#st{font-size:15px;margin-bottom:10px;color:#d8b47c}
pre{background:#0f0e0a;border:1px solid #2e2a20;border-radius:6px;padding:10px;
    font-size:11.5px;line-height:1.55;max-height:320px;overflow:auto;white-space:pre-wrap;
    word-break:break-all;color:#b9b2a2;margin:0}
</style></head><body><div class="wrap">
<h1>QQ 扫码 → 应用宝登录态 验证</h1>
<div class="tip">二维码每 80 秒自动更换，页面会自动刷新，随时扫都是新的</div>
<div class="card">
  <img id="q" src="/qr.png" alt="二维码">
  <div class="right"><div id="st">…</div><pre id="tr"></pre></div>
</div>
</div>
<script>
async function tick(){
  try{
    const r = await fetch('/state',{cache:'no-store'});
    const j = await r.json();
    document.getElementById('st').textContent = j.status + (j.genAt ? '（本张生成于 ' + j.genAt + '）' : '');
    document.getElementById('tr').textContent = (j.trace||[]).join('\n');
    document.getElementById('q').src = '/qr.png?t=' + Date.now();
  }catch(e){}
}
tick(); setInterval(tick, 1500);
</script></body></html>`))


// extractCode 从回调地址里抠出 QQ 互联的授权码。
// 形如 http://auth://tauth.qq.com/?code=B3BA2E11…&ret=0&pf=…&pfkey=…
func extractCode(s string) string {
	m := regexp.MustCompile(`[?&]code=([0-9A-Fa-f]{16,})`).FindStringSubmatch(s)
	if m != nil {
		return m[1]
	}
	return ""
}

// extractPtsigx 从 check_sig 地址里抠出 ptsigx。
func extractPtsigx(raw string) string {
	m := regexp.MustCompile(`[?&]ptsigx=([0-9A-Fa-f]+)`).FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	return m[1]
}

// finish 用 atk 走完最后的校验，把结果写进日志。
func finish(atk, openid, ptk string) {
	addTrace("正在走 qq_verify_login…")
	if err := qqVerifyLogin(atk, openid, ptk); err != nil {
		addTrace("✗ qq_verify_login 失败: " + truncate(err.Error(), 200))
		setStatus("qq_verify_login 失败")
		return
	}
	addTrace("✓✓ qq_verify_login 通过 —— 全链路纯 HTTP 成立")
	setStatus("全链路纯 HTTP 成立")
}





