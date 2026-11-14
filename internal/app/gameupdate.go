package app

import (
	"errors"
	"os/exec"
	"sync"

	"skylogin/internal/fever"
)

// 游戏版本校验（启动器打开时后台跑一次）。
//
// 我们不走官方发烧平台的启动链路，平台那个"先更新再进游戏"的环节就被跳过了：
// 游戏出新版后用户拿旧客户端进服务器会出一堆莫名其妙的 bug，还不知道为什么。
// 所以启动器自己照抄平台的比对逻辑（见 fever/gameupdate.go 的抓包说明），
// 发现版本落后就在界面上提示用户去发烧平台更新游戏。

var (
	gameUpdateMu     sync.Mutex
	gameUpdateState  *fever.GameUpdate
	gameUpdateErrStr string
	// gameUpdateHTTPGet 测试时可替换的下载器（nil = 走真实 HTTP）。
	gameUpdateHTTPGet func(string) ([]byte, error)
)

// checkGameUpdateNow 重新查一次版本比对（阻塞，最多 5 秒网络超时）。
func checkGameUpdateNow() map[string]any {
	local := fever.SkyVersionCode()
	res, err := fever.CheckGameUpdate(local, gameUpdateHTTPGet)

	// 注意：sync.Mutex 不可重入。更新状态和取视图必须在同一次加锁里手工完成，
	// 不能在锁内调 gameUpdateView()（它自己会加锁）——
	// 那样会死锁，把所有 /api/state 请求全卡死，界面表现为"什么数据都没有"。
	gameUpdateMu.Lock()
	gameUpdateState = res
	if err != nil {
		gameUpdateErrStr = err.Error()
	} else {
		gameUpdateErrStr = ""
		if res.UpdateNeeded {
			logf("游戏版本检查：本地 %s 落后于服务器 %s，需要在发烧平台更新",
				fever.PrettyVersionCode(local), fever.PrettyVersionCode(res.LatestVersion))
		} else if res.Checked {
			logf("游戏版本检查：本地已是最新（%s）", fever.PrettyVersionCode(local))
		}
	}
	view := gameUpdateViewLocked()
	gameUpdateMu.Unlock()
	return view
}

// openFeverPlatform 打开发烧平台本体，让用户去那里更新游戏。
// 本体找不到时打开官网，用户照样能走更新。
func openFeverPlatform(openURL func(string) error) error {
	if exe := fever.PlatformExe(); exe != "" {
		return exec.Command(exe).Start()
	}
	if openURL != nil {
		return openURL("https://fever.163.com/")
	}
	return errors.New("没有找到发烧平台，请到官网 https://fever.163.com 下载并更新游戏")
}

// gameUpdateView 给界面的版本检查状态（自己加锁，供 /api/state 用）。
func gameUpdateView() map[string]any {
	gameUpdateMu.Lock()
	defer gameUpdateMu.Unlock()
	return gameUpdateViewLocked()
}

// gameUpdateViewLocked 在已持有 gameUpdateMu 时取视图（禁止对外调用）。
func gameUpdateViewLocked() map[string]any {
	if gameUpdateState == nil {
		// 还没查过（刚启动那几秒）：不惊动界面
		return map[string]any{"checked": false}
	}
	view := map[string]any{
		"checked":        gameUpdateState.Checked,
		"update_needed":  gameUpdateState.UpdateNeeded,
		"local_version":  fever.PrettyVersionCode(gameUpdateState.LocalVersion),
		"latest_version": fever.PrettyVersionCode(gameUpdateState.LatestVersion),
		"local_full":     gameUpdateState.LocalVersion,
		"latest_full":    gameUpdateState.LatestVersion,
	}
	if gameUpdateErrStr != "" {
		view["error"] = gameUpdateErrStr
	}
	return view
}
