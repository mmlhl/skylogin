package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"skylogin/internal/channel"
	"skylogin/internal/secretfile"
)

func accountsPath() string { return filepath.Join(dataDir(), "accounts.json") }
func currentPath() string  { return filepath.Join(dataDir(), "current.txt") }

// normalizeSciID 把科学计数法格式的 id（"4.13918109e+08"）还原成整数
// 字符串（"413918109"）。早期 B站 登录把 auto.login 返回的 float64 直接
// fmt.Sprint 存进了账号文件，游戏服务器认不了这种 id；读档时顺手修掉。
func normalizeSciID(s string) (string, bool) {
	if !strings.Contains(s, "e+") {
		return s, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s, false
	}
	return strconv.FormatInt(int64(f), 10), true
}

// managedChannel 这个渠道的凭证状态需要本工具管理
// （华为账号的凭证走另一套刷新机制，不在范围内）。
func managedChannel(ch string) bool {
	switch ch {
	case "netease", channel.KeyBili, channel.KeyM4399, channel.KeyVivo:
		return true
	}
	return false
}

func loadAccounts() ([]Account, error) {
	b, err := secretfile.ReadFile(accountsPath())
	if err != nil {
		return nil, err
	}
	var list []Account
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	// 旧版明文文件：解析成功立即升级成加密存储（自动迁移，无感）
	if !secretfile.IsEncrypted(accountsPath()) && len(list) > 0 {
		_ = saveList(list)
	}
	// 脏数据修复：改过就回写一次，下次读到的就是干净的
	fixed := false
	for i := range list {
		if v, ok := normalizeSciID(list[i].UserID); ok {
			list[i].UserID = v
			fixed = true
		}
		if v, ok := normalizeSciID(list[i].PlayerID); ok {
			list[i].PlayerID = v
			fixed = true
		}
	}
	if fixed {
		if err := saveList(list); err != nil {
			logf("修复账号 id 失败: %v", err)
		} else {
			logf("已修复账号文件里的科学计数法 id")
		}
		// current.txt 里存的"当前账号"也是同一个坏 id，一起修
		if b, err := os.ReadFile(currentPath()); err == nil {
			if v, ok := normalizeSciID(strings.TrimSpace(string(b))); ok {
				_ = os.WriteFile(currentPath(), []byte(v), 0o600)
			}
		}
	}
	return list, nil
}

// saveList 只负责写文件，不动"当前账号"。
func saveList(list []Account) error {
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return secretfile.WriteFile(accountsPath(), b, 0o600)
}

// saveAccount 保存/更新一条账号记录。
//
// 两条铁律，否则"同账号两个区"的记录会被互相吃掉：
//   - 带 RowID 的副本只覆盖**自己那一行**，绝不碰 RowID 为空的主记录；
//   - RowID 为空的主记录只覆盖"主记录里 PlayerID 相同的行"，
//     不会把副本顶掉，也不会把别人的副本变成主记录。
func saveAccount(a Account) error {
	list, _ := loadAccounts()
	replaced := false
	if a.RowID != "" {
		for i := range list {
			if list[i].RowID != a.RowID {
				continue
			}
			a = mergeCredMeta(a, list[i])
			list[i] = a
			replaced = true
			break
		}
		// RowID 找不到对应行（比如换机恢复）：当成新行追加，绝不去改主记录
	} else {
		if idx := loginTarget(list, a); idx >= 0 {
			a = mergeCredMeta(a, list[idx])
			list[idx] = a
			replaced = true
		} else {
			// 老数据里可能已经有一条同 PlayerID 的主记录（渠道字段为空的那种），兜底认领
			for i := range list {
				if list[i].RowID != "" || list[i].PlayerID != a.PlayerID {
					continue
				}
				a = mergeCredMeta(a, list[i])
				list[i] = a
				replaced = true
				break
			}
		}
	}
	if !replaced {
		list = append(list, a)
	}
	if err := saveList(list); err != nil {
		return err
	}
	// 新登录/更新的账号自动设为当前（按行键，副本才不会被主记录顶掉）
	return setCurrent(accountKey(a))
}

// mergeCredMeta 用旧行的凭证元信息补齐新值的空缺，避免一次普通保存
// （比如只更新票据时间）把 credState/lastError 清空。
func mergeCredMeta(a, old Account) Account {
	if a.Source == "" {
		a.Source = old.Source
	}
	if a.LoginMethod == "" {
		a.LoginMethod = old.LoginMethod
	}
	if a.CredState == "" {
		a.CredState = old.CredState
		a.LastError = old.LastError
		a.LastCheck = old.LastCheck
	}
	if a.DeviceID == "" {
		a.DeviceID = old.DeviceID
	}
	if a.RowID == "" && old.RowID != "" {
		a.RowID = old.RowID
	}
	if a.Region == "" && old.Region != "" {
		a.Region = old.Region
	}
	return a
}

// deleteAccount 删除指定账号（id 可以是 RowID，也可以是旧版 PlayerID）。
func deleteAccount(id string) error {
	list, err := loadAccounts()
	if err != nil {
		return errors.New("还没有账号")
	}
	key := resolveAccountKey(list, id)
	out := make([]Account, 0, len(list))
	for _, a := range list {
		if accountKey(a) != key {
			out = append(out, a)
		}
	}
	if len(out) == len(list) {
		return errors.New("没有找到该账号")
	}
	if err := saveList(out); err != nil {
		return err
	}
	if cur, _ := currentID(); cur == key || cur == id {
		_ = os.Remove(currentPath())
		if len(out) > 0 {
			_ = setCurrent(accountKey(out[0]))
		}
	}
	return nil
}

// renameAccount 修改账号的本地别名（只存在这台机器上）。
func renameAccount(playerID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("别名不能为空")
	}
	if len([]rune(name)) > 24 {
		return errors.New("别名最多 24 个字")
	}
	list, err := loadAccounts()
	if err != nil {
		return errors.New("还没有账号")
	}
	found := false
	key := resolveAccountKey(list, playerID)
	for i := range list {
		if accountKey(list[i]) == key {
			list[i].Name = name
			found = true
			break
		}
	}
	if !found {
		return errors.New("没有找到该账号")
	}
	return saveList(list)
}

func currentID() (string, error) {
	b, err := os.ReadFile(currentPath())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func setCurrent(id string) error {
	if id == "" {
		return errors.New("缺少账号 ID")
	}
	list, err := loadAccounts()
	if err != nil {
		return errors.New("还没有账号")
	}
	key := resolveAccountKey(list, id)
	found := false
	for _, a := range list {
		if accountKey(a) == key {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("没有找到账号 %s", id)
	}
	return os.WriteFile(currentPath(), []byte(key), 0o600)
}

// pickAccount 优先返回当前选中的账号，否则返回最近使用的。
func pickAccount() (Account, error) {
	list, err := loadAccounts()
	if err != nil || len(list) == 0 {
		return Account{}, errors.New("还没有已保存的账号，先添加一个华为账号")
	}
	if cur, err := currentID(); err == nil && cur != "" {
		for _, a := range list {
			if accountKey(a) == cur || a.PlayerID == cur {
				return a, nil
			}
		}
	}
	newest := list[0]
	for _, a := range list[1:] {
		if a.UpdatedAt > newest.UpdatedAt {
			newest = a
		}
	}
	return newest, nil
}

// maskID 把玩家 ID 打码：保留前 4 位与后 4 位，中间用星号。
func maskID(id string) string {
	r := []rune(id)
	switch {
	case len(r) == 0:
		return ""
	case len(r) <= 8:
		return strings.Repeat("*", len(r))
	default:
		return string(r[:4]) + strings.Repeat("*", len(r)-8) + string(r[len(r)-4:])
	}
}

// displayName 优先用别名，没有就用昵称；副本带上区标记，方便和主记录区分。
func displayName(a Account) string {
	if strings.TrimSpace(a.Name) != "" {
		return a.Name
	}
	base := ""
	if strings.TrimSpace(a.NickName) != "" {
		base = a.NickName
	} else if len(a.PlayerID) >= 4 {
		base = "玩家 " + a.PlayerID[len(a.PlayerID)-4:]
	} else {
		base = "未命名账号"
	}
	if a.RowID != "" {
		if l := regionLabel(a.Region); l != "" {
			return base + " · " + l
		}
		return base + " · 第二区"
	}
	return base
}

// accountView 把账号转成 UI 用的结构（不含"是否当前"）。
func accountView(a Account) map[string]any {
	// 各渠道统一叫「账号 ID」：
	// 华为存的是渠道侧玩家 id，网易存的是 MPay 账号 id，B站存的是 B站 uid，
	// 都不是游戏内角色 id（那个只在游戏服务器侧，拿不到）。
	ch, label := "huawei", "华为渠道"
	switch a.Channel {
	case "netease":
		ch, label = "netease", "网易官方"
	default:
		if channel.Known(a.Channel) {
			// 渠道服：key 直接用账号文件里的渠道名，展示名带"渠道"后缀
			ch = a.Channel
			label = channel.LabelOf(a.Channel) + "渠道"
		}
	}
	// 凭证状态：给界面显示"这个账号还能不能直接用"
	state := a.CredState
	if state == "" {
		state = "ok"
	}
	source := a.Source
	if source == "" {
		if a.Channel == "netease" {
			source = "scan"
		} else {
			source = "channel"
		}
	}

	return map[string]any{
		"credState":    state,
		"running":      accountRunning(accountKey(a)),
		"source":       source,
		"loginMethod":  a.LoginMethod,
		"lastError":    a.LastError,
		"lastCheck":    a.LastCheck,
		"id":           accountKey(a),
		"rowId":        a.RowID,
		"region":       a.Region,
		"regionLabel":  regionLabel(a.Region),
		"name":         displayName(a),
		"nick":         a.NickName,
		"channel":      ch,
		"channelLabel": label,
		"playerId":     a.PlayerID,
		"accountId":    a.PlayerID,
		"idLabel":      "账号 ID",
		"masked":       maskID(a.PlayerID),
		"level":        a.PlayerLevel,
		"updatedTs":    a.UpdatedAt,
		"updated":      time.Unix(a.UpdatedAt, 0).Format("01-02 15:04"),
	}
}

// setAccountCredState 更新某个账号的凭证状态，并记下原因。
//
// 状态含义：
//
//	ok      凭证可用
//	stale   可能过期（比如签票失败过一次），下次启动会再试
//	expired 确认失效，需要用户重新获取凭证
func setAccountCredState(userID, state, reason string) {
	if userID == "" {
		return
	}
	list, err := loadAccounts()
	if err != nil {
		return
	}
	changed := false
	for i := range list {
		if !managedChannel(list[i].Channel) {
			continue
		}
		if list[i].UserID != userID && list[i].PlayerID != userID {
			continue
		}
		list[i].CredState = state
		list[i].LastError = reason
		list[i].LastCheck = time.Now().Unix()
		changed = true
	}
	if changed {
		if err := saveList(list); err != nil {
			logf("保存账号状态失败: %v", err)
			return
		}
		if state == "expired" {
			setAuthHint("netease_expired", "登录凭证已过期", "登录信息已过期，请刷新凭证", "", userID)
		}
	}
}
