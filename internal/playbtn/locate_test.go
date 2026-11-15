package playbtn

// 定位算法的单元测试：手工画"登录界面"，验证能在不同尺寸/位置下找到按钮，
// 并且在"游戏画面"（亮块周围也是亮的）上不乱报。

import (
	"math"
	"testing"
)

// newCanvas 造一张 BGRA 画布，底色用登录界面右侧的深蓝。
func newCanvas(w, h int) *image {
	img := &image{w: w, h: h, pix: make([]byte, w*h*4)}
	for i := 0; i < len(img.pix); i += 4 {
		img.pix[i] = 40   // B
		img.pix[i+1] = 18 // G
		img.pix[i+2] = 8  // R
		img.pix[i+3] = 255
	}
	return img
}

// putPixel 写一个像素（传 RGB，内部转 BGRA）。
func putPixel(img *image, x, y int, r, g, b byte) {
	if x < 0 || y < 0 || x >= img.w || y >= img.h {
		return
	}
	i := (y*img.w + x) * 4
	img.pix[i] = b
	img.pix[i+1] = g
	img.pix[i+2] = r
}

// drawButton 画一个"深色圆角按钮 + 暖白三角"，返回三角中心。
func drawButton(img *image, cx, cy, halfW, halfH int) (int, int) {
	for y := cy - halfH; y <= cy+halfH; y++ {
		for x := cx - halfW; x <= cx+halfW; x++ {
			// 按钮底：接近黑
			putPixel(img, x, y, 26, 26, 28)
		}
	}
	// 三角：向右，约占按钮的三分之一（实测真实按钮 60x66、三角 20x20）
	size := int(math.Min(float64(halfW), float64(halfH)) * 0.33)
	for dy := -size; dy <= size; dy++ {
		for dx := -size; dx <= size; dx++ {
			if float64(dx) <= float64(dy)*0.9+float64(size)*0.35 {
				putPixel(img, cx+dx, cy+dy, 249, 241, 213)
			}
		}
	}
	return cx, cy
}

func TestLocateFindsButtonAtDifferentSizes(t *testing.T) {
	cases := []struct{ w, h, cx, cy int }{
		{1024, 720, 686, 374},
		{819, 576, 548, 299},
		{1280, 900, 857, 468},
	}
	for _, c := range cases {
		img := newCanvas(c.w, c.h)
		wantX, wantY := drawButton(img, c.cx, c.cy, 30, 33)
		gotX, gotY, ok := locate(img)
		if !ok {
			t.Fatalf("%dx%d：应该能找到按钮", c.w, c.h)
		}
		if abs(gotX-wantX) > 6 || abs(gotY-wantY) > 6 {
			t.Fatalf("%dx%d：定位偏了，期望 (%d,%d)，实际 (%d,%d)",
				c.w, c.h, wantX, wantY, gotX, gotY)
		}
	}
}

func TestLocateRejectsBrightScene(t *testing.T) {
	// 游戏画面：整块区域都是亮的，暖白块四周不深
	img := newCanvas(1024, 720)
	for y := 200; y < 600; y++ {
		for x := 500; x < 900; x++ {
			putPixel(img, x, y, 220, 200, 170)
		}
	}
	for y := 300; y < 360; y++ {
		for x := 650; x < 700; x++ {
			putPixel(img, x, y, 249, 241, 213)
		}
	}
	if _, _, ok := locate(img); ok {
		t.Fatal("亮色场景不该被认成播放按钮")
	}
}

func TestLocateRejectsBlobTooLarge(t *testing.T) {
	// 一大片暖白（比如过场动画）：形状校验应该拦下
	img := newCanvas(1024, 720)
	for y := 300; y < 420; y++ {
		for x := 620; x < 740; x++ {
			putPixel(img, x, y, 249, 241, 213)
		}
	}
	if _, _, ok := locate(img); ok {
		t.Fatal("过大的暖白块不该被认成播放按钮")
	}
}

func TestLocateRejectsEmptyCanvas(t *testing.T) {
	if _, _, ok := locate(newCanvas(1024, 720)); ok {
		t.Fatal("空画面不该有按钮")
	}
}

func TestChangedDetectsTransition(t *testing.T) {
	a := newCanvas(200, 200)
	b := newCanvas(200, 200)
	if changed(a, b) {
		t.Fatal("完全相同的画面不该判定为变化")
	}
	// 覆盖 10% 面积
	for y := 0; y < 200; y++ {
		for x := 0; x < 20; x++ {
			putPixel(b, x, y, 255, 255, 255)
		}
	}
	if !changed(a, b) {
		t.Fatal("10% 像素变化应该判定为已切换画面")
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
