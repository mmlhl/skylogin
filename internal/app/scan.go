package app

import (
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
)

// 手机扫码登录：工具只负责"亮码 + 轮询"，确认动作由手机（光遇手游 / 网易大神）完成。
//
// 这样不区分渠道：手机里登录的是华为账号就是华为，是网易账号就是网易，
// 票据里的 login_channel 会一起返回，op14 照抄即可。

// ScanState 是扫码登录的当前状态，界面直接渲染。
type ScanState struct {
	Status   string `json:"status"` // idle|waiting|scanned|ready|error
	Message  string `json:"message"`
	QR       string `json:"qr"` // data:image/png;base64,...
	Channel  string `json:"channel"`
	UUID     string `json:"uuid"`
	ExpireAt int64  `json:"expireAt"`
}

// pendingTicket 是扫码确认后拿到的一次性票据，等游戏要票时用。
type pendingTicket struct {
	code    string
	channel string
	at      time.Time
}

var (
	scanMu    sync.Mutex
	scanGame  string // 当前扫码用的 game_id（查询时要一致）
	scanState = ScanState{Status: "idle"}
	scanStop  chan struct{}
	pending   *pendingTicket
)

func currentScan() ScanState {
	scanMu.Lock()
	defer scanMu.Unlock()
	return scanState
}

func setScan(st ScanState) {
	scanMu.Lock()
	scanState = st
	scanMu.Unlock()
}

// StartScanLogin 创建二维码并开始轮询，返回二维码状态。
func StartScanLogin() (ScanState, error) {
	scanMu.Lock()
	if scanStop != nil {
		close(scanStop)
	}
	stop := make(chan struct{})
	scanStop = stop
	scanMu.Unlock()

	devID, err := ensureA50DeviceID()
	if err != nil && devID == "" {
		return ScanState{Status: "error", Message: "设备标识不可用"}, err
	}
	// 手机扫码必须用"发烧平台托管"那套建码参数：
	// 抓包里发烧平台的扫码登录就是这么建的（game_id=a50 长 id、dst_jf_game_id=ma75、cv=c0.0.0）。
	// 用光遇自己的参数建码 + 手机确认，游戏会拒绝这张票（实测"登录失败，请切换网络后再试"）。
	c := mpay.New()
	scanGame = profile.HostedGameID
	uuid, _, err := c.CreateLoginHosted(devID, profile.HostedGameID, profile.ShortGameID, proto().AppChannel, profile.HostedCv)
	if err != nil {
		st := ScanState{Status: "error", Message: "建码失败: " + err.Error()}
		setScan(st)
		return st, err
	}
	png, ierr := c.QRImage(uuid, profile.HostedGameID, profile.HostedCv)
	if ierr != nil {
		st := ScanState{Status: "error", Message: "取二维码失败: " + ierr.Error()}
		setScan(st)
		return st, ierr
	}
	st := ScanState{
		Status:   "waiting",
		Message:  "用手机扫一扫（光遇手游 / 网易大神都行）",
		QR:       "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		UUID:     uuid,
		ExpireAt: time.Now().Add(3 * time.Minute).UnixMilli(),
	}
	setScan(st)
	go waitScan(uuid, c, stop)
	return st, nil
}

func waitScan(uuid string, c *mpay.Client, stop chan struct{}) {
	longID := scanGame
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-stop:
			return
		case <-time.After(time.Second):
		}
		status, code, channel, err := c.ScanStatus(uuid, longID)
		if err != nil {
			continue
		}
		if status == 1 {
			cur := currentScan()
			if cur.Status == "waiting" {
				cur.Status = "scanned"
				cur.Message = "已扫码，请在手机上确认"
				setScan(cur)
			}
			continue
		}
		if status == 2 && code != "" {
			if channel == "" {
				channel = profile.HuaweiChannel
			}
			// 关键一步：二维码只是凭据编号，游戏要的是 AAAA 票。
			// 用发烧平台自己的换票接口把它换成票（不需要登录态）。
			// 换票必须带**建码时用的同一个设备号**，否则服务器回 "登录已失效"
			xdev, _ := ensureA50DeviceID()
			saved := false
			if t, xerr := launcher.ExchangeTicket(code, xdev); xerr != nil {
				logf("换票失败（退回原始 code）: %v", xerr)
			} else {
				logf("换票成功: %s -> %s (channel=%s)", code, t.Ticket, t.Channel)
				code = t.Ticket
				if t.Channel != "" {
					channel = t.Channel
				}
				// 网易账号：把票换成用户信息存下来，再现签一张票给游戏。
				// 存下来的 token 就是"持久化凭证"，下次启动不用再扫码。
				if nt, perr := persistNetEase(t.Ticket, xdev); perr != nil {
					logf("保存网易账号失败（本次仍用换来的票）: %v", perr)
				} else {
					code = nt
					saved = true
				}
			}
			// 账号已经存下来了，就用账号里的凭证启动（正常路径）；
			// 只有没存下来（比如换票失败）才退回到"一次性票据"。
			if saved {
				scanMu.Lock()
				pending = nil
				scanMu.Unlock()
				setScan(ScanState{Status: "done", Message: "账号已添加，直接点启动即可", Channel: channel})
				logf("扫码添加完成: channel=%s（已保存为账号）", channel)
			} else {
				scanMu.Lock()
				pending = &pendingTicket{code: code, channel: channel, at: time.Now()}
				scanMu.Unlock()
				setScan(ScanState{Status: "ready", Message: "这次先临时用扫码的票", Channel: channel})
				logf("扫码登录完成（未保存账号）: channel=%s", channel)
			}
			return
		}
	}
	setScan(ScanState{Status: "error", Message: "二维码已过期，请重新生成"})
}

// StopScanLogin 取消当前扫码。
func StopScanLogin() {
	scanMu.Lock()
	if scanStop != nil {
		close(scanStop)
		scanStop = nil
	}
	scanMu.Unlock()
	setScan(ScanState{Status: "idle"})
}

// takePending 取出待用票据（只能取一次，5 分钟内有效）。
func takePending() (code, channel string, ok bool) {
	scanMu.Lock()
	defer scanMu.Unlock()
	if pending == nil {
		return "", "", false
	}
	if time.Since(pending.at) > 5*time.Minute {
		pending = nil
		return "", "", false
	}
	code, channel = pending.code, pending.channel
	pending = nil
	// 票被取走后就清掉界面上的"已就绪"，避免用户以为还有一张票可用
	scanState = ScanState{Status: "idle"}
	return code, channel, true
}

// peekPending 只看不取（界面判断用）。
func peekPending() bool {
	scanMu.Lock()
	defer scanMu.Unlock()
	return pending != nil
}

// proto 是 protocol.Get 的短别名，避免在这个文件里反复写包名。
func proto() protocol.Current { return protocol.Get() }

// scanToMap 把扫码状态转成界面用的结构。
func scanToMap(st ScanState) map[string]any {
	return map[string]any{
		"status":   st.Status,
		"message":  st.Message,
		"qr":       st.QR,
		"channel":  st.Channel,
		"uuid":     st.UUID,
		"expireAt": st.ExpireAt,
	}
}

// persistNetEase 把"换来的票"兑换成用户信息，存成账号，再现签一张票给游戏。
//
// 顺序很重要：login/ticket 会消耗这张票，所以必须用它自己换到的新票再给游戏。
// 返回给游戏用的票。
func persistNetEase(ticket, devID string) (string, error) {
	c := mpay.New()
	// 用 a50（发烧平台）上下文兑换：建码就是 a50 托管会话，上下文要一致。
	// 换出来的 token 也是 a50 原生的，和密码 / 短信登录的账号行为完全一致。
	out, err := loginTicketAuto(c, ticket, devID)
	if err != nil {
		return "", fmt.Errorf("兑换票据失败: %w", err)
	}
	user, _ := out["user"].(map[string]any)
	if user == nil {
		return "", fmt.Errorf("响应里没有 user")
	}
	id := str(user["id"])
	token := str(user["token"])
	name := str(user["display_username"])
	if id == "" || token == "" {
		return "", fmt.Errorf("用户信息不完整: id=%q token=%q", id, token)
	}
	if name == "" {
		name = id
	}

	acc := Account{
		Name:      name,
		NickName:  name,
		PlayerID:  id,
		Channel:   "netease",
		Username:  name,
		UserID:    id,
		MpayToken: token,
		DeviceID:  devID,
		UpdatedAt: time.Now().Unix(),
	}
	if serr := saveAccount(acc); serr != nil {
		logf("账号保存失败: %v", serr)
	} else {
		logf("网易账号已保存: %s (id=%s)", name, id)
	}
	clearAuthHint()

	// 现签一张票给游戏（我们自己那张已经被 login/ticket 消耗掉了）
	nt, _, terr := c.CreateTicket(token, id, devID)
	if terr != nil {
		return "", fmt.Errorf("现签票据失败: %w", terr)
	}
	logf("已为游戏现签票据: %s", nt)
	return nt, nil
}

// str 安全取字符串。
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
