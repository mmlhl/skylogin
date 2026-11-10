package app

// 自动重登的节流。
//
// 为什么需要：重登是"最后手段"，代价不小——
//   - 连续的错误登录尝试更容易触发风控（.ai/04 第 76 条：两份登录串行的
//     真正理由就是"密码错时不要连发两次失败请求"）
//   - 游戏客户端会话层面确实存在顶号（用户实际反馈；跨上下文不顶号是实测结论，
//     但那是 MPay 层面，不等于游戏里没事）
//
// 2026-11-10 的日志里，因为网络故障被误判成凭证过期，单个账号在 52 秒内
// 重登了 5 次，9 个账号一共重登 45 次。这既没用（网络没通，重登也过不去），
// 又在疯狂打接口。
//
// 策略：同一个账号在冷却窗口内只允许自动重登一次。失败也算用掉，
// 否则一直失败的账号会被无限重试。用户手动点「刷新凭证」不受限制
// （见 ReloginAccount，那条路会先清掉记录）。

import (
	"sync"
	"time"
)

const (
	// reloginCooldown 同一账号两次自动重登之间的最小间隔。
	// 取 10 分钟：足够覆盖一次用户操作里的反复重试，又不会让"真的过期了"
	// 的账号等太久。
	reloginCooldown = 10 * time.Minute
)

var (
	reloginMu sync.Mutex
	// reloginAt 记每个账号上次自动重登的时间。
	reloginAt = map[string]time.Time{}
)

// allowRelogin 判断这个账号现在能不能自动重登，并当场记账。
//
// 记账放在"允许"这一侧而不是"成功后"，是为了挡住并发：
// 多开时几个 goroutine 可能同时走到重登，只有第一个能通过。
func allowRelogin(key string) bool {
	if key == "" {
		return true
	}
	reloginMu.Lock()
	defer reloginMu.Unlock()
	now := time.Now()
	if t, ok := reloginAt[key]; ok && now.Sub(t) < reloginCooldown {
		return false
	}
	reloginAt[key] = now
	return true
}

// resetReloginThrottle 清掉记录。用户手动刷新凭证后调用：
// 那条路是他主动要重登，不该被这里的冷却挡住。
func resetReloginThrottle(key string) {
	if key == "" {
		return
	}
	reloginMu.Lock()
	defer reloginMu.Unlock()
	delete(reloginAt, key)
}
