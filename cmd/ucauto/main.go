// 探针：试 mtop.ieu.member.passport.login.autologinbycode 的参数名。
//
//	go run ./cmd/ucauto
//
// 扫码确认后轮询会返回 {"status":0,"vcode":"…","connectScene":"…"}，
// 页面紧接着用 vcode 调这个接口完成登录。这里先空跑，靠错误信息撞字段名。
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"skylogin/internal/ucqrcode"
)

func main() {
	c := ucqrcode.New()
	c.Verbose = false

	cases := []struct {
		name  string
		extra map[string]any
	}{
		{"空参数", map[string]any{}},
		{"vcode", map[string]any{"vcode": "test"}},
		{"code", map[string]any{"code": "test"}},
		{"loginCode", map[string]any{"loginCode": "test"}},
		{"vcode+connectScene", map[string]any{"vcode": "test", "connectScene": "JIUYOU_ANDROID_APP_JIUYOU_PC"}},
	}

	for _, cs := range cases {
		raw, err := c.CallWithExtra(ucqrcode.APIAutoLoginByCode, cs.extra)
		body := string(raw)
		if len(body) > 500 {
			body = body[:500] + "…"
		}
		fmt.Printf("%-22s err=%v\n", cs.name, err)
		if body != "" {
			var pretty any
			if json.Unmarshal([]byte(body), &pretty) == nil {
				b, _ := json.Marshal(pretty)
				body = string(b)
			}
			fmt.Printf("   data: %s\n", trim(body, 400))
		}
	}
	os.Exit(0)
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
