package main

// 临时探针：验证 ucsdk 的加密层能否被九游服务端接受。
// 跑一次 sendSmsCode，看 state.code 是不是 1。跑完即删。

import (
	"fmt"
	"os"

	"skylogin/internal/ucsdk"
)

func main() {
	mobile := os.Getenv("UC_MOBILE")

	c := ucsdk.New(ucsdk.SkyGame())
	c.Verbose = true

	// 最小探针：请求公钥。能拿到明文就说明信封完全正确
	key, err := c.FetchSecurityKey()
	if err != nil {
		fmt.Println("取公钥失败:", err)
		if code := ucsdk.Code(err); code != 0 {
			fmt.Printf("业务码 = %d\n", code)
		}
		return
	}
	if len(key) > 70 {
		fmt.Printf("取公钥成功，%d 字符，开头 %s\n", len(key), key[:70])
	} else {
		fmt.Printf("取公钥成功: %q\n", key)
	}
	fmt.Println()

	// 对照探针：同域、无副作用的业务接口。
	// 能拿到明文就说明信封整体可用，发码失败就只可能是业务参数问题。
	if raw, err := c.Call(ucsdk.HostAccount, ucsdk.ServiceUserInfo, map[string]any{}); err != nil {
		fmt.Println("getUserSimpleInfo 失败:", err)
		if code := ucsdk.Code(err); code != 0 {
			fmt.Printf("业务码 = %d\n", code)
		}
	} else {
		s := string(raw)
		if len(s) > 200 {
			s = s[:200] + "…"
		}
		fmt.Println("getUserSimpleInfo 返回:", s)
	}
	fmt.Println()

	if mobile == "" {
		fmt.Println("未设置 UC_MOBILE，只验证到公钥为止")
		return
	}
	if err := c.SendSmsCode(mobile, "86"); err != nil {
		fmt.Println("发码失败:", err)
		if code := ucsdk.Code(err); code != 0 {
			fmt.Printf("业务码 = %d\n", code)
		}
		return
	}
	fmt.Println("发码成功，服务端返回 code=1")
	fmt.Println("注意：这会真的给该手机号发一条短信")
}
