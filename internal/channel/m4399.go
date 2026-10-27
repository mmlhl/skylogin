package channel

// 4399 渠道服接入。
//
// 登录走 4399 开放平台的 OAuth：先向 oauth.html 要一个授权页地址，
// 用户在页面里完成 4399 通行证登录后，浏览器自然跳到 callback 页
// （正文是 JSON，refresh_token 只出现在这次的 URL query 里），脚本
// 从中取到 uid / state / access_token / refresh_token。
//
// 凭证的两半用途不同：state 是 uni_sauth 的 session；access_token 是
// confirm_login 表单里的 token（app 层落库时分开存）。
//
// 静默续期链（oauth-check → refresh_token 轮换 → state 兜底恢复）：
//   - oauth-check 给出 state 的在线真值
//   - 两个刷新接口的成功响应都不含 refresh_token，轮换后必须保留旧值
//   - refresh_token 只在网页登录的 callback URL 里出现过一次，丢了
//     就只能重新登录

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// 渠道身份取自官方渠道配置：app_key 是 4399 平台级应用 id
	// （各游戏通用），包名是光遇 4399 渠道服的安卓包名。
	M4399GameKey     = "114816"
	M4399PackageName = "com.netease.sky.m4399"

	m4399OauthURL        = "https://m.4399api.com/openapiv2/oauth.html"
	m4399OauthCheckURL   = "http://m.4399api.com/openapiv2/oauth-check.html"
	m4399OauthRefreshURL = "https://m.4399api.com/openapiv2/oauth-getinfobyrefresh.html"

	m4399HTTPTimeout = 20 * time.Second
)

// M4399Device 是 4399 侧的稳定设备身份。凭证与它绑定，
// 首次生成后必须长期复用（app 层负责落盘）。
type M4399Device struct {
	DeviceIdentifier string `json:"device_identifier"`
	Udid             string `json:"udid"`
}

// NewM4399Device 生成一份新设备身份。UDID 为 4399 SDK 风格的 62 位：
// 时间戳(14) + md5 随机数(32) + "00" + 校验尾(14)。
func NewM4399Device() M4399Device {
	prefix := time.Now().Format("20060102150405") + m4399Random() + "00"
	return M4399Device{
		DeviceIdentifier: "pc-skylogin",
		Udid:             prefix + m4399MD5("cn.m4399.uuid" + prefix)[:14],
	}
}

func m4399MD5(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func m4399Random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// payload 组装 OAuth 请求里的 device 表单值（SDK 上报的元数据字段）。
func (d M4399Device) payload(uid string) map[string]any {
	return map[string]any{
		"DEVICE_IDENTIFIER": d.DeviceIdentifier,
		"PLATFORM_TYPE":     "android",
		"SDK_VERSION":       m4399Info.SDKVersion,
		"GAME_KEY":          M4399GameKey,
		"GAME_VERSION":      "1.0.286",
		"GAME_VERSION_CODE": 286,
		"BID":               M4399PackageName,
		"RUNTIME":           "android",
		"CANAL_IDENTIFIER":  m4399Info.Login,
		"UDID":              d.Udid,
		"DEBUG":             "false",
		"VIP_INFO":          "",
		"TEAM":              0,
		"UID":               uid,
		"SCREEN_RESOLUTION": "1080*1920",
		"DEVICE_MODEL":      "Pixel 7",
		"SYSTEM_VERSION":    "13",
		"NETWORK_TYPE":      "wifi",
	}
}

// M4399Login 是一次成功登录 / 刷新换回的登录材料。
type M4399Login struct {
	UID         string
	State       string // uni_sauth 的 session
	AccessToken string // confirm_login 的 token
	Nick        string
}

// Complete 网页登录的材料必须齐全；刷新接口只保证前三样。
func (r M4399Login) Complete() bool {
	return r.UID != "" && r.State != "" && r.AccessToken != ""
}

func m4399Post(endpoint string, dev M4399Device, uid string, extra map[string]string) (map[string]any, error) {
	form := url.Values{}
	devJSON, _ := json.Marshal(dev.payload(uid))
	form.Set("device", string(devJSON))
	for k, v := range extra {
		form.Set(k, v)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36")

	client := &http.Client{Timeout: m4399HTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("响应解析失败")
	}
	return out, nil
}

// m4399Code 把响应 code 统一成字符串（数字 / 字符串都可能出现）。
func m4399Code(out map[string]any) string {
	switch v := out["code"].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

func m4399Str(out map[string]any, path ...string) string {
	cur := out
	for i, p := range path {
		m, ok := cur[p].(map[string]any)
		if !ok {
			return ""
		}
		if i == len(path)-1 {
			s, _ := m["result"].(string)
			return s
		}
		cur = m
	}
	return ""
}

// m4399Parse 把成功响应转成登录材料；code 非 100/200 或材料不全返回 false。
func m4399Parse(out map[string]any) (M4399Login, bool) {
	code := m4399Code(out)
	if code != "100" && code != "200" {
		return M4399Login{}, false
	}
	result, _ := out["result"].(map[string]any)
	str := func(key string) string { s, _ := result[key].(string); return s }
	res := M4399Login{
		UID:         str("uid"),
		State:       str("state"),
		AccessToken: str("access_token"),
		Nick:        str("nick"),
	}
	if res.Nick == "" {
		res.Nick = str("username")
	}
	return res, res.Complete()
}

// M4399LoginURL 每次网页登录都向 oauth.html 要一个新的授权页地址。
func M4399LoginURL(dev M4399Device) (string, error) {
	out, err := m4399Post(m4399OauthURL, dev, "", map[string]string{"top_bar": "1"})
	if err != nil {
		return "", err
	}
	if m4399Code(out) != "607" {
		return "", fmt.Errorf("4399 授权接口未返回登录页（code=%s）", m4399Code(out))
	}
	result, _ := out["result"].(map[string]any)
	u, _ := result["login_url"].(string)
	if u == "" {
		u, _ = result["login_url_backup"].(string)
	}
	if u == "" {
		return "", fmt.Errorf("4399 授权接口返回里没有 login_url")
	}
	return u, nil
}

// M4399Check oauth-check：state 的在线真值。返回 error 表示网络原因查不了，
// 此时不能断言凭证无效。
func M4399Check(uid, state string) (bool, error) {
	if uid == "" || state == "" {
		return false, nil
	}
	q := url.Values{"state": {state}, "uid": {uid}, "key": {M4399GameKey}}
	req, err := http.NewRequest(http.MethodGet, m4399OauthCheckURL+"?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36")
	client := &http.Client{Timeout: m4399HTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return m4399Code(out) == "200", nil
}

// M4399RefreshByToken 用 refresh_token 换新登录材料（响应不含 refresh_token）。
func M4399RefreshByToken(dev M4399Device, uid, refreshToken string) (M4399Login, error) {
	if uid == "" || refreshToken == "" {
		return M4399Login{}, fmt.Errorf("缺少 refresh_token")
	}
	out, err := m4399Post(m4399OauthRefreshURL, dev, uid, map[string]string{
		"refresh_token": refreshToken,
		"source":        "4399",
		"cloud_ext":     "",
	})
	if err != nil {
		return M4399Login{}, err
	}
	if res, ok := m4399Parse(out); ok {
		return res, nil
	}
	return M4399Login{}, fmt.Errorf("refresh_token 轮换失败（code=%s）", m4399Code(out))
}

// M4399RefreshByState 用业务 state 走 refresh=1 兜底恢复。
func M4399RefreshByState(dev M4399Device, state string) (M4399Login, error) {
	if state == "" {
		return M4399Login{}, fmt.Errorf("缺少 state")
	}
	out, err := m4399Post(m4399OauthURL, dev, "", map[string]string{
		"state":   state,
		"source":  "4399",
		"refresh": "1",
	})
	if err != nil {
		return M4399Login{}, err
	}
	if res, ok := m4399Parse(out); ok {
		return res, nil
	}
	return M4399Login{}, fmt.Errorf("state 恢复失败（code=%s）", m4399Code(out))
}

// M4399LoginScript 返回注入到 4399 授权流程里的脚本。
//
// 授权链路会经历多次导航，脚本在每次文档创建时执行；只有落在
// oauth-callback.html（正文是 JSON）时才采集：
//   - refresh_token 只在 callback 的 URL query 里
//   - uid / state / access_token 在页面正文的 JSON 里
func M4399LoginScript() string {
	return `(function () {
  if (window.__skyM4399Hooked) return;
  window.__skyM4399Hooked = true;

  function send(payload) {
    try { window.__skyloginPayload(JSON.stringify(payload)); } catch (e) {}
  }
  function logNet(url) {
    // 只报 host+path：callback 的 query 里有 refresh_token，不能整条落日志
    try { send({ kind: "net", url: location.host + location.pathname, status: 0, body: "" }); }
    catch (e) {}
  }
  logNet();

  function banner(text) {
    var el = document.getElementById("__sky_m4399_banner");
    if (el) { el.textContent = text; return; }
    el = document.createElement("div");
    el.id = "__sky_m4399_banner";
    el.textContent = text;
    el.style.cssText =
      "position:fixed;left:0;right:0;bottom:0;z-index:2147483647;pointer-events:none;" +
      "padding:8px 12px;font:12px/1.5 sans-serif;background:#1a72d1;color:#fff;text-align:center;";
    (document.body || document.documentElement).appendChild(el);
  }
  setTimeout(function () { banner("请在窗口里登录 4399 账号，完成后会自动保存并关闭窗口"); }, 800);

  var reported = false;
  function isCallback() {
    return location.hostname === "m.4399api.com" &&
      location.pathname.indexOf("/openapi/oauth-callback.html") === 0;
  }

  function capture() {
    if (reported || !isCallback()) return;
    var mRT = /[?&]refresh_token=([^&]*)/.exec(location.search);
    var refreshToken = "";
    if (mRT) {
      try { refreshToken = decodeURIComponent(mRT[1].replace(/\+/g, " ")); }
      catch (e) { refreshToken = mRT[1]; }
    }
    // 正文是 JSON 文档，等 body 渲染出来再读
    var tries = 0;
    var timer = setInterval(function () {
      tries++;
      if (reported || tries > 100) { clearInterval(timer); return; }
      var text = document.body ? (document.body.innerText || "") : "";
      if (!text) return;
      var data = null;
      try { data = JSON.parse(text); } catch (e) { return; }
      clearInterval(timer);
      var code = Number(data && data.code);
      var r = data && data.result;
      if ((code === 100 || code === 200) && r && r.uid && r.state && r.access_token) {
        reported = true;
        banner("登录成功，正在保存…");
        send({
          kind: "login",
          uid: String(r.uid),
          secret: String(r.state),
          uname: r.nick || r.username || "",
          extra: { accessToken: String(r.access_token), refreshToken: refreshToken }
        });
      }
    }, 200);
  }

  capture();
})();`
}
