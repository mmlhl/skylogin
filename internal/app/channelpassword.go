package app

// 渠道的「协议型」登录（账号密码 / 短信）：不开窗，Go 侧直接跟渠道服务端对话。
//
// 与网页登录（channellogin.go）的区别只有一个：凭证是算出来的，不是从页面捡的。
// 所以落库前的 uni_sauth 校验、账号字段组装、保存流程完全复用同一套。

import (
	"context"
	"fmt"
	"time"

	"skylogin/internal/channel"
)

// StartChannelPasswordLogin 用「账号密码」方式登录某个渠道（不开窗）。
//
// 立即返回，进度由 /api/<key>/login/status 轮询；需要用户输入时
// status 里会带 ask，前端提交到 /api/<key>/login/answer。
func StartChannelPasswordLogin(key string) error {
	pv, ok := channel.Lookup(key)
	if !ok {
		return fmt.Errorf("不支持的渠道: %s", key)
	}
	var m channel.Method
	for _, x := range pv.Methods() {
		if x.Kind() == channel.KindPassword {
			m = x
			break
		}
	}
	if m == nil {
		return fmt.Errorf("%s 不支持账号密码登录", pv.Label())
	}

	tr := trackerOf(key)
	if !tr.begin("等待填写账号…") {
		return fmt.Errorf("%s 登录已经在进行中", pv.Label())
	}

	go runChannelMethod(key, tr, m)
	return nil
}

// runChannelMethod 跑一次 Method.Login，成功后校验并落库。
//
// 单独抽出来是为了让短信方式以后能复用同一套壳，不用再写一遍收尾逻辑。
func runChannelMethod(key string, tr *loginTracker, m channel.Method) {
	defer tr.finish()

	// 用户可能一直不填表单，给个上限，别让 goroutine 永久挂着
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cred, err := m.Login(ctx, &channelProgress{tr: tr, ask: askOf(key)})
	if err != nil {
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
		return Account{}, err
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
