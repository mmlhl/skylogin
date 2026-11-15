package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"skylogin/internal/win"
	"strings"
	"testing"
)

func TestLayoutPlanRouteReturnsPhysicalPlanAndFallback(t *testing.T) {
	var request win.LayoutRequest
	srv := New(Hooks{LayoutPlan: func(req win.LayoutRequest) win.LayoutPlan {
		request = req
		p := win.PlanLayout(req, win.TargetRect{W: 2560, H: 1390}, 1024, 720)
		p.ScreenW, p.ScreenH = 2560, 1440
		return p
	}})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/layout/plan", strings.NewReader(`{"count":7,"mode":"tile","cols":3,"rows":3}`)))
	var out struct {
		OK bool `json:"ok"`
		win.LayoutPlan
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Mode != "cascade:cols" || out.Feasible || len(out.Rects) != 7 || out.MinW != 1024 || out.ScreenW != 2560 || out.WorkArea.H != 1390 || out.Reason == "" || request.Count != 7 {
		t.Fatalf("前端必须取得实际同源fallback：%s", rec.Body.String())
	}
	if out.Rects[0].ZOrder != 1 || len(out.Options) != 4 {
		t.Fatal("缺少层级或方案选项")
	}
}

func TestLayoutPlanRouteRejectsBadPayload(t *testing.T) {
	called := false
	srv := New(Hooks{LayoutPlan: func(win.LayoutRequest) win.LayoutPlan { called = true; return win.LayoutPlan{} }})
	for _, body := range []string{`bad`, `{"count":-1}`, `{"count":257}`} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/layout/plan", strings.NewReader(body)))
		if called || !strings.Contains(rec.Body.String(), `"ok":false`) {
			t.Fatalf("非法请求被规划：%s", rec.Body.String())
		}
	}
}
