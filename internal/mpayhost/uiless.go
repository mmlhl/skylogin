package mpayhost

// 诊断：直接调用 mpay.dll 的 "无界面登录" 系列接口。
//
// 为什么要试这个
//
//	写游戏本地 mpay.db 需要破解它的加密（AES + User::Save 变换），代价很高。
//	但 mpay.dll 自己导出了一些不弹窗就能登录的方法，其中最有价值的是：
//
//	  SetUserInfoLoginByUrs(structUserResponseInfo*)  直接注入"已登录"的用户信息
//	  UILessTokenLogin(uid, token)                    用 uid + token 登录
//	  TicketLogin(ticket, bool)                       直接拿一张票去登录
//
//	structUserResponseInfo 就是登录回调里那个 char* 数组（slot0..slot8），
//	内容我们早就解析过。所以只要把扫码拿到的槽位原样灌进去，DLL 就会：
//	  自己保存账号 → 自己写 mpay.db → 自己换票
//	完全不需要我们碰数据库。
//
// 这个文件只做诊断：调用接口、把每个回调写进日志、再试着要一张票。
// 不接进正式流程，失败也不影响任何现有功能。

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// UILessAction 是这次要试的接口。
type UILessAction string

const (
	ActionSetUser UILessAction = "setuser" // SetUserInfoLoginByUrs
	ActionToken   UILessAction = "token"   // UILessTokenLogin
	ActionTicket  UILessAction = "ticket"  // TicketLogin
	// ActionList 调 GetAccountList：问 DLL "你记住的账号有哪些"。
	// 平台客户端那个"记住上次的账号，是否登录"就是这个列表，用它可以直接
	// 看到某个号（尤其是渠道服扫进来的号）有没有被记住。
	ActionList UILessAction = "list"
	// ActionSetPlatform 单独调 SetUserPlatform(platform)，写"当前用户"的平台记录。
	ActionSetPlatform UILessAction = "setplatform"
	// ActionUrs 无头账号密码登录：UILessUrsLogin(user, pass, ?, savePwd)。
	// 参数含义待验证（探针），成功后 DLL 走完整登录链（写库 + 推送）。
	ActionUrs UILessAction = "urs"
)

// UILessOptions 是诊断需要的输入。
type UILessOptions struct {
	Options              // 复用登录那套：SkyDir / SkinZip / GameID / LogPath / DataPath
	Action  UILessAction // 调哪个接口
	Slots   []string     // setuser 用：9 个槽位（来自 channel_login_slots.json）
	UID     string       // token 用
	Token   string       // token 用
	Ticket  string       // ticket 用
	Device  string       // 设备号：非空时先 SetDeviceInfo，再调接口
	Ch      string       // app_channel：空 = a50_sdk_cn
	FakeURL bool         // true 时把服务地址指到本地假服务器（看 DLL 发什么）
	// Platform 非空且登录成功时，顺手调 SetUserPlatform（ad / ios / hm）。
	// 目的：把"这个账号玩的是哪个手机平台"直接写进 DLL 的记录，
	// 免得游戏登录后再弹一次 iOS/安卓选择框。
	Platform string
	WaitSec  int // 调完之后等回调的时间
}

// 这些必须是包级变量：DLL 会长期持有字符串地址，GC 不能回收
var (
	slotBytes [16][]byte
	slotPtrs  [16]uintptr
)

// ProbeUILess 执行一次诊断调用并记录全部回调。
func ProbeUILess(o UILessOptions) Result {
	if o.Timeout <= 0 {
		o.Timeout = 3 * time.Minute
	}
	if o.WaitSec <= 0 {
		o.WaitSec = 15
	}
	openLog(o.LogPath)
	defer closeLog()

	// 窗口、消息循环、DLL 必须在同一个 OS 线程上
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := os.Chdir(o.SkyDir); err != nil {
		return fail("进入游戏目录失败: " + err.Error())
	}
	os.Setenv("PATH", o.SkyDir+`;`+os.Getenv("PATH"))
	if p, err := syscall.UTF16PtrFromString(o.SkyDir); err == nil {
		procSetDllDirectoryW.Call(uintptr(unsafe.Pointer(p)))
	}
	procCoInitializeEx.Call(0, 2)

	cx, cy := o.CenterX, o.CenterY
	if cx <= 0 || cy <= 0 {
		cx, cy = screenCenter()
	}
	hwnd := createHostWindow(cx, cy)
	if hwnd == 0 {
		return fail("创建宿主窗口失败")
	}

	dllPath := filepath.Join(o.SkyDir, "mpay.dll")
	dll, err := syscall.LoadDLL(dllPath)
	if err != nil {
		return fail("加载 mpay.dll 失败: " + err.Error())
	}
	dllHandle = dll
	logf("加载 mpay.dll: %s", dllPath)

	create, err := dll.FindProc("?Create_Interface@CMpay_Interface@Mpay@@SAPEAV12@XZ")
	if err != nil {
		return fail("找不到 Create_Interface")
	}
	inst, _, _ := create.Call()
	if inst == 0 {
		return fail("创建 MPay 接口失败")
	}

	installLogHook(dll, inst)
	setParentWindow(dll, hwnd)
	setDataPath(dll, o.DataPath)
	setSkin(dll, inst, o.SkinZip)
	// list 模式下账号列表走的是"指针参数"，认不出内容时得看原始字节
	dumpArgs = o.Action == ActionList
	buildCallbacksLogged()

	game, _ := syscall.BytePtrFromString(o.GameID)
	channel := o.Ch
	if channel == "" {
		channel = "a50_sdk_cn"
	}
	ch, _ := syscall.BytePtrFromString(channel)
	logf("init 用的 app_channel = %s", channel)
	empty, _ := syscall.BytePtrFromString("")

	initProc, err := dll.FindProc("?init@CMpay_Interface@Mpay@@QEAAHPEBD0000PEAVApiCallBack@2@H@Z")
	if err != nil {
		return fail("找不到 init")
	}
	r, _, _ := initProc.Call(inst,
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
		return fail("MPay 初始化失败")
	}

	// 把服务地址指到本地假服务器：DLL 的请求会明文发过来，我们能看也能改。
	var fake *FakeAPI
	if o.FakeURL {
		f, ferr := StartFakeAPI(logf)
		if ferr != nil {
			logf("本地假服务器起不来: %v", ferr)
		} else {
			fake = f
			defer fake.Close()
			url := "http://" + f.Addr
			setOptionString(dll, "mpay_option_custom_service_url", url)
			setOptionString(dll, "mpay_option_custom_httpdns_service_url", url)
			logf("已把服务地址指向 %s（本机明文）", url)
		}
	}

	// 有设备号就先告诉 DLL：它内部换票要用同一个设备号，否则 login/ticket 会 1306
	if o.Device != "" {
		if proc, err := dll.FindProc("?SetDeviceInfo@CMpay_Interface@Mpay@@QEAAXPEBD@Z"); err == nil {
			d, _ := syscall.BytePtrFromString(o.Device)
			proc.Call(inst, uintptr(unsafe.Pointer(d)))
			logf("已 SetDeviceInfo(%s)", o.Device)
		} else {
			logf("没有 SetDeviceInfo: %v", err)
		}
	}

	// 按选择的动作调用对应接口
	switch o.Action {
	case ActionSetUser:
		if err := callSetUserInfo(dll, inst, o.Slots); err != nil {
			return fail(err.Error())
		}
	case ActionToken:
		callUILessTokenLogin(dll, inst, o.UID, o.Token)
	case ActionTicket:
		callTicketLogin(dll, inst, o.Ticket)
	case ActionList:
		callGetAccountList(dll, inst)
	case ActionSetPlatform:
		// 不需要先登录——游戏退出后库里留着"当前用户"，跟它写就行。
		// 用途：登录窗（a50）写的平台选择游戏（ma75）看不见，两份库是分开的，
		// 得分别对 ma75 再写一次（2026-11-08 实测：只写 a50 游戏内照样弹选择框）。
		// 先试 UILessUidLogin 把当前用户加载起来（SetUserPlatform 需要库里
		// 有"已登录的当前用户"，直接调返回 0），成了再写平台。
		if proc, err := dll.FindProc("?UILessUidLogin@CMpay_Interface@Mpay@@QEAAXPEBD00H_NH@Z"); err == nil {
			u, _ := syscall.BytePtrFromString(o.UID)
			t, _ := syscall.BytePtrFromString(o.Token)
			e, _ := syscall.BytePtrFromString("")
			logf("调用 UILessUidLogin(uid=%s, token=%d字符, \"\", 0, true, 0)", o.UID, len(o.Token))
			proc.Call(inst,
				uintptr(unsafe.Pointer(u)),
				uintptr(unsafe.Pointer(t)),
				uintptr(unsafe.Pointer(e)),
				0, 1, 0)
		} else {
			logf("没有 UILessUidLogin: %v", err)
		}
		// 另一个入口：UpdateAasAcountPlatform，名字上是"直接改账号平台记录"，
		// 顺带调一次对比（Aas = 官方账号服务的内部叫法，猜测）。
		if proc, err := dll.FindProc("?UpdateAasAcountPlatform@CMpay_Interface@Mpay@@QEAAXPEBD@Z"); err == nil {
			p, _ := syscall.BytePtrFromString(o.Platform)
			proc.Call(inst, uintptr(unsafe.Pointer(p)))
			logf("UpdateAasAcountPlatform(%s) 已调", o.Platform)
		} else {
			logf("没有 UpdateAasAcountPlatform: %v", err)
		}
	case ActionUrs:
		// UILessRSAKeyGen → 槽位 14 回调 PEM 公钥 → RSA 加密密码 →
		// UILessUrsLogin(user, 密文, "", true)。明文密码会 decrypt fail。
		passText := o.Token // Token 字段复用为密码
		if rg, err := dll.FindProc("?UILessRSAKeyGen@CMpay_Interface@Mpay@@QEAAXXZ"); err == nil {
			logf("调用 UILessRSAKeyGen()（等槽位 14 回调公钥）")
			rg.Call(inst)
			deadline := time.Now().Add(6 * time.Second)
			for time.Now().Before(deadline) {
				pumpMessages()
				mu.Lock()
				pk := rsaPublicKey
				mu.Unlock()
				if pk != "" {
					break
				}
				time.Sleep(30 * time.Millisecond)
			}
			mu.Lock()
			pk := rsaPublicKey
			mu.Unlock()
			if pk != "" {
				if enc, eerr := rsaEncryptPassword(pk, passText); eerr == nil {
					logf("密码已用 DLL 公钥加密（密文 %d 字符）", len(enc))
					passText = enc
				} else {
					logf("RSA 加密失败（将传明文试一次）: %v", eerr)
				}
			} else {
				logf("没等到 RSA 公钥回调（将传明文试一次）")
			}
		} else {
			logf("没有 UILessRSAKeyGen: %v", err)
		}
		if proc, err := dll.FindProc("?UILessUrsLogin@CMpay_Interface@Mpay@@QEAAXPEBD00_N@Z"); err == nil {
			u, _ := syscall.BytePtrFromString(o.UID)
			p, _ := syscall.BytePtrFromString(passText)
			e, _ := syscall.BytePtrFromString("")
			logf("调用 UILessUrsLogin(user=%d字符, pass=%d字符, \"\", true)", len(o.UID), len(passText))
			proc.Call(inst,
				uintptr(unsafe.Pointer(u)),
				uintptr(unsafe.Pointer(p)),
				uintptr(unsafe.Pointer(e)),
				1)
		} else {
			logf("没有 UILessUrsLogin: %v", err)
		}
	default:
		return fail("未知动作: " + string(o.Action))
	}

	// 泵消息，等回调（登录成功/失败）
	logf("等待回调 %d 秒…", o.WaitSec)
	if res, ok := waitResult(o.WaitSec); ok {
		if o.Platform != "" && res.OK {
			callSetUserPlatform(dll, inst, o.Platform)
		}
		return res
	}

	// list 只是"读一下列表"，本来就不该有登录结果，等到这里就收工
	// setplatform 同理：写完就收工，结果看日志里的返回值
	if o.Action == ActionList || o.Action == ActionSetPlatform {
		logf("%s 模式结束", o.Action)
		return Result{OK: false, Error: string(o.Action) + " 模式没有登录结果", Finished: time.Now().Unix()}
	}

	// 还没结果：主动要一张票看看
	logf("没有登录回调，主动调 GetUserTicket 试一次")
	if proc, err := dll.FindProc("?GetUserTicket@CMpay_Interface@Mpay@@QEAAXXZ"); err == nil {
		proc.Call(inst)
		if res, ok := waitResult(10); ok {
			return res
		}
	}

	mu.Lock()
	got := append([]string(nil), slots...)
	mu.Unlock()
	logf("超时，槽位: %s", describe(got))
	return Result{OK: false, Error: "诊断超时，没有回调", Slots: got, Finished: time.Now().Unix()}
}

// waitResult 泵消息直到拿到结果或超时。
func waitResult(seconds int) (Result, bool) {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(deadline) {
		pumpMessages()
		select {
		case res := <-decided:
			logf("拿到结果: ok=%v ticket=%d字符 err=%s", res.OK, len(res.Ticket), res.Error)
			return res, true
		default:
		}
		time.Sleep(15 * time.Millisecond)
	}
	return Result{}, false
}

// callSetUserInfo 调 SetUserInfoLoginByUrs(structUserResponseInfo*)。
//
// 结构就是登录回调给的那个 char* 数组；这里按同样布局填 16 个指针
// （实测回调只用到前 9 个，多给几个防止 DLL 读越界）。
func callSetUserInfo(dll *syscall.DLL, inst uintptr, slots []string) error {
	proc, err := dll.FindProc("?SetUserInfoLoginByUrs@CMpay_Interface@Mpay@@QEAAXPEBUstructUserResponseInfo@12@@Z")
	if err != nil {
		return fmt.Errorf("这个 mpay.dll 没有 SetUserInfoLoginByUrs")
	}
	if len(slots) < 9 {
		return fmt.Errorf("槽位不够（%d 个），至少需要 9 个", len(slots))
	}

	for i := 0; i < 16; i++ {
		v := ""
		if i < len(slots) {
			v = slots[i]
		}
		slotBytes[i] = append([]byte(v), 0) // NUL 结尾
		slotPtrs[i] = uintptr(unsafe.Pointer(&slotBytes[i][0]))
	}
	logf("调用 SetUserInfoLoginByUrs（%d 个槽位，slot0=%d字符 slot7=%s slot8=%d字符）",
		len(slots), len(slotAt(slots, 0)), slotAt(slots, 7), len(slotAt(slots, 8)))
	proc.Call(inst, uintptr(unsafe.Pointer(&slotPtrs[0])))
	return nil
}

// rsaEncryptPassword 用 DLL 回调的 PEM 公钥加密密码（RSA PKCS#1 v1.5）。
// URS 密码登录的标准做法；具体 padding 若不对，DLL 会报 decrypt fail，
// 到时再换 OAEP 试。
func rsaEncryptPassword(publicKeyPEM, password string) (string, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return "", fmt.Errorf("公钥 PEM 解析失败")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		// 也可能是 PKCS#1 裸格式（BEGIN RSA PUBLIC KEY）
		if rsaKey, e2 := x509.ParsePKCS1PublicKey(block.Bytes); e2 == nil {
			pubAny = rsaKey
		} else {
			return "", fmt.Errorf("公钥解析失败: %v / %v", err, e2)
		}
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("不是 RSA 公钥: %T", pubAny)
	}
	enc, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(password))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

// callUILessTokenLogin 调 UILessTokenLogin(uid, token)。
func callUILessTokenLogin(dll *syscall.DLL, inst uintptr, uid, token string) {
	proc, err := dll.FindProc("?UILessTokenLogin@CMpay_Interface@Mpay@@QEAAXPEBD0@Z")
	if err != nil {
		logf("没有 UILessTokenLogin: %v", err)
		return
	}
	u, _ := syscall.BytePtrFromString(uid)
	t, _ := syscall.BytePtrFromString(token)
	logf("调用 UILessTokenLogin(uid=%d字符, token=%d字符)", len(uid), len(token))
	proc.Call(inst, uintptr(unsafe.Pointer(u)), uintptr(unsafe.Pointer(t)))
}

// callTicketLogin 调 TicketLogin(ticket, bool)。
func callTicketLogin(dll *syscall.DLL, inst uintptr, ticket string) {
	proc, err := dll.FindProc("?TicketLogin@CMpay_Interface@Mpay@@QEAAXPEBD_N@Z")
	if err != nil {
		logf("没有 TicketLogin: %v", err)
		return
	}
	t, _ := syscall.BytePtrFromString(ticket)
	// 第二个参数语义不明（官方两处调用都传 1）；排查 login_type=unknown 时
	// 允许用环境变量改成 0 试一次。
	second := uintptr(1)
	if os.Getenv("SKYLOGIN_TICKET_BOOL0") == "1" {
		second = 0
	}
	logf("调用 TicketLogin(%d 字符, bool=%d)", len(ticket), second)
	proc.Call(inst, uintptr(unsafe.Pointer(t)), second)
}

// callGetAccountList 调 GetAccountList()：让 DLL 把它记住的账号列表回调出来。
//
// 这个接口没有参数也没有返回值，结果全走 ApiCallBack 的某个槽位，
// 所以调用方要用 buildCallbacksLogged 那套"每个槽位都记一笔"的回调表。
func callGetAccountList(dll *syscall.DLL, inst uintptr) {
	proc, err := dll.FindProc("?GetAccountList@CMpay_Interface@Mpay@@QEAAXXZ")
	if err != nil {
		logf("没有 GetAccountList: %v", err)
		return
	}
	logf("调用 GetAccountList()（读 DLL 记住的账号）")
	proc.Call(inst)
}

// callSetUserPlatform 调 SetUserPlatform(platform)：把"这个账号玩哪个平台"记进 DLL。
//
// 只接受 ad / ios / hm（DLL 里的报错原文：only support 'ad', 'ios', 'hm'）。
// 它改的是**当前用户**的记录，所以必须已经登录成功再调；成功了游戏那边
// 就不会再弹"你玩的是 iOS 还是安卓"。
func callSetUserPlatform(dll *syscall.DLL, inst uintptr, platform string) {
	proc, err := dll.FindProc("?SetUserPlatform@CMpay_Interface@Mpay@@QEAA_NPEBD@Z")
	if err != nil {
		logf("这个 mpay.dll 没有 SetUserPlatform: %v", err)
		return
	}
	p, _ := syscall.BytePtrFromString(platform)
	r, _, _ := proc.Call(inst, uintptr(unsafe.Pointer(p)))
	logf("SetUserPlatform(%s) -> %d", platform, r)
}

// setPlatformWanted 返回"要不要替用户固定平台、固定成哪个"。
//
// 两个开关，环境变量优先（命令行启动时用），否则读文件——
// 用户平时是双击 exe，环境变量带不进去，只有文件这条路走得通。
// 文件：%APPDATA%\SkyLogin\set_platform.txt，内容写 ad / ios / hm。
func setPlatformWanted() string {
	if v := strings.TrimSpace(os.Getenv("SKYLOGIN_SET_PLATFORM")); v != "" {
		return v
	}
	p := filepath.Join(os.Getenv("APPDATA"), "SkyLogin", "set_platform.txt")
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	return line
}

// LoadSlotsFile 读 channel_login_slots.json 里的 slots 数组。
func LoadSlotsFile(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var dump struct {
		Slots []string `json:"slots"`
	}
	if err := json.Unmarshal(b, &dump); err != nil {
		return nil, err
	}
	if len(dump.Slots) == 0 {
		return nil, fmt.Errorf("文件里没有 slots")
	}
	return dump.Slots, nil
}

// setOptionString 调 SetOption(key, char*)。
//
// 注意值是 **C 字符串指针**：DLL 内部按字符串解析（对整数型键用 &int）。
func setOptionString(dll *syscall.DLL, key, val string) {
	p, err := dll.FindProc("?SetOption@CMpay_Interface@Mpay@@SAXPEBDPEBX@Z")
	if err != nil {
		logf("没有 SetOption: %v", err)
		return
	}
	k, _ := syscall.BytePtrFromString(key)
	v, _ := syscall.BytePtrFromString(val)
	p.Call(uintptr(unsafe.Pointer(k)), uintptr(unsafe.Pointer(v)))
	logf("SetOption(%s, %s)", key, val)
}
