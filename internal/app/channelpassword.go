package app

// 渠道的「协议型」登录（账号密码 / 短信）：不开窗，Go 侧直接跟渠道服务端对话。
//
// 与网页登录（channellogin.go）的区别只有一个：凭证是算出来的，不是从页面捡的。
// 所以落库前的 uni_sauth 校验、账号字段组装、保存流程完全复用同一套。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"skylogin/internal/channel"
)

// StartChannelMethodLogin 用指定方式登录某个渠道（不开窗）。
//
// method 为空时按「账号密码」处理，兼容只认 password 的旧调用。
func StartChannelMethodLogin(key, method string) error {
	pv, ok := channel.Lookup(key)
	if !ok {
		return fmt.Errorf("不支持的渠道: %s", key)
	}
	want := channel.Kind(method)
	if want == "" {
		want = channel.KindPassword
	}
	var m channel.Method
	for _, x := range pv.Methods() {
		// method 既可以是 Kind（"qr"），也可以是方式名（"微信扫码"）。
		// 后者用来区分同类方式：应用宝的微信扫码和 QQ 扫码都是 KindQR，
		// 只按 Kind 取会永远落到第一个。
		if x.Kind() == want || (method != "" && x.Label() == method) {
			m = x
			break
		}
	}
	if m == nil {
		return fmt.Errorf("%s 不支持「%s」这种方式", pv.Label(), method)
	}

	tr := trackerOf(key)
	if !tr.begin(m.Hint()) {
		return fmt.Errorf("%s 登录已经在进行中", pv.Label())
	}

	go runChannelMethod(key, tr, m)
	return nil
}

// StartChannelPasswordLogin 只走账号密码，保留给旧调用点。
func StartChannelPasswordLogin(key string) error {
	return StartChannelMethodLogin(key, string(channel.KindPassword))
}

// runChannelMethod 跑一次 Method.Login，成功后校验并落库。
//
// 单独抽出来是为了让短信方式以后能复用同一套壳，不用再写一遍收尾逻辑。
func runChannelMethod(key string, tr *loginTracker, m channel.Method) {
	defer tr.finish()

	// 用户可能一直不填表单（或扫完码不确认），给个上限，别让 goroutine 永久挂着
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// 注册取消入口：界面关掉对话框时要能立刻结束，否则 tracker 一直占着，
	// 用户在 5 分钟内既不能重新登录、也看不到新的二维码。
	registerChannelCancel(key, cancel)
	defer unregisterChannelCancel(key)

	cred, err := m.Login(ctx, &channelProgress{tr: tr, ask: askOf(key)})
	if err != nil {
		// 用户主动关掉对话框：这不是"失败"，别把 context canceled 甩给用户看
		if errors.Is(err, context.Canceled) {
			tr.set("idle", "已取消登录", "", "")
			logf("%s 登录已取消", channel.LabelOf(key))
			return
		}
		tr.set("failed", err.Error(), "", "")
		logf("%s 登录失败: %v", channel.LabelOf(key), err)
		return
	}

	acc, err := finalizeChannelCred(key, cred)
	if err != nil {
		tr.set("failed", err.Error(), "", "")
		return
	}
	if err := saveChannelAccount(acc); err != nil {
		tr.set("failed", "保存账号失败: "+err.Error(), "", "")
		return
	}
	tr.set("done", channel.LabelOf(key)+"账号登录成功，已保存", acc.Name, acc.PlayerID)
	logf("%s账号登录成功并已保存: %s (playerId=%s)", channel.LabelOf(key), acc.Name, acc.PlayerID)
}

// finalizeChannelCred 把渠道给的 Cred 组装成可以落库的 Account。
//
// 落库前必须先过一遍 uni_sauth：那才是"这个凭证真能换到光遇登录态"的
// 唯一证据，光拿到渠道侧的 sid 不算数。
func finalizeChannelCred(key string, cred channel.Cred) (Account, error) {
	switch key {
	case channel.KeyUC:
		return finalizeUC(key, cred)
	case channel.KeyMyapp:
		return finalizeMyapp(key, cred)
	}
	return Account{}, fmt.Errorf("渠道 %s 还没有接协议登录的落库逻辑", channel.LabelOf(key))
}

// finalizeUC 九游：accountId 才是账号的唯一标识，sdkuid 固定 "0"。
func finalizeUC(key string, cred channel.Cred) (Account, error) {
	accountID := cred.Extra["accountId"]
	if accountID == "" {
		return Account{}, fmt.Errorf("九游登录材料不完整（缺 accountId）")
	}
	if err := channelSAUTH(key, cred.UID, cred.Secret); err != nil {
		// 网络不通时保留登录结果：凭证是刚登录拿到的，启动时还会再验一次
		if channel.IsNetwork(err) {
			logf("%s凭证在线验证暂时失败（%v），保留本次登录结果", channel.LabelOf(key), err)
		} else {
			return Account{}, err
		}
	}
	name := cred.Nickname
	if name == "" {
		name = accountID
	}
	a50dev, _ := ensureA50DeviceID()
	return Account{
		Name:         name,
		NickName:     name,
		Username:     name,
		Channel:      key,
		UserID:       cred.UID,
		PlayerID:     accountID,
		GameAuthSign: cred.Secret,
		RefreshToken: cred.Extra["refreshToken"],
		DeviceID:     a50dev,
		Source:       "ucpassword",
		LoginMethod:  "password",
		CredState:    "ok",
		LastCheck:    time.Now().Unix(),
		UpdatedAt:    time.Now().Unix(),
	}, nil
}



// ── 协议登录的取消入口 ──────────────────────────────────────────
//
// 扫码类流程（应用宝微信 / QQ）会一直阻塞等用户扫，最长 5 分钟。
// 用户关掉对话框必须能立刻结束，否则 tracker.begin 会一直拦着，
// 表现为"点了没反应"。

var (
	channelCancelsMu sync.Mutex
	channelCancels   = map[string]context.CancelFunc{}
)

func registerChannelCancel(key string, cancel context.CancelFunc) {
	channelCancelsMu.Lock()
	channelCancels[key] = cancel
	channelCancelsMu.Unlock()
}

func unregisterChannelCancel(key string) {
	channelCancelsMu.Lock()
	delete(channelCancels, key)
	channelCancelsMu.Unlock()
}

// StopChannelMethodLogin 取消正在进行的协议登录（关对话框时调）。
func StopChannelMethodLogin(key string) {
	channelCancelsMu.Lock()
	cancel := channelCancels[key]
	channelCancelsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}




