package win

import "testing"

// 级联排行的数学：放得下顺序铺，放不下等距错开且不出屏。
// 场景基准：2560×1440 屏 125% 缩放（工作区高约 1390），引擎最小窗 1024×720，
// 7 开收缩成 2 列 × 4 行——正是用户遇到的"有的窗口盖住别的"场景。
func TestCascadeYPositions(t *testing.T) {
	const top, bottom = int32(0), int32(1390)

	t.Run("放得下就顺序铺", func(t *testing.T) {
		got := cascadeYPositions([]int32{600, 600}, top, bottom)
		want := []int32{0, 600}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("行 %d: got %d want %d (all=%v)", i, got[i], want[i], got)
			}
		}
	})

	t.Run("7开4行级联错开", func(t *testing.T) {
		got := cascadeYPositions([]int32{720, 720, 720, 720}, top, bottom)
		// 步长 = (1390-720)/3 = 223，每行错开 223px，末行贴底
		want := []int32{0, 223, 446, 669}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("行 %d: got %d want %d (all=%v)", i, got[i], want[i], got)
			}
		}
		// 相邻两行必须错开足够一眼分辨、且点得到标题（≥32px）
		for i := 1; i < len(got); i++ {
			if got[i]-got[i-1] < 32 {
				t.Fatalf("行 %d 和 %d 只错开 %dpx，标题条点不到 (all=%v)", i-1, i, got[i]-got[i-1], got)
			}
		}
	})

	t.Run("任何一行底边不出屏", func(t *testing.T) {
		cases := [][]int32{
			{720, 720, 720, 720},
			{900, 300, 500, 720, 100},
			{1000, 1000},
		}
		for _, rowH := range cases {
			got := cascadeYPositions(rowH, top, bottom)
			for i, y := range got {
				if y < top {
					t.Fatalf("rowH=%v 行 %d 顶部越界 %d", rowH, i, y)
				}
				if y+rowH[i] > bottom {
					t.Fatalf("rowH=%v 行 %d 底部越界 %d+%d", rowH, i, y, rowH[i])
				}
			}
		}
	})

	t.Run("单行比屏幕还高时钉在顶部", func(t *testing.T) {
		got := cascadeYPositions([]int32{1500}, top, bottom)
		if got[0] != top {
			t.Fatalf("got %d want %d", got[0], top)
		}
	})

	t.Run("极端挤压也保持递增可点", func(t *testing.T) {
		// 10 行远超屏幕：步长被压到 1px 但顺序不乱、不出屏
		rows := make([]int32, 10)
		for i := range rows {
			rows[i] = 720
		}
		got := cascadeYPositions(rows, top, bottom)
		for i := 1; i < len(got); i++ {
			if got[i] < got[i-1] {
				t.Fatalf("行 %d 比上一行还高: %d < %d", i, got[i], got[i-1])
			}
		}
		last := len(got) - 1
		if got[last]+rows[last] > bottom {
			t.Fatalf("末行底部越界")
		}
	})
}
