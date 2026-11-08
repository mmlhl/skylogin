package app

// 本地探针（默认跳过）：列出各网易账号的凭证等级与渠道，排查"选了区不生效"。
//
//	go test ./internal/app -run TestZZCredSurvey -v
//
// 只读本机账号记录，不发网络请求。

import (
	"testing"
)

func TestZZCredSurvey(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	for i := range list {
		a := list[i]
		if a.Channel != "netease" {
			continue
		}
		t.Logf("账号=%s uid=%s region=%q sa=%.0f tokenLen=%d method=%s source=%s 可渠道签发=%v",
			displayName(a), a.UserID, a.Region, tokenSaValue(a.MpayToken),
			len(a.MpayToken), a.LoginMethod, a.Source, channelMintable(a))
		t.Logf("    g_i=%s 拦截原因=%q", tokenGI(a.MpayToken), regionBlockReason(a))
	}
}
