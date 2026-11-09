package app

// 本地探针（默认跳过）：列出各网易账号的凭证等级与渠道，排查"选了区不生效"。
//
//	ZZ_SURVEY=1 go test ./internal/app -run TestZZCredSurvey -v
//
// 只读本机账号记录，不发网络请求。
//
// 必须默认跳过：CI 上没有账号库，直接读 accounts.json 会让 go test 整体失败
// （曾经只在 testing.Short() 下跳过，导致远程构建挂掉）。

import (
	"os"
	"testing"
)

func TestZZCredSurvey(t *testing.T) {
	if os.Getenv("ZZ_SURVEY") == "" {
		t.Skip("未开启账号凭证巡查")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Skip("本机没有账号库，跳过")
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
