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
	// 旧版副本键（随机 hex 的 row_id，仅本机短暂存在过）转成可读后缀键
	// "PlayerID-2"。发布版从未带过该字段，其他用户无感。
	legacy, hadLegacy := legacyRowIDs(b)
	converted := map[int]string{} // 下标 → 新副本键
	for i := range list {
		if old, ok := legacy[i]; ok && old != "" {
			list[i].CopyKey = nextCopyKey(list, list[i].PlayerID)
			converted[i] = list[i].CopyKey
			fixed = true
		}
	}
	// 老记录没有 token 元信息，但 token 本身就存着——解析出来补上，
	// 这样不用等下次登录，详情页立刻能看到凭证等级和签发时间。
	for i := range list {
		if list[i].MpayToken == "" {
			continue
		}
		sa := int(tokenSaValue(list[i].MpayToken))
		at := tokenLoginUnix(list[i].MpayToken)
		if sa != 0 && list[i].TokenSa != sa {
			list[i].TokenSa = sa
			fixed = true
		}
		if at > 0 && list[i].TokenAt != at {
			list[i].TokenAt = at
			fixed = true
		}
	}
	if fixed {
		if err := saveList(list); err != nil {
			logf("修复账号 id 失败: %v", err)
		} else {
			if hadLegacy {
				logf("已把 %d 条副本记录的键转成可读格式", len(converted))
			} else {
				logf("已修复账号文件里的科学计数法 id")
			}
		}
		// current.txt 里存的旧键一起修：科学计数法 id、旧副本 RowID
		if b, err := os.ReadFile(currentPath()); err == nil {
			cur := strings.TrimSpace(string(b))
			if v, ok := normalizeSciID(cur); ok {
				_ = os.WriteFile(currentPath(), []byte(v), 0o600)
			} else if newKey, ok := convertedByOldKey(converted, legacy, cur); ok {
				_ = os.WriteFile(currentPath(), []byte(newKey), 0o600)
			}
		}
	}
	return list, nil
}

// legacyRowIDs 从原始 json 里找旧版 row_id 字段（随机 hex 键），返回
// "数组下标 → 旧键"。新版字段叫 copy_key，这里只为读老文件。
func legacyRowIDs(raw []byte) (map[int]string, bool) {
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, false
	}
	out := map[int]string{}
	for i, row := range rows {
		if v, ok := row["row_id"].(string); ok && v != "" {
			out[i] = v
		}
	}
	return out, len(out) > 0
}

// convertedByOldKey 把旧副本键映射到转换后的新键（current.txt 迁移用）。
func convertedByOldKey(converted map[int]string, legacy map[int]string, oldKey string) (string, bool) {
	for i, newKey := range converted {
		if legacy[i] == oldKey {
			return newKey, true
		}
	}
	return "", false
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
// 两条铁律，否则同账号的多条记录会被互相吃掉：
//   - 副本（CopyKey 非空）只覆盖**自己那一行**，绝不碰原始行；
//   - 原始行（CopyKey 为空，来自登录流程）只覆盖"同 PlayerID 的原始行"，
//     不会把副本顶掉。
//
// 凭证是按 PlayerID（远程身份）共享的：登录拿到新 token 后，同 PlayerID
// 的副本也一起更新——副本才不会拿着旧 token 报"凭证失效"。
func saveAccount(a Account) error {
	list, _ := loadAccounts()
	replaced := false
	// 保存前的旧凭证：用来判断"这次是不是换了新 token"，
	// 只有换了凭证才值得把号重新喂给 mpay.dll（见 remember.go）
	var oldToken string
	if a.CopyKey != "" {
		for i := range list {
			if list[i].CopyKey != a.CopyKey {
				continue
			}
			a = mergeCredMeta(a, list[i])
			oldToken = list[i].MpayToken
			list[i] = a
			replaced = true
			break
		}
		// CopyKey 找不到对应行（比如换机恢复）：当成新行追加，绝不去改原始行
	} else if a.RowKey != "" {
		// 行键：同一个玩家的不同来源（网页登录 / 渠道服扫码）各占一行，
		// 只覆盖自己那一行，不去动另一条。
		for i := range list {
			if list[i].RowKey != a.RowKey {
				continue
			}
			a = mergeCredMeta(a, list[i])
			oldToken = list[i].MpayToken
			list[i] = a
			replaced = true
			break
		}
	} else {
		if idx := loginTarget(list, a); idx >= 0 {
			a = mergeCredMeta(a, list[idx])
			oldToken = list[idx].MpayToken
			list[idx] = a
			replaced = true
		}
	}
	if !replaced {
		a.fillMeta() // 新行：补上首次添加时间 / token 元信息
		list = append(list, a)
	}
	// 凭证按 PlayerID 同步给同身份的所有行（token/凭证状态是身份级的）
	if a.MpayToken != "" || a.CredState != "" {
		for i := range list {
			if list[i].PlayerID == a.PlayerID && accountKey(list[i]) != accountKey(a) {
				if a.MpayToken != "" {
					list[i].MpayToken = a.MpayToken
				}
				if a.CredState != "" {
					list[i].CredState = a.CredState
					list[i].LastError = a.LastError
					list[i].LastCheck = a.LastCheck
				}
			}
		}
	}
	if err := saveList(list); err != nil {
		return err
	}
	// 新登录/更新的账号自动设为当前（按行键）
	if err := setCurrent(accountKey(a)); err != nil {
		return err
	}
	// 换了新凭证的网易号：顺手把它写进游戏本地 mpay.db（后台、尽力而为）
	if a.Channel == "netease" && a.MpayToken != "" && a.MpayToken != oldToken {
		syncNetEaseToMpayDB(a)
	}
	return nil
}

// markAccountLaunched 记下"这个账号最后一次成功启动"的时间。
//
// 只改这一个字段，不整行回写：启动期间别的流程（凭证状态、票据时间）
// 也在动同一行，整行写回容易把它们覆盖掉。
func markAccountLaunched(key, gameID string) {
	if key == "" {
		return
	}
	list, err := loadAccounts()
	if err != nil {
		return
	}
	for i := range list {
		if accountKey(list[i]) != key {
			continue
		}
		list[i].LastLogin = time.Now().Unix()
		if list[i].CreatedAt == 0 {
			// 老记录没有建档时间：拿这次登录当起点，总比一直显示「—」强，
			// 界面上"添加时间"就是"本版本开始记录的时间"。
			list[i].CreatedAt = list[i].LastLogin
		}
		if gameID != "" {
			list[i].GameID = gameID
		}
		if err := saveList(list); err != nil {
			logf("记录登录时间失败: %v", err)
		}
		return
	}
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
	if a.CopyKey == "" && old.CopyKey != "" {
		a.CopyKey = old.CopyKey
	}
	if a.Region == "" && old.Region != "" {
		a.Region = old.Region
	}
	// 账号密码与平台身份凭证不属于"这一次登录的结果"，扫码/短信这类入口
	// 回写账号时不会带上它们。不在这里兜住，一次扫码刷新就会把保存过的密码
	// 和 a50 凭证清掉，之后既不能自动重登，也不能再按渠道签发。
	if a.SavedUser == "" {
		a.SavedUser = old.SavedUser
	}
	if a.SavedPass == "" {
		a.SavedPass = old.SavedPass
	}
	if a.A50Token == "" {
		a.A50Token = old.A50Token
	}
	if a.A50DeviceID == "" {
		a.A50DeviceID = old.A50DeviceID
	}
	if a.UnisdkData == "" {
		a.UnisdkData = old.UnisdkData
	}
	// 环境和时间戳：这一行是"同一个号"的续写，旧值一律保留
	if a.MpayPath == "" {
		a.MpayPath = old.MpayPath
	}
	if a.MpayVersion == "" {
		a.MpayVersion = old.MpayVersion
	}
	if a.GameID == "" {
		a.GameID = old.GameID
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = old.CreatedAt
	}
	if a.LastLogin == 0 {
		a.LastLogin = old.LastLogin
	}
	if a.TokenAt == 0 {
		a.TokenSa, a.TokenAt = old.TokenSa, old.TokenAt
	}
	a.fillMeta()
	return a
}

// fillMeta 补齐"记账用"的字段：首次添加时间、token 的等级与签发时间。
//
// 放在保存这一层做，是因为账号有六七个入口（扫码/短信/密码窗/渠道网页/
// 游戏内扫码/华为扫码），让每个入口各自记得写一遍迟早会漏。
func (a *Account) fillMeta() {
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().Unix()
	}
	if a.MpayToken != "" {
		if sa := int(tokenSaValue(a.MpayToken)); sa != 0 {
			a.TokenSa = sa
		}
		if t := tokenLoginUnix(a.MpayToken); t > 0 {
			a.TokenAt = t
		}
	}
}

// deleteAccount 删除指定账号（id 可以是副本键，也可以是原始行的 PlayerID）。
func deleteAccount(id string) error {
	list, err := loadAccounts()
	if err != nil {
		return errors.New("还没有账号")
	}
	key := resolveAccountKey(list, id)
	out := make([]Account, 0, len(list))
	// 删之前先记下这个号的 UserID：槽位账本要按它解除寄宿关系。
	var removedUserIDs []string
	for _, a := range list {
		if accountKey(a) != key {
			out = append(out, a)
			continue
		}
		if a.UserID != "" {
			removedUserIDs = append(removedUserIDs, a.UserID)
		}
	}
	if len(out) == len(list) {
		return errors.New("没有找到该账号")
	}
	if err := saveList(out); err != nil {
		return err
	}
	// 解除它作为宿主/被寄宿的关系，并让寄宿池缓存失效。
	// 服务端不会因为我们删了本地记录就清槽，但至少下次选宿主时会重新读真实状态。
	for _, uid := range removedUserIDs {
		if n := releaseSlotBindings(uid); n > 0 {
			logf("删除账号：已解除 %d 条寄宿关系", n)
		}
		invalidateHostSlot(uid)
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
		// 先按行键精确找：同一个玩家可能有两行（网页登录 / 整包扫码），
		// 只靠 PlayerID 会永远命中列表里靠前的那一条，点另一条也会启动成前一条。
		for _, a := range list {
			if accountKey(a) == cur {
				return a, nil
			}
		}
		// 精确找不到才退回 PlayerID（兼容老版本 current.txt）
		for _, a := range list {
			if a.PlayerID == cur {
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
	if a.CopyKey != "" {
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
		} else if a.Channel != "" {
			// 渠道服扫码带回来的其它渠道（nearme_vivo 这类）：
			// 名字就是网易服务端的渠道标识，直接用；展示名走 displayChannel
			ch = a.Channel
			label = displayChannel(a.Channel) + "渠道"
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
		"credState":   state,
		"running":     accountRunning(accountKey(a)),
		"source":      source,
		"loginMethod": a.LoginMethod,
		"lastError":   a.LastError,
		"lastCheck":   a.LastCheck,
		"id":          accountKey(a),
		"copyKey":     a.CopyKey,
		"qualityMode": a.QualityMode,
		"qualityFps":  a.QualityFps,
		"region":      a.Region,
		"regionLabel": regionLabel(a.Region),
		// hosted = 这个号住在某个平台账号的寄宿槽里。
		// 这类号的渠道由扫码设备固定（槽位票继承），再给选区选择器只会误导：
		// 选什么都没用，因为它没有平台凭证，走不了"声明 + 确认"那条路。
		"hosted": isHosted(a.UserID),
		// regionBlocked 非空 = 虽然选了渠道，但当前凭证用不了（界面要提示原因）
		"regionBlocked": regionBlockReason(a),
		"name":          displayName(a),
		"nick":          a.NickName,
		"channel":       ch,
		"channelLabel":  label,
		"playerId":      a.PlayerID,
		"accountId":     a.PlayerID,
		"idLabel":       "账号 ID",
		"masked":        maskID(a.PlayerID),
		"level":         a.PlayerLevel,
		"updatedTs":     a.UpdatedAt,
		"updated":       time.Unix(a.UpdatedAt, 0).Format("01-02 15:04"),

		// ── 详情页用的环境/时间信息 ──────────────────────────────
		// 全给原始值 + 格式化值两份：界面显示用后者，以后做"存了几天"的
		// 相对时间计算用前者。
		"mpayPath":      a.MpayPath,
		"mpayVersion":   a.MpayVersion,
		"gameId":        a.GameID,
		"createdAt":     a.CreatedAt,
		"created":       fmtStamp(a.CreatedAt),
		"lastLogin":     a.LastLogin,
		"lastLoginText": fmtStamp(a.LastLogin),
		"tokenSa":       a.TokenSa,
		"tokenSaLabel":  tokenSaLabel(a.TokenSa),
		"tokenAt":       a.TokenAt,
		"tokenAtText":   fmtStamp(a.TokenAt),
	}
}

// fmtStamp 把 unix 秒格式化成"2026-11-05 21:30"；0 返回空串（界面显示 —）。
func fmtStamp(t int64) string {
	if t <= 0 {
		return ""
	}
	return time.Unix(t, 0).Format("2006-01-02 15:04")
}

// tokenSaLabel 把 sa 等级翻成详情页上的说明。
//
// 用户明确要求：不要把 sa=-4 叫作“大神登录”——-4 只是凭证的一个等级值，
// 扫码、大神、渠道服都可能出现，叫“大神”会误导。所以标签里始终带上
// 真实数字，同时给出“能不能长期用”这个用户真正关心的信息。
func tokenSaLabel(sa int) string {
	switch sa {
	case -99:
		return "账号级（sa=-99，长期）"
	case -9:
		return "账号级（sa=-9，长期）"
	case 3:
		return "会话级（sa=3，可续期）"
	case -4:
		return "会话级（sa=-4，需重登或续期）"
	case 0:
		return ""
	default:
		return fmt.Sprintf("sa=%d", sa)
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
