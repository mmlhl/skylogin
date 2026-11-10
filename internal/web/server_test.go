package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer 用一组假回调建一个服务，便于在无界面环境里测试路由。
func newTestServer() *Server {
	return New(Hooks{
		Accounts: func() []map[string]any {
			return []map[string]any{{"id": "1", "name": "示例玩家", "channelLabel": "华为渠道", "masked": "1181****5885"}}
		},
		Start:    func() (string, error) { return "已启动", nil },
		Status:   func() string { return "未启动" },
		Select:   func(string) error { return nil },
		Rename:   func(string, string) error { return nil },
		Delete:   func(string) error { return nil },
		Settings: func() map[string]any { return map[string]any{"version": "test"} },
		SaveSettings: func(skyExe, deviceID, mirrorDir *string, closeLauncher *bool, multiQuality *string, multiFps *int, mouseArbiter *bool, arbiterMin *int) error {
			return nil
		},
		Cleanup:     func(accounts, cache, logs bool) ([]string, error) { return []string{"x"}, nil },
		OpenLogs:    func() error { return nil },
		PickPath:    func() (string, error) { return "", nil },
		PickFolder:  func() (string, error) { return "", nil },
		Reregister:  func(mode string) (string, error) { return mode + "-id", nil },
		SetDeviceID: func(id string) error { return nil },
	})
}

func TestStaticAssets(t *testing.T) {
	srv := httptest.NewServer(newTestServer().Handler())
	defer srv.Close()

	// 前端现在是 Vite 构建产物：首页 + assets 目录里的 js/css。
	// 这里只校验"首页能拿到"和"单页应用兜底"两件事。
	cases := []struct{ path, ctype string }{
		{"/", "text/html"},
		{"/some/unknown/path", "text/html"},
	}
	for _, c := range cases {
		resp, err := http.Get(srv.URL + c.path)
		if err != nil {
			t.Fatalf("请求 %s 失败: %v", c.path, err)
		}
		body := make([]byte, 0)
		buf := make([]byte, 4096)
		for {
			n, rerr := resp.Body.Read(buf)
			body = append(body, buf[:n]...)
			if rerr != nil {
				break
			}
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s 状态码 = %d", c.path, resp.StatusCode)
		}
		if !strings.Contains(resp.Header.Get("Content-Type"), c.ctype) {
			t.Errorf("%s Content-Type = %q，期望包含 %s", c.path, resp.Header.Get("Content-Type"), c.ctype)
		}
		if len(body) < 200 {
			t.Errorf("%s 内容过短（%d 字节）", c.path, len(body))
		}
	}
}

func TestStateEndpoint(t *testing.T) {
	srv := httptest.NewServer(newTestServer().Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out struct {
		Login    map[string]any   `json:"login"`
		Game     string           `json:"game"`
		Accounts []map[string]any `json:"accounts"`
		Settings map[string]any   `json:"settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析 state 失败: %v", err)
	}
	if out.Game != "未启动" {
		t.Errorf("game = %q", out.Game)
	}
	if len(out.Accounts) != 1 {
		t.Errorf("accounts 数量 = %d", len(out.Accounts))
	}
	if out.Settings["version"] != "test" {
		t.Errorf("settings.version = %v", out.Settings["version"])
	}
	if out.Login["status"] != "idle" {
		t.Errorf("login.status = %v", out.Login["status"])
	}
}

func TestLaunchEndpoint(t *testing.T) {
	srv := httptest.NewServer(newTestServer().Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/launch", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Errorf("launch ok = %v", out["ok"])
	}
}

func TestDeviceRegisterEndpoint(t *testing.T) {
	srv := httptest.NewServer(newTestServer().Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/device/register", "application/json",
		strings.NewReader(`{"mode":"random"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Errorf("ok = %v", out["ok"])
	}
	if out["deviceId"] != "random-id" {
		t.Errorf("deviceId = %v", out["deviceId"])
	}
}

func TestDeviceCustomEndpoint(t *testing.T) {
	srv := httptest.NewServer(newTestServer().Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/device/custom?id=abc-d", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Errorf("ok = %v", out["ok"])
	}
}

// TestGameSettingsRoute 守住"/api/accounts/gamesettings"这条路由。
//
// 之前前端调 /api/accounts/quality，后端压根没注册路由，请求落到单页兜底
// 返回 HTML，前端解析失败又没人 catch —— 用户看到的就是"设了没反应、
// 关掉窗口再打开又变回去"。这条测试就是防止再出现"前端在调、后端没这回事"。
func TestGameSettingsRoute(t *testing.T) {
	var gotID, gotMode, gotDisplay string
	var gotFps int
	called := false
	srv := New(Hooks{
		GameSettings: func(id, mode string, fps int, display string) error {
			called = true
			gotID, gotMode, gotFps, gotDisplay = id, mode, fps, display
			return nil
		},
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/accounts/gamesettings", "application/json",
		strings.NewReader(`{"id":"a1","mode":"Presentation","fps":120,"display":"windowed"}`))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("响应不是 JSON（说明路由没注册，落到了单页兜底）: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("保存失败: %v", out["message"])
	}
	if !called || gotID != "a1" || gotMode != "Presentation" || gotFps != 120 || gotDisplay != "windowed" {
		t.Fatalf("回调参数不对: id=%q mode=%q fps=%d display=%q", gotID, gotMode, gotFps, gotDisplay)
	}
}
