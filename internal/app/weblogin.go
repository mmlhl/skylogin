package app

// 网页登录：借用网易自己的 H5 登录 SDK 拿 MPay token。
//
// 为什么要绕这一圈：
//   MPay 的账号密码登录请求里，密码是加密的（/devices/{dev}/users 的 params），
//   密钥在原生 SDK 里，逆不出来。
//   但 fever.163.com 这个网页自己会加载网易官方 H5 SDK
//   （netease-h5-mpay-sdk.umd.js），登录、加密、发票都由它完成。
//   我们只要打开这个页面，让用户在里面登录，然后从 SDK 里把 user_token 读出来。
//
// 拿到的 token 和扫码登录拿到的完全同源，都是 1-eyJ…，存下来就能长期复用。

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	webLoginURL     = "https://fever.163.com/"
	webLoginTmpName = "weblogin-result.json"
)

// webLoginState 网页登录的进行状态，界面轮询它。
type webLoginState struct {
	Running bool   `json:"running"`
	Phase   string `json:"phase"` // waiting | done | failed
	Message string `json:"message"`
	Account string `json:"account"`
	UserID  string `json:"userId"`
}

var (
	webLoginMu   sync.Mutex
	webLoginCur  = webLoginState{Phase: "idle"}
	webLoginProc *os.Process
)

// WebLoginStatus 给界面查询当前网页登录进度。
func WebLoginStatus() map[string]any {
	webLoginMu.Lock()
	defer webLoginMu.Unlock()
	return map[string]any{
		"running": webLoginCur.Running,
		"phase":   webLoginCur.Phase,
		"message": webLoginCur.Message,
		"account": webLoginCur.Account,
		"userId":  webLoginCur.UserID,
	}
}

// StartWebLogin 起一个子进程打开网页登录窗口。
//
// targetUserID 非空时表示"这是给某个已有账号刷新凭证"，
// 登录完成后会校验拿到的 user_id 是否一致，不一致就拒绝写入，
// 避免用户登错账号把 A 的凭证覆盖成 B 的。
func StartWebLogin(targetUserID string) error {
	webLoginMu.Lock()
	if webLoginCur.Running {
		webLoginMu.Unlock()
		return fmt.Errorf("网页登录窗口已经打开了")
	}
	webLoginCur = webLoginState{Running: true, Phase: "waiting", Message: "等待在窗口里登录"}
	webLoginMu.Unlock()

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	profile := filepath.Join(dataDir(), "weblogin")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return err
	}
	outPath := filepath.Join(dataDir(), webLoginTmpName)
	_ = os.Remove(outPath) // 清掉上一次的结果，避免读到旧数据

	cmd := exec.Command(exe, "--weblogin", "--profile", profile, "--out", outPath)
	cmd.Dir = filepath.Dir(exe)
	// 给子进程的 WebView2 开调试端口：这样主进程能用 CDP 发"可信点击"
	// （网页对合成事件的 isTrusted 校验只有 CDP / 系统输入能过）。
	// 子进程里 WebView2 是第一个实例，环境变量不会被已有实例吃掉。
	cmd.Env = append(os.Environ(), "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=9223")
	if err := cmd.Start(); err != nil {
		webLoginMu.Lock()
		webLoginCur = webLoginState{Phase: "failed", Message: "打不开登录窗口: " + err.Error()}
		webLoginMu.Unlock()
		return err
	}

	webLoginMu.Lock()
	webLoginProc = cmd.Process
	webLoginMu.Unlock()

	go watchWebLogin(cmd, outPath, targetUserID)
	return nil
}

// watchWebLogin 等子进程写出结果，然后落库。
func watchWebLogin(cmd *exec.Cmd, outPath, targetUserID string) {
	defer func() {
		webLoginMu.Lock()
		webLoginCur.Running = false
		webLoginProc = nil
		webLoginMu.Unlock()
		_ = os.Remove(outPath)
	}()

	deadline := time.Now().Add(5 * time.Minute)
	var result map[string]any
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(outPath); err == nil && len(b) > 0 {
			if json.Unmarshal(b, &result) == nil {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	setState := func(phase, msg, account, uid string) {
		webLoginMu.Lock()
		webLoginCur = webLoginState{Phase: phase, Message: msg, Account: account, UserID: uid}
		webLoginMu.Unlock()
	}

	if result == nil {
		_ = cmd.Process.Kill()
		setState("failed", "登录超时，请重试", "", "")
		return
	}

	ok, _ := result["ok"].(bool)
	if !ok {
		msg, _ := result["error"].(string)
		if msg == "" {
			msg = "登录失败"
		}
		setState("failed", msg, "", "")
		return
	}

	userID, _ := result["userId"].(string)
	token, _ := result["token"].(string)
	account, _ := result["account"].(string)
	method, _ := result["method"].(string)

	if userID == "" || token == "" {
		setState("failed", "没有拿到有效的登录凭证", "", "")
		return
	}

	// 防串号：给已有账号刷新时，必须还是同一个账号
	if targetUserID != "" && targetUserID != userID {
		setState("failed",
			fmt.Sprintf("登录的账号与要刷新的账号不一致（期望 %s，实际 %s），已取消", targetUserID, userID),
			account, userID)
		return
	}

	dev, _ := ensureDeviceID()
	acc := Account{
		Name:        account,
		Username:    account,
		Channel:     "netease",
		UserID:      userID,
		PlayerID:    userID,
		MpayToken:   token,
		DeviceID:    dev,
		Source:      "weblogin",
		LoginMethod: method,
		CredState:   "ok",
		LastCheck:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	if acc.Name == "" {
		acc.Name = userID
	}

	// 已有同 user_id 的账号就更新，否则新增
	if old, ok := findAccountByUserID(userID); ok {
		acc.Name = old.Name
		acc.NickName = old.NickName
		acc.PlayerLevel = old.PlayerLevel
		acc.SavedUser = old.SavedUser
		acc.SavedPass = old.SavedPass
	}

	if err := saveAccount(acc); err != nil {
		setState("failed", "保存账号失败: "+err.Error(), account, userID)
		return
	}
	_ = setCurrent(acc.PlayerID)
	clearAuthHint()
	setState("done", "登录成功，已保存", account, userID)
}

// findAccountByUserID 按渠道侧账号 id 找已保存的账号
// （网易 MPay id / B站 uid / 4399 uid / vivo 小号 subOpenId）。
func findAccountByUserID(userID string) (Account, bool) {
	list, err := loadAccounts()
	if err != nil {
		return Account{}, false
	}
	for _, a := range list {
		if managedChannel(a.Channel) && (a.UserID == userID || a.PlayerID == userID) {
			return a, true
		}
	}
	return Account{}, false
}

// WebLoginScript 返回注入到网页里的脚本。
//
// 逻辑：
//  1. 等页面把 window.__mpaySDK 建好
//  2. 调 showLogin() 把登录 iframe 建起来
//  3. 先试 silenceLogin（登录态还在时用户无需操作）
//  4. 不行就轮询 getUserInfo，等用户在里面登录完
//  5. 拿到 user_token 后通过 window.__skyloginPayload 回传给 Go
func WebLoginScript() string {
	return `(function () {
  if (window.__skyloginStarted) return;
  window.__skyloginStarted = true;

  var sent = false;
  // 结果只发一次
  function send(o) {
    if (sent) return;
    sent = true;
    rawSend(o);
  }
  // 中间指令（比如报告头像坐标）可以多次发
  function rawSend(o) {
    try { window.__skyloginPayload(JSON.stringify(o)); } catch (e) {}
  }

  // 页面顶部加一条提示，用户不会以为是"随便一个网页"
  function showBanner(text) {
    if (document.getElementById('__skylogin_banner')) return;
    var d = document.createElement('div');
    d.id = '__skylogin_banner';
    d.textContent = text;
    // 放底部并且不吃鼠标事件：放顶部会盖住头像，导致自动点击点在提示条上
    d.style.cssText = 'position:fixed;left:0;right:0;bottom:0;z-index:2147483647;' +
      'pointer-events:none;padding:8px 12px;font:12px/1.5 "Microsoft YaHei UI",sans-serif;' +
      'background:#d8b47c;color:#14120c;text-align:center;';
    document.documentElement.appendChild(d);
  }

  // 登录框出现后，只保留它这一条 DOM 链，其余全部隐藏。
  //
  // 不能直接藏 #app：登录框是 #app 里的一个浮层，藏了它登录框也没了。
  // 所以从登录框往上逐层"隐藏兄弟节点"，最后把根节点做成深色居中容器。
  var slimmed = false;
  function slim() {
    if (slimmed) return;
    var all = [].slice.call(document.querySelectorAll('input'));
    var anchor = null;
    for (var i = 0; i < all.length; i++) {
      if (/手机|账|邮箱|密码|验证码/.test(all[i].placeholder || '')) { anchor = all[i]; break; }
    }
    if (!anchor) return;

    // 往上找到既有标题又有输入框的那个卡片
    var card = anchor;
    for (var j = 0; j < 6 && card.parentElement; j++) {
      if (/邮箱登录|手机号登录|扫码登录/.test(card.parentElement.innerText || '')) { card = card.parentElement; break; }
      card = card.parentElement;
    }
    if (!card || !document.body.contains(card)) return;

    slimmed = true;
    // 逐层隐藏兄弟，直到 body
    var cur = card;
    while (cur && cur !== document.body) {
      var parent = cur.parentElement;
      if (!parent) break;
      [].slice.call(parent.children).forEach(function (sib) {
        if (sib === cur) return;
        if (sib.tagName === 'SCRIPT' || sib.tagName === 'STYLE' || sib.tagName === 'LINK') return;
        sib.style.display = 'none';
      });
      cur = parent;
    }
    document.documentElement.style.background = '#0b0b0c';
    document.body.style.background = '#0b0b0c';
    // 让卡片居中显示
    card.style.cssText += ';position:fixed!important;left:50%!important;top:50%!important;' +
      'transform:translate(-50%,-50%)!important;margin:0!important;z-index:2147483!important;' +
      'max-height:92vh!important;overflow:auto!important;';
    showBanner('在这里登录，完成后窗口会自动关闭');
  }
  // 自动点头像（有些时候点不动，用户也可以自己点）
  function autoOpen() {
    var tries = 0;
    var t = setInterval(function () {
      tries++;
      if (tries > 20) { clearInterval(t); return; }
      var avatar = document.querySelector('.pc-header-avatar');
      if (avatar) {
        avatar.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
        avatar.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
        avatar.dispatchEvent(new MouseEvent('click', { bubbles: true }));
        clearInterval(t);
      }
    }, 800);
  }

  var tries = 0;
  var wait = setInterval(function () {
    tries++;
    if (tries > 180) { clearInterval(wait); send({ error: '页面加载超时' }); return; }
    if (typeof window.MpaySDK !== 'function' || !window.__mpaySDK) return;

    clearInterval(wait);
    var sdk = window.__mpaySDK;

    showBanner('正在打开登录框…');
    // 登录按钮只认真实鼠标事件，所以在页面里定位坐标，交给 Go 用 SendInput 点
    var clickTries = 0;
    var clickTimer = setInterval(function () {
      clickTries++;
      if (clickTries > 8) {
        clearInterval(clickTimer);
        // 自动点不开时，转成人工提示，别让用户干等
        showBanner('请点右上角头像，用邮箱或手机号登录');
        return;
      }
      if (document.querySelector('input[type=password]') || /邮箱登录|手机号登录/.test(document.body.innerText || '')) {
        clearInterval(clickTimer);
        return;
      }
      var avatar = document.querySelector('.pc-header-avatar');
      if (!avatar) return;
      // 页面被我们隐藏过就不再点
      if (avatar.offsetParent === null) return;
      var r = avatar.getBoundingClientRect();
      rawSend({ kind: 'avatar', x: Math.round(r.left + r.width / 2), y: Math.round(r.top + r.height / 2) });
    }, 1200);
    // 登录框一出现就精简页面
    setInterval(slim, 600);

    function report(u) {
      if (!u) return;
      var tk = u.user_token || u.token || '';
      if (!tk) return;
      send({
        userId: u.user_id || '',
        token: tk,
        account: u.full_account || u.username || '',
        method: u.login_type === 7 ? 'sms' : (u.login_type === 1 ? 'password' : '')
      });
    }

    try { sdk.showLogin(); } catch (e) {}

    function pollUser() {
      setInterval(function () {
        try { report(sdk.getUserInfo && sdk.getUserInfo()); } catch (e) {}
      }, 700);
    }

    try {
      sdk.silenceLogin({ isUrsLogin: true }).then(function (u) {
        if (u && (u.user_token || u.token)) report(u);
        else pollUser();
      }).catch(function () { pollUser(); });
    } catch (e) { pollUser(); }
  }, 500);
})();`
}

// webLoginRoot 所有账号的网页登录会话目录的根。
func webLoginRoot() string { return filepath.Join(dataDir(), "weblogin") }

// webLoginProfileDir 某个账号专属的 WebView2 数据目录。
//
// 为什么按账号分：WebView2 的 cookie 是按 profile 存的，
// 多个账号共用一个 profile 时，后登录的会覆盖前一个的会话，
// 导致"刷新 A 的凭证却拿到 B 的会话"这种串号问题。
func webLoginProfileDir(userID string) string {
	if userID == "" {
		return filepath.Join(webLoginRoot(), "_new")
	}
	return filepath.Join(webLoginRoot(), sanitizeDirName(userID))
}

// sanitizeDirName 把账号 id 变成安全的目录名（只保留字母数字和少数符号）。
func sanitizeDirName(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}
