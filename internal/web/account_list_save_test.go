package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAccountListSaveRoutesReturnOnlyNormalizedFields(t *testing.T) {
	accounts := []map[string]any{{"id": "a", "tags": []string{"VIP"}, "password": "不应返回", "accountId": "不应返回"}, {"id": "b", "tags": []string{}}}
	srv := New(Hooks{
		Accounts:    func() []map[string]any { return accounts },
		SetTags:     func(id string, tags []string) error { return nil },
		Reorder:     func(ids []string) error { accounts = []map[string]any{accounts[1], accounts[0]}; return nil },
		SetTagOrder: func(tags []string) ([]string, error) { return []string{"VIP", "未分组"}, nil },
	})
	call := func(path, body string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out["ok"] != true {
			t.Fatalf("保存失败：%v", out)
		}
		if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "accountId") {
			t.Fatalf("保存接口泄漏非必要账号字段：%s", rec.Body.String())
		}
		return out
	}
	tags := call("/api/accounts/tags", `{"id":"a","tags":[" VIP ","vip"]}`)
	if !reflect.DeepEqual(tags["account"], map[string]any{"id": "a", "tags": []any{"VIP"}}) {
		t.Fatalf("应返回服务端结果：%v", tags)
	}
	order := call("/api/accounts/reorder", `{"ids":["b","b","不存在"]}`)
	if !reflect.DeepEqual(order["ids"], []any{"b", "a"}) {
		t.Fatalf("应返回规范后的完整顺序：%v", order)
	}
	groups := call("/api/accounts/tag-order", `{"tags":[" VIP ","未分组"]}`)
	if !reflect.DeepEqual(groups["tagOrder"], []any{"VIP", "未分组"}) {
		t.Fatalf("应返回标签组顺序：%v", groups)
	}
}

func TestAccountListSaveRouteFailuresDoNotPretendSuccess(t *testing.T) {
	calls := 0
	srv := New(Hooks{SetTags: func(string, []string) error { calls++; return errors.New("保存失败") }, Reorder: func([]string) error { calls++; return errors.New("排序失败") }, SetTagOrder: func([]string) ([]string, error) { calls++; return nil, errors.New("分组失败") }})
	for _, path := range []string{"/api/accounts/tags", "/api/accounts/reorder", "/api/accounts/tag-order"} {
		for _, body := range []string{`{`, `{}`} {
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
			var out map[string]any
			json.Unmarshal(rec.Body.Bytes(), &out)
			if out["ok"] != false {
				t.Fatalf("失败不应报成功：%s", rec.Body.String())
			}
		}
	}
	if calls != 3 {
		t.Fatalf("坏JSON不能触发保存：calls=%d", calls)
	}
}
