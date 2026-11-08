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

	"skylogin/internal/win"
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

	// DataPath 非空时把 mpay 的数据目录指过去（对应 mpay_option_data_path）。
	// 诊断用：指到平台自己的 unisdk 目录，就能复用平台缓存的登录态。
	DataPath string
	// SkipLogout 为 true 时不清理旧登录态，让 DLL 直接静默登录上次的账号。
	SkipLogout bool
	// QRCodeOnly 为 true 时登录窗直接显示二维码页，不显示邮箱/手机号表单。
	//
	// 用官方 DLL 的 mpay_option_disable_not_qrcode_login 选项实现：渠道服
	// 扫码入口的用户本来也只能扫码，没必要让他再点一下切页签。
	QRCodeOnly bool
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

	// 渠道服扫码（slot7 是 huawei 这种）时带回来的东西：
	// MPay 没有它的票，但华为的 openid + accessToken 能换出 gameAuthSign，
	// 主进程据此存成一个渠道账号（见 app/channelaccount.go）。
	Channel     string `json:"channel,omitempty"`
	OpenID      string `json:"openId,omitempty"`
	AccessToken string `json:"accessToken,omitempty"`
	// UnisdkData 是 slot8 里 extra_unisdk_data 的原文。
	//
	// 不是每个渠道都像华为那样给 sdk_info.openid/accessToken：vivo 这类
	// 渠道给的是**整包登录凭证**（SAUTH_JSON 里有 sessionid / access_token /
	// udid / sdk_version 等）。存原文就能原样交给 uni_sauth，不用一个渠道
	// 一个渠道地对字段。
	UnisdkData string `json:"unisdkData,omitempty"`

	// 这次登录用的环境：哪份 mpay.dll、什么版本、哪个 game id。
	// 主进程会把它们写进账号记录——以后要查"哪个版本的凭证更容易过期"，
	// 靠的就是这几个字段（见 app/store.go 的 accountView）。
	DLLPath    string `json:"dllPath,omitempty"`
	DLLVersion string `json:"dllVersion,omitempty"`
	GameID     string `json:"gameId,omitempty"`
}

// 登录环境在 Run 里确定一次，emit 时统一补进结果，免得每个回调点都抄一遍。
var (
	loginEnvMu sync.Mutex
	loginEnv   struct{ dll, ver, game string }
)

func setLoginEnv(dll, ver, game string) {
	loginEnvMu.Lock()
	loginEnv.dll, loginEnv.ver, loginEnv.game = dll, ver, game
	loginEnvMu.Unlock()
}

func fillLoginEnv(res Result) Result {
	loginEnvMu.Lock()
	defer loginEnvMu.Unlock()
	if res.DLLPath == "" {
		res.DLLPath = loginEnv.dll
	}
	if res.DLLVersion == "" {
		res.DLLVersion = loginEnv.ver
	}
	if res.GameID == "" {
		res.GameID = loginEnv.game
	}
	return res
}

const (
	// 官方 ApiCallBack 实测 33 个槽位；多给一些，表尾空指针会直接把进程打崩。
	vtableSlots = 40

	// 回调槽位（按 NtUniSdkMpay.dll 的虚表排布，末尾的几个槽官方 ApiCallBack 没用到，填空函数占位）
	slotLogin  = 1 // 登录完成 (this, code, structUserResponseInfo*)
	slotLogout = 2 // 退出登录完成 (this)
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

	// logout 回调只到一次；用它判断"旧登录态清完了没有"，别死等固定时间
	logoutOnce    sync.Once
	logoutDone    = make(chan struct{})
	clearingLogin bool // 正在清旧登录态：这期间进来的回调都不是登录结果

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

	dllPath := filepath.Join(opts.SkyDir, "mpay.dll")
	dll, err := syscall.LoadDLL(dllPath)
	if err != nil {
		return fail("加载 mpay.dll 失败: " + err.Error())
	}
	// 把实际加载的路径和版本打进日志：诊断"窗口行为不一样"时先看这条。
	// 同时记进结果里写回账号（排查"凭证能活多久"要看是哪份 DLL 登出来的）。
	verText := ""
	logf("加载 mpay.dll: %s", dllPath)
	if a, b, c, ok := win.FileVersion(dllPath); ok {
		verText = fmt.Sprintf("%d.%d.%d", a, b, c)
		logf("mpay.dll 版本: %s", verText)
	}
	setLoginEnv(dllPath, verText, opts.GameID)
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
	setDataPath(dll, opts.DataPath)
	setSkin(dll, inst, opts.SkinZip)
	if opts.QRCodeOnly {
		setQRCodeOnly(dll)
		// 扫码窗里会浮一个「年龄提示」小气泡（skin 里的 age_tips.xml），
		// 尺寸 60x76 还跟着窗口走，正好挡在二维码上方。官方留了开关。
		setOptionInt(dll, "mpay_option_show_welcome_tip", 0)
	}

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

	// 弹窗之前先清掉上一次留下的登录态。
	//
	// 上一次登录成功后，DLL 会把账号会话写进自己的 db；下次调用 login() 时它
	// 直接用这个会话静默登录成同一个账号——窗口刚建出来就"自动确认"了，
	// 想添加第二个账号根本插不上手（日志里是 LoginForm show: false 然后
	// NotifyLoginFinishForAas）。官方 logout() 是清这块状态的入口。
	if opts.SkipLogout {
		logf("诊断：跳过清理旧登录态，让 DLL 直接静默登录上次的账号")
	} else {
		clearCachedLogin(dll, inst)
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
			hideTipWindows()
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
	res = fillLoginEnv(res)
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

// setDataPath 把 mpay 的落盘目录指到别处（mpay_option_data_path）。
//
// 传的是 C 字符串指针。诊断用：指到平台自己的 unisdk 目录，
// DLL 就能读到平台缓存的那套登录态，直接静默登录成平台那个账号。
func setDataPath(dll *syscall.DLL, dir string) {
	if dir == "" {
		return
	}
	p, err := dll.FindProc("?SetOption@CMpay_Interface@Mpay@@SAXPEBDPEBX@Z")
	if err != nil {
		return
	}
	key, _ := syscall.BytePtrFromString("mpay_option_data_path")
	val, verr := syscall.BytePtrFromString(dir)
	if verr != nil {
		return
	}
	p.Call(uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(val)))
	logf("数据目录 -> %s", dir)
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

// setQRCodeOnly 让登录窗直接停在二维码页。
//
// mpay.dll 的登录窗有两页：邮箱/手机号表单、二维码。默认停在哪一页由
// DLL 自己决定（还受上次登录方式影响），用户经常要多点一下右下角的小
// 二维码按钮才切过去。mpay_option_disable_not_qrcode_login 是官方给的
// "只留扫码登录"开关，设上以后窗口一出来就是码。
//
// 值按 DLL 的整数型选项传（SetOption 收的是指针）。
func setQRCodeOnly(dll *syscall.DLL) {
	p, err := dll.FindProc("?SetOption@CMpay_Interface@Mpay@@SAXPEBDPEBX@Z")
	if err != nil {
		logf("这个 mpay.dll 没有 SetOption，无法强制二维码页")
		return
	}
	key, _ := syscall.BytePtrFromString("mpay_option_disable_not_qrcode_login")
	val := 1
	p.Call(uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&val)))
	logf("已设置只用扫码登录（mpay_option_disable_not_qrcode_login=1）")
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
	vtable[slotLogout] = syscall.NewCallback(onLogoutFinish)
	vtable[slotTicket] = syscall.NewCallback(onTicketResult)
	cbObject[0] = uintptr(unsafe.Pointer(&vtable[0]))
}

// buildCallbacksLogged 和 buildCallbacks 一样，但把**没用到的那几十个槽位**
// 全换成"记一笔再返回"的回调，用来查某个接口到底走哪个槽位。
//
// 背景：UILess* 那几个接口调用后没有任何表现，很可能它们是通过我们当成
// 空函数的某个槽位回调的。这个版本会把每个被触发的槽位编号写进日志。
func buildCallbacksLogged() {
	for i := range vtable {
		idx := i
		vtable[i] = syscall.NewCallback(func(a1, a2, a3 uintptr) uintptr {
			logf("回调槽位 %d 被触发  a1=%s  a2=%s  a3=%s",
				idx, argText(a1), argText(a2), argText(a3))
			// urs 探针：槽位 14 是 UILessRSAKeyGen 的公钥回调（PEM 文本，
			// 在 a2），捕获到包级变量，uiless.go 的 ActionUrs 用它加密密码。
			if idx == 14 {
				for _, cand := range []uintptr{a2, a3} {
					if s, ok := readStringAt(cand); ok && strings.Contains(s, "PUBLIC KEY") {
						mu.Lock()
						rsaPublicKey = s
						mu.Unlock()
						logf("[urs] 已捕获 RSA 公钥（%d 字符）", len(s))
						break
					}
				}
			}
			return 0
		})
	}
	// 已知的三个槽位仍然走真正的处理逻辑（它们自己也会打日志）
	vtable[slotLogin] = syscall.NewCallback(onLoginFinish)
	vtable[slotLogout] = syscall.NewCallback(onLogoutFinish)
	vtable[slotTicket] = syscall.NewCallback(onTicketResult)
	cbObject[0] = uintptr(unsafe.Pointer(&vtable[0]))
}

// readStringAt 尽量把指针位置的内存读成字符串（探针用）。
// 读不出来返回 false。
func readStringAt(p uintptr) (string, bool) {
	if p == 0 || !looksLikeString(p) {
		return "", false
	}
	s := goString(p)
	if s == "" {
		return "", false
	}
	return s, true
}

// rsaPublicKey UILessRSAKeyGen 回调（槽位 14）交出来的公钥 PEM。
var rsaPublicKey string

// 小整数直接打数值；像指针的地址先试读成字符串（前 4 个字节都可打印才算），
// 读不出来再按 UTF-16 试一次（DLL 内部大量用宽字符），都不行就打地址 +
// 一小段十六进制，避免把随机内存当字符串读崩。
func argText(v uintptr) string {
	if v < 0x10000 {
		return fmt.Sprintf("%d", v)
	}
	var parts []string
	if s, ok := readJSONAt(v); ok {
		return s
	}
	if dumpArgs {
		parts = append(parts, "hex:"+hexPeek(v, 128))
	}
	if looksLikeString(v) {
		s := goString(v)
		if len(s) > 200 {
			s = s[:200] + "…"
		}
		parts = append(parts, fmt.Sprintf("ascii:%q", s))
	} else if s := lookStringUTF16(v, 200); s != "" {
		parts = append(parts, "u16:"+fmt.Sprintf("%q", s))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("0x%x", v)
	}
	return fmt.Sprintf("0x%x ", v) + strings.Join(parts, " ")
}

// readJSONAt 判断地址开头是不是 JSON（'{' 或 '['），是就整段读到 NUL。
//
// 为什么单独做：DLL 的账号列表是带换行/缩进的 JSON，首字节不是可打印的
// "连续 4 字节"，用 looksLikeString 那套会判成"不是字符串"，列表就被截成
// 一小段十六进制了。
func readJSONAt(p uintptr) (string, bool) {
	if p < 0x10000 {
		return "", false
	}
	c0 := *(*byte)(unsafe.Pointer(p))
	if c0 != '{' && c0 != '[' {
		return "", false
	}
	var sb strings.Builder
	for i := 0; i < 8192; i++ {
		c := *(*byte)(unsafe.Pointer(p + uintptr(i)))
		if c == 0 {
			return sb.String(), true
		}
		if c < 9 || (c > 13 && c < 32) {
			return "", false
		}
		sb.WriteByte(c)
	}
	return "", false
}

// dumpArgs 打开后，认不出来的指针参数会额外附一段十六进制（诊断用）。
var dumpArgs bool

// lookStringUTF16 把地址当成以 NUL 结尾的宽字符串读，不像文本就返回空串。
//
// 起点会试 p、p+1、p-1：DLL 有时把宽字符串塞在结构体里，指针落在首字节
// 或末字节上，直接按 p 读会整体错位一个字节（读出来是一串怪字）。
func lookStringUTF16(p uintptr, max int) string {
	best := ""
	for _, off := range []int{0, 1, -1} {
		addr := p
		if off > 0 {
			addr = p + uintptr(off)
		} else if off < 0 {
			if p < 2 {
				continue
			}
			addr = p - 1
		}
		if s := readUTF16(addr, max); len(s) > len(best) {
			best = s
		}
	}
	return best
}

// readUTF16 从 addr 起按宽字符读到 NUL 为止；遇到控制字符就判为"不是文本"。
func readUTF16(addr uintptr, max int) string {
	var sb strings.Builder
	for i := 0; i < max; i++ {
		c := *(*uint16)(unsafe.Pointer(addr + uintptr(i*2)))
		if c == 0 {
			return sb.String()
		}
		// 控制字符、代理区、以及明显不是文字的码位，直接判为"不是字符串"
		if c < 32 || (c >= 0xD800 && c <= 0xDFFF) || c == 0xFFFF {
			return ""
		}
		sb.WriteRune(rune(c))
	}
	return ""
}

// hexPeek 读一段内存做十六进制 + 可打印预览（长度受限，读崩的风险和读字符串一样）。
func hexPeek(p uintptr, n int) string {
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = *(*byte)(unsafe.Pointer(p + uintptr(i)))
	}
	var sb strings.Builder
	for _, c := range b {
		fmt.Fprintf(&sb, "%02x", c)
	}
	return sb.String()
}

// looksLikeString 粗判一个地址指向的是不是可打印文本（只看前 4 字节）。
func looksLikeString(p uintptr) bool {
	for i := 0; i < 4; i++ {
		c := *(*byte)(unsafe.Pointer(p + uintptr(i)))
		if c == 0 {
			return i > 0
		}
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}

// onLogoutFinish 对应官方 ApiCallBack 的"退出登录完成"回调（槽位 2）。
//
// 只给 clearCachedLogin 用：清旧登录态是异步的，等这个回调比干等固定时间稳。
// 这里只关一下信号量，绝不能 emit —— 它不是登录结果。
func onLogoutFinish(this uintptr) uintptr {
	logoutOnce.Do(func() { close(logoutDone) })
	return 0
}

// clearCachedLogin 用官方 logout() 清掉上一次登录留下的会话。
//
// 不清的话，下次 login() 时 DLL 会拿缓存的 AAS 会话直接静默登录成上次那个
// 账号：登录窗刚建出来就"自动确认"了（日志表现为 LoginForm show: false →
// NotifyLoginFinishForAas），用户根本没法换成另一个账号，也就添不了新账号。
//
// 这个 logout 走的是"本进程还没登录过"的状态，作用是清本地缓存的账号会话，
// 不会把我们已经保存的账号凭证在服务端作废；它失败也不影响后面弹窗登录。
func clearCachedLogin(dll *syscall.DLL, inst uintptr) {
	p, err := dll.FindProc("?logout@CMpay_Interface@Mpay@@QEAAXXZ")
	if err != nil {
		logf("这个 mpay.dll 没有 logout，跳过清理旧登录态")
		return
	}
	mu.Lock()
	clearingLogin = true
	mu.Unlock()
	defer func() {
		mu.Lock()
		clearingLogin = false
		mu.Unlock()
	}()

	logf("先清掉上次留下的登录态，否则登录窗会直接静默登录成上次的账号")
	p.Call(inst)

	timeout := time.After(2 * time.Second)
	for {
		pumpMessages()
		select {
		case <-logoutDone:
			logf("旧登录态已清掉，登录窗会从空白开始")
			return
		case <-timeout:
			// 没登录过的时候 DLL 可能压根不回调，那就继续弹窗
			logf("没等到 logout 回调，直接弹登录窗")
			return
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// onLoginFinish 对应官方 ApiCallBack 的登录完成回调。
//
// structUserResponseInfo 是 char* 数组，实测：
//
//	[0] 用户 id   [1] 设备 id   [2] user_token   [8] 扩展信息 JSON
func onLoginFinish(this, code, info uintptr) uintptr {
	mu.Lock()
	clearing := clearingLogin
	mu.Unlock()
	if clearing {
		// 清旧登录态时 DLL 偶尔也会走一遍登录回调；这时候还没有人操作窗口，
		// 当成登录结果会把刚弹出来的窗口直接判失败，所以只能丢掉。
		logf("清旧登录态期间的回调，忽略: code=%d", code)
		return 0
	}

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

	// 诊断/试验：登录成功这一刻把"这个账号玩哪个平台"写死。
	// 官方客户端是登录完让用户点 iOS/安卓，选完 DLL 自己记下来；我们在这里
	// 替用户点，之后游戏里就不该再弹那个选择框了。
	if plat := setPlatformWanted(); plat != "" {
		callSetUserPlatform(dllHandle, this, plat)
		// 再推一次服务端账号服务（Aas）：SetUserPlatform 只写本地库，
		// 游戏用的是自己那份 ma75 库，看不见这边写的东西（2026-11-08 实测
		// 游戏内照样弹选择框）。UpdateAasAcountPlatform 看名字是把平台
		// 记录推到服务端账号上，游戏侧就能读到。失败只写日志。
		if proc, err := dllHandle.FindProc("?UpdateAasAcountPlatform@CMpay_Interface@Mpay@@QEAAXPEBD@Z"); err == nil {
			p, _ := syscall.BytePtrFromString(plat)
			proc.Call(this, uintptr(unsafe.Pointer(p)))
			logf("已调 UpdateAasAcountPlatform(%s)（推服务端账号平台）", plat)
		}
	}

	// 渠道服扫的码：拿回来的是渠道账号（slot7=huawei 这种），MPay 没有它的票。
	// 但回调里带着华为的 openid + accessToken，交给主进程换 gameAuthSign 存成渠道账号。
	if ch := slotAt(got, 7); ch != "" && ch != "netease" {
		openid, access := channelSdkInfo(got)
		unisdk := unisdkDataOf(got)
		logf("扫码的是渠道服（%s）：openid %d 字符，accessToken %d 字符，登录包 %d 字符",
			ch, len(openid), len(access), len(unisdk))
		dumpChannelSlots(got)
		// 诊断：问一句 DLL 肯不肯自己给这个 unisdk 账号出票。
		// 它要是肯，"记住账号"和"取票"就都不用我们操心了；不肯也只是一条日志。
		if askChannelTicketEnabled() {
			askChannelTicketFor(this, got, ch, openid, access, unisdk)
			return 0
		}
		// 华为走 sdk_info（openid + accessToken）；vivo 这类只给整包
		// （extra_unisdk_data），两者有一个可用就算拿到凭证。
		if access == "" && unisdk == "" {
			emit(Result{OK: false, DeviceID: slotAt(got, 1), Account: accountOf(got), Slots: got,
				Error: fmt.Sprintf("扫的是渠道服账号（%s），但回调里没有可用的凭证", ch)})
			return 0
		}
		emit(Result{OK: true, DeviceID: slotAt(got, 1), Account: accountOf(got), Slots: got,
			Channel: ch, OpenID: openid, AccessToken: access, UnisdkData: unisdk})
		return 0
	}

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

// 渠道服取票诊断（环境变量 SKYLOGIN_ASK_CHANNEL_TICKET=1 打开）。
//
// 为什么要试：渠道服扫码时 MPay 已经回了 code=0（它认为这次登录成功了），
// 但本地"记住的账号"列表里始终只有网易号。如果它其实也能给 unisdk 账号
// 出票，说明"没记住"跟票据无关、是别的地方漏了；出不了票，那就是 MPay
// 把渠道服登录当成"半程"，不落库也说得通。
var (
	awaitingChannelTicket bool
	channelTicket         string
)

// askChannelTicketEnabled 读环境变量（子进程会继承，界面上不用加开关）。
func askChannelTicketEnabled() bool {
	return os.Getenv("SKYLOGIN_ASK_CHANNEL_TICKET") == "1"
}

// askChannelTicketFor 渠道服登录成功后也向 DLL 要一张票，记完日志再照常交付。
//
// 不能立刻把结果交出去：票是异步回调回来的，主进程一收到就会收工、
// 关窗退出，所以先等 8 秒，让 onTicketResult 有机会把结论写进日志。
func askChannelTicketFor(inst uintptr, got []string, ch, openid, access, unisdk string) {
	mu.Lock()
	awaitingChannelTicket = true
	channelTicket = ""
	mu.Unlock()

	logf("[诊断] 渠道服 %s 登录成功，再向 DLL 要一张票（看它认不认 unisdk 账号）", ch)
	if tp, err := dllHandle.FindProc("?GetUserTicket@CMpay_Interface@Mpay@@QEAAXXZ"); err != nil {
		logf("[诊断] 这个 DLL 没有 GetUserTicket: %v", err)
	} else {
		tp.Call(inst)
	}

	go func() {
		time.Sleep(8 * time.Second)
		mu.Lock()
		t := channelTicket
		awaitingChannelTicket = false
		mu.Unlock()
		logf("[诊断] 渠道服取票结论：%d 字符", len(t))
		emit(Result{OK: true, DeviceID: slotAt(got, 1), Account: accountOf(got), Slots: got,
			Channel: ch, OpenID: openid, AccessToken: access, UnisdkData: unisdk})
	}()
}

func onTicketResult(this, code, ticket, _ uintptr) uintptr {
	t := goString(ticket)

	// 渠道服取票诊断：这时候结果只记日志，不能当成"登录完成"抛出去，
	// 否则主进程会拿着一张来路不明的票去换 token。
	mu.Lock()
	probing := awaitingChannelTicket
	if probing {
		channelTicket = t
	}
	mu.Unlock()
	if probing {
		logf("[诊断] DLL 对渠道服账号的回票: code=%d ticket=%d 字符", code, len(t))
		return 0
	}

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

// channelSdkInfo 从回调槽位里抠出渠道的 openid / accessToken。
//
// 结构（2026-11-05 华为实测）：
//
//	slot8 = {"extra_unisdk_data": "<JSON 字符串>"}
//	  extra_unisdk_data = {"extra_data": "<JSON 字符串>", "SAUTH_STR": "...", ...}
//	    extra_data = {"playerLevel":"7","sdk_info":{"openid":"...","accessToken":"..."}}
func channelSdkInfo(slots []string) (openid, access string) {
	var outer, inner, extra map[string]any
	if json.Unmarshal([]byte(slotAt(slots, 8)), &outer) != nil {
		return "", ""
	}
	s, _ := outer["extra_unisdk_data"].(string)
	if json.Unmarshal([]byte(s), &inner) != nil {
		return "", ""
	}
	// extra_data 有时是字符串、有时直接是对象，两种都认
	switch v := inner["extra_data"].(type) {
	case string:
		if json.Unmarshal([]byte(v), &extra) != nil {
			return "", ""
		}
	case map[string]any:
		extra = v
	default:
		return "", ""
	}
	sdk, _ := extra["sdk_info"].(map[string]any)
	openid, _ = sdk["openid"].(string)
	access, _ = sdk["accessToken"].(string)
	if openid == "" {
		openid, _ = sdk["openId"].(string)
	}
	return openid, access
}

// unisdkDataOf 从 slot8 里取出 extra_unisdk_data 原文（渠道整包登录凭证）。
//
//	vivo 实测：{"extra_unisdk_data": "{\"SAUTH_STR\": \"…\", \"SAUTH_JSON\": \"…\", …}", …}
//
// 华为也是这个结构，只是里面多了 sdk_info。
func unisdkDataOf(slots []string) string {
	var outer map[string]any
	if json.Unmarshal([]byte(slotAt(slots, 8)), &outer) != nil {
		return ""
	}
	s, _ := outer["extra_unisdk_data"].(string)
	return s
}

// dumpChannelSlots 把渠道服登录的回调槽位原样写到数据目录，供分析用（仅诊断）。
func dumpChannelSlots(slots []string) {
	path := filepath.Join(os.Getenv("APPDATA"), "SkyLogin", "channel_login_slots.json")
	b, err := json.MarshalIndent(map[string]any{"slots": slots}, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, b, 0o600); err == nil {
		logf("渠道服回调已写入 %s（分析用）", path)
	}
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

// setOptionInt 设置整数型选项（DLL 的 SetOption 对数字收的是指针）。
func setOptionInt(dll *syscall.DLL, key string, val int) {
	p, err := dll.FindProc("?SetOption@CMpay_Interface@Mpay@@SAXPEBDPEBX@Z")
	if err != nil {
		return
	}
	k, _ := syscall.BytePtrFromString(key)
	v := val
	p.Call(uintptr(unsafe.Pointer(k)), uintptr(unsafe.Pointer(&v)))
	logf("已设置 %s=%d", key, val)
}

// hideTipWindows 把登录窗旁边那个「年龄提示」小气泡藏掉。
//
// skin 里它叫 age_tips.xml，是个 60x76 的独立窗口，会浮在二维码上方挡视线。
// DLL 有个 mpay_option_show_welcome_tip 选项，但实测设了没反应（DLL 日志里
// 连 setOption 都没记），所以改成直接把它藏起来：只认类名或标题里带 age/tip
// 的窗口，别的一概不动，避免误伤其它弹窗。
var tipClassesLogged sync.Map

func hideTipWindows() {
	pid, _, _ := procGetCurrentProcessId.Call()
	enumProc := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if uintptr(wpid) != pid {
			return 1
		}
		buf := make([]uint16, 128)
		procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 128)
		class := syscall.UTF16ToString(buf)
		// 只处理 MPAY_ 开头的自建窗
		if len(class) < 5 || class[:5] != "MPAY_" {
			return 1
		}
		lower := strings.ToLower(class)
		// 第一次见到就记一笔，方便以后核对类名
		if _, seen := tipClassesLogged.LoadOrStore(class, true); !seen {
			logf("发现 mpay 窗口: class=%s", class)
		}
		if strings.Contains(lower, "age") || strings.Contains(lower, "tip") {
			if vis, _, _ := procIsWindowVisible.Call(hwnd); vis != 0 {
				procShowWindow.Call(hwnd, 0) // SW_HIDE
				logf("已隐藏提示窗: %s", class)
			}
		}
		return 1
	})
	procEnumWindows.Call(enumProc, 0)
}
