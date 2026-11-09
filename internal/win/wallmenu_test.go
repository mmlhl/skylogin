package win

import "testing"

// 顶栏布局的硬约束：任何宽度下两组都不能重叠、不能越界。
// 480 是监控墙的最小宽度（saneWallRect），也是最挤的情况。
func TestWallMenuLayout(t *testing.T) {
	for _, cw := range []int32{480, 560, 800, 1120, 1600} {
		barH := layoutWallMenu(96, cw)
		if barH <= 0 {
			t.Fatalf("栏高应为正，got %d", barH)
		}
		var prevRight int32 = -1
		for i, b := range wallMenu {
			r := wallMenuRects[i]
			if r.right <= r.left || r.bottom <= r.top {
				t.Fatalf("客户区宽 %d：第 %d 项没有尺寸 %+v", cw, i, r)
			}
			if r.left < 0 || r.right > cw {
				t.Fatalf("客户区宽 %d：第 %d 项越界 %+v", cw, i, r)
			}
			if b.group != menuGroupArrange {
				continue
			}
			if prevRight >= 0 && r.left < prevRight {
				t.Fatalf("客户区宽 %d：排列项重叠（第 %d 项 left=%d < %d）", cw, i, r.left, prevRight)
			}
			prevRight = r.right
		}
		if wallSlotRect.right != prevRight {
			t.Fatalf("客户区宽 %d：排列组右边 %d 与最后一项 %d 不一致", cw, wallSlotRect.right, prevRight)
		}
		// 工具按钮必须在排列组右侧，不能压上去
		for i, b := range wallMenu {
			if b.group != menuGroupTool {
				continue
			}
			if wallMenuRects[i].left < wallSlotRect.right+4 {
				t.Fatalf("客户区宽 %d：工具按钮 %s 压到排列组（%d < %d）",
					cw, b.id, wallMenuRects[i].left, wallSlotRect.right)
			}
		}
	}
}
