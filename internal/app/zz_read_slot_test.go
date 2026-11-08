package app

// 只读诊断：用一次新鲜平台登录换 sid，看记住槽里存的 id 是什么形态。
//
// 动机：槽位实验里 "命中本账号=false" 可能是误判——槽里存的也许不是 MPay 的
// UserID，而是平台侧的 account_id。不确认这一点，就没法判断"带 sid 换票到底写没写槽"。
//
// 只输出长度与前缀，不输出完整 id 与任何凭证。
// 开关：SLOT_READ_DIR=<本地忽略目录>

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/launcher"
	"skylogin/internal/profile"
)

func TestZZReadSlot(t *testing.T) {
	dir := os.Getenv("SLOT_READ_DIR")
	if dir == "" {
		t.Skip("未开启槽位只读诊断")
	}
	if os.Getenv("URS_USER") == "" || os.Getenv("URS_PASS") == "" {
		t.Fatal("缺少本机登录凭据")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal("建立实验目录失败")
	}
	out := map[string]any{}
	defer func() {
		b, _ := json.MarshalIndent(out, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, "slot-read.json"), b, 0o600)
	}()

	// 用平台身份登录：只有 a50 账号级 token 换得出平台会话 sid
	login, err := HeadlessPasswordLogin(os.Getenv("URS_USER"), os.Getenv("URS_PASS"), profile.HostedGameID)
	if err != nil {
		t.Fatal("平台身份登录失败")
	}
	out["userIDLen"] = len(login.UserID)
	out["userIDPre"] = prefix(login.UserID, 2)
	out["userIDIsAE"] = len(login.UserID) > 1 && login.UserID[:2] == "ae"

	sid, serr := launcher.Login(login.UserID, login.Token, login.DeviceID)
	if serr != nil || sid == "" {
		out["sid"] = "failed"
		if serr != nil {
			out["sidErr"] = trunc(serr.Error(), 120)
		}
		t.Logf("换 sid 失败：%v", serr)
		return
	}
	out["sid"] = "ok"
	out["sidLen"] = len(sid)

	saved, verr := launcher.SavedAccount(sid)
	out["savedErr"] = verr != nil
	out["savedEmpty"] = saved == ""
	out["savedLen"] = len(saved)
	if saved != "" {
		out["savedPre"] = prefix(saved, 2)
		out["savedIsAE"] = len(saved) > 1 && saved[:2] == "ae"
	}
	out["savedEqualsUserID"] = saved == login.UserID

	// 手机抓包里的 user_id（B）。平台账号 A 与扫码账号 B 本来就可以不同：
	// 槽挂在 A 的平台身份上，里面放的是被扫的 B。
	if phone := os.Getenv("PHONE_UID"); phone != "" {
		out["phoneUIDLen"] = len(phone)
		out["phoneUIDPre"] = prefix(phone, 2)
		out["savedEqualsPhone"] = saved == phone
		out["userEqualsPhone"] = login.UserID == phone
		t.Logf("手机侧账号 B：长度 %d、前缀 %s；槽内等于B=%v；平台账号A等于B=%v",
			len(phone), prefix(phone, 2), saved == phone, login.UserID == phone)
	}

	t.Logf("本账号 UserID：长度 %d、前缀 %s（ae 开头=%v）", len(login.UserID), prefix(login.UserID, 2), out["userIDIsAE"])
	t.Logf("槽内容：空=%v 长度=%d 前缀=%v ae开头=%v 等于UserID=%v",
		saved == "", len(saved), out["savedPre"], out["savedIsAE"], saved == login.UserID)
}

func prefix(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
