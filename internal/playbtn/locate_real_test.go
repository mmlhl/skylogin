package playbtn

// 用真实登录界面截图验证定位（不依赖游戏在跑）。
//
// 数据来源：真机抓的一张 1024x720 登录界面（桌面截图，经 Python 转成
// BGRA 原始像素放在 tmp/ 下）。tmp/ 不入库，所以文件不在时跳过。
//
// 手动跑：go test ./internal/playbtn/ -run TestLocateRealScreenshot -v
// 换一张截图：PLAYBTN_SAMPLE=<path> go test ...

import (
	"os"
	"testing"
)

func TestLocateRealScreenshot(t *testing.T) {
	path := os.Getenv("PLAYBTN_SAMPLE")
	if path == "" {
		path = "../../tmp/login_1024x720.bgra"
	}
	pix, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("没有样本图（%v），跳过", err)
	}
	if len(pix) != 1024*720*4 {
		t.Skipf("样本尺寸不对：%d 字节", len(pix))
	}
	img := &image{w: 1024, h: 720, pix: pix}

	x, y, ok := locate(img)
	if !ok {
		t.Fatal("真实登录界面上应该能定位到播放按钮")
	}
	// 真机上按钮中心约 (686,374)，容许几十像素的偏差
	if x < 620 || x > 750 || y < 300 || y > 450 {
		t.Fatalf("定位结果偏了：(%d,%d)", x, y)
	}
	t.Logf("定位到 (%d,%d)", x, y)
}
