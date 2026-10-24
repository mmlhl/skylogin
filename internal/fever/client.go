package fever

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Client 扮演"游戏侧"：建一个 LHMW_FG_clientFGp<gid>_i<n> 窗口，
// 向宿主发 op13 取票，并接收 op14 票据。仅用于自检与诊断。
type Client struct {
	cls  string
	mu   sync.Mutex
	hwnd windows.Handle
	proc uintptr
	got  chan TicketReply
}

// TicketReply 是收到的票据。
type TicketReply struct {
	LoginChannel string
	Ticket       string
}

func NewClient(cls string) *Client {
	return &Client{cls: cls, got: make(chan TicketReply, 1)}
}

func (c *Client) Start() error {
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		instance, _, _ := procGetModuleHandleW.Call(0)
		clsName, _ := windows.UTF16PtrFromString(c.cls)

		c.proc = windows.NewCallback(func(hwnd windows.Handle, message uint32, wParam, lParam uintptr) uintptr {
			switch message {
			case wmCopyData:
				cds := (*copyDataStruct)(unsafe.Pointer(lParam))
				if cds.cbData >= 12 && cds.lpData != 0 {
					raw := unsafe.Slice((*byte)(unsafe.Pointer(cds.lpData)), cds.cbData)
					opcode := binary.LittleEndian.Uint32(raw[4:8])
					if opcode == 14 {
						body := xorBytes(raw[12:])
						var m map[string]string
						if json.Unmarshal(body, &m) == nil {
							select {
							case c.got <- TicketReply{LoginChannel: m["login_channel"], Ticket: m["ticket"]}:
							default:
							}
						}
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
		})

		wc := wndClassEx{lpfnWndProc: c.proc, hInstance: windows.Handle(instance), lpszClassName: clsName}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if atom == 0 {
			if e, ok := err.(windows.Errno); !ok || e != 1410 {
				ready <- fmt.Errorf("客户端 RegisterClassExW 失败: %v", err)
				return
			}
		}
		hwnd, _, err := procCreateWindowExW.Call(0,
			uintptr(unsafe.Pointer(clsName)), uintptr(unsafe.Pointer(clsName)),
			0, 0, 0, 0, 0, hwndMessage, 0, instance, 0)
		if hwnd == 0 {
			ready <- fmt.Errorf("客户端 CreateWindowExW 失败: %v", err)
			return
		}
		c.mu.Lock()
		c.hwnd = windows.Handle(hwnd)
		c.mu.Unlock()
		procChangeWindowMessageFilterEx.Call(hwnd, wmCopyData, 1, 0)
		ready <- nil

		var m msgT
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(ret) <= 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		c.mu.Lock()
		c.hwnd = 0
		c.mu.Unlock()
	}()
	return <-ready
}

// SendTicketRequest 向指定类名的宿主窗口发送 op13 取票包。
func (c *Client) SendTicketRequest(hostClass string, instance int) error {
	host, ok := existingFor(hostClass)
	if !ok {
		return fmt.Errorf("找不到宿主窗口 %s", hostClass)
	}
	payload := make([]byte, 260)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(instance+101))
	for i := 4; i < 260; i++ {
		payload[i] = byte('A' + i%26)
	}
	packet := make([]byte, 12+len(payload))
	binary.LittleEndian.PutUint32(packet[0:4], uint32(len(payload)))
	binary.LittleEndian.PutUint32(packet[4:8], 13)
	copy(packet[12:], xorBytes(payload))

	c.mu.Lock()
	self := c.hwnd
	c.mu.Unlock()
	if self == 0 {
		return errors.New("客户端窗口未就绪")
	}
	cds := copyDataStruct{dwData: 13, cbData: uint32(len(packet)), lpData: uintptr(unsafe.Pointer(&packet[0]))}
	var result uintptr
	ret, _, _ := procSendMessageTimeoutW.Call(
		uintptr(host), wmCopyData, uintptr(self), uintptr(unsafe.Pointer(&cds)),
		smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)),
	)
	if ret == 0 {
		return errors.New("发送 op13 失败")
	}
	return nil
}

// WaitTicket 等待 op14 票据。
func (c *Client) WaitTicket(timeout time.Duration) (TicketReply, error) {
	select {
	case r := <-c.got:
		return r, nil
	case <-time.After(timeout):
		return TicketReply{}, errors.New("等待票据超时")
	}
}

func (c *Client) Stop() {
	c.mu.Lock()
	hwnd := c.hwnd
	c.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
	}
}

// SelfTest 端到端验证 IPC：宿主建窗口 → 客户端发 op13 → 宿主回 op14 → 客户端收到票据。
func SelfTest() error {
	const hostCls = "LHMW_FG_Main_SELFTEST"
	const ticket = "SELFTEST-TICKET-1234567890"

	var host *Host
	host = NewHostWithClass(hostCls, func(req TicketRequest) bool {
		if debugIPC {
			fmt.Printf("[selftest] 宿主收到取票请求 pid=%d game=%s\n", req.ProcessID, req.GameID)
		}
		go func() {
			time.Sleep(150 * time.Millisecond)
			_ = host.SendTicket(windows.Handle(req.SenderHwnd), "huawei", ticket)
		}()
		return true
	})
	if err := host.Start(); err != nil {
		return err
	}
	defer host.Stop()

	client := NewClient("LHMW_FG_clientFGpma75_i0")
	if err := client.Start(); err != nil {
		return err
	}
	defer client.Stop()

	time.Sleep(200 * time.Millisecond)
	if err := client.SendTicketRequest(hostCls, 0); err != nil {
		return err
	}
	reply, err := client.WaitTicket(5 * time.Second)
	if err != nil {
		return err
	}
	if reply.Ticket != ticket {
		return fmt.Errorf("票据不一致: %q", reply.Ticket)
	}
	if reply.LoginChannel != "huawei" {
		return fmt.Errorf("渠道不一致: %q", reply.LoginChannel)
	}
	return nil
}
