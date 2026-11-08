package app

// 用户档案字段探测（默认跳过）：
//
//	PROFILE_PROBE=1 go test ./internal/app/ -run TestZZProfile -v
//
// 用各账号自己的长期凭证调 mpay 的用户档案接口，放开 opt_fields，
// 找「平台选择」存在哪个字段。选过渠道的号（mumu）和没选过的对照。
import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"skylogin/internal/mpay"
)

func TestZZProfile(t *testing.T) {
	if os.Getenv("PROFILE_PROBE") == "" {
		t.Skip("没给 PROFILE_PROBE，跳过")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	// opt_fields 放开：加上抓包里没出现过的疑似字段
	const fields = "nickname,avatar,realname_status,mobile_bind_status,related_login_status,mask_related_mobile,user_platform,platform,login_platform,client_username,display_username"

	c := mpay.New()
	for i := range list {
		a := list[i]
		if a.Channel != "netease" || a.MpayToken == "" || a.UserID == "" || a.DeviceID == "" {
			continue
		}
		longID, _ := ticketContextForToken(a.MpayToken)
		q := url.Values{}
		q.Set("app_channel", "a50_sdk_cn")
		q.Set("app_mode", "2")
		q.Set("app_type", "games")
		q.Set("arch", "win_x64")
		q.Set("cv", "c4.21.0")
		q.Set("device_id", a.DeviceID)
		q.Set("game_id", longID)
		q.Set("gv", "0.0.0.0")
		q.Set("jf_game_id", "ma75")
		q.Set("opt_fields", fields)
		q.Set("token", a.MpayToken)
		q.Set("user_id", a.UserID)
		path := "/mpay/games/" + longID + "/devices/" + a.DeviceID + "/users/" + a.UserID
		out, err := c.GetRaw(path, q)
		if err != nil {
			t.Logf("%-24s 查询失败: %v", displayName(a), err)
			continue
		}
		resp := fmt.Sprint(out)
		t.Logf("%-24s → 长度%d", displayName(a), len(resp))
		if i := strings.Index(resp, "plat"); i >= 0 {
			t.Logf("  含plat字段: %s", resp[i-40:])
		} else {
			t.Logf("  无plat字样, 尾部: %s", resp[len(resp)-260:])
		}
	}
}
