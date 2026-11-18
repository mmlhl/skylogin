package app

// 自动进入的顺序与槽位规则：这两条直接决定"8 开只点了 3 个"会不会复现。

import (
	"testing"
	"time"
)

// 一个窗口只能点一次：点完游戏可能已经进去了，再点就是在大厅/商城里乱点。
func TestClaimClickOnlyOnce(t *testing.T) {
	resetClickClaims()
	if !claimClick(1001) {
		t.Fatal("第一次应当允许点")
	}
	if claimClick(1001) {
		t.Fatal("同一个 pid 不该允许第二次")
	}
	if !claimClick(1002) {
		t.Fatal("不同 pid 互不影响")
	}
	// 换批清账后可以重新点（pid 会被系统复用）
	resetClickClaims()
	if !claimClick(1001) {
		t.Fatal("清账后应当可以再点")
	}
	resetClickClaims()
}

// 槽位：默认 4 秒，可配，限制在 1~15 秒。
func TestAutoEnterSlot(t *testing.T) {
	cases := []struct {
		ms   int
		want time.Duration
	}{
		{0, 10 * time.Second},
		{2000, 2 * time.Second},
		{6000, 6 * time.Second},
		{100, time.Second},         // 下限
		{999999, 30 * time.Second}, // 上限
	}
	for _, c := range cases {
		cfg := Config{AutoEnterSlotMs: c.ms}
		if got := cfg.AutoEnterSlot(); got != c.want {
			t.Errorf("AutoEnterSlotMs=%d => %v want %v", c.ms, got, c.want)
		}
	}
}
