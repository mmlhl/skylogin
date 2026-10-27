package app

import "sync"

// 三种登录窗（网易网页 / 官方 mpay.dll / 渠道网页）共用的进度状态机：
// 界面每秒轮询 phase（idle → waiting → done | failed），同渠道重复打开
// 由 begin 拦下。

// loginProgress 一次登录流程的进行状态。
type loginProgress struct {
	Running bool
	Phase   string // idle | waiting | done | failed
	Message string
	Account string
	UID     string
}

// loginTracker 带锁的登录进度。
type loginTracker struct {
	mu  sync.Mutex
	cur loginProgress
}

func newLoginTracker() *loginTracker {
	return &loginTracker{cur: loginProgress{Phase: "idle"}}
}

func (t *loginTracker) snapshot() loginProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cur
}

// begin 标记流程开始；已在进行时返回 false（调用方提示"窗口已打开"）。
func (t *loginTracker) begin(msg string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cur.Running {
		return false
	}
	t.cur = loginProgress{Running: true, Phase: "waiting", Message: msg}
	return true
}

// set 写入终态（done / failed）。
func (t *loginTracker) set(phase, msg, account, uid string) {
	t.mu.Lock()
	t.cur = loginProgress{Phase: phase, Message: msg, Account: account, UID: uid}
	t.mu.Unlock()
}

// finish 流程收尾：Running 清掉，phase/message 留给界面展示。
func (t *loginTracker) finish() {
	t.mu.Lock()
	t.cur.Running = false
	t.mu.Unlock()
}
