package app

import (
	"strings"
	"testing"
	"time"
)

const gameUpdateReply = `{"data":{"apps":[{"version_code":"v3_999_new","app_id":63,` +
	`"app_origin_version":null,"update_actions":[]}]},"code":200}`

// TestCheckGameUpdateNoDeadlock 回归：checkGameUpdateNow 内部不能再对同一把
// sync.Mutex 重复加锁（2026-11-14 曾因此死锁，/api/state 全部挂起，
// 界面表现为账号空、弹窗空、点不动）。
func TestCheckGameUpdateNoDeadlock(t *testing.T) {
	oldGet := gameUpdateHTTPGet
	gameUpdateHTTPGet = func(string) ([]byte, error) { return []byte(gameUpdateReply), nil }
	defer func() { gameUpdateHTTPGet = oldGet }()

	done := make(chan map[string]any, 1)
	go func() { done <- checkGameUpdateNow() }()

	select {
	case view := <-done:
		if view["update_needed"] != true || view["checked"] != true {
			t.Fatalf("比对结果不对: %v", view)
		}
		if !strings.Contains(view["latest_version"].(string), "999") {
			t.Fatalf("latest_version 不对: %v", view)
		}
		// 锁已释放：并发再取一次视图必须立刻返回
		v2 := gameUpdateView()
		if v2["update_needed"] != true {
			t.Fatalf("视图状态丢失: %v", v2)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("checkGameUpdateNow 卡死了（互斥锁不可重入的老问题回归）")
	}
}
