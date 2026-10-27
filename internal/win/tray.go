package win

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	procRegisterCls   = user32dll.NewProc("RegisterClassExW")
	procCreateWinEx   = user32dll.NewProc("CreateWindowExW")
	procDefWinProc    = user32dll.NewProc("DefWindowProcW")
	procDestroyWin    = user32dll.NewProc("DestroyWindow")
	procGetMessage    = user32dll.NewProc("GetMessageW")
	procTranslateMsg  = user32dll.NewProc("TranslateMessage")
	procDispatchMsg   = user32dll.NewProc("DispatchMessageW")
	procPostQuitMsg   = user32dll.NewProc("PostQuitMessage")
	procCreatePopup   = user32dll.NewProc("CreatePopupMenu")
	procAppendMenu    = user32dll.NewProc("AppendMenuW")
	procTrackPopup    = user32dll.NewProc("TrackPopupMenu")
	procDestroyMenu   = user32dll.NewProc("DestroyMenu")
	procGetCursorPos  = user32dll.NewProc("GetCursorPos")
	procLoadIcon      = user32dll.NewProc("LoadIconW")
	procSendMessage   = user32dll.NewProc("SendMessageW")
	procGetModule     = kernel32dll.NewProc("GetModuleHandleW")
	procGetModuleFile = kernel32dll.NewProc("GetModuleFileNameW")
	procShellNotify   = shell32dll.NewProc("Shell_NotifyIconW")
	procExtractIcon   = shell32dll.NewProc("ExtractIconExW")
)

const (
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmCommand      = 0x0111
	wmRButtonUp    = 0x0205
	wmLButtonUp    = 0x0202
	wmTrayMsg      = 0x0400 + 1 // WM_APP + 1
	nimAdd         = 0
	nimDelete      = 2
	nifMessage     = 0x00000001
	nifIcon        = 0x00000002
	nifTip         = 0x00000004
	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	tpmReturnCmd   = 0x0100
	tpmRightButton = 0x0002
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         guid
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type trayMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

// TrayOptions 是托盘菜单的回调。
type TrayOptions struct {
	Title    string
	OnShow   func()
	OnLaunch func()
	OnQuit   func()
}

const (
	menuShow   = 1001
	menuLaunch = 1002
	menuQuit   = 1003
)

var (
	trayOpts TrayOptions
	trayNID  notifyIconData
)

// StartTray 在后台线程创建托盘图标与菜单，立即返回。
func StartTray(opts TrayOptions) {
	trayOpts = opts
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		runTray()
	}()
}

func runTray() {
	hInst, _, _ := procGetModule.Call(0)
	clsName, _ := syscall.UTF16PtrFromString("SkyLoginTrayWnd")

	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   syscall.NewCallback(trayWndProc),
		HInstance:     hInst,
		LpszClassName: clsName,
	}
	if r, _, _ := procRegisterCls.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return
	}
	hwnd, _, _ := procCreateWinEx.Call(0, 0, uintptr(unsafe.Pointer(clsName)), 0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		return
	}

	trayNID = notifyIconData{
		HWnd:             hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayMsg,
		HIcon:            trayIcon(hInst),
	}
	trayNID.CbSize = uint32(unsafe.Sizeof(trayNID))
	tip := syscall.StringToUTF16(trayOpts.Title)
	copy(trayNID.SzTip[:], tip[:minInt(len(tip), len(trayNID.SzTip)-1)])

	procShellNotify.Call(nimAdd, uintptr(unsafe.Pointer(&trayNID)))

	var msg trayMsg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
	}
	procShellNotify.Call(nimDelete, uintptr(unsafe.Pointer(&trayNID)))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// trayIcon 优先用 exe 内嵌图标，取不到就用系统默认图标。
func trayIcon(hInst uintptr) uintptr {
	buf := make([]uint16, 1024)
	if n, _, _ := procGetModuleFile.Call(0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n > 0 && n < uintptr(len(buf)) {
		var big, small uintptr
		c, _, _ := procExtractIcon.Call(uintptr(unsafe.Pointer(&buf[0])), 0,
			uintptr(unsafe.Pointer(&big)), uintptr(unsafe.Pointer(&small)), 1)
		if c > 0 && small != 0 {
			return small
		}
	}
	icon, _, _ := procLoadIcon.Call(hInst, 32512) // IDI_APPLICATION
	return icon
}

func trayWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmTrayMsg:
		switch lparam & 0xFFFF {
		case wmRButtonUp:
			showTrayMenu(hwnd)
		case wmLButtonUp:
			if trayOpts.OnShow != nil {
				trayOpts.OnShow()
			}
		}
		return 0
	case wmCommand:
		switch wparam & 0xFFFF {
		case menuShow:
			if trayOpts.OnShow != nil {
				trayOpts.OnShow()
			}
		case menuLaunch:
			if trayOpts.OnLaunch != nil {
				trayOpts.OnLaunch()
			}
		case menuQuit:
			if trayOpts.OnQuit != nil {
				trayOpts.OnQuit()
			}
		}
		return 0
	case wmClose:
		procDestroyWin.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMsg.Call(0)
		return 0
	}
	r, _, _ := procDefWinProc.Call(hwnd, msg, wparam, lparam)
	return r
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopup.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	show, _ := syscall.UTF16PtrFromString("显示主界面")
	launch, _ := syscall.UTF16PtrFromString("启动光遇")
	quit, _ := syscall.UTF16PtrFromString("退出")
	procAppendMenu.Call(menu, mfString, menuShow, uintptr(unsafe.Pointer(show)))
	procAppendMenu.Call(menu, mfString, menuLaunch, uintptr(unsafe.Pointer(launch)))
	procAppendMenu.Call(menu, mfSeparator, 0, 0)
	procAppendMenu.Call(menu, mfString, menuQuit, uintptr(unsafe.Pointer(quit)))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForeground.Call(hwnd)
	procTrackPopup.Call(menu, tpmReturnCmd|tpmRightButton, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	procSendMessage.Call(hwnd, 0, 0, 0) // WM_NULL，让菜单正常收起
}

// SetWindowIcon 把 exe 内嵌的第一个图标设为窗口图标。
func SetWindowIcon(hwnd uintptr, exe string) {
	if hwnd == 0 || exe == "" {
		return
	}
	path, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return
	}
	var big, small uintptr
	n, _, _ := procExtractIcon.Call(uintptr(unsafe.Pointer(path)), 0,
		uintptr(unsafe.Pointer(&big)), uintptr(unsafe.Pointer(&small)), 1)
	if n == 0 || n == 0xFFFFFFFF {
		return
	}
	if big != 0 {
		procSendMessage.Call(hwnd, 0x0080, 1, big) // WM_SETICON, ICON_BIG
	}
	if small != 0 {
		procSendMessage.Call(hwnd, 0x0080, 0, small) // WM_SETICON, ICON_SMALL
	}
}
