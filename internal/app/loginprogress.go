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
	// QR 是扫码登录的二维码 PNG（base64，不带 data: 前缀）。
	//
	// 应用宝（微信 / QQ）走纯协议扫码，没有窗口可看，二维码必须由后端
	// 传给界面显示。空表示当前流程不需要二维码。
	QR string
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

// set 写入状态文案。
//
// 注意保留 Running：登录流程中途会多次调它更新文案（比如「正在登录…」），
// 如果这里重置成 false，界面就会误以为流程已结束。
// 真正的收尾走 finish()。
func (t *loginTracker) set(phase, msg, account, uid string) {
	t.mu.Lock()
	running := t.cur.Running
	qr := t.cur.QR // 文案更新不能把二维码冲掉：扫码流程会多次调 set
	t.cur = loginProgress{Running: running, Phase: phase, Message: msg, Account: account, UID: uid, QR: qr}
	t.mu.Unlock()
}

// setQR 写入扫码二维码（PNG 的 base64）。
func (t *loginTracker) setQR(b64 string) {
	t.mu.Lock()
	t.cur.QR = b64
	t.mu.Unlock()
}

// finish 流程收尾：Running 清掉，phase/message 留给界面展示。
func (t *loginTracker) finish() {
	t.mu.Lock()
	t.cur.Running = false
	t.mu.Unlock()
}

