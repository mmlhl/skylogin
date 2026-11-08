package mpaydb

import (
	"os"
	"testing"
)

// 空库实验：put_account 对 0 字节输入能不能建出新库。
func TestZZPutEmpty(t *testing.T) {
	if os.Getenv("MPAYDB_PUT_TOKEN") == "" {
		t.Skip("跳过")
	}
	db, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	args := map[string]any{
		"game_id":            "aecfs5n2y4aaaakd-g-ma75",
		"login_channel":      "netease",
		"sdkuid":             os.Getenv("MPAYDB_PUT_UID"),
		"name":               "测试",
		"position":           0,
		"machine_identifier": MachineIdentifier(),
		"credentials":        map[string]any{"token": os.Getenv("MPAYDB_PUT_TOKEN"), "pc_ext_info": map[string]any{}},
		"create": map[string]any{
			"login_channel": "netease", "login_type": 0, "__user_login_method": 17,
			"__user_platform_selected": "", "__user_platform_selected_second": "",
			"__user_platform_selected_third": "", "__user_typed_username": "",
			"client_username": "测试", "display_username": "测试", "nickname": "",
			"avatar": "", "ext_access_token": "", "need_mask": false,
			"pc_ext_info": map[string]any{}, "related_login_status": 0,
			"mask_related_mobile": "",
		},
	}
	changed, newDB, result, err := db.Invoke(OpPut, nil, args)
	t.Logf("changed=%v newDB=%d result=%d字符 err=%v", changed, len(newDB), len(result), err)
	if err == nil && len(newDB) > 0 {
		os.WriteFile(os.Getenv("TEMP")+"\\mpay-new.db", newDB, 0o644)
		// 新库上再 list 一次，确认记录写进去了
		_, _, res2, err2 := db.Invoke(OpList, newDB, map[string]any{})
		t.Logf("新库 list: err=%v result=%s", err2, string(res2))
	}
}
