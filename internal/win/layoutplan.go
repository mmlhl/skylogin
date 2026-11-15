package win

import (
	"fmt"
	"strings"
)

// LayoutRequest 同时供预览与实际排列使用；cols/rows 是旧接口的偏好，不得迫使窗口小于引擎下限。
type LayoutRequest struct {
	Count int    `json:"count"`
	Mode  string `json:"mode"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	W     int    `json:"w"`
	H     int    `json:"h"`
}

type LayoutOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Reason      string `json:"reason"`
}

type LayoutPlan struct {
	Mode            string         `json:"mode"`
	RequestedMode   string         `json:"requestedMode"`
	RecommendedMode string         `json:"recommendedMode"`
	Feasible        bool           `json:"feasible"`
	Reason          string         `json:"reason"`
	Message         string         `json:"message"`
	ScreenW         int            `json:"screenW"`
	ScreenH         int            `json:"screenH"`
	WorkArea        TargetRect     `json:"workArea"`
	MinW            int            `json:"minW"`
	MinH            int            `json:"minH"`
	MinSource       string         `json:"minSource"`
	Rects           []TargetRect   `json:"rects"`
	Options         []LayoutOption `json:"options"`
}

// CanonicalLayoutMode 保留旧配置入口，但方案名称只保留标题、画面、真平铺和全屏。
func CanonicalLayoutMode(mode string) string {
	switch mode {
	case "fullscreen":
		return "fullscreen"
	case "cascade:rows", "cascade:fan":
		return "cascade:rows"
	case "cascade", "cascade:dual", "cascade:cols":
		return "cascade:cols"
	case "tile", "":
		return "tile"
	default:
		if strings.HasPrefix(mode, "cascade:") {
			return "cascade:cols"
		}
		return "tile"
	}
}

func layoutMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func layoutMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func spread(start, span, index, count int) int {
	if count <= 1 {
		return start + span/2
	}
	return start + span*index/(count-1)
}

// PlanLayout 是纯物理像素规划：不量窗口、不移动窗口，也不访问本机账号数据。
// 最小值来自同机七个游戏窗口的只读测量；不能把用户放大的当前尺寸当成最小值。
func PlanLayout(req LayoutRequest, area TargetRect, minW, minH int) LayoutPlan {
	if minW <= 0 {
		minW = engineMinW
	}
	if minH <= 0 {
		minH = engineMinH
	}
	n := req.Count
	if n < 0 {
		n = 0
	}
	if n > 256 {
		n = 256
	}
	w, h := layoutMax(minW, req.W), layoutMax(minH, req.H)
	requested := CanonicalLayoutMode(req.Mode)
	p := LayoutPlan{Mode: requested, RequestedMode: req.Mode, Feasible: true, WorkArea: area, MinW: minW, MinH: minH, MinSource: "measured-default", Rects: []TargetRect{}}
	maxCols, maxRows := 0, 0
	if area.W > 0 && area.H > 0 {
		maxCols, maxRows = area.W/w, area.H/h
	}
	capacity := maxCols * maxRows
	tileOK := n > 0 && n <= capacity
	tileReason := ""
	if !tileOK && n > 0 {
		tileReason = fmt.Sprintf("工作区 %d×%d 只能并排容纳 %d 个至少 %d×%d 的窗口", area.W, area.H, capacity, w, h)
	}
	spanX, spanY := layoutMax(0, area.W-w), layoutMax(0, area.H-h)
	columns := layoutMin(layoutMax(1, maxCols), 2)
	if n < columns {
		columns = layoutMax(1, n)
	}
	// 两列优先；数量太多使标题间隔不足时，再利用工作区能容纳的更多列。
	for columns < maxCols && (n+columns-1)/columns > spanY/44+1 {
		columns++
	}
	perCol := (n + columns - 1) / columns
	titleOK := n > 0 && area.W >= w && area.H >= h && (perCol <= 1 || spanY/(perCol-1) >= 44)
	titleReason := ""
	if !titleOK && n > 0 {
		titleReason = "当前工作区无法为每个窗口露出至少 44 像素标题，请通过监控墙切换"
	}
	pictureRowCount := (n + 1) / 2
	pictureOK := n > 0 && area.W >= w && area.H >= h && (n == 1 || spanY >= 44 && (pictureRowCount <= 1 || spanX/(pictureRowCount-1) >= 44))
	pictureReason := ""
	if !pictureOK && n > 0 {
		pictureReason = "工作区余量不足，画面方案会完全遮住部分窗口；已改为标题布局，请通过监控墙切换"
	}
	p.RecommendedMode = "cascade:cols"
	if tileOK {
		p.RecommendedMode = "tile"
	}
	if req.Mode == "" {
		p.Mode = p.RecommendedMode
		requested = p.Mode
	}
	p.Options = []LayoutOption{
		{ID: "tile", Label: "窗口并排", Description: "窗口互不遮挡，只在工作区真正放得下时可用", Enabled: tileOK, Reason: tileReason},
		{ID: "cascade:cols", Label: "标题易切换", Description: "左右分列并充分展开标题，方便逐个点击", Enabled: n > 0, Reason: titleReason},
		{ID: "cascade:rows", Label: "画面多露出", Description: "上下两行横向展开，允许重叠以露出更多画面", Enabled: pictureOK, Reason: pictureReason},
		{ID: "fullscreen", Label: "专注全屏", Description: "每个账号使用全屏，通过监控墙或 Alt+Tab 切换", Enabled: n > 0},
	}
	if n == 0 {
		p.Feasible = false
		p.Reason = "没有需要排列的窗口"
		return p
	}
	if requested == "fullscreen" {
		p.Message = "全屏不执行窗口位置排列，请通过监控墙或 Alt+Tab 切换"
		return p
	}
	if requested == "tile" && !tileOK {
		p.Mode = "cascade:cols"
		p.Feasible = false
		p.Reason = tileReason + "，已改为标题易切换"
	}
	if requested == "cascade:rows" && !pictureOK {
		p.Mode = "cascade:cols"
		p.Feasible = false
		p.Reason = pictureReason
	}
	if n == 1 {
		p.Rects = []TargetRect{{X: area.X + spanX/2, Y: area.Y + spanY/2, W: w, H: h, ZOrder: 1}}
	} else if p.Mode == "tile" {
		// 容量由最小尺寸和工作区决定；旧正方形网格不再产生伪平铺。
		cols := layoutMin(maxCols, n)
		if req.Cols > 0 && req.Cols <= maxCols && (n+req.Cols-1)/req.Cols <= maxRows {
			cols = req.Cols
		}
		rows := (n + cols - 1) / cols
		for i := 0; i < n; i++ {
			c, r := i%cols, i/cols
			p.Rects = append(p.Rects, TargetRect{X: area.X + (area.W-cols*w)/2 + c*w, Y: area.Y + (area.H-rows*h)/2 + r*h, W: w, H: h, ZOrder: i + 1})
		}
	} else if p.Mode == "cascade:rows" {
		rows := 2
		if n < rows {
			rows = n
		}
		left := n
		offset := 0
		for row := 0; row < rows; row++ {
			count := (left + rows - row - 1) / (rows - row)
			y := spread(area.Y, spanY, row, rows)
			for i := 0; i < count; i++ {
				p.Rects = append(p.Rects, TargetRect{X: spread(area.X, spanX, i, count), Y: y, W: w, H: h, ZOrder: offset + i + 1})
			}
			left -= count
			offset += count
		}
		if !pictureOK {
			p.Feasible = false
			p.Reason = pictureReason
		}
	} else {
		left := n
		offset := 0
		for col := 0; col < columns; col++ {
			count := (left + columns - col - 1) / (columns - col)
			x := spread(area.X, spanX, col, columns)
			for i := 0; i < count; i++ {
				p.Rects = append(p.Rects, TargetRect{X: x, Y: spread(area.Y, spanY, i, count), W: w, H: h, ZOrder: offset + i + 1})
			}
			left -= count
			offset += count
		}
		if !titleOK {
			p.Feasible = false
			if p.Reason != "" {
				p.Reason += "；"
			}
			p.Reason += titleReason
		}
	}
	if area.W < w || area.H < h {
		p.Feasible = false
		if p.Reason == "" {
			p.Reason = pictureReason
		}
	}
	p.Message = p.Reason
	if p.Message == "" {
		p.Message = fmt.Sprintf("%d 个窗口按%s排列，窗口至少 %d×%d", n, layoutModeLabel(p.Mode), w, h)
	}
	return p
}

func layoutModeLabel(mode string) string {
	for _, v := range []struct{ id, label string }{{"tile", "窗口并排"}, {"cascade:cols", "标题易切换"}, {"cascade:rows", "画面多露出"}} {
		if v.id == mode {
			return v.label
		}
	}
	return "窗口"
}

func CurrentLayoutPlan(req LayoutRequest) LayoutPlan {
	p := PlanLayout(req, PrimaryWorkAreaRect(), engineMinW, engineMinH)
	p.ScreenW, p.ScreenH = ScreenSize()
	return p
}

func planWindowRects(req LayoutRequest, wa windowRect) ([]windowRect, LayoutPlan) {
	p := PlanLayout(req, TargetRect{X: int(wa.left), Y: int(wa.top), W: int(wa.right - wa.left), H: int(wa.bottom - wa.top)}, engineMinW, engineMinH)
	out := make([]windowRect, len(p.Rects))
	for i, r := range p.Rects {
		out[i] = windowRect{left: int32(r.X), top: int32(r.Y), right: int32(r.X + r.W), bottom: int32(r.Y + r.H)}
	}
	return out, p
}
