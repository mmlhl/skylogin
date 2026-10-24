package fever

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// XOR 混淆表（与发烧平台客户端一致，256 字节）。
const xorHex = "4c85667cf22497662300819a8e03cbc7901d15ea463d7bacc36fbda7277813b4" +
	"8e7cee311aa2a72d09e5b315de16379c940c7f53949007a7b016534c32f641dc" +
	"88e4175eb1038cd48456ee4e108c20f3648cc591ae75f343a3d64316cf9329bd" +
	"cb8581c8db100d1a05f087cb1318f5c4f994eeb573d05c1e41c96f409b561846" +
	"3ee0d3f8a26901170ca5c6fd2a1ec90cfea21c3de8ecf9d406220a3ff7b6da73" +
	"c8c4efeffa33062499adf61ba32d230f45d4d16c59476477a653e4a52aaf62dc" +
	"c1897bfb9bfd03efff932c1cd59c6e91968b19bdc82531288a1acb3a92dffa09" +
	"7b0d9154addc3379851a41a25746ae6d23bb2480b155b68b297fd9d22c2e5fbc" +
	"cfd1bed01575f63a0b966da3b19271a22c224c9dde51b0247b3a2bcac44763b4" +
	"47c13c0cece78000a0d191f9379af2be052f21a9015a7220fb4c914499c9c676" +
	"f4feba3bf9191292b92a82d7c4d0ec2562ab1761b595807b70359afb889ac1a3" +
	"d314c00257684514d44c242994aec3a5ef4cc14e4173c2e324f624e2fd4dec15" +
	"48e25db7c336ad807b446f9ab8ab32be23806c71a3bcbf77f0f131b572021e2a" +
	"c9460e9f25aa142dc52b2dba54c10e8513c79b87a020a3013945a33aefb3894c" +
	"54cf1b98055f373e877b10a6830d32bf4038420e8627677a4b5b57a5a73c039f" +
	"76e505c656989efff5284b901125cb78"

var xorTable []byte

// debugIPC 打开后会把收到的 IPC 包打到控制台（设置 SKYLOGIN_DEBUG=1）。
var debugIPC = os.Getenv("SKYLOGIN_DEBUG") != ""

func init() {
	xorTable, _ = hex.DecodeString(xorHex)
}

func xorBytes(data []byte) []byte {
	out := make([]byte, len(data))
	for i, v := range data {
		out[i] = v ^ xorTable[i%len(xorTable)]
	}
	return out
}

const (
	wmCopyData      = 0x004A
	wmDestroy       = 0x0010
	wmClose         = 0x0010
	wmNCDestroy     = 0x0082
	hwndMessage     = ^uintptr(2) // -3
	smtoAbortIfHung = 0x0002
	className       = "LHMW_FG_Main"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW            = user32.NewProc("RegisterClassExW")
	procCreateWindowExW             = user32.NewProc("CreateWindowExW")
	procDefWindowProcW              = user32.NewProc("DefWindowProcW")
	procGetMessageW                 = user32.NewProc("GetMessageW")
	procTranslateMessage            = user32.NewProc("TranslateMessage")
	procDispatchMessageW            = user32.NewProc("DispatchMessageW")
	procPostQuitMessage             = user32.NewProc("PostQuitMessage")
	procFindWindowExW               = user32.NewProc("FindWindowExW")
	procChangeWindowMessageFilterEx = user32.NewProc("ChangeWindowMessageFilterEx")
	procGetClassNameW               = user32.NewProc("GetClassNameW")
	procGetWindowThreadProcessId    = user32.NewProc("GetWindowThreadProcessId")
	procSendMessageTimeoutW         = user32.NewProc("SendMessageTimeoutW")
	procDestroyWindow               = user32.NewProc("DestroyWindow")
	procPostMessageW                = user32.NewProc("PostMessageW")
	procGetModuleHandleW            = kernel32.NewProc("GetModuleHandleW")
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type msgT struct {
	hwnd    windows.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
	private uint32
}

type copyDataStruct struct {
	dwData uintptr
	cbData uint32
	lpData uintptr
}

// TicketRequest 是游戏发来的取票请求。
type TicketRequest struct {
	SenderHwnd uintptr
	ProcessID  uint32
	GameID     string
	Instance   int
	Identifier []byte
}

// Host 是冒充发烧平台的 IPC 宿主。
type Host struct {
	cls      string
	mu       sync.Mutex
	hwnd     windows.Handle
	proc     uintptr
	ready    chan error
	onTicket func(TicketRequest) bool

	stopOnce sync.Once
}

var clientClassRe = regexp.MustCompile(`^LHMW_FG_clientFGp(.+)_i(\d+)$`)

// NewHost 创建宿主；onTicket 返回 true 表示已受理。
func NewHost(onTicket func(TicketRequest) bool) *Host {
	return &Host{cls: className, onTicket: onTicket, ready: make(chan error, 1)}
}

// NewHostWithClass 使用自定义窗口类名（仅用于自检，避免与真实平台冲突）。
func NewHostWithClass(cls string, onTicket func(TicketRequest) bool) *Host {
	return &Host{cls: cls, onTicket: onTicket, ready: make(chan error, 1)}
}

// Existing 检查是否已有发烧平台在运行。
func Existing() (windows.Handle, bool) {
	return existingFor(className)
}

func existingFor(cls string) (windows.Handle, bool) {
	clsPtr, _ := windows.UTF16PtrFromString(cls)
	h, _, _ := procFindWindowExW.Call(hwndMessage, 0, uintptr(unsafe.Pointer(clsPtr)), uintptr(unsafe.Pointer(clsPtr)))
	if h != 0 {
		return windows.Handle(h), true
	}
	return 0, false
}

// 窗口类只能注册一次（同名再注册会返回 1410），而窗口的消息永远发往
// **注册时那个** wndproc。所以这里把 wndproc 做成进程级单例，
// 由它把消息转发给"当前宿主"——否则第二次启动时消息会打到已经停掉的旧宿主上，
// 表现就是「收到了取票请求，但回票时报宿主未启动」。
var (
	procOnce   sync.Once
	globalProc uintptr
	activeHost atomic.Pointer[Host]
)

func hostWndProc(hwnd windows.Handle, message uint32, wParam, lParam uintptr) uintptr {
	h := activeHost.Load()
	switch message {
	case wmCopyData:
		if h != nil && h.onTicket != nil {
			cds := (*copyDataStruct)(unsafe.Pointer(lParam))
			if cds.cbData >= 12 && cds.lpData != 0 {
				raw := unsafe.Slice((*byte)(unsafe.Pointer(cds.lpData)), cds.cbData)
				h.handlePacket(windows.Handle(wParam), raw)
			}
		}
		return 1
	case wmClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmNCDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}

// Start 在独立线程上建窗口并跑消息循环。
func (h *Host) Start() error {
	if _, ok := existingFor(h.cls); ok {
		return errors.New("发烧平台正在运行，不能同时启用平台模拟")
	}
	activeHost.Store(h)
	go func() {
		runtime.LockOSThread()
		if err := h.run(); err != nil {
			h.ready <- err
		}
	}()
	return <-h.ready
}

func (h *Host) run() error {
	instance, _, _ := procGetModuleHandleW.Call(0)
	clsName, _ := windows.UTF16PtrFromString(h.cls)

	procOnce.Do(func() { globalProc = windows.NewCallback(hostWndProc) })
	wc := wndClassEx{
		style:         0,
		lpfnWndProc:   globalProc,
		hInstance:     windows.Handle(instance),
		lpszClassName: clsName,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		// 1410 = ERROR_CLASS_ALREADY_EXISTS
		if e, ok := err.(windows.Errno); !ok || e != 1410 {
			return fmt.Errorf("RegisterClassExW 失败: %v", err)
		}
	}

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(clsName)),
		uintptr(unsafe.Pointer(clsName)),
		0, 0, 0, 0, 0,
		hwndMessage, 0, instance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW 失败: %v", err)
	}
	h.mu.Lock()
	h.hwnd = windows.Handle(hwnd)
	h.mu.Unlock()

	procChangeWindowMessageFilterEx.Call(hwnd, wmCopyData, 1, 0)
	h.ready <- nil

	var m msgT
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	h.mu.Lock()
	h.hwnd = 0
	h.mu.Unlock()
	if activeHost.Load() == h {
		activeHost.CompareAndSwap(h, nil)
	}
	return nil
}

func (h *Host) handlePacket(sender windows.Handle, raw []byte) {
	instance, identifier, ok := ParseTicketRequest(raw)
	if !ok {
		return
	}

	cls := WindowClass(sender)
	if debugIPC {
		fmt.Printf("[ipc] op13 from class=%q hwnd=0x%x\n", cls, uintptr(sender))
	}
	m := clientClassRe.FindStringSubmatch(cls)
	if m == nil {
		return
	}
	gid := m[1]
	inst, _ := strconv.Atoi(m[2])
	if inst != instance {
		return
	}
	pid := WindowPID(sender)
	h.onTicket(TicketRequest{
		SenderHwnd: uintptr(sender),
		ProcessID:  pid,
		GameID:     gid,
		Instance:   inst,
		Identifier: identifier,
	})
}

// ParseTicketRequest 解析 op13 取票包，返回 (instance, identifier, ok)。
func ParseTicketRequest(raw []byte) (int, []byte, bool) {
	if len(raw) < 12 {
		return 0, nil, false
	}
	opcode := int32(binary.LittleEndian.Uint32(raw[4:8]))
	if opcode != 13 {
		return 0, nil, false
	}
	payload := xorBytes(raw[12:])
	if len(payload) < 260 {
		return 0, nil, false
	}
	instance := int(int32(binary.LittleEndian.Uint32(payload[0:4]))) - 101
	identifier := make([]byte, 256)
	copy(identifier, payload[4:260])
	return instance, identifier, true
}

// BuildTicketPacket 构造 op14 票据包（供测试与自检使用）。
func BuildTicketPacket(loginChannel, ticket string) []byte {
	body, _ := json.Marshal(map[string]string{
		"login_channel": loginChannel,
		"ticket":        ticket,
	})
	packet := make([]byte, 12+len(body))
	binary.LittleEndian.PutUint32(packet[0:4], uint32(len(body)))
	binary.LittleEndian.PutUint32(packet[4:8], 14)
	binary.LittleEndian.PutUint32(packet[8:12], 0)
	copy(packet[12:], xorBytes(body))
	return packet
}

// WindowClass 取窗口类名。
func WindowClass(h windows.Handle) string {
	buf := make([]uint16, 256)
	ret, _, _ := procGetClassNameW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if ret == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:ret])
}

// WindowPID 取窗口所属进程 ID。
func WindowPID(h windows.Handle) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(uintptr(h), uintptr(unsafe.Pointer(&pid)))
	return pid
}

// SendTicket 回票据给游戏（op14）。
func (h *Host) SendTicket(target windows.Handle, loginChannel, ticket string) error {
	h.mu.Lock()
	self := h.hwnd
	h.mu.Unlock()
	if self == 0 {
		return errors.New("IPC 宿主未启动")
	}
	body, _ := json.Marshal(map[string]string{
		"login_channel": loginChannel,
		"ticket":        ticket,
	})
	packet := make([]byte, 12+len(body))
	binary.LittleEndian.PutUint32(packet[0:4], uint32(len(body)))
	binary.LittleEndian.PutUint32(packet[4:8], 14)
	binary.LittleEndian.PutUint32(packet[8:12], 0)
	copy(packet[12:], xorBytes(body))

	cds := copyDataStruct{
		dwData: 14,
		cbData: uint32(len(packet)),
		lpData: uintptr(unsafe.Pointer(&packet[0])),
	}
	var result uintptr
	ret, _, _ := procSendMessageTimeoutW.Call(
		uintptr(target), wmCopyData, uintptr(self), uintptr(unsafe.Pointer(&cds)),
		smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)),
	)
	if ret == 0 {
		return errors.New("SendMessageTimeoutW 失败（游戏可能已退出）")
	}
	return nil
}

// Stop 关闭宿主窗口。
func (h *Host) Stop() {
	h.stopOnce.Do(func() {
		h.mu.Lock()
		hwnd := h.hwnd
		h.mu.Unlock()
		if hwnd == 0 {
			return
		}
		procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)

		// 等窗口真正销毁再返回：下一次 Start 会用同名窗口类，
		// 旧窗口还在的话会误判成"发烧平台正在运行"。
		for i := 0; i < 50; i++ {
			if h.HWND() == 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}

// HWND 返回宿主窗口句柄。
func (h *Host) HWND() windows.Handle {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hwnd
}

// ExistingFor 检查指定类名的窗口是否存在（自检用）。
func ExistingFor(cls string) (windows.Handle, bool) { return existingFor(cls) }

// DefaultClassName 是真实发烧平台使用的窗口类名。
const DefaultClassName = className

// 记录非 op13 的包，方便排查（正式使用只写日志，不做别的）。
var packetLogHook func(op int32, body []byte)
var packetLogMu sync.Mutex

func logPacket(op int32, body []byte) {
	packetLogMu.Lock()
	h := packetLogHook
	packetLogMu.Unlock()
	if h != nil {
		h(op, body)
	}
}

// SetPacketLogHook 注册一个观察回调（可为 nil）。
func SetPacketLogHook(fn func(op int32, body []byte)) {
	packetLogMu.Lock()
	packetLogHook = fn
	packetLogMu.Unlock()
}
