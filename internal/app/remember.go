package app

// 把网易账号直接写进游戏本地的 mpay.db（ma75），让游戏"记住"它。
//
// 背景（2026-11-08 实测闭环）：
//
//   游戏启动后那个"iOS / 安卓"渠道选择框，每次都弹的根因是游戏自己的
//   ma75 库里**一条账号记录都没有**——你在游戏里选完平台，选择无处落，
//   下次启动照样新人来一遍。官方发烧平台没这个问题，是因为它的登录窗
//   登录成功后 DLL 会把账号写进库；而本工具网页/短信/扫码落库的号
//   DLL 都不认识（UILessTokenLogin / TicketLogin 喂号实测全部被拒）。
//
//   解法：绕过 DLL，直接改库。mpay.db 的加密/哈希变换在官方 MPay Wasm
//   里（见 internal/mpaydb），用 wazero（纯 Go）跑它的 put_account，
//   写入带 __user_platform_selected 的完整账号记录，游戏就不问了。
//
// 触发时机：网易账号保存成功且换了新凭证（见 store.go 的钩子）。
// 约束：游戏在跑时不能写库（会被游戏进程覆盖/锁住），此时跳过、
// 写日志说明；单飞防并发写坏库。

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"skylogin/internal/fever"
	"skylogin/internal/mpaydb"
	"skylogin/internal/profile"
)

// syncMu 单飞锁：mpay.db 同一时刻只允许一个写者。
var syncMu sync.Mutex

// platformWanted 读"这个账号玩哪个端"（登录窗流程用的同一份配置，
// 见 mpayhost.setPlatformWanted：环境变量优先，否则 %APPDATA%\SkyLogin\set_platform.txt）。
// 空字符串 = 用户没固定过，记录里就留空（游戏会问一次，但选完能存住了）。
func platformWanted() string {
	if v := trimLower(os.Getenv("SKYLOGIN_SET_PLATFORM")); v != "" {
		return v
	}
	b, err := os.ReadFile(filepath.Join(dataDir(), "set_platform.txt"))
	if err != nil {
		return ""
	}
	line := ""
	if len(b) > 0 {
		parts := splitFirstLine(string(b))
		line = trimLower(parts)
	}
	switch line {
	case "ad", "ios", "hm":
		return line
	}
	return ""
}

func trimLower(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

func splitFirstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// syncNetEaseToMpayDB 保存网易凭证成功后的后台任务：把账号写进游戏 ma75 库。
func syncNetEaseToMpayDB(a Account) {
	// 单元测试里也会走 saveAccount，绝不能真写用户的库
	if testing.Testing() {
		return
	}
	if a.Channel != "netease" || a.UserID == "" || a.MpayToken == "" {
		return
	}
	if fever.CountProcess("sky.exe") > 0 {
		logf("[记住] 光遇正在运行，暂不写 mpay.db（避免写坏），下次换凭证时会补")
		return
	}
	if !syncMu.TryLock() {
		logf("[记住] 上一轮 mpay.db 同步还在跑，跳过 %s", a.Name)
		return
	}
	go func() {
		defer syncMu.Unlock()
		syncNetEaseNow(a)
	}()
}

// syncNetEaseNow 实际执行写库（不带守卫，端到端探针也走这里）。
func syncNetEaseNow(a Account) {
	dbPath := filepath.Join(mpayDataDir(), profile.LongGameID+"-64-mpay.db")
	if _, err := os.Stat(dbPath); err != nil {
		// 游戏还没生成过库：空库没法凭空造，只能等游戏跑过一次。写日志说明即可。
		logf("[记住] 游戏账号库还不存在（%s），跳过", filepath.Base(dbPath))
		return
	}
	db, err := mpaydb.New()
	if err != nil {
		logf("[记住] 初始化 mpay wasm 失败: %v", err)
		return
	}
	defer db.Close()

	// 已有同名记录就不重复写（游戏自己在用这份库，少动为妙）
	existing, err := mpaydb.ListAccounts(db, dbPath)
	if err != nil {
		logf("[记住] 读账号库失败: %v", err)
		return
	}
	for _, it := range existing {
		if it.SDKUID == a.UserID && it.LoginChannel == "netease" {
			logf("[记住] %s 已在游戏账号库里，无需重写", a.Name)
			return
		}
	}

	plat := platformWanted()
	args := mpaydb.PutArgs{
		GameID:            profile.LongGameID,
		LoginChannel:      "netease",
		SDKUID:            a.UserID,
		Name:              a.Name,
		Position:          0,
		MachineIdentifier: mpaydb.MachineIdentifier(),
		Credentials: mpaydb.Credentials{
			Token:     a.MpayToken,
			PcExtInfo: map[string]any{},
		},
		Create: mpaydb.CreateRecord{
			LoginChannel:         "netease",
			LoginType:            0,
			UserLoginMethod:      17,
			UserPlatformSelected: plat,
			ClientUsername:       a.Name,
			DisplayUsername:      a.Name,
		},
	}
	changed, err := mpaydb.PutAccount(db, dbPath, args)
	if err != nil {
		logf("[记住] 写 %s 进游戏账号库失败: %v", a.Name, err)
		return
	}
	if plat != "" {
		logf("[记住] 已把 %s 写进游戏账号库（平台固定为 %s），游戏里不会再弹渠道选择", a.Name, plat)
	} else {
		logf("[记住] 已把 %s 写进游戏账号库（平台未固定，游戏里会问最后一次，选完能记住）", a.Name)
	}
	_ = changed
}

// mpayDataDir mpay 账号库所在目录（和发烧平台/游戏共用）。
func mpayDataDir() string {
	return filepath.Join(os.Getenv("APPDATA"), "Netease", "Mpay")
}
