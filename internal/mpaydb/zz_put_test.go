// 诊断：put_account 变体扫描（默认跳过）。
//
//	MPAYDB_PUT_DB=<库路径> go test ./internal/mpaydb/ -run TestZZPutVariants -v
//
// 在库的**副本**上逐个试参数变体，找出 status=1003 的触发条件。
package mpaydb

import (
	"os"
	"testing"
)

func TestZZPutVariants(t *testing.T) {
	src := os.Getenv("MPAYDB_PUT_DB")
	if src == "" {
		t.Skip("没给 MPAYDB_PUT_DB，跳过")
	}
	original, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("读库失败: %v", err)
	}
	db, err := New()
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	defer db.Close()

	uid := os.Getenv("MPAYDB_PUT_UID")
	token := os.Getenv("MPAYDB_PUT_TOKEN")
	if uid == "" || token == "" {
		t.Fatal("需要 MPAYDB_PUT_UID / MPAYDB_PUT_TOKEN")
	}

	mk := func(mutate func(args map[string]any)) map[string]any {
		args := map[string]any{
			"game_id":            "aecfs5n2y4aaaakd-g-ma75",
			"login_channel":      "netease",
			"sdkuid":             uid,
			"name":               "测试",
			"position":           0,
			"machine_identifier": MachineIdentifier(),
			"credentials": map[string]any{
				"token":       token,
				"pc_ext_info": map[string]any{},
			},
			"create": map[string]any{
				"login_channel":                     "netease",
				"login_type":                        0,
				"__user_login_method":               17,
				"__user_platform_selected":          "",
				"__user_platform_selected_second":   "",
				"__user_platform_selected_third":    "",
				"__user_typed_username":             "",
				"client_username":                   "测试",
				"display_username":                  "测试",
				"nickname":                          "",
				"avatar":                            "",
				"ext_access_token":                  "",
				"need_mask":                         false,
				"pc_ext_info":                       map[string]any{},
				"related_login_status":              0,
				"mask_related_mobile":               "",
			},
		}
		if mutate != nil {
			mutate(args)
		}
		return args
	}

	variants := []struct {
		name string
		args map[string]any
	}{
		{"基线", mk(nil)},
		{"机器标识=0", mk(func(a map[string]any) { a["machine_identifier"] = "0" })},
		{"机器标识=空", mk(func(a map[string]any) { a["machine_identifier"] = "" })},
		{"无机器标识", mk(func(a map[string]any) { delete(a, "machine_identifier") })},
		{"短game_id", mk(func(a map[string]any) { a["game_id"] = "ma75" })},
		{"无create", mk(func(a map[string]any) { delete(a, "create") })},
		{"无credentials", mk(func(a map[string]any) { delete(a, "credentials") })},
		{"无position", mk(func(a map[string]any) { delete(a, "position") })},
		{"无name", mk(func(a map[string]any) { delete(a, "name") })},
	}

	for _, v := range variants {
		dbCopy := make([]byte, len(original))
		copy(dbCopy, original)
		_, newDB, _, err := db.Invoke(OpPut, dbCopy, v.args)
		if err != nil {
			t.Logf("%-14s -> 失败: %v", v.name, err)
			continue
		}
		t.Logf("%-14s -> 成功，新库 %d 字节（原 %d 字节）", v.name, len(newDB), len(dbCopy))
	}
}
