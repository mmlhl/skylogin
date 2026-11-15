package win

import "testing"

// 新增保存图标后，最小尺寸与常见 DPI 下的工具按钮不能压住窗口控制。
func TestWallSaveMenuFitsMinimumWidth(t *testing.T) {
	oldRects, oldSlot, oldDivs, oldH := wallMenuRects, wallSlotRect, wallMenuDivs, wallBarH
	t.Cleanup(func() { wallMenuRects, wallSlotRect, wallMenuDivs, wallBarH = oldRects, oldSlot, oldDivs, oldH })
	for _, dpi := range []int32{96, 120, 144, 192} {
		width := 480 * dpi / 96
		layoutWallMenu(dpi, width)
		saveFound := false
		for i, b := range wallMenu {
			if b.id == "save" {
				saveFound = true
			}
			r := wallMenuRects[i]
			if r.left < 0 || r.right > width || r.right <= r.left {
				t.Fatalf("DPI=%d 按钮%s越界：%+v", dpi, b.id, r)
			}
			for j := 0; j < i; j++ {
				other := wallMenuRects[j]
				if r.left < other.right && r.right > other.left && r.top < other.bottom && r.bottom > other.top {
					t.Fatalf("DPI=%d 按钮%s与%s重叠", dpi, b.id, wallMenu[j].id)
				}
			}
		}
		if !saveFound {
			t.Fatal("顶栏必须存在保存入口")
		}
	}
}
