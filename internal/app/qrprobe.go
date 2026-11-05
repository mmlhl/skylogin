package app

// 渠道服扫码探针（开发用）：自己建一张"游戏码"，全程不碰 mpay.dll、不开窗口。
//
// 想回答的问题
//
//	官方登录窗之所以要弹出来，只是为了让它替我们建码 + 收渠道凭证。
//	但建码这事儿我们自己也会（mpay.CreateScanSession 就是照抄游戏内参数）。
//	所以先验证：**自建的这张码，别的渠道 App 扫了以后，服务端会不会
//	也把渠道凭证一起吐回来**。
//
//	会 —— 渠道服扫码就不用再弹官方窗，二维码我们自己建、（以后）自己画；
//	不会 —— 说明凭证只在 DLL 回调和渠道 SDK 之间流动，那条路绕不开。
//
// 怎么用
//
//	skylogin.exe --qr-probe [--qr-probe-seconds 180]
//
//	二维码图片写到 %APPDATA%\SkyLogin\qr\<uuid>.png，并用系统看图打开；
//	轮询过程和最终响应写到 %APPDATA%\SkyLogin\qr_probe.log。
//	（程序是 GUI 子系统，没有控制台，所以全靠日志文件。）
//
//	拿到渠道凭证时顺带用华为那套接口验一次"能不能换出 gameAuthSign"，
//	这样一次扫码就能判定这条路通不通，不用再开游戏。

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/fever"
	"skylogin/internal/huawei"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

// qrProbeLog 追加一行到探针日志（每次单独开文件，进程被杀也不丢）。
func qrProbeLog(path, format string, args ...any) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	line := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	_, _ = f.WriteString(line)
}

// ProbeChannelQR 建一张码并轮询，直到手机确认或超时。
//
// hosted 决定建码用哪套参数——这两套的差别是整条链路的关键：
//
//	false（游戏码）：game_id=光遇自己的长 id、jf_game_id=ma75
//	                渠道 App 能扫；但加载湾换票回 6006（不在托管上下文里）
//	true （托管码）：game_id=发烧平台 a50、dst_jf_game_id=ma75、cv=c0.0.0
//	                加载湾能换票；渠道 App 认不认还没试过
func ProbeChannelQR(seconds int, hosted bool) {
	if seconds <= 0 {
		seconds = 900
	}
	logPath := filepath.Join(dataDir(), "qr_probe.log")
	log := func(format string, args ...any) { qrProbeLog(logPath, format, args...) }

	mode := "游戏码（ma75 本体）"
	if hosted {
		mode = "托管码（a50 + dst_jf_game_id=ma75）"
	}
	log("=== 渠道服扫码探针开始（最多 %d 秒，%s） ===", seconds, mode)

	// 建码用的是"游戏上下文"的设备号：这条链路的票最终是给 ma75 用的，
	// 设备号必须是 ma75 那边登记的（a50 的号在这里不认）。
	devID, err := ensureDeviceID()
	if err != nil || devID == "" {
		log("失败: 设备标识不可用: %v", err)
		return
	}
	log("设备号: %s", maskMiddle(devID))

	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	qrRound := 0
	imgGame := profile.LongGameID
	if hosted {
		imgGame = profile.HostedGameID
	}
	for time.Now().Before(deadline) {
		qrRound++
		uuid, ok := serveOneQR(log, devID, qrRound, hosted)
		if !ok {
			return
		}
		// 一张码等 150 秒；未确认就换一张（服务端那张已经作废了）
		if confirmed := pollQR(log, uuid, imgGame, devID, 150*time.Second); confirmed {
			// 验多个渠道时不能扫完就退：换一张码继续等下一个 App
			log("换下一个渠道继续验（这张码已作废）")
			continue
		}
		log("这一张码过期了，重新建一张")
	}
	log("超时：整个探针时间内没有确认")
}

// serveOneQR 建一张码、取图、打开给用户扫。返回 uuid。
func serveOneQR(log func(string, ...any), devID string, round int, hosted bool) (string, bool) {
	c := mpay.New()
	var uuid string
	var raw map[string]any
	var err error
	if hosted {
		// 托管码：和界面里「扫码登录」建的是同一种（a50 托管 + dst_jf_game_id=ma75），
		// 加载湾换票认它。
		uuid, raw, err = c.CreateLoginHostedFull(devID, profile.HostedGameID, profile.ShortGameID,
			proto().AppChannel, profile.HostedCv, "2")
	} else {
		// 游戏码：参数照抄游戏内扫码（game_id 用光遇自己的长 id、jf_game_id=ma75），
		// 和官方登录窗用 ma75 初始化时建出来的码是同一类。
		uuid, raw, err = c.CreateScanSession(devID, profile.LongGameID, profile.ShortGameID,
			proto().AppChannel, "c4.20.0", "0.0.0.0", fever.SystemVersion(), "win_x64")
	}
	if err != nil {
		log("建码失败: %v  响应: %s", err, compactJSON(raw))
		return "", false
	}
	log("#%d 建码成功: uuid=%s", round, maskMiddle(uuid))

	imgGame, imgCv := profile.LongGameID, "c4.20.0"
	if hosted {
		imgGame, imgCv = profile.HostedGameID, profile.HostedCv
	}
	png, ierr := c.QRImage(uuid, imgGame, imgCv)
	if ierr != nil || len(png) == 0 {
		log("取二维码图片失败: %v", ierr)
		return "", false
	}
	dir := filepath.Join(dataDir(), "qr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log("建目录失败: %v", err)
		return "", false
	}
	// 固定文件名：后一张覆盖前一张，用户那边不会堆一串窗口
	img := filepath.Join(dir, "current.png")
	if err := os.WriteFile(img, png, 0o600); err != nil {
		log("写二维码失败: %v", err)
		return "", false
	}
	log("#%d 二维码图片: %s", round, img)
	openImage(img)
	return uuid, true
}

// pollQR 盯着这张码的状态，最多等 d。返回是否已确认。
func pollQR(log func(string, ...any), uuid, gameID, devID string, d time.Duration) bool {
	c := mpay.New()
	deadline := time.Now().Add(d)
	lastStatus := -1
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		status, code, channel, resp, qerr := c.ScanStatus(uuid, gameID)
		if qerr != nil {
			// 网络抖一下不算失败，记一次继续等
			log("查询出错（继续等）: %v", qerr)
			continue
		}
		if status != lastStatus {
			lastStatus = status
			log("状态变化: status=%d（0=未扫 1=已扫未确认 2=已确认）", status)
		}
		if status != 2 || code == "" {
			continue
		}
		log("已确认: channel=%s code=%s", channel, maskCode(code))
		// 整条响应先落盘：字段结构是这次要研究的东西，别只看我们认得的几个
		log("响应原文: %s", compactJSON(resp))
		dumpQRCredentials(log, resp)
		// 同一张码还在，把"要不要多带参数才给渠道登录包"一次问清楚
		probeQueryVariants(log, uuid, gameID, devID)
		// 紧接着把"这条链能不能换成账号"也验一次，省得回头再扫一遍
		tryExchange(log, code, channel, devID)
		return true
	}
	return false
}

// probeQueryVariants 用不同的额外参数反复查同一张"已确认"的码。
//
// 官方 DLL 走渠道服扫码时，回调里是**整套渠道登录包**
// （extra_unisdk_data → SAUTH_STR / sdk_info）；我们自己查只拿到一个 code。
// 差别大概率在客户端带了哪些参数上，所以把能想到的候选挨个加上试，
// 响应"变胖"的那个就是答案。
func probeQueryVariants(log func(string, ...any), uuid, gameID, devID string) {
	c := mpay.New()
	trio := map[string]string{"login_channel": "huawei", "app_channel": "huawei", "pay_channel": "huawei"}
	all := map[string]string{}
	for k, v := range trio {
		all[k] = v
	}
	all["jf_game_id"] = profile.ShortGameID
	all["device_id"] = devID
	all["udid"] = devID
	all["sdk_version"] = profile.SdkVersion
	all["get_access_token"] = "1"

	variants := []struct {
		name  string
		extra map[string]string
	}{
		{"基线", nil},
		{"app_channel", map[string]string{"app_channel": "huawei"}},
		{"三件套", trio},
		{"jf_game_id", map[string]string{"jf_game_id": profile.ShortGameID}},
		{"device_id", map[string]string{"device_id": devID}},
		{"udid", map[string]string{"udid": devID}},
		{"sdk_version", map[string]string{"sdk_version": profile.SdkVersion}},
		{"get_access_token", map[string]string{"get_access_token": "1"}},
		{"unisdk_data", map[string]string{"unisdk_data": "1"}},
		{"need_sdk_info", map[string]string{"need_sdk_info": "1"}},
		{"with_unisdk", map[string]string{"with_unisdk": "1"}},
		{"全带", all},
	}
	for _, v := range variants {
		resp, err := c.QueryExtra(uuid, gameID, v.extra)
		if err != nil {
			log("  参数[%s] 查询失败: %v", v.name, err)
			continue
		}
		s := compactJSON(resp)
		mark := ""
		if strings.Contains(s, "extra_unisdk_data") || strings.Contains(s, "SAUTH") {
			mark = "   <<< 出现渠道登录包"
		}
		log("  参数[%s] -> %s%s", v.name, cut(s, 240), mark)
	}
}

// cut 截断长字符串，日志里只留开头。
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// tryExchange 用扫到的 code 走一遍"完全不经过 mpay.dll"的换票链路。
//
// 官方登录窗那条路能存出渠道账号，是因为 DLL（或者它背后的渠道 SDK）
// 把 openid / accessToken 一起拿到了。这里验证的是另一条路：
//
//	二维码 code → 加载湾换票 → login/ticket → 账号 + token
//
// 通的话，渠道服账号就能脱离 mpay.dll 添加：码我们自己建，票我们自己换。
func tryExchange(log func(string, ...any), code, channel, devID string) {
	c := mpay.New()
	t, err := launcher.ExchangeTicketWithSid(code, devID, platformSid())
	if err != nil {
		log("加载湾换票失败: %v", err)
		return
	}
	log("加载湾换票成功: 票里的 channel=%s ticket=%s", t.Channel, maskTicket(t.Ticket))

	out, err := loginTicketAuto(c, t.Ticket, devID)
	if err != nil {
		log("login/ticket 失败: %v  响应: %s", err, redactJSON(out))
		return
	}
	user, _ := out["user"].(map[string]any)
	token := userTokenOf(out)
	log("login/ticket 通过：user.id=%s token=%s 等级=%s",
		maskMiddle(toStr(user["id"])), maskValue(token), tokenGrade(token))
	// 把 user 的字段名全打出来（值里的凭证打码）：渠道不同，凭证字段的位置不同，
	// 先看清楚结构再决定怎么接。
	log("user 字段: %s", redactJSON(user))
}

// dumpQRCredentials 在响应里找渠道凭证，找到就用华为接口验一次。
//
// 不写死路径：渠道不同、服务端版本不同，凭证埋的位置都会变
// （华为那次在 slot8.extra_unisdk_data.extra_data.sdk_info 里）。
// 所以整棵树走一遍，按字段名认。
func dumpQRCredentials(log func(string, ...any), resp map[string]any) {
	hits := map[string]string{}
	walkJSON(resp, "", func(path, key string, val any) {
		switch strings.ToLower(key) {
		case "sdk_info", "openid", "open_id", "access_token", "accesstoken",
			"uid", "union_id", "unionid", "session", "sessionid", "session_id", "token":
			hits[path+"."+key] = fmt.Sprint(val)
		}
	})
	if len(hits) == 0 {
		log("响应里没有找到任何渠道凭证字段（只有网易自己的 code）")
		return
	}
	paths := make([]string, 0, len(hits))
	for k := range hits {
		paths = append(paths, k)
	}
	sortStrings(paths)
	for _, p := range paths {
		log("  凭证字段 %s = %s", p, maskValue(hits[p]))
	}

	access := firstByKey(hits, "accesstoken", "access_token")
	openid := firstByKey(hits, "openid", "open_id")
	if access == "" {
		log("没找到 accessToken，没法继续验")
		return
	}
	dev, derr := device.LoadOrCreateAndroid(filepath.Join(dataDir(), "android.json"))
	if derr != nil {
		log("准备设备失败: %v", derr)
		return
	}
	sign, serr := huawei.GetGameSession(access, profile.HuaweiAppID, dev.PhoneType, dev)
	if serr != nil {
		log("用这个 accessToken 换 gameAuthSign 失败: %v", serr)
		return
	}
	log("换 gameAuthSign 成功！playerId=%s openid=%s 等级=%s ts=%s",
		toStr(sign["playerId"]), maskMiddle(openid), toStr(sign["playerLevel"]), toStr(sign["ts"]))
}

// walkJSON 深度遍历任意 JSON 值，把叶子字段（带路径）交给回调。
func walkJSON(v any, path string, fn func(path, key string, val any)) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			p := k
			if path != "" {
				p = path + "." + k
			}
			switch child.(type) {
			case map[string]any, []any:
				walkJSON(child, p, fn)
			default:
				fn(path, k, child)
			}
		}
	case []any:
		for i, child := range t {
			walkJSON(child, fmt.Sprintf("%s[%d]", path, i), fn)
		}
	}
}

// firstByKey 按（小写）字段名从命中表里随便取一个非空值。
func firstByKey(hits map[string]string, keys ...string) string {
	for _, k := range keys {
		for path, val := range hits {
			if strings.HasSuffix(strings.ToLower(path), "."+k) && val != "" {
				return val
			}
		}
	}
	return ""
}

// maskMiddle 只留前 6 位，避免日志里出现完整设备号 / uuid / openid。
func maskMiddle(s string) string {
	if len(s) <= 8 {
		return "**"
	}
	return s[:6] + fmt.Sprintf("…(%d)", len(s))
}

// maskValue 给凭证字段打码：短值原样（渠道名这类），长值只留长度。
func maskValue(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:4] + fmt.Sprintf("…(%d 字符)", len(s))
}

// openImage 用系统默认看图程序打开图片。失败不影响流程（路径已经写进日志）。
func openImage(path string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
}

// redactJSON 把任意 JSON 压成一行，凭证类字段的值打码。
//
// 探针要看的是"响应里有哪些字段"，不是凭证本身；打码后这份日志
// 可以留档、也可以贴进分析笔记。
func redactJSON(v any) string {
	b, err := marshalCompact(redactValue(v, ""))
	if err != nil {
		return "(无法序列化)"
	}
	return string(b)
}

// redactValue 递归复制一份，把敏感的字段值换成打码形式。
func redactValue(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = redactValue(child, k)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = redactValue(child, key)
		}
		return out
	default:
		if isSecretKey(key) {
			return maskValue(fmt.Sprint(v))
		}
		return v
	}
}

// isSecretKey 判断字段名是不是"不能明文落盘"的那类。
func isSecretKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range []string{"token", "secret", "sign", "access", "pwd", "password", "ticket", "session", "code", "key"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// marshalCompact 是 json.Marshal 的小包装（单独抽出来，方便上面复用）。
func marshalCompact(v any) ([]byte, error) { return json.Marshal(v) }

// sortStrings 是个小排序，避免为了一个排序引入新依赖。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
