package app

// 一次性探针：同一账号先后登录 ma75 / a50 两个上下文，验证两份凭证会不会互相顶掉。
// 只读账号库取密码，不保存任何新凭证（不影响现有账号状态）。
//
//	ZZ_TWOLOGIN=账号名 go test ./internal/app -run TestZZTwoLogin -v -timeout 300s
//
// 2026-11-09 实测结论：
//   - 两个上下文互不影响：先登 ma75 再登 a50，回头用 ma75 的 token 签票依然成功；
//   - 同一上下文、同一设备号连续登两次，旧的那份也仍能换 sid（本次没复现顶号）；
//   - 两边设备号由 SDK 按 app 分配，本来就不一样（…qrqapv-d / …ngo6bk-d）。
//
// 注意：这里只验证了"服务端 token 还能不能用"。顶号在别处确实存在（用户的
// 实际反馈、以及游戏客户端会话层面），所以 ensureHostCred 之类"能换到 sid
// 就别重登"的谨慎逻辑仍然保留，别拿这个探针的结论去删那些保护。

import (
	"os"
	"testing"

	"skylogin/internal/launcher"
	"skylogin/internal/mpay"
	"skylogin/internal/profile"
)

func TestZZTwoLogin(t *testing.T) {
	target := os.Getenv("ZZ_TWOLOGIN")
	if target == "" {
		t.Skip("没给 ZZ_TWOLOGIN，跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatalf("读账号失败: %v", err)
	}
	if target == "list" {
		for _, a := range list {
			hosted, by := isHostedBy(a.UserID)
			inSlot, _ := slotHostedBy(a.UserID)
			t.Logf("%-22s 有密码=%-5v 被寄宿=%-5v 当宿主槽里=%q 宿主=%q",
				displayName(a), hasSavedPassword(a), hosted, inSlot, by)
		}
		return
	}
	var acc Account
	found := false
	for _, a := range list {
		if (a.Name == target || a.Username == target) && hasSavedPassword(a) {
			acc, found = a, true
			break
		}
	}
	if !found {
		t.Fatalf("%q 没有保存密码，无法测", target)
	}
	user, pass, err := accountPassword(acc)
	if err != nil {
		t.Fatalf("取密码失败: %v", err)
	}
	t.Logf("测试账号 %s（当前 MpayToken=%d 字符 sa=%v，A50Token=%d 字符）",
		displayName(acc), len(acc.MpayToken), acc.TokenSa, len(acc.A50Token))

	// ① 先登游戏身份（ma75）
	r1, err := HeadlessPasswordLogin(user, pass, profile.LongGameID)
	if err != nil {
		t.Fatalf("ma75 登录失败: %v", err)
	}
	t.Logf("① ma75 登录成功：token=%d 字符 gameID=%s device=%s",
		len(r1.Token), r1.GameID, r1.DeviceID)

	// ② 再登平台身份（a50）——如果会顶号，① 的 token 应该在这一步之后失效
	r2, err := HeadlessPasswordLogin(user, pass, profile.HostedGameID)
	if err != nil {
		t.Fatalf("a50 登录失败: %v", err)
	}
	t.Logf("② a50 登录成功：token=%d 字符 gameID=%s device=%s",
		len(r2.Token), r2.GameID, r2.DeviceID)

	// ③ 验证 ① 的凭证还能不能用：用 ma75 上下文签一张票
	_, _, e1 := mpay.New().CreateTicketFor(r1.Token, r1.UserID, r1.DeviceID,
		profile.LongGameID, profile.ShortGameID)
	if e1 != nil {
		t.Logf("③ ma75 凭证签票失败（说明被 a50 那次登录顶掉了）: %v", e1)
	} else {
		t.Logf("③ ma75 凭证签票成功（没被顶掉）")
	}

	// ④ 验证 ② 的凭证：换加载湾会话（宿主资格就看这一步）
	sid, e2 := launcher.Login(r2.UserID, r2.Token, r2.DeviceID)
	if e2 != nil || sid == "" {
		t.Logf("④ a50 凭证换 sid 失败: %v", e2)
	} else {
		t.Logf("④ a50 凭证换 sid 成功（%d 字符）", len(sid))
	}

	// ⑤ 反向再验一次 ①：确认上面 ③ 的结果不是因为顺序碰巧
	if _, _, e3 := mpay.New().CreateTicketFor(r1.Token, r1.UserID, r1.DeviceID,
		profile.LongGameID, profile.ShortGameID); e3 != nil {
		t.Logf("⑤ ma75 凭证再签一次失败: %v", e3)
	} else {
		t.Logf("⑤ ma75 凭证再签一次仍成功")
	}

	// ⑥ 同一个上下文 + 同一个设备号再登一次（a50 用 r2 的设备号）：
	//    这一步是为了把"顶号"的边界钉死——顶号只发生在**同上下文重复登录**，
	//    而且只顶掉它自己那份，另一个上下文（ma75）不受影响。
	r3, err := HeadlessPasswordLogin(user, pass, profile.HostedGameID)
	if err != nil {
		t.Fatalf("a50 第二次登录失败: %v", err)
	}
	t.Logf("⑥ a50 再登一次：token=%d 字符 device=%s（与 ② 同设备=%v）",
		len(r3.Token), r3.DeviceID, r3.DeviceID == r2.DeviceID)

	if sid, e := launcher.Login(r2.UserID, r2.Token, r2.DeviceID); e != nil || sid == "" {
		t.Logf("⑦ 旧的 a50 凭证（②那份）换 sid 失败 → 被同上下文重登顶掉了: %v", e)
	} else {
		t.Logf("⑦ 旧的 a50 凭证仍能换 sid（没被顶掉）")
	}
	if _, _, e := mpay.New().CreateTicketFor(r1.Token, r1.UserID, r1.DeviceID,
		profile.LongGameID, profile.ShortGameID); e != nil {
		t.Logf("⑧ ma75 凭证被 a50 重登影响了: %v", e)
	} else {
		t.Logf("⑧ ma75 凭证不受影响（跨上下文不顶号）")
	}
	if sid, e := launcher.Login(r3.UserID, r3.Token, r3.DeviceID); e != nil || sid == "" {
		t.Logf("⑨ 新的 a50 凭证换 sid 失败: %v", e)
	} else {
		t.Logf("⑨ 新的 a50 凭证换 sid 成功（%d 字符）", len(sid))
	}
}

// 顺带验一下「刷新凭证」的静默重登接口（会真实登录并保存）。
//
//	ZZ_RELOGIN_CHECK=账号名 go test ./internal/app -run TestZZReloginCheck -v -timeout 300s
func TestZZReloginCheck(t *testing.T) {
	target := os.Getenv("ZZ_RELOGIN_CHECK")
	if target == "" {
		t.Skip("没给 ZZ_RELOGIN_CHECK，跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatalf("读账号失败: %v", err)
	}
	var acc Account
	found := false
	for _, a := range list {
		if a.Name == target || a.Username == target {
			acc, found = a, true
			break
		}
	}
	if !found {
		t.Fatalf("没找到 %q", target)
	}
	t.Logf("重登前：cred=%s MpayToken=%d A50Token=%d 有密码=%v",
		acc.CredState, len(acc.MpayToken), len(acc.A50Token), hasSavedPassword(acc))

	res, err := ReloginAccount(acc.PlayerID)
	if err != nil {
		t.Fatalf("静默重登失败: %v", err)
	}
	t.Logf("静默重登成功: %v", res)

	after, _ := findAccountByUserID(acc.UserID)
	t.Logf("重登后：cred=%s MpayToken=%d sa=%v A50Token=%d",
		after.CredState, len(after.MpayToken), after.TokenSa, len(after.A50Token))
}
