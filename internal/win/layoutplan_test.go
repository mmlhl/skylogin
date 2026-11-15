package win

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func overlaps(a, b TargetRect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func TestLayoutPlannerPhysicalMatrix(t *testing.T) {
	for _, area := range []TargetRect{{W: 1920, H: 1040}, {W: 2560, H: 1390}, {W: 3840, H: 2110}} {
		for _, n := range []int{1, 2, 3, 4, 5, 7, 9, 16} {
			for _, mode := range []string{"tile", "cascade:cols", "cascade:rows", "fullscreen"} {
				t.Run(fmt.Sprintf("%dx%d/%d/%s", area.W, area.H, n, mode), func(t *testing.T) {
					p := PlanLayout(LayoutRequest{Count: n, Mode: mode}, area, 1024, 720)
					if mode == "fullscreen" {
						if len(p.Rects) != 0 {
							t.Fatal("全屏不应移动窗口")
						}
						return
					}
					if len(p.Rects) != n {
						t.Fatalf("数量=%d", len(p.Rects))
					}
					for i, r := range p.Rects {
						if r.W < 1024 || r.H < 720 {
							t.Fatalf("窗口%d小于引擎下限：%+v", i, r)
						}
						if r.X < area.X || r.Y < area.Y || r.X+r.W > area.X+area.W || r.Y+r.H > area.Y+area.H {
							t.Fatalf("窗口%d越界：%+v", i, r)
						}
						if r.ZOrder != i+1 {
							t.Fatal("层级应为底1顶N")
						}
					}
					capacity := area.W / 1024 * (area.H / 720)
					if p.Mode == "tile" {
						if n > capacity {
							t.Fatal("不得提供伪平铺")
						}
						for i, a := range p.Rects {
							for _, b := range p.Rects[i+1:] {
								if overlaps(a, b) {
									t.Fatal("真平铺发生遮挡")
								}
							}
						}
					}
					if mode == "tile" && n > capacity {
						if p.Mode != "cascade:cols" || p.Feasible || p.Reason == "" {
							t.Fatal("无法平铺应明确回退标题布局")
						}
					}
					if p.Mode == "cascade:cols" && p.Reason == "" {
						last := map[int]int{}
						for _, r := range p.Rects {
							if y, ok := last[r.X]; ok && r.Y-y < 44 {
								t.Fatalf("标题间隔不足44：%d", r.Y-y)
							}
							last[r.X] = r.Y
						}
					}
				})
			}
		}
	}
}

func TestLayoutMeasuredTwoColumnAndPicturePositions(t *testing.T) {
	area := TargetRect{W: 2560, H: 1390}
	four := PlanLayout(LayoutRequest{Count: 4, Mode: "cascade:cols"}, area, 1024, 720)
	want := [][2]int{{0, 0}, {0, 670}, {1536, 0}, {1536, 670}}
	for i, r := range four.Rects {
		if r.X != want[i][0] || r.Y != want[i][1] {
			t.Fatalf("4窗口标题位置%d=%+v", i, r)
		}
	}
	seven := PlanLayout(LayoutRequest{Count: 7, Mode: "cascade:cols"}, area, 1024, 720)
	if seven.Rects[3].Y != 670 || seven.Rects[6].Y != 670 || seven.Rects[1].Y < 44 || seven.Rects[5].Y < 44 {
		t.Fatalf("7窗口必须充分利用670纵向余量：%+v", seven.Rects)
	}
	picture := PlanLayout(LayoutRequest{Count: 7, Mode: "cascade:rows"}, area, 1024, 720)
	if picture.Rects[3].X != 1536 || picture.Rects[4].Y != 670 || picture.Rects[6].X != 1536 {
		t.Fatalf("画面方案应上下4+3：%+v", picture.Rects)
	}
	if p := PlanLayout(LayoutRequest{Count: 2, Mode: "tile", Cols: 2, Rows: 2}, area, 1024, 720); p.Mode != "tile" || !p.Feasible {
		t.Fatal("2K工作区两窗确实可并排，不能受旧2×2偏好误导")
	}
}

func TestLayoutSmallScreenKeepsMinimumAndTitle(t *testing.T) {
	area := TargetRect{X: 20, Y: 30, W: 800, H: 600}
	p := PlanLayout(LayoutRequest{Count: 7, Mode: "cascade:cols"}, area, 1024, 720)
	if p.Feasible || p.Reason == "" {
		t.Fatal("小于引擎下限必须明确提示")
	}
	for _, r := range p.Rects {
		if r.W != 1024 || r.H != 720 || r.X != 20 || r.Y != 30 {
			t.Fatalf("不能假缩小或将标题放屏外：%+v", r)
		}
	}
}

func TestLayoutCanonicalCompatibilityAndRecommendation(t *testing.T) {
	area := TargetRect{W: 2560, H: 1390}
	for _, mode := range []string{"cascade", "cascade:dual", "cascade:cols"} {
		if p := PlanLayout(LayoutRequest{Count: 7, Mode: mode}, area, 0, 0); p.Mode != "cascade:cols" {
			t.Fatal(mode)
		}
	}
	if p := PlanLayout(LayoutRequest{Count: 7, Mode: "cascade:fan"}, area, 0, 0); p.Mode != "cascade:rows" {
		t.Fatal("旧满扇应映射画面方案")
	}
	if p := PlanLayout(LayoutRequest{Count: 2}, area, 0, 0); p.Mode != "tile" {
		t.Fatal("空模式采用推荐")
	}
	if p := PlanLayout(LayoutRequest{Count: 0}, area, 0, 0); len(p.Rects) != 0 || len(p.Options) != 4 {
		t.Fatal("空组仍提供选项与环境")
	}
}

// 只在人工指定路径时输出审阅数据，正常测试不产生仓库外文件。
func TestLayoutMeasuredReport(t *testing.T) {
	path := os.Getenv("SKYLOGIN_LAYOUT_REPORT")
	if path == "" {
		t.Skip("未请求规划报告")
	}
	out := map[string]map[string]LayoutPlan{}
	for _, n := range []int{1, 2, 3, 4, 5, 7, 9} {
		m := map[string]LayoutPlan{}
		for _, mode := range []string{"tile", "cascade:cols", "cascade:rows", "fullscreen"} {
			p := PlanLayout(LayoutRequest{Count: n, Mode: mode}, TargetRect{W: 2560, H: 1390}, 1024, 720)
			p.ScreenW, p.ScreenH = 2560, 1440
			m[mode] = p
		}
		out[fmt.Sprint(n)] = m
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLayoutPictureInsufficientSpanDoesNotPretendExposure(t *testing.T) {
	for _, area := range []TargetRect{{W: 2560, H: 720}, {W: 1024, H: 1040}} {
		p := PlanLayout(LayoutRequest{Count: 4, Mode: "cascade:rows"}, area, 1024, 720)
		if p.Mode != "cascade:cols" || p.Feasible || p.Reason == "" {
			t.Fatalf("完全遮挡方案必须禁用并明确回退：%+v", p)
		}
		for _, o := range p.Options {
			if o.ID == "cascade:rows" && o.Enabled {
				t.Fatal("无展开余量不能推荐画面方案")
			}
		}
	}
}
