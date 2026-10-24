package mpay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 用本地假服务器验证 create_login → scan → confirm → query 的完整时序。
func TestLoginFlow(t *testing.T) {
	var mu sync.Mutex
	queries := 0
	scanned := false
	confirmed := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/mpay/api/qrcode/create_login":
			if r.URL.Query().Get("game_id") == "" {
				t.Errorf("create_login 缺少 game_id")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"uuid": "uuid-1", "qrcode": map[string]any{}})
		case "/mpay/api/qrcode/scan":
			if r.URL.Query().Get("uuid") != "uuid-1" {
				t.Errorf("scan uuid=%q", r.URL.Query().Get("uuid"))
			}
			scanned = true
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		case "/mpay/api/qrcode/confirm_login":
			_ = r.ParseForm()
			if r.Form.Get("uuid") != "uuid-1" || r.Form.Get("user_id") != "u9" {
				t.Errorf("confirm 表单不对: %v", r.PostForm)
			}
			confirmed = true
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		case "/mpay/api/qrcode/query":
			queries++
			if queries < 2 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": -1, "qrcode": map[string]any{"status": 1}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":       0,
				"qrcode":     map[string]any{"status": 2},
				"login_info": map[string]any{"code": "TICKET-9"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New()
	c.Base = srv.URL
	ticket, _, err := c.Login("aecfs5n2y4aaaakd-g-ma75", map[string]string{
		"user_id": "u9",
		"token":   "tok",
	})
	if err != nil {
		t.Fatalf("Login 失败: %v", err)
	}
	if ticket != "TICKET-9" {
		t.Fatalf("ticket=%q", ticket)
	}
	if !scanned || !confirmed {
		t.Fatalf("scan=%v confirm=%v", scanned, confirmed)
	}
}

func TestLoginConfirmRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mpay/api/qrcode/create_login":
			_ = json.NewEncoder(w).Encode(map[string]any{"uuid": "u"})
		case "/mpay/api/qrcode/scan":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		case "/mpay/api/qrcode/confirm_login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 401, "reason": "bad credentials"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New()
	c.Base = srv.URL
	_, _, err := c.Login("g", map[string]string{"user_id": "u"})
	if err == nil {
		t.Fatal("confirm 被拒绝时应当报错")
	}
	if !strings.Contains(err.Error(), "confirm") {
		t.Fatalf("错误信息应指明 confirm: %v", err)
	}
}
