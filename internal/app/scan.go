package app

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"skylogin/internal/fever"
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
	// Existing 表示这次登录的是列表里已有的账号（只是更新了凭证）
	Existing bool `json:"existing,omitempty"`
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
	// 诊断开关：本次扫码用一台**新登记**的设备，验证"记住状态按设备隔离"。
	if scanNewDeviceEnabled() {
		if d, derr := registerFreshA50Device(); derr == nil {
			devID = d
			logf("[诊断] 本次扫码使用新设备：%s", d)
		} else {
			logf("[诊断] 登记新设备失败，仍用原来的 %s: %v", devID, derr)
		}
	}
	// 手机扫码必须用"发烧平台托管"那套建码参数：
	// 抓包里发烧平台的扫码登录就是这么建的（game_id=a50 长 id、dst_jf_game_id=ma75、cv=c0.0.0）。
	// 用光遇自己的参数建码 + 手机确认，游戏会拒绝这张票（实测"登录失败，请切换网络后再试"）。
	c := mpay.New()
	scanGame = profile.HostedGameID
	var uuid string
	var cerr error
	if scanGameParamsEnabled() {
		// 诊断分支：照抄**游戏内**扫码的建码参数（ma75 本体 + app_mode/arch/gv/sv/
		// mcount 那一串），用来验证"构造参数不同会不会改变换出来的凭证"。
		scanGame = profile.LongGameID
		uuid, _, cerr = c.CreateScanSession(devID, profile.LongGameID, profile.ShortGameID,
			proto().AppChannel, "c4.20.0", "0.0.0.0", fever.SystemVersion(), "win_x64")
		if cerr == nil {
			logf("[诊断] 建码用的是游戏内参数（game_id=%s）", profile.LongGameID)
		}
	} else {
		uuid, _, cerr = c.CreateLoginHosted(devID, profile.HostedGameID, profile.ShortGameID, proto().AppChannel, profile.HostedCv)
	}
	if cerr != nil {
		st := ScanState{Status: "error", Message: "建码失败: " + cerr.Error()}
		setScan(st)
		return st, cerr
	}
	imgGame, imgCv := profile.HostedGameID, profile.HostedCv
	if scanGameParamsEnabled() {
		imgGame, imgCv = profile.LongGameID, "c4.20.0"
	}
	png, ierr := c.QRImage(uuid, imgGame, imgCv)
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
	go waitScan(uuid, c, stop, devID)
	return st, nil
}

func waitScan(uuid string, c *mpay.Client, stop chan struct{}, devID string) {
	longID := scanGame
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-stop:
			return
		case <-time.After(time.Second):
		}
		status, code, channel, raw, err := c.ScanStatus(uuid, longID)
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
			// 手机确认那一刻勾了什么（比如"不失效"）如果会改返回字段，
			// 只能从原始响应里看出来，所以整条记一笔。
			logf("二维码确认响应: %s", maskScanRaw(raw, code))
			if channel == "" {
				channel = profile.HuaweiChannel
			}
			// 关键一步：二维码只是凭据编号，游戏要的是 AAAA 票。
			// 用发烧平台自己的换票接口把它换成票（不需要登录态）。
			// 换票必须带**建码时用的同一个设备号**，否则服务器回 "登录已失效"
			xdev := devID
			rawCode := code // 诊断用：留着最初的 10 位 code
			saved := false
			alreadyAdded := false
			if t, xerr := launcher.ExchangeTicket(code, xdev); xerr != nil {
				logf("换票失败（退回原始 code）: %v", xerr)
			} else {
				logf("换票成功: %s -> %s (channel=%s)", maskCode(code), maskTicket(t.Ticket), t.Channel)
				code = t.Ticket
				if t.Channel != "" {
					channel = t.Channel
				}
				// 网易账号：把票换成用户信息存下来，再现签一张票给游戏。
				// 存下来的 token 就是"持久化凭证"，下次启动不用再扫码。
				if nt, existed, perr := persistNetEase(t.Ticket, xdev); perr != nil {
					logf("保存网易账号失败（本次仍用换来的票）: %v", perr)
				} else {
					code = nt
					saved = true
					alreadyAdded = existed
				}
			}
			// 账号已经存下来了，就用账号里的凭证启动（正常路径）；
			// 只有没存下来（比如换票失败）才退回到"一次性票据"。
			if saved {
				scanMu.Lock()
				pending = nil
				scanMu.Unlock()
				if alreadyAdded {
					// 扫到的还是列表里已有的号：凭证更新了，但不能说"添加成功"，
					// 否则用户会以为多了一个账号（2026-11-05 反馈的问题）。
					setScan(ScanState{Status: "done", Message: "这个账号已经在列表里了，凭证已更新", Channel: channel, Existing: true})
					logf("扫码登录的是已有账号，已更新凭证（不是新增）: channel=%s", channel)
				} else {
					setScan(ScanState{Status: "done", Message: "账号已添加，直接点启动即可", Channel: channel})
					logf("扫码添加完成: channel=%s（已保存为账号）", channel)
				}
				if scanProbeEnabled() {
					go probeQrExchange(rawCode, xdev)
				}
			} else {
				scanMu.Lock()
				pending = &pendingTicket{code: code, channel: channel, at: time.Now()}
				scanMu.Unlock()
				setScan(ScanState{Status: "ready", Message: "账号没保存成功，请重新扫码再试", Channel: channel})
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
		"existing": st.Existing,
	}
}

// persistNetEase 把"换来的票"兑换成用户信息，存成账号，再现签一张票给游戏。
//
// 顺序很重要：login/ticket 会消耗这张票，所以必须用它自己换到的新票再给游戏。
// 返回给游戏用的票。
//
// 兑换口**只能用 login/ticket**：加载湾换来的票是一次性的，先拿它试
// mobile/finish 会被服务端吃掉登录态——后面再用同一张票 login/ticket 就回
// 1306 用户登录失效（2026-11-05 实测，扫码因此加不了账号）。
// finish 那条账号级的口只对短信票有效，见 exchangeSMSLoginTicket。
func persistNetEase(ticket, devID string) (string, bool, error) {
	c := mpay.New()
	// 用 a50（发烧平台）上下文兑换：建码就是 a50 托管会话，上下文要一致。
	// 换出来的 token 也是 a50 原生的，和密码 / 短信登录的账号行为完全一致。
	out, err := loginTicketAuto(c, ticket, devID)
	if err != nil {
		return "", false, fmt.Errorf("兑换票据失败: %w", err)
	}
	user, _ := out["user"].(map[string]any)
	if user == nil {
		return "", false, fmt.Errorf("响应里没有 user")
	}
	id := str(user["id"])
	token := str(user["token"])
	// 平台这条换票链路里，user.pc_ext_info.extra_unisdk_data 里也带
	// EXT_ACCESS_TOKEN（38 位十六进制，和 DLL 回调里那个是同一个值）。
	// 但 2026-11-05 实测：**拿到手就已经失效**（1311 用户登录已失效），
	// 所以它不是能存的凭证，只在诊断开关打开时才探一下。
	if access := accessTokenInUser(user); access != "" && scanProbeEnabled() {
		logf("换票响应里带 EXT_ACCESS_TOKEN（%d 字符）", len(access))
		go probeAccessToken(access, id, devID, 0)
	}
	// 展示名优先用脱敏的 client_username（finish 的 display_username 是完整手机号）
	name := str(user["client_username"])
	if name == "" {
		name = str(user["display_username"])
	}
	if id == "" || token == "" {
		return "", false, fmt.Errorf("用户信息不完整: id=%q token=%q", id, token)
	}
	if name == "" {
		name = id
	}
	// 是不是"已经在列表里"的号——决定提示语，别再假装添加了一个新账号
	_, existed := findAccountByUserID(id)

	acc := Account{
		Name:        name,
		NickName:    name,
		PlayerID:    id,
		Channel:     "netease",
		Username:    name,
		UserID:      id,
		MpayToken:   token,
		DeviceID:    devID,
		Source:      "qrcode",
		LoginMethod: "qrcode",
		UpdatedAt:   time.Now().Unix(),
	}
	if serr := saveAccount(acc); serr != nil {
		logf("账号保存失败: %v", serr)
	} else {
		logf("网易账号已保存: %s (id=%s) 凭证等级: %s", name, id, tokenGrade(token))
	}
	clearAuthHint()

	// 现签一张票给游戏（我们自己那张已经被 login/ticket 消耗掉了）
	nt, _, terr := c.CreateTicket(token, id, devID)
	if terr != nil {
		return "", existed, fmt.Errorf("现签票据失败: %w", terr)
	}
	logf("已为游戏现签票据: %s", maskTicket(nt))
	return nt, existed, nil
}

// str 安全取字符串。
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// maskScanRaw 把 query 响应压成一行写进日志，顺手把 code 打码。
//
// code 是能换票的凭据，日志里不留原文；其余字段（状态、渠道、有没有
// is_remember / expire 之类的额外字段）都保留，方便对比两种扫码的差别。
func maskScanRaw(raw map[string]any, code string) string {
	b, err := json.Marshal(raw)
	if err != nil {
		return "(响应无法序列化)"
	}
	s := string(b)
	if code != "" {
		s = strings.ReplaceAll(s, code, maskCode(code))
	}
	const max = 900
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// maskCode 十位码只留前两位。
func maskCode(code string) string {
	if len(code) <= 2 {
		return "**"
	}
	return code[:2] + "******"
}

// scanProbeEnabled 由数据目录里的开关文件控制：诊断用，正常情况不跑。
func scanProbeEnabled() bool {
	_, err := os.Stat(filepath.Join(dataDir(), "scan_probe.on"))
	return err == nil
}

// scanGameParamsEnabled 让建码改用游戏内那套参数（诊断用）。
//
// 平台那套（a50 托管）和游戏那套（ma75 本体 + app_mode/arch/gv/sv/mcount）
// 到底会不会换出不同等级的凭证，只能真机扫一次对比。
func scanGameParamsEnabled() bool {
	_, err := os.Stat(filepath.Join(dataDir(), "scan_gameparams.on"))
	return err == nil
}

// mpayGameParamsEnabled 让「账号密码登录」那个官方窗口用光遇自己的 game id 初始化。
//
// 窗口里的二维码是 mpay.dll 按 init 时给的 game_id 建的：给 a50 就是平台码
// （只能网易大神扫），给 ma75 就是游戏内的码（光遇手游可扫）。
func mpayGameParamsEnabled() bool {
	_, err := os.Stat(filepath.Join(dataDir(), "mpay_game.on"))
	return err == nil
}

// mpayPlatformDLLEnabled 让「账号密码登录」加载**平台目录**那份 mpay.dll（4.21），
// 而不是游戏目录里的 4.20。诊断用：想验证平台的登录会不会签出带 u_ki 的 token。
func mpayPlatformDLLEnabled() bool {
	_, err := os.Stat(filepath.Join(dataDir(), "mpay_dll_platform.on"))
	return err == nil
}

// mpayPlatformDBEnabled 让 DLL 用平台自己的数据目录（unisdk），并跳过清理旧登录态。
//
// 目的：复用平台缓存的登录态，让 DLL 静默登录成平台那个账号，
// 看它回调出来的 token 里有没有 u_ki（平台登录才有的字段）。
func mpayPlatformDBEnabled() bool {
	_, err := os.Stat(filepath.Join(dataDir(), "mpay_use_platform_db.on"))
	return err == nil
}

// scanNewDeviceEnabled 让本次扫码用一台新登记的 a50 设备。
//
// 诊断用：验证服务端的"记住状态"是不是按设备隔离的——是的话，每个账号
// 用自己的设备就不会互相顶掉。
func scanNewDeviceEnabled() bool {
	_, err := os.Stat(filepath.Join(dataDir(), "scan_new_device.on"))
	return err == nil
}

// probeQrExchange 诊断：拿刚扫到的 code 试"游戏自己那条" exchange_token。
//
// 只想回答一个问题——服务端的 encrypt_code 是不是必须由 mpay.dll 加密。
// 依次试明文 code 和几种常见哈希，把每次的回答写日志。此时账号已经存好，
// 探针失败不影响用户；成功的话就能顺着官方那条路走。
func probeQrExchange(code, devID string) {
	if code == "" || devID == "" {
		return
	}
	cands := []struct{ name, val string }{
		{"明文 code", code},
		{"md5(code)", md5hex(code)},
		{"md5(code+device)", md5hex(code + devID)},
		{"md5(device+code)", md5hex(devID + code)},
	}
	for _, cd := range cands {
		out, err := mpay.New().ExchangeQrCode(cd.val, devID, profile.LongGameID, profile.ShortGameID)
		if err != nil {
			logf("[诊断] exchange_token（%s）被拒: %v 响应: %s", cd.name, err, compactJSON(out))
			continue
		}
		logf("[诊断] exchange_token（%s）通过！凭证等级: %s", cd.name, tokenGrade(userTokenOf(out)))
		return
	}
}

// md5hex 小写十六进制 MD5。
func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", sum)
}
