package channel

// vivo 渠道服接入。
//
// 和 B站/4399 的"抓 token"不同，vivo 的持久凭证是浏览器 Cookie 会话：
//
//	登录：passport.vivo.com.cn 通行证登录 → 带 openid 跳回
//	      joint.vivo.com.cn/game-subaccount-login
//	取号：凭 Cookie 同源调 /h5/union/get?gamePackage=… 拿 openId + 小号列表
//	换票：POST /h5/union/use {subOpenId, gamePackage} → openToken
//	游戏：openToken 即 uni_sauth 的 session 与 confirm_login 的 token
//
// 所以 joint API 的调用放进注入脚本（同源请求自动带 Cookie），
// Go 侧只拿最终的 subOpenId + openToken。Cookie 由 WebView2 的 profile
// 目录持久化：按小号分目录，重开同一小号的窗口时 Cookie 还活着就是
// 静默续期，过期了才需要重新登录。
//
// openToken 没有独立的在线校验接口，uni_sauth 本身就是验证。
// 一个 vivo 主账号可带多个游戏小号，subOpenId 各自独立成账号。

import "encoding/json"

const (
	// VivoPassportURL vivo 通行证登录页。
	// client_id=67 是 vivo 联合登录平台的应用 id（各游戏通用）。
	VivoPassportURL = "https://passport.vivo.com.cn/#/login?client_id=67" +
		"&redirect_uri=https%3A%2F%2Fjoint.vivo.com.cn%2Fgame-subaccount-login%3Ffrom%3Dlogin"
	// VivoGamePackage 光遇 vivo 渠道服的安卓包名。
	VivoGamePackage = "com.netease.sky.vivo"
)

// VivoLoginScript 返回注入到 vivo 登录流程里的脚本。
//
// target 非空表示给指定小号刷新：脚本自动选中它，不再弹选择列表。
//
// 页面本身是一整张广告 banner，登录表单是叠在上面的浮层；脚本把广告
// 藏掉、表单改成全窗口居中（手机版形态）。跳到 joint 域名后这些
// 选择器不匹配，无副作用。
func VivoLoginScript(target string) string {
	return `(function () {
  if (window.__skyVivoHooked) return;
  window.__skyVivoHooked = true;

  var GAME_PACKAGE = "` + VivoGamePackage + `";
  var TARGET = ` + vivoJSString(target) + `;

  function send(payload) {
    try { window.__skyloginPayload(JSON.stringify(payload)); } catch (e) {}
  }
  function logNet() {
    // 只报 host+path：跳转 URL 里带 openid，不能整条落日志
    try { send({ kind: "net", url: location.host + location.pathname, status: 0, body: "" }); }
    catch (e) {}
  }
  logNet();

  function banner(text) {
    try {
      var el = document.getElementById("__sky_vivo_banner");
      if (el) { el.textContent = text; return; }
      el = document.createElement("div");
      el.id = "__sky_vivo_banner";
      el.textContent = text;
      el.style.cssText =
        "position:fixed;left:0;right:0;bottom:0;z-index:2147483647;pointer-events:none;" +
        "padding:8px 12px;font:12px/1.5 sans-serif;background:#415fff;color:#fff;text-align:center;";
      var root = document.body || document.documentElement;
      if (!root) return;
      root.appendChild(el);
    } catch (e) {}
  }
  setTimeout(function () { banner("请在窗口里登录 vivo 账号，完成后会自动保存并关闭窗口"); }, 800);

  // ── 页面瘦身：藏广告图与 logo，登录表单全窗口居中 ──────────
  function slim() {
    // 极简错误页上脚本跑得比 HTML 解析还早：head/documentElement 可能
    // 还是 null，这里任何异常都不能外泄——否则整个脚本会当场死掉
    try {
      var st = document.getElementById("__sky_vivo_slim");
      if (st) return;
      var root = document.head || document.documentElement;
      if (!root) return;
      st = document.createElement("style");
      st.id = "__sky_vivo_slim";
      st.textContent = [
        "img.banner-img, .banner-img { display: none !important; }",
        ".logo { display: none !important; }",
        ".banner { width: 100% !important; height: 100vh !important; min-height: 0 !important; overflow: hidden !important; background: #101216 !important; }",
        ".login.pc-login-banner-content { position: fixed !important; left: 50% !important; top: 50% !important; transform: translate(-50%,-50%) !important; margin: 0 !important; max-height: 96vh !important; overflow-y: auto !important; }",
        "body, html { overflow: hidden !important; margin: 0 !important; height: 100% !important; }",
        "body::-webkit-scrollbar, html::-webkit-scrollbar { display: none !important; width: 0 !important; }"
      ].join("\n");
      root.appendChild(st);
    } catch (e) {}
  }
  setTimeout(slim, 0);
  setTimeout(slim, 300);
  setTimeout(slim, 1200);

  // ── 小号选择与换票 ────────────────────────────────────────
  var reported = false;
  var picked = false;

  function reportLogin(uid, secret, uname) {
    if (reported) return;
    reported = true;
    banner("登录成功，正在保存…");
    send({ kind: "login", uid: String(uid), secret: String(secret), uname: uname || "" });
  }

  // joint 接口回包只记 code/msg（不落 openId / openToken 的值）
  function logApi(name, resp) {
    try {
      send({ kind: "net", url: name + " code=" + (resp && resp.code) + " msg=" + ((resp && resp.msg) || ""), status: 0, body: "" });
    } catch (e) {}
  }
  function logLine(s) {
    try { send({ kind: "net", url: s, status: 0, body: "" }); } catch (e) {}
  }

  // 接口偶发返回非 JSON（服务端抽风时给 HTML 错误页），这里统一收成
  // {data 或 rawText}，调用方据此重试或记录
  function apiGet(path) {
    return fetch(path, { credentials: "same-origin" }).then(function (r) {
      return r.text().then(function (t) {
        var data = null;
        try { data = JSON.parse(t); } catch (e) {}
        return { raw: t, data: data };
      });
    });
  }
  function apiUseOnce(subOpenId) {
    var body = new URLSearchParams();
    body.set("noLoading", "true");
    body.set("subOpenId", subOpenId);
    body.set("gamePackage", GAME_PACKAGE);
    return fetch("/h5/union/use", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
      body: body.toString()
    }).then(function (r) {
      return r.text().then(function (t) {
        var data = null;
        try { data = JSON.parse(t); } catch (e) {}
        return { raw: t, data: data };
      });
    });
  }

  function use(sub, master) {
    banner("正在获取登录凭证…");
    // union/use 偶发被拒（服务端繁忙 / 会话未就绪），重试几次；一直不行
    // 就用账号列表里现成的 openToken 兜底（union/get 的响应里带着）
    var tries = 0;
    function go() {
      tries++;
      apiUseOnce(String(sub.subOpenId)).then(function (r) {
        if (r.data) logApi("union/use#" + tries, r.data);
        else logLine("union/use#" + tries + " non-json: " + r.raw.slice(0, 80));
        if (r.data && r.data.code === 0 && r.data.data) {
          var name = master ? (master + "-" + (sub.nickName || "")) : (sub.nickName || "");
          reportLogin(sub.subOpenId, r.data.data, name);
          return;
        }
        if (tries < 3) {
          banner("服务器繁忙，正在重试（" + tries + "/2）…");
          setTimeout(go, 2000);
          return;
        }
        if (sub.openToken) {
          banner("换票接口繁忙，使用账号信息里的凭证");
          var name = master ? (master + "-" + (sub.nickName || "")) : (sub.nickName || "");
          reportLogin(sub.subOpenId, sub.openToken, name);
          return;
        }
        banner("获取凭证失败（" + ((r.data && r.data.msg) || "未知错误") + "），请重新登录");
        picked = false;
      }).catch(function () {
        if (tries < 3) { setTimeout(go, 2000); return; }
        if (sub.openToken) {
          var name = master ? (master + "-" + (sub.nickName || "")) : (sub.nickName || "");
          reportLogin(sub.subOpenId, sub.openToken, name);
          return;
        }
        banner("网络错误，获取凭证失败，请重试");
        picked = false;
      });
    }
    go();
  }

  function pickAccount(data) {
    if (picked) return;
    picked = true;
    var subs = (data && data.subAccounts) || [];
    var master = (data && (data.nickName || data.phone)) || "";
    if (!subs.length) {
      // 还没登录完：放开让用户继续操作，下次跳转再试
      picked = false;
      return;
    }
    var selected = null;
    if (TARGET) {
      for (var i = 0; i < subs.length; i++) {
        if (String(subs[i].subOpenId) === TARGET) { selected = subs[i]; break; }
      }
      if (!selected) banner("这个 vivo 账号里没有找到要刷新的小号，请在列表里选择");
    } else if (subs.length === 1) {
      selected = subs[0];
    }
    if (selected) { use(selected, master); return; }

    // 多个小号：窗口里渲染一个点选列表
    banner("检测到多个小号，请点击要登录的账号");
    var box = document.createElement("div");
    box.id = "__sky_vivo_picker";
    box.style.cssText =
      "position:fixed;left:50%;top:50%;transform:translate(-50%,-50%);z-index:2147483646;" +
      "background:#fff;border-radius:12px;padding:16px;box-shadow:0 12px 48px rgba(0,0,0,.35);" +
      "font:13px/1.6 sans-serif;min-width:260px;";
    var title = document.createElement("div");
    title.textContent = "选择要登录的小号";
    title.style.cssText = "font-weight:600;margin-bottom:10px;color:#222;";
    box.appendChild(title);
    subs.forEach(function (s) {
      var b = document.createElement("button");
      b.textContent = (s.nickName || "默认帐号") + (s.lastLogin ? "（上次登录）" : "");
      b.style.cssText =
        "display:block;width:100%;margin:6px 0;padding:10px 12px;border:1px solid #e3e5eb;" +
        "border-radius:8px;background:#f7f8fa;cursor:pointer;font-size:13px;text-align:left;";
      b.onmouseover = function () { b.style.background = "#eef1ff"; };
      b.onmouseout = function () { b.style.background = "#f7f8fa"; };
      b.onclick = function () { box.remove(); use(s, master); };
      box.appendChild(b);
    });
    (document.body || document.documentElement).appendChild(box);
  }

  // 只在 joint.vivo.com.cn 的页面上调 API（同源才带得上会话 Cookie）。
  // 落地页自己偶尔会渲染"系统繁忙"（服务端抽风，接口层仍可用，实测几分
  // 钟内自己恢复），所以这里不看页面长什么样，只管把接口轮询到通为止，
  // 窗口里同步显示重试进度。
  function maybeCapture() {
    if (reported || picked) return;
    if (location.hostname !== "joint.vivo.com.cn") return;
    var tries = 0;
    var MAX = 40; // 40 次 × 3 秒 ≈ 2 分钟
    function go() {
      if (reported || picked) return;
      tries++;
      try {
        apiGet("/h5/union/get?gamePackage=" + encodeURIComponent(GAME_PACKAGE)).then(function (r) {
          if (r.data) logApi("union/get#" + tries, r.data);
          else logLine("union/get#" + tries + " non-json: " + r.raw.slice(0, 80));
          if (r.data && r.data.code === 0 && r.data.data) {
            pickAccount(r.data.data);
            return;
          }
          if (tries < MAX) {
            banner("vivo 服务器繁忙，自动重试中（第 " + tries + " 次）…");
            setTimeout(go, 3000);
          } else {
            banner("vivo 服务器持续繁忙，请点页面上的「刷新」按钮或稍后再试");
          }
        }).catch(function () {
          if (tries < MAX) setTimeout(go, 3000);
          else banner("网络错误，请点页面上的「刷新」按钮重试");
        });
      } catch (e) {
        logLine("poll error: " + e);
        if (tries < MAX) setTimeout(go, 3000);
      }
    }
    setTimeout(go, 1500);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", maybeCapture);
  } else {
    maybeCapture();
  }
})();`
}

// vivoJSString 把 Go 字符串安全嵌进 JS（小号 id 只会是数字，防御性转义）。
func vivoJSString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
