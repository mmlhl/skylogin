package playbtn

// 定位播放按钮：不做模板匹配、不接 OCR/模型，只做"颜色阈值 + 最大连通块。
//
// 依据（实测 2026-11-16，游戏 0.16.8.421730）：
//   - 登录界面右侧预览区里，播放三角是唯一的"暖白"实心块，RGB ≈ (249,241,213)
//   - 它四周是按钮的深色底，再外边是深蓝预览画面
//   - 三角必然完整落在按钮内部，所以点它的质心就等价于点按钮
//
// 所以顺序是：按比例圈定预览区 → 暖白阈值 → 最大连通块 → 形状 + 四周是否深色。
// 任何一条不满足就返回 false（宁可不动，也不要在游戏里乱点）。

import "sort"

const (
	// 预览区在整窗中的比例范围：留足余量，窗口尺寸/DPI 变了也还在框里
	regionX0Ratio, regionX1Ratio = 52, 90
	regionY0Ratio, regionY1Ratio = 32, 75
	// 降采样倍数：1024x720 下掩膜只有 256x180，连通域跑起来是微秒级
	sampleStep = 4
	// 三角块的形状范围（降采样后按格算）
	blobMinCells = 4
	blobMaxCells = 10
	// 暖白阈值
	whiteR, whiteG, whiteB = 200, 185, 140
	warmLow, warmHigh      = 15, 60 // r-b 的区间
	// 四周必须是按钮深色底（60 分位亮度上限）
	surroundP60Max = 110
)

// locate 在整窗画面里找播放三角，返回它的中心（窗口坐标）。
func locate(img *image) (int, int, bool) {
	if img == nil || img.w < 200 || img.h < 150 {
		return 0, 0, false
	}
	x0 := img.w * regionX0Ratio / 100
	x1 := img.w * regionX1Ratio / 100
	y0 := img.h * regionY0Ratio / 100
	y1 := img.h * regionY1Ratio / 100

	sw := (x1 - x0 + sampleStep - 1) / sampleStep
	sh := (y1 - y0 + sampleStep - 1) / sampleStep
	if sw <= 0 || sh <= 0 {
		return 0, 0, false
	}

	mask := make([]bool, sw*sh)
	total := 0
	for sy := 0; sy < sh; sy++ {
		py := y0 + sy*sampleStep
		for sx := 0; sx < sw; sx++ {
			px := x0 + sx*sampleStep
			i := (py*img.w + px) * 4
			b, g, r := int(img.pix[i]), int(img.pix[i+1]), int(img.pix[i+2])
			warm := r - b
			if r > whiteR && g > whiteG && b > whiteB && warm > warmLow && warm < warmHigh {
				mask[sy*sw+sx] = true
				total++
			}
		}
	}
	// 面积太小：不是按钮（可能是纯文字/角标被漏掉后的碎片）
	if total*sampleStep*sampleStep < 60 {
		return 0, 0, false
	}

	area, cx, cy, minX, minY, maxX, maxY, ok := largestComponent(mask, sw, sh)
	if !ok {
		return 0, 0, false
	}

	bw := maxX - minX + 1
	bh := maxY - minY + 1
	if bw < blobMinCells || bh < blobMinCells || bw > blobMaxCells || bh > blobMaxCells {
		return 0, 0, false
	}
	fill := float64(area) / float64(bw*bh)
	if fill < 0.4 || fill > 0.85 {
		return 0, 0, false
	}

	// 四周必须是深色按钮底：外扩 6px 取 60 分位亮度
	if !darkSurround(img, x0+minX*sampleStep, y0+minY*sampleStep,
		x0+maxX*sampleStep, y0+maxY*sampleStep) {
		return 0, 0, false
	}
	return x0 + int(cx)*sampleStep, y0 + int(cy)*sampleStep, true
}

// largestComponent 在二值掩膜上找面积最大的 4 连通块（广度/深度都行，这里用栈）。
func largestComponent(mask []bool, w, h int) (area int, cx, cy float64, minX, minY, maxX, maxY int, ok bool) {
	seen := make([]bool, len(mask))
	stack := make([]int, 0, 64)
	for start := 0; start < len(mask); start++ {
		if !mask[start] || seen[start] {
			continue
		}
		seen[start] = true
		stack = append(stack[:0], start)
		a := 0
		sumX, sumY := 0, 0
		lx, ly, hx, hy := w, h, -1, -1
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			y, x := cur/w, cur%w
			a++
			sumX += x
			sumY += y
			if x < lx {
				lx = x
			}
			if y < ly {
				ly = y
			}
			if x > hx {
				hx = x
			}
			if y > hy {
				hy = y
			}
			if x > 0 && mask[cur-1] && !seen[cur-1] {
				seen[cur-1] = true
				stack = append(stack, cur-1)
			}
			if x < w-1 && mask[cur+1] && !seen[cur+1] {
				seen[cur+1] = true
				stack = append(stack, cur+1)
			}
			if y > 0 && mask[cur-w] && !seen[cur-w] {
				seen[cur-w] = true
				stack = append(stack, cur-w)
			}
			if y < h-1 && mask[cur+w] && !seen[cur+w] {
				seen[cur+w] = true
				stack = append(stack, cur+w)
			}
		}
		if a > area {
			area = a
			cx, cy = float64(sumX)/float64(a), float64(sumY)/float64(a)
			minX, minY, maxX, maxY = lx, ly, hx, hy
			ok = true
		}
	}
	return
}

// darkSurround 检查外接框外扩一圈是不是深色（按钮底/深蓝背景），
// 用来排除游戏画面里的暖色灯光、角色身上的浅色块。
func darkSurround(img *image, x0, y0, x1, y1 int) bool {
	const pad = 6
	vx0, vy0 := clampInt(x0-pad, 0, img.w-1), clampInt(y0-pad, 0, img.h-1)
	vx1, vy1 := clampInt(x1+pad, 0, img.w-1), clampInt(y1+pad, 0, img.h-1)
	vals := make([]uint8, 0, (vx1-vx0+1)*(vy1-vy0+1))
	for py := vy0; py <= vy1; py++ {
		for px := vx0; px <= vx1; px++ {
			i := (py*img.w + px) * 4
			m := img.pix[i]
			if v := img.pix[i+1]; v > m {
				m = v
			}
			if v := img.pix[i+2]; v > m {
				m = v
			}
			vals = append(vals, m)
		}
	}
	if len(vals) == 0 {
		return false
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	return int(vals[len(vals)*60/100]) <= surroundP60Max
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
