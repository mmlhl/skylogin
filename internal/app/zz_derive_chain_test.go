package app

// 链式派生实验：a50 账号级 → ma75 会话级 → 再用它继续派生。
//
// 问题：a50 先签一个会话级 ma75，再拿这个 ma75 继续走，会不会变样？
// 已知单层结果（2026-11-09）：a50 sa=-9 → create_ticket(a50) → login/ticket(ma75) → sa=3，
// 用它做 confirm 是 1306。
//
// 这里补的是"继续"的另一层含义：把派生出来的 sa=3 当作新起点，再签票再兑换，
// 观察等级、身份、会话字段是否逐层变化（例如是否某层变成账号级或换了 g_i）。
// 每层都记录 sa / g_i / 是否带 lt / odi，最后用末层做一次完整确认尝试。
//
// 开关：DERIVE_CHAIN_DIR=<本地忽略目录>；DERIVE_CHAIN_DEPTH 默认 3。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

func TestZZDeriveChain(t *testing.T) {
	dir := os.Getenv("DERIVE_CHAIN_DIR")
	if dir == "" {
		t.Skip("未开启链式派生实验")
	}
	if os.Getenv("URS_USER") == "" || os.Getenv("URS_PASS") == "" {
		t.Fatal("缺少本机登录凭据")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal("建立实验目录失败")
	}
	write := func(name string, v any) {
		t.Helper()
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal("编码记录失败")
		}
		if os.WriteFile(filepath.Join(dir, name+".json"), b, 0o600) != nil {
			t.Fatal("保存记录失败")
		}
	}
	depth := 3
	if v, err := strconv.Atoi(os.Getenv("DERIVE_CHAIN_DEPTH")); err == nil && v > 0 && v <= 6 {
		depth = v
	}
	summary := map[string]any{}
	defer func() { write("summary", summary) }()

	dev, err := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if err != nil {
		t.Fatal("读取客户端配置失败")
	}

	login, err := HeadlessPasswordLogin(os.Getenv("URS_USER"), os.Getenv("URS_PASS"), profile.HostedGameID)
	if err != nil {
		t.Fatal("平台身份登录失败")
	}
	write("a50-login", login)

	token := login.Token
	longID, shortID := profile.HostedGameID, "a50"
	levels := []map[string]any{{
		"level": 0, "sa": tokenSaValue(token), "gi": tokenGI(token),
		"has_lt": tokenLoginUnix(token) > 0, "len": len(token),
	}}
	t.Logf("第 0 层（平台原生）：g_i=%s sa=%.0f len=%d", tokenGI(token), tokenSaValue(token), len(token))

	for i := 1; i <= depth; i++ {
		ticket, _, err := mpay.New().CreateTicketFor(token, login.UserID, login.DeviceID, longID, shortID)
		if err != nil {
			summary["stop_at"] = i
			summary["stop_error"] = "create_ticket failed"
			t.Logf("第 %d 层签票失败，链条中断", i)
			break
		}
		res, err := mpay.New().LoginTicket(ticket, login.DeviceID, profile.LongGameID, profile.ShortGameID)
		if err != nil {
			summary["stop_at"] = i
			summary["stop_error"] = "login_ticket failed"
			t.Logf("第 %d 层兑换失败，链条中断", i)
			break
		}
		write("level-"+strconv.Itoa(i), res)
		user, _ := res["user"].(map[string]any)
		next, _ := user["token"].(string)
		if next == "" {
			summary["stop_at"] = i
			summary["stop_error"] = "empty token"
			break
		}
		token = next
		// 之后统一用 ma75 上下文继续
		longID, shortID = profile.LongGameID, profile.ShortGameID
		fields := tokenFields(token)
		levels = append(levels, map[string]any{
			"level": i, "sa": tokenSaValue(token), "gi": tokenGI(token),
			"has_lt": tokenLoginUnix(token) > 0, "len": len(token),
			"t": fields["t"], "has_odi": fields["odi"] != nil,
		})
		t.Logf("第 %d 层：g_i=%s sa=%.0f len=%d t=%v", i, tokenGI(token), tokenSaValue(token), len(token), fields["t"])
	}
	summary["levels"] = levels

	// 用末层尝试完整确认，确认"继续"之后是否仍然 1306
	custom := map[string]any{
		"login_channel":      "netease",
		"app_channel":        "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd",
		"platform":           "ad",
		"source_platform":    "ad",
		"sdk_version":        "4.21.0",
		"deviceid":           login.DeviceID,
		"username":           os.Getenv("URS_USER"),
	}
	sauth := netsec.NewSAUTH(login.UserID, token, profile.ShortGameID, custom, dev)
	auth, err := netsec.PostSigned(sauth, profile.ShortGameID)
	if err != nil {
		summary["final_sauth_error"] = err.Error()
		t.Log("末层声明请求失败")
		return
	}
	write("final-sauth", map[string]any{"request": sauth, "response": auth})
	summary["final_sauth_code"] = auth["code"]
	t.Logf("末层声明：code=%v first_login_platform=%v", auth["code"], auth["first_login_platform"])
	if toStr(auth["code"]) != "200" {
		return
	}

	lp, err := netsec.NewLoginPayloadFromSAUTH(auth, sauth, token, "0",
		strconv.FormatInt(time.Now().Unix(), 10), dev, netsec.NeteaseChannelInfo())
	if err != nil {
		summary["payload"] = "failed"
		return
	}
	lp.Body["token"] = token
	lp.Body["device_id"] = login.DeviceID

	c := mpay.New()
	c.AppChannel = "netease"
	uuid, _, err := c.CreateLoginHostedFull(login.DeviceID, profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
	if err != nil {
		summary["create_login"] = "failed"
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if _, err := c.CompleteHostedQRCode(ctx, uuid, profile.HostedGameID, lp.Body); err != nil {
		summary["final_authorize_error"] = err.Error()
		t.Logf("末层确认失败：%v", err)
		return
	}
	summary["final_authorize"] = "ok"
	t.Log("末层确认与轮询通过")
}
