package app

// 启动组：一套"可重复执行的启动配方"。
//
// 和账号列表的区别（用户 2026-11-15 定的）：
//   账号列表是"我有哪些号"；
//   启动组是"这次要拉起哪几个、各自摆在哪儿、用什么画质"。
//
// 参数分三层，改哪一层影响谁必须清楚：
//   组  —— 排列方式、默认画质/帧率/显示、启动行为（停旧的、间隔、开监控墙）
//   成员 —— 启动顺序、窗口位置、画质覆盖、显示覆盖、这次是否启用
//   账号 —— 它自己的画质（组和成员都没设时用它）
//
// 存储：%APPDATA%\SkyLogin\groups.json，和账号文件一样走 DPAPI 加密。
// 为什么不塞进 config.json：组是"用户数据"（可能有十几套、含账号键），
// 配置是"本机偏好"；混在一起以后改任何一个都要小心别覆盖另一个。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"skylogin/internal/secretfile"
	"skylogin/internal/web"
	"skylogin/internal/win"
)

// groupsMu 串行化启动组文件的读-改-写（和账号文件同一个理由：整文件覆盖写）。
var groupsMu sync.Mutex

func groupsPath() string { return filepath.Join(dataDir(), "groups.json") }

// GroupSlot 组里的一个成员。
//
// Disabled 用反向字段是因为 Go 的零值坑：`Enabled bool` 缺省是 false，
// 读旧数据/前端漏传时会静默变成"这个号不启动"。反向写就没有这个问题。
type GroupSlot struct {
	AccountKey string `json:"account_key"`
	Mode       string `json:"mode,omitempty"`    // "" = 跟随组
	Fps        int    `json:"fps,omitempty"`     // 0 = 跟随组
	Display    string `json:"display,omitempty"` // "" = 跟随组
	Disabled   bool   `json:"disabled,omitempty"`
	// 窗口位置（物理像素）。HasRect=false 表示"这套没记过位置"，
	// 启动时按排列方式算，不去摆一个 0,0,0,0。
	X          int  `json:"x,omitempty"`
	Y          int  `json:"y,omitempty"`
	W          int  `json:"w,omitempty"`
	H          int  `json:"h,omitempty"`
	HasRect    bool `json:"has_rect,omitempty"`
}

// LaunchGroup 一套启动组。
type LaunchGroup struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Layout  string      `json:"layout"` // tile / cascade / fullscreen
	Cols    int         `json:"cols"`
	Rows    int         `json:"rows"`
	DefMode string      `json:"def_mode,omitempty"`
	DefFps  int         `json:"def_fps,omitempty"`
	DefDisp string      `json:"def_display,omitempty"`
	// 启动行为
	StopFirst bool `json:"stop_first"`      // 启动前停掉正在跑的客户端（默认开）
	NoAutoWall bool `json:"no_auto_wall"`   // 反向字段：缺省 = 自动开监控墙
	GapSec    int  `json:"gap_sec,omitempty"`
	// 内存里的当前屏幕尺寸（保存时记下，启动时对一对就知道位置还作不作数）
	ScreenW int `json:"screen_w,omitempty"`
	ScreenH int `json:"screen_h,omitempty"`
	Slots   []GroupSlot `json:"slots"`

	CreatedAt int64 `json:"created_at,omitempty"`
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

// normalizeGroup 收拾前端传来的组：名字、排列、画质、槽位去重。
//
// 校验宁可"改"也不要"拒"：用户点了保存，因为一个不认识的画质档位整份丢掉
// 是最糟的体验。真正非法的（比如名字全空）才补默认值。
func normalizeGroup(g LaunchGroup) LaunchGroup {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		g.Name = "未命名启动组"
	}
	if len([]rune(g.Name)) > 24 {
		g.Name = string([]rune(g.Name)[:24])
	}
	switch g.Layout {
	case "tile", "cascade", "fullscreen":
	default:
		g.Layout = "tile"
	}
	if g.Cols < 1 || g.Cols > 4 {
		g.Cols = 2
	}
	if g.Rows < 1 || g.Rows > 4 {
		g.Rows = 2
	}
	if !validQualityModes[g.DefMode] {
		g.DefMode = ""
	}
	if !validQualityFps[g.DefFps] {
		g.DefFps = 0
	}
	if !validDisplays[g.DefDisp] {
		g.DefDisp = ""
	}
	if g.GapSec < 0 {
		g.GapSec = 0
	}
	if g.GapSec > 30 {
		g.GapSec = 30
	}
	// 槽位：同一个 accountKey 只能占一个槽（副本是不同 key，可以各占一个），
	// 位置不能是负数。
	seen := make(map[string]bool, len(g.Slots))
	slots := make([]GroupSlot, 0, len(g.Slots))
	for _, s := range g.Slots {
		if s.AccountKey == "" || seen[s.AccountKey] {
			continue
		}
		seen[s.AccountKey] = true
		if !validQualityModes[s.Mode] {
			s.Mode = ""
		}
		if !validQualityFps[s.Fps] {
			s.Fps = 0
		}
		if !validDisplays[s.Display] {
			s.Display = ""
		}
		if s.X < 0 {
			s.X = 0
		}
		if s.Y < 0 {
			s.Y = 0
		}
		if s.W < 0 {
			s.W = 0
		}
		if s.H < 0 {
			s.H = 0
		}
		if s.W == 0 || s.H == 0 {
			s.HasRect = false
		}
		slots = append(slots, s)
	}
	g.Slots = slots
	return g
}

// saveGroupFromPayload 把前端传来的结构转成磁盘结构再存。
//
// 顺手量一次当前屏幕尺寸并记进去：成员的位置是"在这个分辨率下摆出来的"，
// 换屏之后这套坐标就作废，启动时要退化成网格（详见 launch 那条链）。
func saveGroupFromPayload(p web.GroupPayload) (string, error) {
	g := LaunchGroup{
		ID:        p.ID,
		Name:      p.Name,
		Layout:    p.Layout,
		Cols:      p.Cols,
		Rows:      p.Rows,
		DefMode:   p.DefMode,
		DefFps:    p.DefFps,
		DefDisp:   p.DefDisplay,
		StopFirst: p.StopFirst,
		// 前端是"自动开监控墙"，磁盘上是反向字段
		NoAutoWall: !p.AutoWall,
		GapSec:     p.GapSec,
	}
	for _, s := range p.Slots {
		g.Slots = append(g.Slots, GroupSlot{
			AccountKey: s.AccountKey,
			Mode:       s.Mode,
			Fps:        s.Fps,
			Display:    s.Display,
			Disabled:   s.Disabled,
			X:          s.X,
			Y:          s.Y,
			W:          s.W,
			H:          s.H,
			HasRect:    s.HasRect,
		})
	}
	g.ScreenW, g.ScreenH = win.ScreenSize()
	return saveGroup(g)
}

func loadGroups() ([]LaunchGroup, error) {
	b, err := secretfile.ReadFile(groupsPath())
	if err != nil {
		return nil, err
	}
	var list []LaunchGroup
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func saveGroups(list []LaunchGroup) error {
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return secretfile.WriteFile(groupsPath(), b, 0o600)
}

// groupsView 给界面用的启动组清单。
//
// 带上每个槽位的账号显示名和"这个号还在不在"：账号被删掉以后，
// 组里那条槽位不能静默消失（用户会以为配置丢了），要能显示成"已删除的账号"。
func groupsView() []map[string]any {
	list, err := loadGroups()
	if err != nil || len(list) == 0 {
		return []map[string]any{}
	}
	accs, _ := loadAccounts()
	byKey := make(map[string]Account, len(accs))
	for _, a := range accs {
		byKey[accountKey(a)] = a
	}

	out := make([]map[string]any, 0, len(list))
	for _, g := range list {
		slots := make([]map[string]any, 0, len(g.Slots))
		missing := 0
		for _, s := range g.Slots {
			a, ok := byKey[s.AccountKey]
			name := "已删除的账号"
			if ok {
				name = displayName(a)
			} else {
				missing++
			}
			slots = append(slots, map[string]any{
				"accountKey": s.AccountKey,
				"name":       name,
				"missing":    !ok,
				"mode":       s.Mode,
				"fps":        s.Fps,
				"display":    s.Display,
				"disabled":   s.Disabled,
				"x":          s.X, "y": s.Y, "w": s.W, "h": s.H,
				"hasRect": s.HasRect,
			})
		}
		out = append(out, map[string]any{
			"id":         g.ID,
			"name":       g.Name,
			"layout":     g.Layout,
			"cols":       g.Cols,
			"rows":       g.Rows,
			"defMode":    g.DefMode,
			"defFps":     g.DefFps,
			"defDisplay": g.DefDisp,
			"stopFirst":  g.StopFirst,
			"autoWall":   !g.NoAutoWall,
			"gapSec":     g.GapSec,
			"screenW":    g.ScreenW,
			"screenH":    g.ScreenH,
			"slots":      slots,
			"missing":    missing,
			"updated":    fmtStamp(g.UpdatedAt),
		})
	}
	return out
}

// saveGroup 新建或更新一套启动组，返回它的 id。
func saveGroup(g LaunchGroup) (string, error) {
	g = normalizeGroup(g)
	now := time.Now().Unix()

	groupsMu.Lock()
	defer groupsMu.Unlock()
	list, err := loadGroups()
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("读启动组失败: %w", err)
	}

	if g.ID == "" {
		g.ID = fmt.Sprintf("g%d", now*1000+int64(len(list)))
		g.CreatedAt = now
	}
	g.UpdatedAt = now
	replaced := false
	for i := range list {
		if list[i].ID != g.ID {
			continue
		}
		if g.CreatedAt == 0 {
			g.CreatedAt = list[i].CreatedAt
		}
		list[i] = g
		replaced = true
		break
	}
	if !replaced {
		list = append(list, g)
	}
	if err := saveGroups(list); err != nil {
		return "", fmt.Errorf("保存启动组失败: %w", err)
	}
	return g.ID, nil
}

// deleteGroup 删除一套启动组。找不到也算成功（幂等）：
// 用户连点两次删除不该看到报错。
func deleteGroup(id string) error {
	if id == "" {
		return fmt.Errorf("缺少启动组")
	}
	groupsMu.Lock()
	defer groupsMu.Unlock()
	list, err := loadGroups()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读启动组失败: %w", err)
	}
	out := list[:0]
	for _, g := range list {
		if g.ID != id {
			out = append(out, g)
		}
	}
	return saveGroups(out)
}
