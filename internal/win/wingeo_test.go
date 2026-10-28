package win

import "testing"

// 主窗默认尺寸算法：设计尺寸要夹进工作区，小屏上宁可变小也不能出屏；
// 极小屏保底用整个工作区。场景：200% 缩放屏（物理 3000×2120）与
// 小屏（1366×768）两种典型。
func TestFitInWorkArea(t *testing.T) {
	t.Run("大屏不夹", func(t *testing.T) {
		w, h := fitInWorkArea(2800, 1800, 3000, 2040)
		if w != 2800 || h != 1800 {
			t.Fatalf("got %dx%d want 2800x1800", w, h)
		}
	})

	t.Run("小屏夹进工作区", func(t *testing.T) {
		w, h := fitInWorkArea(2800, 1800, 1366, 728)
		if w != 1366-48 || h != 728-48 {
			t.Fatalf("got %dx%d want %dx%d", w, h, 1366-48, 728-48)
		}
	})

	t.Run("极小屏保底用整个工作区", func(t *testing.T) {
		w, _ := fitInWorkArea(2800, 1800, 300, 400)
		if w != 300 {
			t.Fatalf("got %d want 300", w)
		}
	})
}

// 记录的矩形还能不能用：尺寸太小或完全在虚拟屏幕外的都要拒掉，
// 否则换了显示器布局后主窗会恢复到看不见的地方。
func TestSaneWinRect(t *testing.T) {
	t.Run("正常矩形", func(t *testing.T) {
		if !saneWinRect(100, 120, 2800, 1800) {
			t.Fatal("正常矩形被拒了")
		}
	})

	t.Run("太小", func(t *testing.T) {
		if saneWinRect(0, 0, 400, 300) {
			t.Fatal("过小矩形通过了")
		}
	})

	t.Run("完全在屏幕外", func(t *testing.T) {
		// 虚拟屏幕从 (0,0) 开始，(-32000,-32000) 是最小化窗口的老巢
		if saneWinRect(-32000, -32000, 2800, 1800) {
			t.Fatal("屏幕外矩形通过了")
		}
	})
}
