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

// 四种堆叠方案的结构特征 + 不出屏（基准屏 2560×1388 = 用户实测工作区）。
func TestStackSchemes(t *testing.T) {
	wa := windowRect{left: 0, top: 0, right: 2560, bottom: 1388}
	n := 8
	for _, tc := range []struct {
		scheme, name string
		check        func(t *testing.T, rs []windowRect)
	}{
		{SchemeDual, "双摞", func(t *testing.T, rs []windowRect) {
			// 左摞 4 个全在左半区、右摞 4 个全在右半区
			for i, r := range rs {
				mid := int32(1280)
				side := r.right <= mid+44
				if i < 4 && !side {
					t.Fatalf("窗 %d 应在左半区: %+v", i+1, r)
				}
				if i >= 4 && r.left < mid-44 {
					t.Fatalf("窗 %d 应在右半区: %+v", i+1, r)
				}
			}
		}},
		{SchemeRows, "两行横向", func(t *testing.T, rs []windowRect) {
			// 只有两个 y 值：上行 4 个贴顶、下行 4 个贴底
			ys := map[int32]int{}
			for _, r := range rs {
				ys[r.top]++
			}
			if len(ys) != 2 {
				t.Fatalf("应有且仅有两行，实际 y 值: %v", ys)
			}
		}},
		{SchemeCols, "两列竖向", func(t *testing.T, rs []windowRect) {
			// 只有两个 x 值，且两列不重叠（右列左 ≥ 左列右）
			if rs[4].left < rs[0].right {
				t.Fatalf("两列重叠: 左列右=%d 右列左=%d", rs[0].right, rs[4].left)
			}
		}},
		{SchemeFan, "满扇形", func(t *testing.T, rs []windowRect) {
			step := rs[1].left - rs[0].left
			if step <= 44 {
				t.Fatalf("满扇步长应 > 44，实际 %d", step)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs := planStacking(tc.scheme, n, 0, 0, wa)
			if len(rs) != n {
				t.Fatalf("窗口数 %d != %d", len(rs), n)
			}
			for i, r := range rs {
				if r.left < wa.left || r.top < wa.top || r.right > wa.right || r.bottom > wa.bottom {
					t.Fatalf("窗 %d 出屏: %+v", i+1, r)
				}
				if r.right-r.left != 1024 || r.bottom-r.top != 720 {
					t.Fatalf("窗 %d 尺寸不对: %dx%d", i+1, r.right-r.left, r.bottom-r.top)
				}
			}
			if tc.check != nil {
				tc.check(t, rs)
			}
		})
	}
}
