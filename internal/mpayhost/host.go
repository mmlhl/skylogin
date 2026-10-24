// Package mpayhost 在独立进程里托管官方 mpay.dll，弹出网易自己的登录窗。
//
// 为什么要单独一个进程：
//
//	mpay.dll 是官方 C++ SDK，内部会起 WebView2、拉消息循环，出错时还会直接
//	访问违例。把它放进主界面进程，一次崩溃就连界面一起带走。
//	所以主进程只负责"起子进程 + 收结果"，和 --weblogin 是同一套路。
//
// 为什么账号密码要交给 DLL：
//
//	MPay 账号密码接口里的 params 是原生 SDK 加密的（密钥在 DLL 里），
//	纯 Go 复现不了。让官方 DLL 自己弹窗登录，拿到票后再交给现有的
//	login/ticket 链路，最稳。
//
// 约束：不装证书、不改 hosts、不开代理。这里只加载 DLL 和开一个窗口。
package mpayhost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Options 是托管登录需要的输入。
type Options struct {
	SkyDir  string        // mpay.dll 所在目录（游戏安装目录）
	SkinZip string        // 官方皮肤包，来自发烧平台安装目录
	GameID  string        // 长 game id，例如 aecglf6ee4aaaarz-g-a50
	Channel string        // app_channel，例如 a50_sdk_cn
	Timeout time.Duration // 等用户登录的最长时间
	LogPath string        // 诊断日志（子进程没有控制台）

	// 登录窗以宿主窗口为中心弹出，所以把宿主放到主界面的中心
	CenterX int
	CenterY int
}

// Result 是登录结果，由子进程写给主进程。
type Result struct {
	OK       bool     `json:"ok"`
	Ticket   string   `json:"ticket"`
	DeviceID string   `json:"deviceId"`
	Account  string   `json:"account"`
	Slots    []string `json:"slots,omitempty"`
	Error    string   `json:"error"`
	Finished int64    `json:"finished"`
}

const (
	// 官方 ApiCallBack 实测 33 个槽位；多给一些，表尾空指针会直接把进程打崩。
	vtableSlots = 40

	// 回调槽位（按 NtUniSdkMpay.dll 的虚表排布，末尾的几个槽官方 ApiCallBack 没用到，填空函数占位）
	slotLogin  = 1 // 登录完成 (this, code, structUserResponseInfo*)
	slotTicket = 5 // 取票结果 (this, code, ticket, ?)

	wsPopup        = 0x80000000
	wsVisible      = 0x10000000
	wsExToolWindow = 0x00000080
	wsExLayered    = 0x00080000
	wsExNoActivate = 0x08000000

	// 登录成功后等 DLL 出票的时间
	ticketWait = 8 * time.Second
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")

	procGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	procSetDllDirectoryW           = kernel32.NewProc("SetDllDirectoryW")
	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procUpdateWindow               = user32.NewProc("UpdateWindow")
	procPeekMessageW               = user32.NewProc("PeekMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procEnumWindows                = user32.NewProc("EnumWindows")
	procGetClassNameW              = user32.NewProc("GetClassNameW")
	procIsWindowVisible            = user32.NewProc("IsWindowVisible")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	procGetCurrentProcess          = kernel32.NewProc("GetCurrentProcess")
	procGetCurrentProcessId        = kernel32.NewProc("GetCurrentProcessId")
	procTerminateProcess           = kernel32.NewProc("TerminateProcess")
	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	procSwitchToThisWindow         = user32.NewProc("SwitchToThisWindow")
	procCoInitializeEx             = ole32.NewProc("CoInitializeEx")
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// 这些必须是包级变量：DLL 会长期持有它们的地址，Go 的 GC 也不会移动它们。
var (
	vtable   [vtableSlots]uintptr
	cbObject [520]uintptr

	mu       sync.Mutex
	logFile  *os.File
	slots    []string // 最近一次登录回调里拿到的信息
	sendOnce sync.Once
	decided  = make(chan Result, 1)
	loginOK  bool // 登录回调成功了（取票可能还在路上）

	dllHandle *syscall.DLL
	hostHwnd  uintptr
)

// Login 阻塞在登录流程上：弹窗 → 等用户登录 → 拿到票。
func Login(opts Options) Result {
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.Channel == "" {
		opts.Channel = "a50_sdk_cn"
	}
	openLog(opts.LogPath)
	defer closeLog()

	// 窗口、消息循环、DLL 必须在同一个 OS 线程上
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := os.Chdir(opts.SkyDir); err != nil {
		return fail("进入游戏目录失败: " + err.Error())
	}
	if _, err := os.Stat(filepath.Join(opts.SkyDir, "mpay.dll")); err != nil {
		return fail("游戏目录里没有 mpay.dll，请确认光遇已正确安装")
	}
	// mpay.dll 依赖同目录的一堆 DLL（webview_support_helper 等）
	os.Setenv("PATH", opts.SkyDir+`;`+os.Getenv("PATH"))
	if p, err := syscall.UTF16PtrFromString(opts.SkyDir); err == nil {
		procSetDllDirectoryW.Call(uintptr(unsafe.Pointer(p)))
	}
	procCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED

	cx, cy := opts.CenterX, opts.CenterY
	if cx <= 0 || cy <= 0 {
		cx, cy = screenCenter()
	}
	hwnd := createHostWindow(cx, cy)
	if hwnd == 0 {
		return fail("创建宿主窗口失败")
	}
	logf("宿主窗口: 0x%x（中心 %d,%d）", hwnd, cx, cy)

	dll, err := syscall.LoadDLL(filepath.Join(opts.SkyDir, "mpay.dll"))
	if err != nil {
		return fail("加载 mpay.dll 失败: " + err.Error())
	}
	dllHandle = dll

	create, err := dll.FindProc("?Create_Interface@CMpay_Interface@Mpay@@SAPEAV12@XZ")
	if err != nil {
		return fail("这个 mpay.dll 没有 Create_Interface，可能版本不兼容")
	}
	inst, _, _ := create.Call()
	if inst == 0 {
		return fail("创建 MPay 接口失败")
	}

	installLogHook(dll, inst)
	setParentWindow(dll, hwnd)
	setSkin(dll, inst, opts.SkinZip)

	buildCallbacks()

	game, _ := syscall.BytePtrFromString(opts.GameID)
	ch, _ := syscall.BytePtrFromString(opts.Channel)
	empty, _ := syscall.BytePtrFromString("")

	initProc, err := dll.FindProc("?init@CMpay_Interface@Mpay@@QEAAHPEBD0000PEAVApiCallBack@2@H@Z")
	if err != nil {
		return fail("这个 mpay.dll 没有 init，可能版本不兼容")
	}
	r, _, _ := initProc.Call(
		inst,
		uintptr(unsafe.Pointer(game)),
		uintptr(unsafe.Pointer(empty)),
		uintptr(unsafe.Pointer(ch)),
		uintptr(unsafe.Pointer(empty)),
		uintptr(unsafe.Pointer(empty)),
		uintptr(unsafe.Pointer(&cbObject[0])),
		0,
	)
	logf("init -> %d", r)
	if r == 0 {
		return fail("MPay 初始化失败，请确认游戏能正常被发烧平台启动")
	}

	// login(1, 0)：1 = 显示登录窗，0 = 默认皮肤
	loginProc, err := dll.FindProc("?login@CMpay_Interface@Mpay@@QEAAXHH@Z")
	if err != nil {
		return fail("这个 mpay.dll 没有 login")
	}
	loginProc.Call(inst, 1, 0)
	logf("已弹出官方登录窗口，等待登录…")
	go activateLoginWindow()

	// 主循环：泵消息 + 盯着登录窗有没有被直接关掉。
	//
	// 用户点右上角 X 时，DLL 有时会回调 code=1，有时只是把窗口藏起来，
	// 后者会让宿主窗口一直挂着（进程不退出）。这里补一个看门狗。
	deadline := time.Now().Add(opts.Timeout)
	appeared := false
	lastProbe := time.Now()
	for time.Now().Before(deadline) {
		pumpMessages()
		select {
		case res := <-decided:
			return res
		default:
		}

		if time.Since(lastProbe) > 400*time.Millisecond {
			lastProbe = time.Now()
			alive := findWindowByClass("MPAY_LOGIN") != 0
			if alive {
				appeared = true
			} else if appeared && !loginReady() {
				logf("登录窗口被直接关闭，按取消处理")
				emit(Result{OK: false, Error: "登录窗口已关闭"})
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	return fail("登录超时，请重试")
}

func fail(msg string) Result {
	logf("失败: %s", msg)
	return Result{OK: false, Error: msg, Finished: time.Now().Unix()}
}

// emit 只发一次结果。回调可能在 DLL 的原生线程里，绝不能阻塞。
func emit(res Result) {
	if res.Finished == 0 {
		res.Finished = time.Now().Unix()
	}
	sendOnce.Do(func() {
		decided <- res
	})
}

func installLogHook(dll *syscall.DLL, inst uintptr) {
	fn := syscall.NewCallback(func(msg uintptr) uintptr {
		if msg != 0 {
			logf("[mpay] %s", goString(msg))
		}
		return 0
	})
	if p, err := dll.FindProc("?SetLogHook@CMpay_Interface@Mpay@@QEAAXP6AXPEBD@Z@Z"); err == nil {
		p.Call(inst, fn)
	}
}

func setParentWindow(dll *syscall.DLL, hwnd uintptr) {
	p, err := dll.FindProc("?SetOption@CMpay_Interface@Mpay@@SAXPEBDPEBX@Z")
	if err != nil {
		return
	}
	key, _ := syscall.BytePtrFromString("mpay_option_parent_hwnd")
	val := hwnd
	p.Call(uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&val)))
}

// setSkin 加载官方皮肤包。没有它 DLL 根本不会把登录窗建出来。
func setSkin(dll *syscall.DLL, inst uintptr, skin string) {
	if skin == "" {
		logf("没有皮肤包，登录窗可能弹不出来")
		return
	}
	if st, err := os.Stat(skin); err != nil || st.Size() == 0 {
		logf("皮肤包不可用: %s", skin)
		return
	}
	p, err := dll.FindProc("?SetResPath@CMpay_Interface@Mpay@@QEAAHHPEBD@Z")
	if err != nil {
		return
	}
	sp, _ := syscall.BytePtrFromString(skin)
	r, _, _ := p.Call(inst, 2, uintptr(unsafe.Pointer(sp)))
	logf("SetResPath -> %d (%s)", r, skin)
}

func createHostWindow(cx, cy int) uintptr {
	wndProc := syscall.NewCallback(func(hwnd, m, wp, lp uintptr) uintptr {
		r, _, _ := procDefWindowProcW.Call(hwnd, m, wp, lp)
		return r
	})
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cls, _ := syscall.UTF16PtrFromString("SkyLoginMpayHost")
	title, _ := syscall.UTF16PtrFromString("SkyLogin mpay host")

	wc := wndClassExW{
		lpfnWndProc:   wndProc,
		hInstance:     hinst,
		lpszClassName: cls,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// DLL 会检查父窗口"有效且可见"，但用户不该看见它：
	//   - WS_EX_LAYERED + 全透明：看不见
	//   - WS_EX_TOOLWINDOW：不进任务栏、不进 Alt+Tab
	//   - WS_EX_NOACTIVATE：不抢焦点
	// 位置就是"登录窗中心"，因为官方登录窗是以宿主窗口为中心弹出的。
	hwnd, _, _ := procCreateWindowExW.Call(
		wsExToolWindow|wsExLayered|wsExNoActivate,
		uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		wsPopup|wsVisible, uintptr(cx), uintptr(cy), 1, 1, 0, 0, hinst, 0)
	if hwnd != 0 {
		procSetLayeredWindowAttributes.Call(hwnd, 0, 0, 2) // LWA_ALPHA，alpha=0
	}
	procShowWindow.Call(hwnd, 4) // SW_SHOWNOACTIVATE
	procUpdateWindow.Call(hwnd)
	hostHwnd = hwnd
	return hwnd
}

// screenCenter 是拿不到主界面位置时的兜底。
func screenCenter() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	h, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	return int(w) / 2, int(h) / 2
}

func buildCallbacks() {
	stub := syscall.NewCallback(func(a1, a2, a3 uintptr) uintptr { return 0 })
	for i := range vtable {
		vtable[i] = stub
	}
	vtable[slotLogin] = syscall.NewCallback(onLoginFinish)
	vtable[slotTicket] = syscall.NewCallback(onTicketResult)
	cbObject[0] = uintptr(unsafe.Pointer(&vtable[0]))
}

// onLoginFinish 对应官方 ApiCallBack 的登录完成回调。
//
// structUserResponseInfo 是 char* 数组，实测：
//
//	[0] 用户 id   [1] 设备 id   [2] user_token   [8] 扩展信息 JSON
func onLoginFinish(this, code, info uintptr) uintptr {
	got := make([]string, 9)
	if info != 0 {
		for i := range got {
			p := *(*uintptr)(unsafe.Pointer(info + uintptr(i)*unsafe.Sizeof(uintptr(0))))
			got[i] = goString(p)
		}
	}
	mu.Lock()
	slots = got
	mu.Unlock()

	if int(code) != 0 {
		logf("登录未完成: code=%d %s", code, describe(got))
		emit(Result{OK: false, Error: fmt.Sprintf("登录未完成（code=%d）", code), Slots: got})
		return 0
	}
	logf("登录成功: %s", describe(got))
	mu.Lock()
	loginOK = true
	mu.Unlock()

	// 登录成功后向 DLL 要一张票，后面的 login/ticket 换 token 走现成链路
	ticketProc, err := dllHandle.FindProc("?GetUserTicket@CMpay_Interface@Mpay@@QEAAXXZ")
	if err != nil {
		emit(Result{OK: true, DeviceID: slotAt(got, 1), Account: accountOf(got), Slots: got})
		return 0
	}
	ticketProc.Call(this)

	// 万一 DLL 不出票，别让界面一直转圈
	go func() {
		time.Sleep(ticketWait)
		emit(Result{OK: true, DeviceID: slotAt(got, 1), Account: accountOf(got), Slots: got,
			Error: "登录成功，但没能从登录窗口拿到票据"})
	}()
	return 0
}

func onTicketResult(this, code, ticket, _ uintptr) uintptr {
	t := goString(ticket)

	mu.Lock()
	got := append([]string(nil), slots...)
	mu.Unlock()

	if int(code) != 0 || t == "" {
		logf("取票失败: code=%d", code)
		emit(Result{OK: true, DeviceID: slotAt(got, 1), Account: accountOf(got), Slots: got,
			Error: "登录成功，但 MPay 没有返回票据"})
		return 0
	}
	logf("拿到票据: %s…", t[:min(len(t), 4)])
	emit(Result{OK: true, Ticket: t, DeviceID: slotAt(got, 1), Account: accountOf(got), Slots: got})
	return 0
}

func slotAt(s []string, i int) string {
	if i >= 0 && i < len(s) {
		return s[i]
	}
	return ""
}

// accountOf 尽量从回调里找出登录用的账号名（邮箱/手机号）用于展示。
//
// 注意：槽位 8 可能是扩展信息 JSON，里面也可能带账号；不能把整段
// JSON 当成账号名返回，否则界面上会显示一大串花括号。
func accountOf(s []string) string {
	for _, v := range s {
		v = strings.TrimSpace(v)
		if len(v) < 3 || looksLikeToken(v) {
			continue
		}
		if strings.HasPrefix(v, "{") {
			if name := accountFromJSON(v); name != "" {
				return name
			}
			continue
		}
		if containsRune(v, '@') || isDigits(v, 11) {
			return v
		}
	}
	return ""
}

// accountFromJSON 从扩展信息里挑账号名。
func accountFromJSON(raw string) string {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	for _, k := range []string{"account", "username", "email", "full_account", "display_username", "mobile"} {
		if v, _ := m[k].(string); strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func isDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func looksLikeToken(s string) bool {
	return len(s) > 40 && s[0] == '1' && s[1] == '-'
}

// Describe 供外部（主进程日志）使用，保证同样的脱敏规则。
func Describe(s []string) string { return describe(s) }

// describe 把回调内容变成可写进日志的形式。
//
// 回调里带 user_token（等于登录凭证）和登录账号，日志里不能留原文，
// 只保留"哪几个槽位有值 + 长度"，排查够用。
func describe(s []string) string {
	parts := make([]string, 0, len(s))
	for i, v := range s {
		switch {
		case v == "":
			continue
		case looksLikeToken(v):
			parts = append(parts, fmt.Sprintf("slot%d=token(%d)", i, len(v)))
		case isDigits(v, 11) || containsRune(v, '@'):
			parts = append(parts, fmt.Sprintf("slot%d=账号(已隐藏)", i))
		case len(v) > 30:
			parts = append(parts, fmt.Sprintf("slot%d=<%d字节>", i, len(v)))
		default:
			parts = append(parts, fmt.Sprintf("slot%d=%s", i, v))
		}
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func pumpMessages() {
	var m winMsg
	for {
		r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1) // PM_REMOVE
		if r == 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// activateLoginWindow 把官方登录窗切到前台。
//
// 窗口是子进程创建的，Windows 的前台窗口锁经常不让它自己抢焦点；
// 用户的体感会是"点了没反应"。这里等窗口建出来后主动激活一次。
func activateLoginWindow() {
	for i := 0; i < 40; i++ {
		h := findWindowByClass("MPAY_LOGIN")
		if h != 0 {
			procShowWindow.Call(h, 9) // SW_RESTORE
			if r, _, _ := procSetForegroundWindow.Call(h); r == 0 {
				// 前台锁没放开时，这个"未公开但有效"的接口通常还能用
				procSwitchToThisWindow.Call(h, 1)
			}
			logf("已激活登录窗口: 0x%x", h)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// findWindowByClass 在**本进程**里找指定类名的可见窗口。
func findWindowByClass(class string) uintptr {
	var found uintptr
	enumProc := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if found != 0 {
			return 1
		}
		buf := make([]uint16, 128)
		procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 128)
		if syscall.UTF16ToString(buf) == class {
			if vis, _, _ := procIsWindowVisible.Call(hwnd); vis != 0 {
				found = hwnd
				return 0 // 找到就停
			}
		}
		return 1
	})
	procEnumWindows.Call(enumProc, 0)
	return found
}

// goString 读 DLL 给的 NUL 结尾字符串。必须在回调里立刻拷贝，
// 原生侧随后就会释放这块内存。
func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	const maxLen = 8192
	buf := make([]byte, 0, 64)
	for i := 0; i < maxLen; i++ {
		c := *(*byte)(unsafe.Pointer(p + uintptr(i)))
		if c == 0 {
			break
		}
		buf = append(buf, c)
	}
	return string(buf)
}

func openLog(path string) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		logFile = f
	}
}

func closeLog() {
	if logFile != nil {
		_ = logFile.Close()
	}
}

func logf(format string, args ...any) {
	if logFile == nil {
		return
	}
	line := fmt.Sprintf("[%s] [mpayhost] %s\n",
		time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	_, _ = logFile.WriteString(line)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// loginReady 表示登录已经成功（窗口随之关闭是正常的，不算取消）。
func loginReady() bool {
	mu.Lock()
	defer mu.Unlock()
	return loginOK
}

// Shutdown 结束宿主进程。
//
// 为什么不用正常 return + os.Exit：mpay.dll 内部有一堆 WebView2 线程，
// 直接走 ExitProcess 偶尔会卡在 loader lock 上，表现就是"窗口关了但进程
// 还在"。这里先把窗口拆掉（保证屏幕上不留东西），再 TerminateProcess 硬退。
func Shutdown() {
	if hostHwnd != 0 {
		procDestroyWindow.Call(hostHwnd)
		hostHwnd = 0
	}
	hideMpayWindows()

	h, _, _ := procGetCurrentProcess.Call()
	procTerminateProcess.Call(h, 0)
	select {} // 正常情况下到不了这里
}

// hideMpayWindows 把本进程里 DLL 建的 MPAY_* 窗口藏起来。
func hideMpayWindows() {
	pid, _, _ := procGetCurrentProcessId.Call()
	enumProc := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if uintptr(wpid) != pid {
			return 1
		}
		buf := make([]uint16, 128)
		procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 128)
		if len(syscall.UTF16ToString(buf)) >= 5 && syscall.UTF16ToString(buf)[:5] == "MPAY_" {
			procShowWindow.Call(hwnd, 0) // SW_HIDE
		}
		return 1
	})
	procEnumWindows.Call(enumProc, 0)
}
