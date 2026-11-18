package app

// 挂起批量放行的回归：只有"凑齐这一批"（或兜底超时）才能放行，
// 否则等于又退回普通连发。

import (
	"testing"
	"time"
)

func TestSuspendBatchReleasesWhenFull(t *testing.T) {
	beginSuspendBatch(3)
	defer endSuspendBatch()

	b := currentSuspendBatch()
	if b == nil {
		t.Fatal("beginSuspendBatch(3) 之后应当有批次")
	}
	b.add()
	b.add()
	select {
	case <-b.open:
		t.Fatal("只建了 2/3 就放行了")
	default:
	}
	b.add()
	select {
	case <-b.open:
	default:
		t.Fatal("3/3 建好后应当放行")
	}
}

func TestEndSuspendBatchReleases(t *testing.T) {
	beginSuspendBatch(2)
	b := currentSuspendBatch()
	b.add() // 只建了一个
	endSuspendBatch()
	select {
	case <-b.open:
	default:
		t.Fatal("收尾时必须放行，否则挂起的进程会一直卡着")
	}
	if currentSuspendBatch() != nil {
		t.Fatal("收尾后批次应当清空")
	}
}

// 单开不启用挂起批次：n<=1 时 currentSuspendBatch 必须是 nil，
// launchAt 才会走原来的启动路径。
func TestSuspendBatchSkippedForSingle(t *testing.T) {
	beginSuspendBatch(1)
	if currentSuspendBatch() != nil {
		t.Fatal("单开不该启用挂起批次")
	}
	endSuspendBatch()
}

func TestSuspendBatchWaitTimeout(t *testing.T) {
	beginSuspendBatch(5)
	b := currentSuspendBatch()
	b.add()
	start := time.Now()
	b.wait(50 * time.Millisecond) // 模拟兜底：等不到齐就自己放行
	if d := time.Since(start); d < 40*time.Millisecond {
		t.Fatalf("wait 应该等到超时，实际 %v", d)
	}
	endSuspendBatch()
}
