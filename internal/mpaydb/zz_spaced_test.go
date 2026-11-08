package mpaydb

import (
	"os"
	"testing"
)

// 编码差异探测：带空格的 JSON（Python json.dumps 默认风格）。
func TestZZSpaced(t *testing.T) {
	src := os.Getenv("MPAYDB_PUT_DB")
	uid := os.Getenv("MPAYDB_PUT_UID")
	token := os.Getenv("MPAYDB_PUT_TOKEN")
	if src == "" || uid == "" {
		t.Skip("跳过")
	}
	original, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	db, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	shapes := []string{
		`{"name": "x"}`,
		`{"sdkuid": "` + uid + `"}`,
		`{"game_id": "aecglf6ee4aaaarz-g-a50", "login_channel": "netease", "sdkuid": "` + uid + `", "name": "测试", "position": 0, "machine_identifier": "0", "credentials": {"token": "` + token + `", "pc_ext_info": {}}, "create": {"login_channel": "netease", "login_type": 0, "__user_login_method": 17, "__user_platform_selected": "", "__user_platform_selected_second": "", "__user_platform_selected_third": "", "__user_typed_username": "", "client_username": "测试", "display_username": "测试", "nickname": "", "avatar": "", "ext_access_token": "", "need_mask": false, "pc_ext_info": {}, "related_login_status": 0, "mask_related_mobile": ""}}`,
	}
	for i, shape := range shapes {
		dbCopy := make([]byte, len(original))
		copy(dbCopy, original)
		_, newDB, _, err := db.InvokeJSON(7, dbCopy, []byte(shape))
		if err != nil {
			t.Logf("形状%d -> %v", i, err)
		} else {
			t.Logf("形状%d -> ✅ 成功 result=%s", i, truncateStr(string(newDB), 80))
		}
	}
}
