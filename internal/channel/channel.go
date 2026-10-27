// Package channel 集中管理所有渠道服的接入细节。
//
// 一个渠道要接进来，需要四块东西：
//
//	常量       渠道名 / sdk 版本 / 应用标识（供 uni_sauth 与登录包使用）
//	验证客户端 凭证在线校验与静默续期（能做多少做多少，做不了的说明原因）
//	登录脚本   注入到官方登录页里，把登录结果抓回来
//	窗口参数   登录窗标题与尺寸（按渠道页面的真实形态定）
//
// app 层的 channellogin.go 只认本包的注册表，不关心具体渠道——
// 以后加新渠道，在这里加一个文件、注册一行，app 层不用动。
package channel

import "skylogin/internal/netsec"

// 渠道标识（账号文件里的 channel 字段、前端 channelLabel 都用它）。
const (
	KeyBili  = "bilibili"
	KeyM4399 = "m4399"
	KeyVivo  = "vivo"
)

// Known 判断是不是本工具管理的渠道服账号。
func Known(key string) bool {
	switch key {
	case KeyBili, KeyM4399, KeyVivo:
		return true
	}
	return false
}

// LabelOf 渠道的中文名（界面展示用）。
func LabelOf(key string) string {
	switch key {
	case KeyBili:
		return "B站"
	case KeyM4399:
		return "4399"
	case KeyVivo:
		return "vivo"
	}
	return key
}

// LoginWindow 渠道登录窗的外观与注入脚本（子进程打开窗口时用）。
type LoginWindow struct {
	Title         string
	Width, Height uint
	Script        func(target string) string
}

// LoginWindowOf 登录窗按渠道页面的真实形态定尺寸：
// B站是桌面版宽页，4399 / vivo 瘦身后是手机版窄窗。
func LoginWindowOf(key string) LoginWindow {
	switch key {
	case KeyBili:
		return LoginWindow{Title: "B站账号登录", Width: 1100, Height: 860, Script: func(string) string { return BiliLoginScript() }}
	case KeyM4399:
		return LoginWindow{Title: "4399账号登录", Width: 430, Height: 700, Script: func(string) string { return M4399LoginScript() }}
	case KeyVivo:
		return LoginWindow{Title: "vivo账号登录", Width: 460, Height: 720, Script: VivoLoginScript}
	}
	return LoginWindow{Title: "渠道登录", Width: 460, Height: 720, Script: func(string) string { return "" }}
}

// InfoOf 渠道的 uni_sauth 身份参数。不认识的渠道回落华为默认值。
func InfoOf(key string) netsec.ChannelInfo {
	switch key {
	case KeyBili:
		return biliInfo
	case KeyM4399:
		return m4399Info
	case KeyVivo:
		return vivoInfo
	}
	return netsec.HuaweiChannelInfo()
}

// 三个渠道的身份参数。渠道名与 sdk 版本是渠道服服务端点名的取值，
// 均以官方客户端实测为准。
var (
	biliInfo = netsec.ChannelInfo{
		Login:          "bilibili_sdk",
		SDKVersion:     "5.9.6",
		GetAccessToken: "1",
		Realname:       `{"realname_type": 0, "age": 22}`,
	}
	m4399Info = netsec.ChannelInfo{
		Login:          "4399com",
		SDKVersion:     "3.16.0",
		GetAccessToken: "1",
		Realname:       `{"realname_type": 0, "age": 22}`,
	}
	vivoInfo = netsec.ChannelInfo{
		Login:          "nearme_vivo",
		SDKVersion:     "4.7.2.0",
		GetAccessToken: "1",
		Realname:       `{"realname_type": 0, "age": 22}`,
	}
)
