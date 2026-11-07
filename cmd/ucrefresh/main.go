package main

// 临时探针：验证 refreshToken 能不能换到新 sid。
//
//	go run ./cmd/ucrefresh
//
// 凭证从账号库里读（和主程序同一份），不读环境变量、不重新登录，
// 所以能测"只用续期令牌、不碰密码"这条路到底通不通。

import "skylogin/internal/app"

func main() {
	app.ProbeUCRefresh()
}
