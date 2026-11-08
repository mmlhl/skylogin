package mpay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 验证服务端要求切换游戏后，确认和轮询使用同一目标；授权码只从 login_info 读取。
func TestCompleteHostedQRCodeFlow(t *testing.T) {
	var calls []string
	payload := map[string]string{"user_id": "test-user", "token": "test-token", "uuid": "old", "game_id": "old"}
	original := map[string]string{}
	for k, v := range payload {
		original[k] = v
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		var out any
		switch r.URL.Path {
		case "/mpay/api/qrcode/scan":
			if r.URL.Query().Get("uuid") != "test-uuid" || r.URL.Query().Get("login_channel") != "netease" {
				t.Error("标记扫描未携带正确会话和渠道")
			}
			if len(calls) == 1 {
				out = map[string]any{"code": 1424, "game": map[string]any{"id": "target-game"}}
			} else {
				if r.URL.Query().Get("game_id") != "target-game" {
					t.Error("未按服务端目标重新扫描")
				}
				out = map[string]any{"code": 0}
			}
		case "/mpay/api/qrcode/confirm_login":
			_ = r.ParseForm()
			if r.PostForm.Get("uuid") != "test-uuid" || r.PostForm.Get("game_id") != "target-game" || r.PostForm.Get("token") != "test-token" {
				t.Error("确认未绑定正确的二维码或丢失凭证")
			}
			out = map[string]any{} // 官方确认成功可以没有 code 字段。
		case "/mpay/api/qrcode/query":
			if r.URL.Query().Get("game_id") != "target-game" {
				t.Error("轮询未沿用扫描目标")
			}
			out = map[string]any{"code": 0, "qrcode": map[string]any{"status": 2}, "login_info": map[string]any{"code": "authorization-code"}}
		default:
			t.Error("发生了未知请求")
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	c := New()
	c.Base, c.AppChannel = srv.URL, "netease"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	code, err := c.CompleteHostedQRCode(ctx, "test-uuid", "host-game", payload)
	if err != nil || code != "authorization-code" {
		t.Fatalf("授权结果 code=%q err=%v", code, err)
	}
	if !reflect.DeepEqual(payload, original) {
		t.Fatal("探针修改了调用方登录包")
	}
	if len(calls) != 4 {
		t.Fatalf("错误的请求时序: %v", calls)
	}
}

// 任一阶段拒绝、空响应或格式错误都必须失败，不能继续兑换槽位票。
func TestCompleteHostedQRCodeRejects(t *testing.T) {
	cases := []struct {
		name, stage, response, want string
		httpStatus                  int
	}{
		{"扫描被拒", "scan", `{"code":1306}`, "标记扫描", 200},
		{"无目标重定向", "scan", `{"code":1424}`, "1424", 200},
		{"确认被拒", "confirm_login", `{"code":"1429"}`, "确认授权", 200},
		{"确认非JSON", "confirm_login", `not json`, "JSON", 200},
		{"确认空对象之外的null", "confirm_login", `null`, "JSON", 200},
		{"HTTP拒绝", "confirm_login", `{"code":0}`, "HTTP=403", 403},
		{"确认业务码格式错", "confirm_login", `{"code":true}`, "确认授权", 200},
		{"轮询被拒", "query", `{"code":1306}`, "轮询授权", 200},
		{"成功状态却没授权码", "query", `{"qrcode":{"status":2},"code":0}`, "login_info.code", 200},
		{"授权码在错误位置", "query", `{"qrcode":{"status":2},"code":"not-authority"}`, "轮询授权", 200},
		{"二维码结束", "query", `{"qrcode":{"status":3}}`, "已结束", 200},
		{"轮询缺状态", "query", `{}`, "二维码状态", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/query") {
					queries++
				}
				if strings.HasSuffix(r.URL.Path, "/"+tc.stage) {
					w.WriteHeader(tc.httpStatus)
					_, _ = w.Write([]byte(tc.response))
				} else {
					_, _ = w.Write([]byte(`{"code":0}`))
				}
			}))
			defer srv.Close()
			c := New()
			c.Base = srv.URL
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			code, err := c.CompleteHostedQRCode(ctx, "uuid", "game", nil)
			if code != "" || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("应失败且包含 %s: code=%q err=%v", tc.want, code, err)
			}
			if tc.stage != "query" && queries > 0 {
				t.Fatal("前序失败后仍然继续轮询")
			}
		})
	}
}

func TestCompleteHostedQRCodeTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/query") {
			_, _ = w.Write([]byte(`{"code":-1,"qrcode":{"status":1}}`))
		} else {
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	c := New()
	c.Base = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := c.CompleteHostedQRCode(ctx, "uuid", "game", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应在等待确认时超时: %v", err)
	}
	if _, err := c.CompleteHostedQRCode(context.Background(), "uuid", "game", nil); err == nil {
		t.Fatal("无限时的实验请求不应被接受")
	}
}
