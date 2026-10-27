package channel

// B站渠道服接入。
//
// 登录走官方 PC 网页（sdk.biligame.com/login）：页面内部完成全部验证
// （账密 / OTP / 扫码，含极验），注入脚本只负责从登录接口的响应里把
// uid + access_key 抓回来。
//
// access_key 即 uni_sauth 的 session，也是 confirm_login 的 token，
// 一个值两处用。续期：auto.login 接口验证活性的同时顺带续 30 天，
// 所以每次启动前验证一次就是续期。
//
// 签名规则（实测）：参数按 key 排序拼值 + app_key 后取 MD5；
// auto.login 只发三个核心参数，多带 sdk_ver / timestamp 反而会 sign error。

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	// BiliGameID 光遇 B站服的 bili game id。
	BiliGameID = "183"
	// BiliAppKey biligame 平台应用标识（B站 SDK 各游戏通用）。
	BiliAppKey = "h9Ejat5tFh81cq8"

	biliAutoLoginURL = "https://wpg-api.biligame.com/api/pcg/auto.login"
)

// BiliClient 是 B站游戏 SDK 的 HTTP 客户端。
type BiliClient struct{ GameID, AppKey string }

// NewBili 用光遇 B站服的默认参数创建客户端。
func NewBili() BiliClient { return BiliClient{GameID: BiliGameID, AppKey: BiliAppKey} }

// biliSign 按 key 排序拼接参数值，末尾追加 app_key 后取 MD5。
func biliSign(params map[string]string, appKey string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(params[k])
	}
	b.WriteString(appKey)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// Verify 验证 access_key 是否可用，返回 B站 uid 与昵称（请求本身会续期 30 天）。
func (c BiliClient) Verify(accessKey string) (uid, uname string, err error) {
	p := map[string]string{
		"access_key": accessKey,
		"game_id":    c.GameID,
		"is_gov_ver": "1",
	}
	q := url.Values{}
	for k, v := range p {
		q.Set(k, v)
	}
	q.Set("sign", biliSign(p, c.AppKey))

	resp, err := http.PostForm(biliAutoLoginURL+"?"+q.Encode(), url.Values{})
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", fmt.Errorf("响应解析失败: %w", err)
	}
	if code, _ := out["code"].(float64); code != 0 {
		return "", "", fmt.Errorf("access_key 已失效（code=%v）", out["code"])
	}
	uid, uname = BiliUID(out["uid"]), uidString(out["uname"])
	return uid, uname, nil
}

// BiliUID 把响应里的 uid 统一转成字符串。JSON 数字解出来是 float64，
// 直接 fmt.Sprint 会输出科学计数法（413918109 → "4.13918109e+08"），
// 游戏服务器不认，必须先转 int64。
func BiliUID(v any) string {
	switch t := v.(type) {
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case string:
		return t
	default:
		return fmt.Sprint(v)
	}
}

func uidString(v any) string {
	s, _ := v.(string)
	return s
}

// BiliLoginScript 返回注入到 B站登录页的脚本。
//
// 页面自己完成全部验证，脚本只做两件事：
//  1. 诊断：把每个请求的 URL / 状态 / 响应片段回传（net 日志排障用）
//  2. 捕获：从 pcg 系登录接口的响应里识别 uid + access_key
//
// 捕获规则（实测的接口形态）：
//   - auto.login：access_key 在请求 URL 上，uid 在响应里，code=0
//   - 其余 pcg 接口（账密 / OTP 登录）：access_key 与 uid 同在响应里
//     （可能在根级，也可能包在 data 里），code=0
//
// XHR 与 fetch 两条通道都汇入同一个 inspect 函数，规则集中一处。
func BiliLoginScript() string {
	return `(function () {
  if (window.__skyBiliHooked) return;
  window.__skyBiliHooked = true;

  // ── 回传管道 ──────────────────────────────────────────────
  function send(payload) {
    try { window.__skyloginPayload(JSON.stringify(payload)); } catch (e) {}
  }
  function logNet(url, status, body) {
    send({ kind: "net", url: url, status: status || 0, body: (body || "").slice(0, 600) });
  }
  var reported = false;
  function reportLogin(uid, secret, uname) {
    if (reported) return;
    reported = true;
    banner("登录成功，正在保存…");
    send({ kind: "login", uid: String(uid), secret: String(secret), uname: uname || "" });
  }

  function banner(text) {
    var el = document.getElementById("__sky_bili_banner");
    if (el) { el.textContent = text; return; }
    el = document.createElement("div");
    el.id = "__sky_bili_banner";
    el.textContent = text;
    el.style.cssText =
      "position:fixed;left:0;right:0;bottom:0;z-index:2147483647;pointer-events:none;" +
      "padding:8px 12px;font:12px/1.5 'Microsoft YaHei UI',sans-serif;" +
      "background:#d8b47c;color:#14120c;text-align:center;";
    document.documentElement.appendChild(el);
  }
  setTimeout(function () {
    banner("请在页面里登录 B站账号（扫码/验证码/密码都行），成功后会自动保存并关闭窗口");
  }, 900);

  // ── 协议配置拦截 ──────────────────────────────────────────
  // 页面启动时会请求 agreement/config 决定是否要弹协议确认；这条请求
  // 在部分页面版本上会失败并卡住登录框。本地直接给一份"无需确认"的
  // 成功应答（真正的协议文本由登录页自己管理，这里只放行）。
  var AGREEMENT_OK = JSON.stringify({
    code: 0, message: "0", success: true,
    data: { cooperation_mode: 0, agreement_switch: "OFF", privacy_tips_switch: "ON" }
  });
  function isAgreementConfig(url) {
    return url.indexOf("/api/agreement/config") !== -1;
  }
  function fakeXHRResponse(xhr, status, body) {
    var props = { readyState: 4, status: status, statusText: "OK", response: body, responseText: body };
    for (var k in props) {
      try { Object.defineProperty(xhr, k, { value: props[k], configurable: true }); } catch (e) {}
    }
    xhr.getAllResponseHeaders = function () { return "content-type: application/json"; };
    xhr.getResponseHeader = function (h) {
      return String(h).toLowerCase() === "content-type" ? "application/json" : null;
    };
    if (typeof xhr.onreadystatechange === "function") xhr.onreadystatechange();
    xhr.dispatchEvent(new Event("load"));
    xhr.dispatchEvent(new Event("loadend"));
  }

  // ── 登录结果识别 ──────────────────────────────────────────
  function parseJSON(text) {
    try { return JSON.parse(text); } catch (e) { return null; }
  }
  // URL 查询串里的 access_key 是编码过的 base64（+ 已转成 %2B），直接解码即可
  function decodeQuery(s) {
    try { return decodeURIComponent(s); } catch (e) { return s; }
  }
  function inspect(url, bodyText) {
    if (url.indexOf("/api/pcg/") === -1) return;
    var data = parseJSON(bodyText);
    if (!data) return;

    if (url.indexOf("/auto.login") !== -1) {
      var m = /access_key=([^&]+)/.exec(url);
      if (data.code === 0 && data.uid && m) {
        reportLogin(data.uid, decodeQuery(m[1]), data.uname);
      }
      return;
    }
    if (data.code !== 0) return;
    var secret = data.access_key || (data.data && data.data.access_key);
    var uid = data.uid || (data.data && data.data.uid);
    var uname = data.uname || (data.data && data.data.uname) || "";
    if (secret && uid) reportLogin(uid, secret, uname);
  }

  // ── 挂钩：XHR 与 fetch 都汇入 inspect ─────────────────────
  var origOpen = XMLHttpRequest.prototype.open;
  var origSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function (method, url) {
    this.__skyURL = typeof url === "string" ? url : String(url);
    return origOpen.apply(this, arguments);
  };
  XMLHttpRequest.prototype.send = function () {
    var xhr = this;
    if (isAgreementConfig(xhr.__skyURL)) {
      setTimeout(function () {
        logNet(xhr.__skyURL, 200, AGREEMENT_OK);
        fakeXHRResponse(xhr, 200, AGREEMENT_OK);
      }, 0);
      return;
    }
    xhr.addEventListener("load", function () {
      logNet(xhr.__skyURL, xhr.status, xhr.responseText);
      try { inspect(xhr.__skyURL, xhr.responseText); } catch (e) {}
    });
    return origSend.apply(this, arguments);
  };

  var origFetch = window.fetch;
  if (origFetch) {
    window.fetch = function (input, init) {
      var url = typeof input === "string" ? input : (input && input.url) || "";
      if (isAgreementConfig(url)) {
        logNet(url, 200, AGREEMENT_OK);
        return Promise.resolve(new Response(AGREEMENT_OK, { status: 200, headers: { "Content-Type": "application/json" } }));
      }
      var p = origFetch.apply(this, arguments);
      p.then(function (res) {
        res.clone().text().then(function (text) {
          logNet(url, res.status, text);
          inspect(url, text);
        }).catch(function () {});
      }).catch(function () {});
      return p;
    };
  }

  window.addEventListener("error", function (ev) {
    logNet("__jserror", 0, String(ev.message || ""));
  });

  try { document.title = "__bili_inited__"; } catch (e) {}
  logNet("__inited", 0, "script executed");
})();`
}
