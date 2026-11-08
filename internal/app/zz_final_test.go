package app

// 自寄宿最终形态：a50 token 走 uni_sauth（gameid=a50）→ 确认自己的码 → 写槽。
//
//	SELFHOST_FINAL=1 URS_USER=… URS_PASS=… go test ./internal/app/ -run TestZZFinal -v -count=1

import (
	"fmt"
	"os"
	"testing"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
)

func TestZZFinal(t *testing.T) {
	if os.Getenv("SELFHOST_FINAL") == "" {
		t.Skip("跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := findAccount(list, "aebggifyh5w2dop4")
	if !ok || acc.A50Token == "" {
		t.Fatal("没有 a50 凭证（先跑 urs 探针）")
	}

	// ① a50 token 换 sid
	sid, lerr := launcher.Login(acc.UserID, acc.A50Token, acc.A50DeviceID)
	if lerr != nil || sid == "" {
		t.Fatalf("① 换 sid 失败: %v", lerr)
	}
	t.Logf("① sid OK（%d 字符）", len(sid))

	// ② 建一张 a50 托管码（is_remember=2）
	c := mpay.New()
	uuid, _, err := c.CreateLoginHostedFull(acc.A50DeviceID, profile.HostedGameID, profile.ShortGameID, "a50_sdk_cn", profile.HostedCv, "2")
	if err != nil {
		t.Fatalf("② 建码失败: %v", err)
	}
	t.Logf("② 码已建 uuid=%s", uuid)

	// ③ a50 token 走 uni_sauth——gameid 必须是 a50（token 的 g_i 才匹配）
	dev, derr := device.LoadOrCreateClient("E:\\skytest\\tmp\\final-device.json")
	if derr != nil {
		t.Fatalf("设备标识失败: %v", derr)
	}
	custom := map[string]any{
		"login_channel":      "netease",
		"app_channel":        "netease.allysdk3rd",
		"source_app_channel": "netease.allysdk3rd",
		"platform":           "ad",
		"source_platform":    "ad",
		"sdk_version":        "4.21.0",
		"deviceid":           acc.A50DeviceID,
		"username":           acc.Name,
	}
	sauth := netsec.NewSAUTH(acc.UserID, acc.A50Token, "a50", custom, dev)
	authResp, aerr := netsec.PostSigned(sauth, "a50")
	if aerr != nil {
		t.Fatalf("③ uni_sauth 失败: %v", aerr)
	}
	code, status, msg := toStr(authResp["code"]), toStr(authResp["status"]), toStr(authResp["msg"])
	t.Logf("③ uni_sauth(gameid=a50): code=%s status=%s msg=%s", code, status, msg)
	if code != "200" {
		return
	}

	// ④ 组登录包并确认码
	lp, perr := netsec.NewLoginPayloadChan(authResp, acc.A50Token, acc.A50Token, acc.UserID, "0", fmt.Sprint(time.Now().Unix()), dev, netsec.NeteaseChannelInfo())
	if perr != nil {
		t.Fatalf("④ 组包失败: %v", perr)
	}
	out, cerr := c.ConfirmForm(lp.Body)
	if cerr != nil {
		t.Fatalf("④ confirm 失败: %v", cerr)
	}
	t.Logf("④ confirm: %.300s", fmt.Sprint(out))

	// ⑤ code + sid 换槽位票（写槽验证）
	out5, _ := out["code"]
	cd := toStr(out5)
	if cd == "" {
		t.Log("⑤ confirm 响应里没有 code，无法换槽位票")
		return
	}
	r, terr := launcher.TicketForCode(cd, sid)
	if terr != nil {
		t.Logf("⑤ 槽位换票失败: %v", terr)
		return
	}
	t.Logf("⑤ ✅✅ 自寄宿写槽成功！ticket=%d 字符——mmgyyx 已占自己的槽", len(r.Ticket))
}
