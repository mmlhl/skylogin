package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGroupSaveRouteKeepsPhysicalCoordinateBasis(t *testing.T) {
	var got GroupPayload
	srv := New(Hooks{SaveGroup: func(p GroupPayload) (string, error) { got = p; return "g1", nil }, Groups: func() []map[string]any { return []map[string]any{{"id": "g1", "screenW": 1920, "screenH": 1080}} }})
	req := httptest.NewRequest(http.MethodPost, "/api/groups/save", strings.NewReader(`{"name":"双开","screenW":1920,"screenH":1080,"slots":[{"accountKey":"a","hasRect":true,"w":800,"h":600}]}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var out struct {
		OK     bool             `json:"ok"`
		ID     string           `json:"id"`
		Groups []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.ID != "g1" || len(out.Groups) != 1 || got.ScreenW != 1920 || got.ScreenH != 1080 || len(got.Slots) != 1 || got.Slots[0].W != 800 {
		t.Fatalf("保存必须透传基准并返回规范化组：got=%+v out=%+v", got, out)
	}
}

func TestClientRectsRouteIncludesScreenWithoutClients(t *testing.T) {
	srv := New(Hooks{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/clients/rects", nil))
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if string(out["ok"]) != "true" || string(out["rects"]) != "[]" {
		t.Fatalf("无窗口仍应返回成功的空清单：%s", rec.Body.String())
	}
	var workArea map[string]int
	if err := json.Unmarshal(out["workArea"], &workArea); err != nil {
		t.Fatalf("缺少物理工作区：%s", rec.Body.String())
	}
	for _, key := range []string{"x", "y", "w", "h"} {
		if _, ok := workArea[key]; !ok {
			t.Fatalf("工作区缺少 %s：%s", key, rec.Body.String())
		}
	}
	if workArea["w"] <= 0 || workArea["h"] <= 0 {
		t.Fatalf("工作区必须可用于窗口分格：%s", rec.Body.String())
	}
	for _, key := range []string{"screenW", "screenH"} {
		var size int
		if b, ok := out[key]; !ok {
			t.Fatalf("缺少物理屏幕基准 %s", key)
		} else if err := json.Unmarshal(b, &size); err != nil || size < 0 {
			t.Fatalf("屏幕基准应为非负整数：%s", rec.Body.String())
		}
	}
}
