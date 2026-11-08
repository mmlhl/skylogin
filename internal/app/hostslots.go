package app

// 槽位归属的本地账本：哪个宿主槽里记着哪个号。
//
// 为什么要本地记：
//   - 服务端只告诉我们"这个 sid 的槽里是谁"，不给"槽属于谁"的索引；
//     每次都要挨个换 sid 去读，既慢又费请求。
//   - 删除一个被寄宿的号时，需要知道它原来住在哪个槽，才能把这条关系解掉
//     （服务端不会因为我们删了本地记录就清槽，但至少要保证下次选宿主时
//     不会把它当成"还占着"）。
//   - 槽满时界面要能列出"替换掉哪一个"，得有这份账。
//
// 这份账只是**本地提示**，不作为真相：真正的占用以服务端读到的为准
// （hostpool.go 的 readHostSlot）。两者不一致时以服务端为准，并回写本地。

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"time"

	"skylogin/internal/secretfile"
)

// SlotBinding 一条寄宿关系：宿主账号的槽里记着被寄宿的号。
type SlotBinding struct {
	HostUserID   string `json:"host"`   // 宿主账号的 UserID（提供槽位的一方）
	HostedUserID string `json:"hosted"` // 被寄宿账号的 UserID（扫码进来的那个）
	At           int64  `json:"at"`     // 写入时间（unix 秒）
}

// slotLedger 是落盘结构。
type slotLedger struct {
	Bindings []SlotBinding `json:"bindings"`
}

var (
	slotMu sync.Mutex
)

func slotLedgerPath() string { return filepath.Join(dataDir(), "hostslots.json") }

// loadSlotLedger 读本地账本；文件不存在或损坏都当作空账本。
func loadSlotLedger() slotLedger {
	var l slotLedger
	b, err := secretfile.ReadFile(slotLedgerPath())
	if err != nil {
		return l
	}
	_ = json.Unmarshal(b, &l)
	return l
}

// saveSlotLedger 写本地账本。
func saveSlotLedger(l slotLedger) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return secretfile.WriteFile(slotLedgerPath(), b, 0o600)
}

// recordSlotBinding 记下"某个号被寄宿进了某个宿主的槽"。
//
// 同一个被寄宿号只保留一条：换宿主时旧的自动作废（服务端也是一对一）。
func recordSlotBinding(hostUserID, hostedUserID string) {
	if hostUserID == "" || hostedUserID == "" {
		return
	}
	slotMu.Lock()
	defer slotMu.Unlock()
	l := loadSlotLedger()
	kept := make([]SlotBinding, 0, len(l.Bindings)+1)
	for _, b := range l.Bindings {
		if b.HostedUserID == hostedUserID {
			continue // 这个号换槽了，旧关系作废
		}
		kept = append(kept, b)
	}
	kept = append(kept, SlotBinding{
		HostUserID:   hostUserID,
		HostedUserID: hostedUserID,
		At:           time.Now().Unix(),
	})
	if err := saveSlotLedger(slotLedger{Bindings: kept}); err != nil {
		logf("槽位账本保存失败: %v", err)
	}
}

// releaseSlotBindings 删除某个账号相关的寄宿关系（它当宿主或被寄宿都算）。
//
// 服务端不会因为本地删除就清槽，所以这里只清本地账：
// 下次选宿主时该槽会被重新读取，读到什么就是什么。
func releaseSlotBindings(userID string) int {
	if userID == "" {
		return 0
	}
	slotMu.Lock()
	defer slotMu.Unlock()
	l := loadSlotLedger()
	kept := make([]SlotBinding, 0, len(l.Bindings))
	removed := 0
	for _, b := range l.Bindings {
		if b.HostUserID == userID || b.HostedUserID == userID {
			removed++
			continue
		}
		kept = append(kept, b)
	}
	if removed == 0 {
		return 0
	}
	if err := saveSlotLedger(slotLedger{Bindings: kept}); err != nil {
		logf("槽位账本保存失败: %v", err)
	}
	return removed
}

// slotBindingOf 查某个号被寄宿在哪个宿主的槽里（本地账）。
func slotBindingOf(hostedUserID string) (SlotBinding, bool) {
	slotMu.Lock()
	defer slotMu.Unlock()
	l := loadSlotLedger()
	for _, b := range l.Bindings {
		if b.HostedUserID == hostedUserID {
			return b, true
		}
	}
	return SlotBinding{}, false
}

// slotHostedBy 查某个宿主的槽里记着谁（本地账）。
func slotHostedBy(hostUserID string) (string, bool) {
	slotMu.Lock()
	defer slotMu.Unlock()
	l := loadSlotLedger()
	for _, b := range l.Bindings {
		if b.HostUserID == hostUserID {
			return b.HostedUserID, true
		}
	}
	return "", false
}

// isHosted 判断这个号是不是住在某个平台账号的寄宿槽里（看本地账本）。
//
// 界面据此决定要不要显示"选区"选择器：被寄宿的号渠道已由扫码设备固定，
// 再让它选区只会误导——它没有平台凭证，走不了"声明 + 确认"那条路。
func isHosted(userID string) bool {
	if userID == "" {
		return false
	}
	_, ok := slotBindingOf(userID)
	return ok
}
