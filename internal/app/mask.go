package app

// 隐私打码的总开关：开着时，游戏窗口标题和监控墙标签里的账号名
// （邮箱 / 手机号）就地打码。前端列表的打码、墙上的切换按钮、
// 这里的状态是同一个开关（API /api/ui/mask 读写）。

import (
	"regexp"
	"sync"

	"skylogin/internal/win"
)

var (
	maskMu     sync.Mutex
	maskOn     = true // 默认开：与界面列表的默认打码一致
	maskLoaded bool
)

var (
	reMaskEmail = regexp.MustCompile(`([A-Za-z0-9._%+-]+)@([A-Za-z0-9.-]+\.[A-Za-z]{2,})`)
	reMaskDigit = regexp.MustCompile(`\d{7,}`)
)

// maskLabel 按当前开关把名字里的邮箱 / 手机号打码（其余文字不动）。
func maskLabel(name string) string {
	if !maskEnabled() {
		return name
	}
	out := reMaskEmail.ReplaceAllStringFunc(name, func(m string) string {
		at := indexByte(m, '@')
		keep := 1
		if at > 4 {
			keep = 3
		}
		return m[:keep] + "***" + m[at:]
	})
	out = reMaskDigit.ReplaceAllStringFunc(out, func(m string) string {
		if len(m) <= 7 {
			return m
		}
		return m[:3] + "****" + m[len(m)-4:]
	})
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func maskEnabled() bool {
	maskMu.Lock()
	defer maskMu.Unlock()
	if !maskLoaded {
		maskOn = loadConfig().MaskTitlesEnabled()
		maskLoaded = true
	}
	return maskOn
}

// setMaskTitlesOn 改开关并应用到所有出口：游戏窗口标题、监控墙标签
// 与按钮文字。返回最终状态。
func setMaskTitlesOn(on bool) bool {
	maskMu.Lock()
	maskOn = on
	maskLoaded = true
	maskMu.Unlock()

	c := loadConfig()
	c.MaskTitles = &on
	_ = saveConfig(c)

	applyMaskEverywhere(on)
	return on
}

func maskMenuText(on bool) string {
	if on {
		return "显示全名"
	}
	return "打码"
}

func maskEnabledForConfig() {}

// displayLabelOf 账号 → 显示标签（按开关打码）。窗口标题、监控墙标签、
// 排列用的 ClientCell 全部走这里，保证打码状态一处切换处处生效。
func displayLabelOf(a Account) string {
	return maskLabel(displayName(a))
}

// applyMaskEverywhere 切换后的全量应用：窗口标题（按记录的槽位）+ 监控墙。
func applyMaskEverywhere(on bool) {
	windowSlotsMu.Lock()
	for pid, slot := range windowSlots {
		h := win.MainWindowOfPID(pid)
		if h == 0 {
			continue
		}
		if a, ok := findAccountByPID(pid); ok {
			win.RenameClientWindow(h, displayLabelOf(a), slot)
		}
	}
	windowSlotsMu.Unlock()
	_ = wallRefresh()
	win.SetWallMenuItem("mask", maskMenuText(on))
}

// findAccountByPID 运行中 pid → 账号行。
func findAccountByPID(pid uint32) (Account, bool) {
	clientsMu.Lock()
	slot := clients[pid]
	clientsMu.Unlock()
	if slot == nil || slot.acc == nil {
		return Account{}, false
	}
	return *slot.acc, true
}

// windowSlots 记录排列时每个客户端窗口的槽位号（标题 "N · 账号名"），
// 打码切换时用它保持编号不变地重写标题。
var (
	windowSlots   = map[uint32]int{}
	windowSlotsMu sync.Mutex
)

func recordWindowSlots(cells []win.ClientCell) {
	windowSlotsMu.Lock()
	windowSlots = map[uint32]int{}
	for i, c := range cells {
		windowSlots[c.PID] = i + 1
	}
	windowSlotsMu.Unlock()
}
