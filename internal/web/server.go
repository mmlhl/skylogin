package web

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/skip2/go-qrcode"

	"skylogin/internal/huawei"
)

// 前端构建产物（web/ 目录用 Vite 构建到这里）。
//
// 改界面：cd web && npm run build，再 go build 就是最新界面。
//
//go:embed dist
var assets embed.FS

// LoginState 是扫码登录的当前状态。
type LoginState struct {
	Status   string `json:"status"` // idle|waiting|scanned|ok|error
	Message  string `json:"message"`
	QR       string `json:"qr"` // data:image/png;base64,...
	Nick     string `json:"nick"`
	Player   string `json:"player"`
	ExpireAt int64  `json:"expireAt"` // 二维码过期时间（unix 毫秒）
	Attempt  int    `json:"attempt"`
	Steps    []int  `json:"steps"` // 1=等待扫码 2=已扫码 3=完成
}

// Hooks 是业务回调集合，由 app 层注入，避免 web 包反向依赖。
type Hooks struct {
	OnLogin         func(*huawei.Client, *huawei.ScanCode, *huawei.ScanReply) (string, string, error)
	Accounts        func() []map[string]any
	Start           func() (string, error)
	StartMany       func(ids []string) (string, error)
	StartManyLayout func(ids []string, cols, rows, w, h int, display string) (string, error)
	Arrange         func(cols, rows, w, h int) error
	WallToggle      func() (string, error)
	StopAccount     func(id string) error
	Status          func() string
	Select          func(string) error
	Rename          func(string, string) error
	SetGameID       func(string, string) error
	Delete          func(string) error
	Settings        func() map[string]any
	SaveSettings    func(skyExe, deviceID, mirrorDir *string, closeLauncher *bool, multiQuality *string, multiFps *int, mouseArbiter *bool, arbiterMin *int) error
	Cleanup         func(accounts, cache, logs bool) ([]string, error)
	OpenLogs        func() error
	PickPath        func() (string, error)
	PickFolder      func() (string, error)
	ScanStart       func() (map[string]any, error)
	ScanStop        func()
	ScanNow         func() map[string]any
	// 渠道网页登录（B站 / 4399 / vivo 共用一套，key 区分）
	ChannelLoginStart  func(key, targetUserID string) error
	ChannelLoginStatus func(key string) map[string]any
	AuthHint           func() map[string]any
	Version            func() string
	Reregister         func(mode string) (string, error)
	SetDeviceID        func(id string) error
	SMSSend            func(mobile string) error
	SMSLogin           func(mobile, code, targetUserID string) error
	WebLoginStart      func(targetUserID string) error
	WebLoginStatus     func() map[string]any
	MpayLoginStart     func(targetUserID string) error
	MpayLoginStatus    func() map[string]any
	MpayLoginCancel    func()
	SaveRailWidth      func(width int) error
	SaveShell          func(shell int) error
	LoadShell          func() int
	MouseState         func() map[string]any // 诊断：多开时鼠标占用与仲裁状态
}

// Server 是对外的本地 Web 服务。
type Server struct {
	mu      sync.Mutex
	state   LoginState
	session *huawei.Client
	cancel  chan struct{}
	hooks   Hooks
}

func New(h Hooks) *Server {
	return &Server{
		hooks: h,
		state: LoginState{Status: "idle", Steps: []int{}},
	}
}

func (s *Server) getState() LoginState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Server) setState(st LoginState) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
}

// Handler 返回 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 静态资源：全部来自 web/ 用 Vite 构建出来的 dist 目录
	mux.Handle("/", staticHandler())

	// 一次性拿全量状态：账号、设置、登录态、游戏状态
	// 浏览器会自动请求这两个图标，回 204 免得日志里一堆 404
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/apple-touch-icon.png", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"login":    s.getState(),
			"game":     safeStatus(s.hooks.Status),
			"accounts": safeAccounts(s.hooks.Accounts),
			"settings": safeSettings(s.hooks.Settings),
			"scan":     safeScan(s.hooks.ScanNow),

			"authHint": safeMap(s.hooks.AuthHint),
			"version":  safeStatus(s.hooks.Version),
		})
	})

	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		st := s.getState()
		if st.Status == "waiting" || st.Status == "scanned" {
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		s.mu.Lock()
		s.cancel = make(chan struct{})
		cancel := s.cancel
		s.mu.Unlock()
		s.setState(LoginState{Status: "waiting", Message: "正在申请二维码…", Steps: []int{1}})
		go s.runLogin(cancel)
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/login/cancel", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if s.cancel != nil {
			close(s.cancel)
			s.cancel = nil
		}
		s.mu.Unlock()
		s.setState(LoginState{Status: "idle", Message: "已取消"})
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/launch", func(w http.ResponseWriter, r *http.Request) {
		msg, err := s.hooks.Start()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "message": msg})
	})

	// 多开：一次启动多个客户端，每个账号一个。
	// 带 cols/rows 时，窗口就绪后由后端自动按网格排列（正方形网格，保持画面比例）；
	// w/h 是自定义窗口尺寸（0 = 铺满格子）。
	mux.HandleFunc("/api/launch-multi", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs     []string `json:"ids"`
			Cols    int      `json:"cols"`
			Rows    int      `json:"rows"`
			W       int      `json:"w"`
			H       int      `json:"h"`
			Display string   `json:"display"` // 空 / "tile" = 窗口平铺；"cascade" = 窗口堆叠；"fullscreen" = 全屏
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body)
		var msg string
		var err error
		if body.Display != "" || (body.Cols >= 1 && body.Rows >= 1) {
			if s.hooks.StartManyLayout != nil {
				msg, err = s.hooks.StartManyLayout(body.IDs, body.Cols, body.Rows, body.W, body.H, body.Display)
			} else {
				msg, err = s.hooks.StartMany(body.IDs)
			}
		} else {
			msg, err = s.hooks.StartMany(body.IDs)
		}
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "message": msg})
	})

	// 手动排列：把当前正在运行的客户端窗口按 cols×rows 网格铺开
	mux.HandleFunc("/api/arrange", func(w http.ResponseWriter, r *http.Request) {
		if s.hooks.Arrange == nil {
			writeJSON(w, map[string]any{"ok": false, "message": "不支持窗口排列"})
			return
		}
		var body struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
			W    int `json:"w"`
			H    int `json:"h"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body)
		if err := s.hooks.Arrange(body.Cols, body.Rows, body.W, body.H); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// 监控墙：把正在运行客户端的实时画面缩进格子里，点格子切到前台
	mux.HandleFunc("/api/wall", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, map[string]any{"ok": false, "message": "用 POST 开/关监控墙"})
			return
		}
		if s.hooks.WallToggle == nil {
			writeJSON(w, map[string]any{"ok": false, "message": "不支持监控墙"})
			return
		}
		msg, err := s.hooks.WallToggle()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "message": msg})
	})

	// 界面壳偏好（控制台 / 账号库…）：WebView2 端口随机，localStorage 记不住，存后端
	mux.HandleFunc("/api/ui/shell", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			shell := 0
			if s.hooks.LoadShell != nil {
				shell = s.hooks.LoadShell()
			}
			writeJSON(w, map[string]any{"ok": true, "shell": shell})
			return
		}
		if s.hooks.SaveShell == nil {
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		var body struct {
			Shell int `json:"shell"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body)
		if err := s.hooks.SaveShell(body.Shell); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// 手机号验证码登录：纯 HTTP，不需要浏览器
	mux.HandleFunc("/api/sms/send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mobile string `json:"mobile"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body)
		if err := s.hooks.SMSSend(body.Mobile); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/sms/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mobile       string `json:"mobile"`
			Code         string `json:"code"`
			TargetUserID string `json:"targetUserId"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body)
		if err := s.hooks.SMSLogin(body.Mobile, body.Code, body.TargetUserID); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// 网页登录：打开一个独立窗口，用户在网易页面里登录，我们把 token 存下来
	mux.HandleFunc("/api/web-login/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TargetUserID string `json:"targetUserId"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body)
		if err := s.hooks.WebLoginStart(body.TargetUserID); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/web-login/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.hooks.WebLoginStatus())
	})

	// 账号密码登录：交给官方 mpay.dll 弹它自己的登录窗（不走网页、不走代理）
	mux.HandleFunc("/api/mpay-login/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TargetUserID string `json:"targetUserId"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body)
		if err := s.hooks.MpayLoginStart(body.TargetUserID); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/mpay-login/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.hooks.MpayLoginStatus())
	})

	mux.HandleFunc("/api/mpay-login/cancel", func(w http.ResponseWriter, r *http.Request) {
		s.hooks.MpayLoginCancel()
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/accounts/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := s.hooks.StopAccount(r.URL.Query().Get("id")); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/accounts/select", func(w http.ResponseWriter, r *http.Request) {
		if err := s.hooks.Select(r.URL.Query().Get("id")); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/accounts/rename", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := s.hooks.Rename(q.Get("id"), q.Get("name")); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/accounts/delete", func(w http.ResponseWriter, r *http.Request) {
		if err := s.hooks.Delete(r.URL.Query().Get("id")); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, s.hooks.Settings())
			return
		}
		var body struct {
			SkyExe           *string `json:"skyExe"`
			DeviceID         *string `json:"deviceId"`
			MirrorDir        *string `json:"mirrorDir"`
			CloseLauncher    *bool   `json:"closeLauncher"`
			MultiQualityMode *string `json:"multiQualityMode"`
			MultiQualityFps  *int    `json:"multiQualityFps"`
			MouseArbiter     *bool   `json:"mouseArbiter"`
			MouseArbiterMin  *int    `json:"mouseArbiterMin"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": "请求格式有误"})
			return
		}
		if err := s.hooks.SaveSettings(body.SkyExe, body.DeviceID, body.MirrorDir, body.CloseLauncher, body.MultiQualityMode, body.MultiQualityFps, body.MouseArbiter, body.MouseArbiterMin); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "settings": s.hooks.Settings()})
	})

	// 左栏宽度：用户拖完存到本地配置（WebView2 每次端口都是随机的，
	// localStorage 按端口隔离，靠它记不住）
	mux.HandleFunc("/api/ui/rail-width", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, map[string]any{"ok": false, "message": "只支持 POST"})
			return
		}
		var body struct {
			Width int `json:"width"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body)
		if err := s.hooks.SaveRailWidth(body.Width); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/cleanup", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Accounts bool `json:"accounts"`
			Cache    bool `json:"cache"`
			Logs     bool `json:"logs"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": "请求格式有误"})
			return
		}
		done, err := s.hooks.Cleanup(body.Accounts, body.Cache, body.Logs)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error(), "done": done})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "done": done})
	})

	// 渠道网页登录（B站 / 4399 / vivo）：子进程打开官方登录页，
	// 捕获凭证后在线验证并存账号。路径保留各渠道原入口。
	channelKeys := []string{"bili", "m4399", "vivo"}
	for _, ck := range channelKeys {
		key := ck
		mux.HandleFunc("/api/"+key+"/login/start", func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				TargetUserID string
			}
			_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body)
			if err := s.hooks.ChannelLoginStart(key, body.TargetUserID); err != nil {
				writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
				return
			}
			writeJSON(w, map[string]any{"ok": true})
		})
		mux.HandleFunc("/api/"+key+"/login/status", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, s.hooks.ChannelLoginStatus(key))
		})
	}

	mux.HandleFunc("/api/scan/start", func(w http.ResponseWriter, r *http.Request) {
		st, err := s.hooks.ScanStart()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error(), "scan": st})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "scan": st})
	})

	mux.HandleFunc("/api/scan/stop", func(w http.ResponseWriter, r *http.Request) {
		s.hooks.ScanStop()
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/device/register", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode string `json:"mode"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&body)
		id, err := s.hooks.Reregister(body.Mode)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "deviceId": id, "settings": s.hooks.Settings()})
	})

	mux.HandleFunc("/api/device/custom", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if err := s.hooks.SetDeviceID(id); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "settings": s.hooks.Settings()})
	})

	mux.HandleFunc("/api/settings/pick-path", func(w http.ResponseWriter, r *http.Request) {
		if s.hooks.PickPath == nil {
			writeJSON(w, map[string]any{"ok": false, "message": "不支持选择文件"})
			return
		}
		path, err := s.hooks.PickPath()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "path": path})
	})

	// 选一个文件夹（多开镜像目录用）
	mux.HandleFunc("/api/settings/pick-folder", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.hooks.PickFolder()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "path": p})
	})

	mux.HandleFunc("/api/open-logs", func(w http.ResponseWriter, r *http.Request) {
		if err := s.hooks.OpenLogs(); err != nil {
			writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	return mux
}

func (s *Server) runLogin(cancel chan struct{}) {
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		sess := huawei.NewClient()
		info, err := sess.GetScanCode()
		if err != nil {
			s.setState(LoginState{Status: "error", Message: "申请二维码失败: " + err.Error(), Steps: []int{}})
			return
		}
		png, err := qrPNG(info.Content)
		if err != nil {
			s.setState(LoginState{Status: "error", Message: "生成二维码失败: " + err.Error(), Steps: []int{}})
			return
		}
		msg := "用华为手机扫一扫"
		if attempt > 1 {
			msg = fmt.Sprintf("已换一张二维码（第 %d 张），请重新扫描", attempt)
		}
		exp := int64(info.ExpiredTime)
		if exp > 0 && exp < 1e12 { // 秒 → 毫秒
			exp *= 1000
		}
		s.setState(LoginState{
			Status:   "waiting",
			Message:  msg,
			QR:       "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
			ExpireAt: exp,
			Attempt:  attempt,
			Steps:    []int{1},
		})

		scan, err := sess.WaitForScan(info, nil, cancel)
		if err != nil {
			if errors.Is(err, huawei.ErrExpired) && attempt < maxAttempts {
				s.setState(LoginState{Status: "waiting", Message: "二维码过期了，正在换一张…", Attempt: attempt, Steps: []int{1}})
				continue
			}
			if errors.Is(err, huawei.ErrCanceled) {
				s.setState(LoginState{Status: "idle", Message: "已取消", Steps: []int{}})
				return
			}
			s.setState(LoginState{Status: "error", Message: err.Error(), Steps: []int{}})
			return
		}
		s.setState(LoginState{Status: "scanned", Message: "已扫码，正在换取凭证…", Attempt: attempt, Steps: []int{1, 2}})

		nick, player, err := s.hooks.OnLogin(sess, info, scan)
		if err != nil {
			s.setState(LoginState{Status: "error", Message: err.Error(), Steps: []int{1, 2}})
			return
		}
		s.setState(LoginState{Status: "ok", Message: "登录成功", Nick: nick, Player: player, Steps: []int{1, 2, 3}})
		return
	}
}

// Serve 在随机空闲端口上启动服务，返回访问地址。
func (s *Server) Serve() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	go func() { _ = http.Serve(ln, s.Handler()) }()
	return "http://" + ln.Addr().String() + "/", nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func qrPNG(content string) ([]byte, error) {
	return qrcode.Encode(content, qrcode.Medium, 480)
}

// 下面这些包装让缺省回调不至于把整个状态接口打挂。
func safeMap(f func() map[string]any) map[string]any {
	if f == nil {
		return map[string]any{}
	}
	return f()
}

func safeStatus(f func() string) string {
	if f == nil {
		return "未知"
	}
	return f()
}

func safeAccounts(f func() []map[string]any) []map[string]any {
	if f == nil {
		return []map[string]any{}
	}
	return f()
}

func safeSettings(f func() map[string]any) map[string]any {
	if f == nil {
		return map[string]any{}
	}
	return f()
}

func safeScan(f func() map[string]any) map[string]any {
	if f == nil {
		return map[string]any{"status": "idle"}
	}
	return f()
}

// staticHandler 提供前端构建产物。
//
// 规则：
//   - 路径能对上文件就返回文件
//   - 对不上就回 index.html（单页应用的前端路由兜底）
func staticHandler() http.Handler {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		if _, statErr := fs.Stat(sub, name); statErr != nil {
			// 单页应用兜底：未知路径一律给首页
			name = "index.html"
		}
		if name == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			b, readErr := fs.ReadFile(sub, "index.html")
			if readErr != nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(b)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + name
		files.ServeHTTP(w, r2)
	})
}
