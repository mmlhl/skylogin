package main

// 无头登录子进程：从 stdin 读一份请求 JSON，跑一次 mpay.dll 的无头动作，
// 结果写进 --out 指定的文件。
//
// 为什么要单开一个入口：
//   mpay.dll 的窗口、消息循环、接口实例必须绑在同一个 OS 线程上，登录失败或
//   账号异常时还可能直接把进程带走。放在主界面进程里跑，崩一次整个工具都得重启。
//   所以和"账号密码登录"（--mpaylogin）一样走子进程，主进程只负责递参数、读结果。
//
// 密码为什么走 stdin 而不是命令行：命令行参数在进程列表里对同机任何用户可见，
// stdin 是私有管道，读完就没了。结果文件落在 %TEMP%，调用方读完立即删除。

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"skylogin/internal/mpayhost"
)

// headlessReq 是子进程的输入。
type headlessReq struct {
	Action   string `json:"action"` // urs | token | ticket | list | setplatform
	SkyDir   string `json:"sky"`
	Skin     string `json:"skin"`
	GameID   string `json:"game"`
	LogPath  string `json:"log"`
	DataPath string `json:"dataPath"`
	UID      string `json:"uid"`
	Password string `json:"password"` // 只有 urs 用
	Token    string `json:"token"`
	Ticket   string `json:"ticket"`
	Platform string `json:"platform"` // ad | ios | hm
	Device   string `json:"device"`
	Channel  string `json:"channel"`
	WaitSec  int    `json:"wait"`
}

// headlessResp 是子进程的输出（写入 --out）。
type headlessResp struct {
	OK       bool     `json:"ok"`
	Error    string   `json:"error"`
	Ticket   string   `json:"ticket"`
	UserID   string   `json:"userId"`
	DeviceID string   `json:"deviceId"`
	GameID   string   `json:"gameId"`
	Token    string   `json:"token"`
	Platform bool     `json:"platform"` // true = 平台身份凭证
	Slots    []string `json:"slots"`
}

// runHeadlessLogin 执行一次无头动作。任何失败都写进结果文件，让父进程能看懂。
func runHeadlessLogin(outPath, skyDir, skin, game, logPath string) {
	var req headlessReq
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		writeHeadlessResp(outPath, headlessResp{Error: "读请求失败: " + err.Error()})
		return
	}
	// 命令行兜底：父进程没在 JSON 里给的，用参数补
	if req.SkyDir == "" {
		req.SkyDir = skyDir
	}
	if req.Skin == "" {
		req.Skin = skin
	}
	if req.GameID == "" {
		req.GameID = game
	}
	if req.LogPath == "" {
		req.LogPath = logPath
	}
	if req.SkyDir == "" {
		writeHeadlessResp(outPath, headlessResp{Error: "没有指定 mpay.dll 所在目录"})
		return
	}
	if req.WaitSec <= 0 {
		req.WaitSec = 30
	}

	o := mpayhost.UILessOptions{
		Options: mpayhost.Options{
			SkyDir:   req.SkyDir,
			SkinZip:  req.Skin,
			GameID:   req.GameID,
			Channel:  "a50_sdk_cn",
			LogPath:  req.LogPath,
			DataPath: req.DataPath,
		},
		UID:      req.UID,
		Token:    req.Token,
		Ticket:   req.Ticket,
		Platform: req.Platform,
		Device:   req.Device,
		Ch:       req.Channel,
		WaitSec:  req.WaitSec,
	}

	var res mpayhost.Result
	switch req.Action {
	case "urs", "password":
		o.Token = req.Password // urs 里 Token 字段就是密码
		res = mpayhost.LoginByPassword(o)
	case "token":
		res = mpayhost.LoginByToken(o)
	case "ticket":
		res = mpayhost.LoginByTicket(o)
	case "list":
		res = mpayhost.ListAccounts(o)
	case "setplatform":
		res = mpayhost.SetPlatform(o)
	default:
		writeHeadlessResp(outPath, headlessResp{Error: "未知动作: " + req.Action})
		return
	}

	id := res.Identity(hostedGameIDForCheck)
	writeHeadlessResp(outPath, headlessResp{
		OK:       res.OK,
		Error:    res.Error,
		Ticket:   res.Ticket,
		UserID:   id.UserID,
		DeviceID: id.DeviceID,
		GameID:   res.GameID,
		Token:    id.Token,
		Platform: id.Platform,
		Slots:    res.Slots,
	})
}

// hostedGameIDForCheck 只用来判断"这份凭证是不是平台身份"，与登录上下文无关。
const hostedGameIDForCheck = "aecglf6ee4aaaarz-g-a50"

// writeHeadlessResp 把结果写成 JSON。失败也要落盘，父进程才分得清"出错"和"崩了"。
func writeHeadlessResp(path string, r headlessResp) {
	if path == "" {
		return
	}
	b, _ := json.Marshal(r)
	_ = os.WriteFile(path, b, 0o600)
	if r.Error != "" {
		fmt.Fprintln(os.Stderr, "headless:", r.Error)
	}
	// 给 DLL 的异步线程一点收尾时间，避免父进程删目录时文件还被占用
	time.Sleep(200 * time.Millisecond)
}
