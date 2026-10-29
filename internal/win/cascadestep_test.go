package win

import "testing"

// 级联步长自适应（2026-10-29 实测调整）：
// ≤4 开 44px 紧凑一摞；>4 开满扇形摊开；小屏富余不足自动收缩且不出屏。
// 基准屏：2K 200%（工作区 3000×2040）与 1080p（1920×1040）。
func TestPlanCascadeSteps(t *testing.T) {
	t.Run("小开数保持44px紧凑步长", func(t *testing.T) {
		wa := windowRect{left: 0, top: 0, right: 3000, bottom: 2040}
		for _, n := range []int{2, 4} {
			rects := planCascade(n, 0, 0, wa)
			step := rects[1].left - rects[0].left
			if step != 44 {
				t.Fatalf("N=%d 步长 %d，want 44", n, step)
			}
		}
	})

	t.Run("大开数切满扇形", func(t *testing.T) {
		wa := windowRect{left: 0, top: 0, right: 3000, bottom: 2040}
		n := 8
		rects := planCascade(n, 0, 0, wa)
		step := rects[1].left - rects[0].left
		// 满扇步长 = min(横富余, 竖富余)/(n-1) = min(1976,1320)/7 = 188
		if step != 188 {
			t.Fatalf("N=8 满扇步长 %d，want 188", step)
		}
		// 整摞居中且不出屏
		last := rects[n-1]
		if last.right > wa.right || last.bottom > wa.bottom {
			t.Fatalf("末窗出屏: %+v", last)
		}
	})

	t.Run("1080p 八开也满扇且不出屏", func(t *testing.T) {
		wa := windowRect{left: 0, top: 0, right: 1920, bottom: 1040}
		n := 8
		rects := planCascade(n, 0, 0, wa)
		// 满扇步长 = min(896,320)/7 = 45（恰好和紧凑步长一个量级，取满扇值）
		step := rects[1].left - rects[0].left
		if step < 1 {
			t.Fatalf("步长被压没了: %d", step)
		}
		for i, r := range rects {
			if r.left < wa.left || r.top < wa.top || r.right > wa.right || r.bottom > wa.bottom {
				t.Fatalf("窗 %d 出屏: %+v", i, r)
			}
		}
	})

	t.Run("小屏富余不足自动收缩", func(t *testing.T) {
		// 笔记本 1366×728：竖向富余只有 8px，步长必须收缩到 1 而不是出屏
		wa := windowRect{left: 0, top: 0, right: 1366, bottom: 728}
		n := 6
		rects := planCascade(n, 0, 0, wa)
		for i, r := range rects {
			if r.left < wa.left || r.top < wa.top || r.right > wa.right || r.bottom > wa.bottom {
				t.Fatalf("窗 %d 出屏: %+v", i, r)
			}
		}
	})
}
