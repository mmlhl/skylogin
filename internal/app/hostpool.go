package app

// 寄宿池：谁有资格当宿主、它的寄宿槽现在记着谁。
//
// 机制（2026-11-09 实测 + 用户确认）：
//
//   - 寄宿是**按 sid 读写**的：读槽 GET /app/v1/third_party_account、出票
//     third_party_account/ticket、写槽（扫码换票）都必须带同一个 sid；
//     匿名调用不会记住任何东西。
//   - 服务端把一个 sid 和它记住的账号绑成一对一：**同一个账号只保留一份有效寄宿，
//     新的寄宿会让之前那份失效**。所以"槽位数量 = 能换 sid 的平台账号数"，
//     一个平台账号只能带一个扫码号。
//   - 宿主资格不看凭证等级，看签发身份：只有平台身份（g_i=a50）的凭证能换到 sid；
//     游戏身份（g_i=ma75）换 sid 直接 1000 sauth error。
//   - 槽位票进游戏不弹渠道，但渠道由扫码那台设备决定（手机=安卓），
//     不能替代 uni_sauth 声明渠道。
//
// 这里只做"查状态 + 选宿主"，写槽由扫码换票时带 sid 完成（见 scan.go）。

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"skylogin/internal/launcher"
)

// HostSlot 是一个宿主账号的寄宿状态，给界面直接展示。
type HostSlot struct {
	AccountID  string `json:"accountId"`            // 宿主账号的 PlayerID
	Name       string `json:"name"`                 // 显示名
	CanHost    bool   `json:"canHost"`              // 有可用的平台身份凭证
	Reason     string `json:"reason,omitempty"`     // 不能当宿主的原因
	HostedID   string `json:"hostedId,omitempty"`   // 槽里记着谁（空 = 空槽）
	HostedName string `json:"hostedName,omitempty"` // 槽里那个号的显示名
	Available  bool   `json:"available"`            // 槽是空的，可以直接用
	Checked    bool   `json:"checked"`              // 这次是否真的查到了服务端
}

// HostPool 是给界面的一整份状态。
type HostPool struct {
	Hosts     []HostSlot `json:"hosts"`
	CanHost   int        `json:"canHost"`   // 能当宿主的账号数
	FreeSlots int        `json:"freeSlots"` // 空槽数
	Note      string     `json:"note,omitempty"`
}

const hostSlotTTL = 3 * time.Minute

var (
	poolMu    sync.Mutex
	hostCache = map[string]hostSlotCache{} // uid -> 槽
)

type hostSlotCache struct {
	hosted string
	at     time.Time
	err    string
}

// errNoHostCred 表示这个账号没有平台身份凭证，不能当宿主。
var errNoHostCred = errors.New("缺少平台身份凭证")

// hostSid 用账号的平台身份凭证换一个加载湾会话。
func hostSid(acc Account) (string, error) {
	if acc.A50Token == "" {
		return "", errNoHostCred
	}
	dev := acc.A50DeviceID
	if dev == "" {
		dev = acc.DeviceID
	}
	sid, err := launcher.Login(acc.UserID, acc.A50Token, dev)
	if err != nil || sid == "" {
		return "", fmt.Errorf("平台凭证换会话失败: %w", err)
	}
	return sid, nil
}

// readHostSlot 读某个宿主账号的寄宿槽（带缓存，避免界面反复刷新打服务端）。
func readHostSlot(acc Account) (hosted string, err error) {
	poolMu.Lock()
	c, ok := hostCache[acc.UserID]
	poolMu.Unlock()
	if ok && time.Since(c.at) < hostSlotTTL {
		if c.err != "" {
			return "", errors.New(c.err)
		}
		return c.hosted, nil
	}

	defer func() {
		poolMu.Lock()
		hostCache[acc.UserID] = hostSlotCache{hosted: hosted, at: time.Now(), err: hostErrText(err)}
		poolMu.Unlock()
	}()

	sid, serr := hostSid(acc)
	if serr != nil {
		return "", serr
	}
	hosted, herr := launcher.SavedAccount(sid)
	if herr != nil {
		return "", herr
	}
	return hosted, nil
}

// hostErrText 把 error 压成字符串（缓存里只存文本，避免持有 error 链）。
func hostErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// HostPoolStatus 汇总所有网易账号的宿主状态。
//
// 只列网易官方账号：渠道服账号的凭证换不了 sid，天生不能当宿主。
func HostPoolStatus() HostPool {
	var pool HostPool
	list, err := loadAccounts()
	if err != nil {
		pool.Note = "读账号失败"
		return pool
	}
	// 先把 uid -> 显示名 建好，读槽时要反查"槽里那个号是谁"
	names := map[string]string{}
	for _, a := range list {
		if a.UserID != "" {
			names[a.UserID] = displayName(a)
		}
	}
	for _, a := range list {
		if a.Channel != "netease" {
			continue
		}
		slot := HostSlot{AccountID: a.PlayerID, Name: displayName(a)}
		if a.A50Token == "" {
			slot.Reason = "还没有平台凭证：用手机号或账号密码登录一次就会补上"
		} else {
			hosted, herr := readHostSlot(a)
			if herr != nil {
				slot.Reason = herr.Error()
			} else {
				slot.CanHost = true
				slot.Checked = true
				slot.HostedID = hosted
				slot.Available = hosted == ""
				if hosted != "" {
					slot.HostedName = names[hosted]
					if slot.HostedName == "" {
						slot.HostedName = hosted
					}
				}
			}
		}
		if slot.CanHost {
			pool.CanHost++
		}
		if slot.Available {
			pool.FreeSlots++
		}
		pool.Hosts = append(pool.Hosts, slot)
	}
	switch {
	case pool.CanHost == 0:
		pool.Note = "没有可用的宿主账号：扫码登录的号只能用临时凭证，进游戏仍会问选区，而且容易过期"
	case pool.FreeSlots == 0:
		pool.Note = "宿主槽位都占满了：新扫码号会顶掉其中一个之前记住的号"
	}
	return pool
}

// PickHostSlot 挑一个宿主：优先空槽，其次任意可用宿主（会被顶掉）。
//
// 返回的 sid 直接给扫码换票用（带 sid 换票 = 服务端把扫码号写进这个槽）。
func PickHostSlot() (slot HostSlot, sid string, err error) {
	pool := HostPoolStatus()
	var fallback HostSlot
	for _, h := range pool.Hosts {
		if !h.CanHost {
			continue
		}
		if h.Available {
			slot = h
			break
		}
		if fallback.AccountID == "" {
			fallback = h
		}
	}
	if slot.AccountID == "" {
		slot = fallback
	}
	if slot.AccountID == "" {
		return slot, "", fmt.Errorf("没有可用的宿主账号")
	}
	list, lerr := loadAccounts()
	if lerr != nil {
		return slot, "", lerr
	}
	acc, ok := findAccount(list, slot.AccountID)
	if !ok {
		return slot, "", fmt.Errorf("宿主账号不见了")
	}
	sid, serr := hostSid(acc)
	if serr != nil {
		return slot, "", serr
	}
	return slot, sid, nil
}

// invalidateHostSlot 让某个账号的槽缓存失效（扫码写完槽后调用）。
func invalidateHostSlot(userID string) {
	poolMu.Lock()
	delete(hostCache, userID)
	poolMu.Unlock()
}
