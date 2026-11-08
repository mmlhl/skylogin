package app

// 一次性探针：对指定账号跑一次完整的渠道签发，打印凭证等级与结果。
//
// 用来区分"凭证等级能不能走完 confirm 出票"：
// 账号级（sa=-9 / -99）预期成功；会话级（sa=3）如果失败会看到 1306。
//
// 开关：ZZ_MINT_ACC=<账号 PlayerID>

import (
	"os"
	"testing"
)

func TestZZMintByAcc(t *testing.T) {
	target := os.Getenv("ZZ_MINT_ACC")
	if target == "" {
		t.Skip("未设置 ZZ_MINT_ACC")
	}
	acc, ok := findAccountByUserID(target)
	if !ok {
		t.Fatalf("找不到账号 %s", target)
	}
	t.Logf("%s：g_i=%s sa=%.0f len=%d region=%q",
		displayName(acc), tokenGI(acc.MpayToken), tokenSaValue(acc.MpayToken),
		len(acc.MpayToken), acc.Region)
	if acc.Region == "" {
		t.Fatal("没选渠道，无法验证签发")
	}

	ticket, _, err := mintTicket(acc, mustDevice(t))
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	t.Logf("签发成功：票 %d 字符（sa=%.0f）", len(ticket), tokenSaValue(acc.MpayToken))
}
