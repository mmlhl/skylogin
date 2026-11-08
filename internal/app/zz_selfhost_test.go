package app

import (
	"os"
	"testing"

	"skylogin/internal/launcher"
)

func TestZZSelfHost(t *testing.T) {
	if os.Getenv("REMEMBER_SELFHOST") == "" {
		t.Skip("没给 REMEMBER_SELFHOST，跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	for i := range list {
		a := list[i]
		if a.Channel != "netease" || a.MpayToken == "" || a.UserID == "" {
			continue
		}
		sid, lerr := launcher.Login(a.UserID, a.MpayToken, a.DeviceID)
		if lerr != nil || sid == "" {
			t.Logf("%-24s 换 sid 失败: %v", displayName(a), lerr)
			continue
		}
		hosted, serr := launcher.SavedAccount(sid)
		if serr != nil {
			t.Logf("%-24s 读自己槽失败: %v", displayName(a), serr)
			continue
		}
		who := hosted
		if who == "" {
			who = "（空槽）"
		}
		r, terr := launcher.TicketForAccount(a.UserID, a.DeviceID, sid)
		if terr != nil {
			t.Logf("%-24s 自己槽=另一身份:%s 自寄宿出票: %v", displayName(a), who, terr)
			continue
		}
		t.Logf("%-24s 自己槽=另一身份:%s ✅ 自寄宿出票成功（票 %d 字符, channel=%s）",
			displayName(a), who, len(r.Ticket), r.Channel)
	}
}
