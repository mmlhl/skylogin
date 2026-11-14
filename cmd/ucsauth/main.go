// 探针：把网页扫码拿到的 ieu 会话票据（pt109265…）直接喂给 uni_sauth，
// 看服务端认不认。认的话，扫码就能直接当九游渠道凭证用。
//
//	go run ./cmd/ucsauth <sessionid> [uid]
package main

import (
	"fmt"
	"os"

	"skylogin/internal/app"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: ucsauth <sessionid> [uid]")
		os.Exit(1)
	}
	sid := os.Args[1]
	uid := "0" // 九游的 sdkuid 固定是 "0"
	if len(os.Args) > 2 {
		uid = os.Args[2]
	}
	err := app.VerifyChannelCredential("uc_platform", uid, sid)
	if err != nil {
		fmt.Println("uni_sauth 失败:", err)
		os.Exit(1)
	}
	fmt.Println("uni_sauth 通过 ✔ 这个会话可以直接当九游凭证用")
}
