package mpaydb

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// 机器标识格式扫描：找出能与现有库匹配的写法。
func TestZZIdentVariants(t *testing.T) {
	src := os.Getenv("MPAYDB_PUT_DB")
	uid := os.Getenv("MPAYDB_PUT_UID")
	token := os.Getenv("MPAYDB_PUT_TOKEN")
	if src == "" || uid == "" || token == "" {
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

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.READ|registry.WOW64_64KEY)
	if err == nil {
		defer k.Close()
	}
	guid, _, _ := k.GetStringValue("MachineGuid")
	base := MachineIdentifier()

	ids := map[string]string{
		"原样":        base,
		"去尾点":       strings.TrimRight(base, "."),
		"大写":        strings.ToUpper(base),
		"小写":        strings.ToLower(base),
		"去下划线":      strings.ReplaceAll(base, "_", ""),
		"MachineGuid": guid,
		"数字0":       "0",
	}
	for name, id := range ids {
		if id == "" {
			continue
		}
		dbCopy := make([]byte, len(original))
		copy(dbCopy, original)
		args := map[string]any{
			"game_id": "aecglf6ee4aaaarz-g-a50", "login_channel": "netease", "sdkuid": uid,
			"name": "测试", "position": 0, "machine_identifier": id,
			"credentials": map[string]any{"token": token, "pc_ext_info": map[string]any{}},
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
		_, newDB, _, err := db.Invoke(OpPut, dbCopy, args)
		if err != nil {
			t.Logf("%-12s(%d字符) -> %v", name, len(id), err)
		} else {
			t.Logf("%-12s(%d字符) -> ✅ 成功！新库 %d 字节", name, len(id), len(newDB))
		}
	}
}
