package app

// 真实无头实验的后半程：读取官方 DLL 刚生成的配套槽位，内存中继续授权。
// HEADLESS_CHAIN_DIR 指向被忽略的运行目录；默认跳过，不改本地账号库。

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"skylogin/internal/device"
	"skylogin/internal/fever"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
	"skylogin/internal/win"
)

func TestZZHeadlessChain(t *testing.T) {
	dir := os.Getenv("HEADLESS_CHAIN_DIR")
	if dir == "" {
		t.Skip("未开启真实无头链路实验")
	}
	readSlots := func(name string) []string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal("缺少 DLL 登录结果文件")
		}
		var slots []string
		if json.Unmarshal(b, &slots) != nil || len(slots) < 9 || slots[0] == "" || slots[1] == "" || slots[2] == "" {
			t.Fatal("DLL 登录结果不完整")
		}
		return slots
	}
	host, game := readSlots("platform-slots.json"), readSlots("game-slots.json")
	if host[0] != game[0] || tokenGI(host[2]) != "aecglf6ee4aaaarz" || tokenGI(game[2]) != "aecfs5n2y4aaaakd" {
		t.Fatal("两份凭证的账号或应用上下文不匹配")
	}
	if _, err := mpay.New().GetUserInfo(game[2], game[0], game[1], profile.LongGameID, profile.ShortGameID); err != nil {
		t.Fatal("阶段0：ma75 token与回调设备号的资料校验失败")
	}
	t.Log("阶段0：ma75 token与配套设备号通过用户资料校验")
	sid, err := launcher.Login(host[0], host[2], host[1])
	if err != nil || sid == "" {
		t.Fatal("阶段1：平台凭证换 sid 失败")
	}
	t.Log("阶段1：a50 凭证换平台 sid 成功")
	before, err := launcher.SavedAccount(sid)
	if err != nil {
		t.Fatal("阶段2：读槽失败")
	}
	t.Logf("阶段2：槽位为空=%v，槽内为当前目标=%v", before == "", before == game[0])
	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatal("读取授权设备失败")
	}
	var accepted map[string]any
	var acceptedSAUTH map[string]any
	// 安卓是用户本次所选平台；若被拒，只用 pc 作一次对照以区别平台声明问题。
	for _, platform := range []string{"ad", "pc"} {
		custom := map[string]any{
			"login_channel": "netease", "app_channel": "netease.allysdk3rd",
			"source_app_channel": "netease.allysdk3rd", "platform": platform,
			"source_platform": platform, "sdk_version": "4.21.0", "deviceid": game[1],
			"username": os.Getenv("URS_USER"),
		}
		sauth := netsec.NewSAUTH(game[0], game[2], profile.ShortGameID, custom, dev)
		resp, err := netsec.PostSigned(sauth, profile.ShortGameID)
		if err != nil {
			t.Fatal("阶段3：ma75 uni_sauth 网络或解析失败")
		}
		raw, _ := json.MarshalIndent(resp, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "private-unisauth-"+platform+".json"), raw, 0o600); err != nil {
			t.Fatal("保存服务端响应失败")
		}
		code := toStr(resp["code"])
		t.Logf("阶段3：ma75 uni_sauth 声明平台=%s，code=%s，有授权包=%v", platform, code, resp["unisdk_login_json"] != nil)
		if code == "200" && platform == "ad" {
			accepted, acceptedSAUTH = resp, sauth
			break
		}
	}
	if accepted == nil {
		t.Fatal("阶段3阻断：新鲜 ma75 凭证未获得安卓游戏授权；没有进入确认或写槽，更没有完成带渠道启动")
	}
	lp, err := netsec.NewLoginPayloadFromSAUTH(accepted, acceptedSAUTH, game[2], "0", strconv.FormatInt(time.Now().Unix(), 10), dev, netsec.NeteaseChannelInfo())
	if err != nil {
		t.Fatal("阶段4：授权组包失败")
	}
	// 网易原生 token 与其它渠道的 base64 包装可能使用不同确认入口语义。
	// 只做显式单变量对照，不把试验形态接入正式链路。
	if os.Getenv("HEADLESS_RAW_CONFIRM_TOKEN") == "1" {
		lp.Body["token"] = game[2]
		t.Log("阶段4：确认 token 使用网易原生文本（单变量实验）")
	}
	if os.Getenv("HEADLESS_NATIVE_FORM_CHANNEL") == "1" {
		lp.Body["app_channel"] = "netease"
		t.Log("阶段4：外层确认 app_channel=netease（对照原渠道组包实现）")
	}
	if os.Getenv("HEADLESS_CONFIRM_DEVICE") == "1" {
		lp.Body["device_id"] = game[1]
		t.Log("阶段4：外层确认显式添加凭证配套 device_id")
	}
	c := mpay.New()
	c.AppChannel = "netease"
	traceIndex := 0
	c.ProbeTrace = func(method, path string, request url.Values, response map[string]any) {
		traceIndex++
		trace := map[string]any{"method": method, "path": path, "request": request, "response": response}
		b, e := json.MarshalIndent(trace, "", "  ")
		if e != nil {
			t.Fatal("记录授权请求失败")
		}
		name := "private-qrtrace-" + strconv.Itoa(traceIndex) + ".json"
		if os.Getenv("HEADLESS_RAW_CONFIRM_TOKEN") == "1" {
			name = "raw-" + name
		}
		if os.Getenv("HEADLESS_NATIVE_FORM_CHANNEL") == "1" {
			name = "native-channel-" + name
		}
		if os.Getenv("HEADLESS_CONFIRM_DEVICE") == "1" {
			name = "with-device-" + name
		}
		if e := os.WriteFile(filepath.Join(dir, name), b, 0o600); e != nil {
			t.Fatal("写入授权请求失败")
		}
	}
	uuid, _, err := c.CreateLoginHostedFull(host[1], profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
	if err != nil {
		t.Fatal("阶段5：建码失败")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	code, err := c.CompleteHostedQRCode(ctx, uuid, profile.HostedGameID, lp.Body)
	if err != nil {
		t.Fatalf("阶段6：%v", err)
	}
	if os.Getenv("HEADLESS_NO_HOSTING") == "1" {
		res, err := launcher.ExchangeTicket(code, host[1])
		if err != nil || res == nil || res.Ticket == "" {
			t.Fatal("阶段7：无寄宿换票失败")
		}
		private := map[string]any{"ticket": res.Ticket, "login_channel": res.Channel, "platform": "ad", "uid": game[0], "device_id": host[1]}
		b, _ := json.MarshalIndent(private, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "private-game-ticket.json"), b, 0o600); err != nil {
			t.Fatal("保存无寄宿游戏票失败")
		}
		after, err := launcher.SavedAccount(sid)
		if err != nil || after != before {
			t.Fatal("阶段8：无寄宿换票后原槽内容发生变化或无法核对")
		}
		t.Log("阶段8：不带sid换得游戏票，回读确认原槽保持不变；仍需启动游戏验证选区")
		if os.Getenv("HEADLESS_LAUNCH_GAME") == "1" {
			launchHeadlessProbeGame(t, res.Channel, res.Ticket)
		}
		return
	}
	if before != "" && before != game[0] && os.Getenv("PROBE_ALLOW_REPLACE") != "1" {
		t.Fatal("阶段7：已取得授权码，但原槽属于另一账号；未获覆盖许可，停止写槽")
	}
	res, err := launcher.ExchangeTicketWithSid(code, host[1], sid)
	if err != nil || res == nil || res.Ticket == "" {
		t.Fatal("阶段7：换票失败")
	}
	after, err := launcher.SavedAccount(sid)
	if err != nil || after != game[0] {
		t.Fatal("阶段8：槽位回读未匹配目标")
	}
	res, err = launcher.TicketForAccount(game[0], host[1], sid)
	if err != nil || res == nil || res.Ticket == "" {
		t.Fatal("阶段9：按账号再次出票失败")
	}
	t.Log("阶段9：写槽和再次出票成功；尚需启动游戏实测渠道")
}

// launchHeadlessProbeGame 只在显式开启时启动真实游戏；不修改安装目录或已有账号。
// 只发本轮新票一次，不把“发出票”当成进入大厅；选区最终结果需观察屏幕。
func launchHeadlessProbeGame(t *testing.T, channel, ticket string) {
	t.Helper()
	if !win.IsElevated() {
		t.Fatal("阶段9：启动真实游戏需要管理员运行探针")
	}
	if fever.IsSkyRunning() {
		t.Fatal("阶段9：已有光遇运行，不启动第二个实例")
	}
	if _, exists := fever.Existing(); exists {
		t.Fatal("阶段9：平台IPC已被占用，不结束其它进程")
	}
	exe := resolveSkyPath()
	if exe == "" {
		t.Fatal("阶段9：没有配置光遇路径")
	}
	sent := make(chan error, 1)
	used := false
	var host *fever.Host
	host = fever.NewHost(func(req fever.TicketRequest) bool {
		if req.GameID != profile.ShortGameID {
			return false
		}
		if used {
			return false
		}
		used = true
		go func() { sent <- host.SendTicket(windows.Handle(req.SenderHwnd), channel, ticket) }()
		return true
	})
	if err := host.Start(); err != nil {
		t.Fatal("阶段9：建立IPC宿主失败")
	}
	defer host.Stop()
	proc, err := fever.StartSky(exe)
	if err != nil {
		t.Fatal("阶段9：启动光遇失败")
	}
	defer proc.Release()
	t.Logf("阶段9：已启动真实光遇 pid=%d", proc.Pid)
	select {
	case err := <-sent:
		if err != nil {
			t.Fatal("阶段10：IPC发票失败")
		}
		t.Log("阶段10：游戏已索票，IPC发票成功；等待45秒供观察渠道和大厅")
	case <-time.After(60 * time.Second):
		t.Fatal("阶段10：60秒内未收到游戏索票请求")
	}
	<-time.After(45 * time.Second)
	t.Log("阶段11：观察窗口结束；本测试不自动断言选区或进大厅成功")
}
