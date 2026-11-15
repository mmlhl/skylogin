package win

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	wallExLayered         = 0x00080000
	wallExTransparent     = 0x00000020
	wallBackgroundStyle   = wallExLayered | wallExTransparent | wsExNoActivate
	wallMinimizeStart     = 0x0016
	wallMinimizeEnd       = 0x0017
	wmWallBackgroundEvent = 0x0400 + 3
)

var (
	wallSetLayered         = user32dll.NewProc("SetLayeredWindowAttributes")
	wallGetLayered         = user32dll.NewProc("GetLayeredWindowAttributes")
	wallUnhookEvent        = user32dll.NewProc("UnhookWinEvent")
	wallSetPlacement       = user32dll.NewProc("SetWindowPlacement")
	wallBackgroundMu       sync.Mutex
	wallBackgroundOpMu     sync.Mutex
	wallBackgroundWatching atomic.Bool
	wallBackground         = make(map[uintptr]wallBackgroundWindow)
)

type wallBackgroundWindow struct {
	pid               uint32
	exStyle           uintptr
	color, layerFlags uint32
	alpha             byte
	placement         windowPlacement
	foreground        uintptr
	restoring         bool
}

func wallWindowPID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// backgroundWindowState 同时验证进程身份，不能把旧 HWND 的恢复操作施加给新窗口。
func backgroundWindowState(hwnd uintptr) (wallBackgroundWindow, bool) {
	wallBackgroundMu.Lock()
	defer wallBackgroundMu.Unlock()
	s, ok := wallBackground[hwnd]
	if ok && wallWindowPID(hwnd) != s.pid {
		delete(wallBackground, hwnd)
		return wallBackgroundWindow{}, false
	}
	return s, ok
}

// backgroundWallWindow 在用户最小化时仅执行一次：先全透明并穿透鼠标，再恢复渲染。
// 不移出屏幕；屏幕外窗口的 DWM 表面在实测中会被裁剪并停帧。
func backgroundWallWindow(hwnd uintptr, pid uint32) bool {
	wallBackgroundOpMu.Lock()
	defer wallBackgroundOpMu.Unlock()
	if !wallBackgroundWatching.Load() {
		return false
	}
	if pid == 0 || wallWindowPID(hwnd) != pid {
		return false
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic == 0 {
		return false
	}
	s, exists := backgroundWindowState(hwnd)
	if !exists {
		s.pid = pid
		s.exStyle, _, _ = procGetWindowLongW.Call(hwnd, gwlExStyle)
		s.placement.length = uint32(unsafe.Sizeof(s.placement))
		if ok, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&s.placement))); ok == 0 {
			return false
		}
		// 已有的分层窗口必须能完整读回透明属性，不能破坏应用自己的逐像素合成方式。
		if s.exStyle&wallExLayered != 0 {
			if ok, _, _ := wallGetLayered.Call(hwnd, uintptr(unsafe.Pointer(&s.color)), uintptr(unsafe.Pointer(&s.alpha)), uintptr(unsafe.Pointer(&s.layerFlags))); ok == 0 {
				return false
			}
		}
	}
	foreground, _ := ForegroundWindow()
	if foreground != hwnd {
		s.foreground = foreground
	}
	s.restoring = true
	procSetWindowLongPtrW.Call(hwnd, gwlExStyle, s.exStyle|wallBackgroundStyle)
	style, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
	if style&wallBackgroundStyle != wallBackgroundStyle {
		restoreBackgroundStyle(hwnd, s)
		return false
	}
	if ok, _, _ := wallSetLayered.Call(hwnd, 0, 0, 2); ok == 0 {
		restoreBackgroundStyle(hwnd, s)
		return false
	}
	wallBackgroundMu.Lock()
	wallBackground[hwnd] = s
	wallBackgroundMu.Unlock()
	// 在透明状态下完成恢复并核对焦点；事件先投墙任务，不把“即将恢复”误当成完成。
	p := s.placement
	p.showCmd = 4
	if ok, _, _ := wallSetPlacement.Call(hwnd, uintptr(unsafe.Pointer(&p))); ok == 0 {
		wallBackgroundMu.Lock()
		delete(wallBackground, hwnd)
		wallBackgroundMu.Unlock()
		restoreBackgroundStyle(hwnd, s)
		return false
	}
	if foreground, _ := ForegroundWindow(); foreground == hwnd && s.foreground != 0 {
		procSetForeground.Call(s.foreground)
	}
	s.restoring = false
	if current, _ := ForegroundWindow(); current == hwnd {
		// 前台锁可能拒绝交还；保持透明，不能把本次自恢复当成用户主动切回。
		s.restoring = true
	}
	wallBackgroundMu.Lock()
	wallBackground[hwnd] = s
	wallBackgroundMu.Unlock()
	return true
}

func restoreBackgroundStyle(hwnd uintptr, s wallBackgroundWindow) {
	if s.exStyle&wallExLayered != 0 {
		wallSetLayered.Call(hwnd, uintptr(s.color), uintptr(s.alpha), uintptr(s.layerFlags))
	}
	current, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
	procSetWindowLongPtrW.Call(hwnd, gwlExStyle, (current&^wallBackgroundStyle)|(s.exStyle&wallBackgroundStyle))
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, clientZRestoreFlags|swpNoZOrder|swpFrameChanged)
}

// revealBackgroundWindow 仅供用户点选、前台切换或手动重排调用，不由普通刷新触发。
func revealBackgroundWindow(hwnd uintptr) {
	wallBackgroundOpMu.Lock()
	defer wallBackgroundOpMu.Unlock()
	s, ok := backgroundWindowState(hwnd)
	if !ok {
		return
	}
	wallBackgroundMu.Lock()
	delete(wallBackground, hwnd)
	wallBackgroundMu.Unlock()
	restoreBackgroundStyle(hwnd, s)
}

func settleBackgroundWindow(hwnd uintptr) {
	wallBackgroundOpMu.Lock()
	defer wallBackgroundOpMu.Unlock()
	s, ok := backgroundWindowState(hwnd)
	if !ok || !s.restoring {
		return
	}
	if foreground, _ := ForegroundWindow(); foreground == hwnd && s.foreground != 0 {
		procSetForeground.Call(s.foreground)
	}
	if current, _ := ForegroundWindow(); current == hwnd {
		return
	}
	wallBackgroundMu.Lock()
	if current, found := wallBackground[hwnd]; found && current.pid == s.pid {
		current.restoring = false
		wallBackground[hwnd] = current
	}
	wallBackgroundMu.Unlock()
}

var wallBackgroundEventCallback = syscall.NewCallback(func(_ uintptr, event, hwnd, object, _child, _thread, _time uintptr) uintptr {
	if !wallBackgroundWatching.Load() || hwnd == 0 || object != 0 {
		return 0
	}
	if h := hwndOfWall(); h != 0 {
		procPostMessageW.Call(h, wmWallBackgroundEvent, event, hwnd)
	}
	return 0
})

func handleWallBackgroundEvent(event, hwnd uintptr) {
	if !wallBackgroundWatching.Load() {
		return
	}
	switch event {
	case wallMinimizeStart:
		for _, t := range wallTiles {
			if t.hwnd == hwnd && wallWindowPID(hwnd) == t.pid {
				if iconic, _, _ := procIsIconic.Call(hwnd); iconic == 0 {
					// START 是“即将最小化”，允许一次延后收尾，不设置周期窗口恢复任务。
					pid := t.pid
					time.AfterFunc(50*time.Millisecond, func() {
						postWallTask(func() {
							if wallBackgroundWatching.Load() && wallWindowPID(hwnd) == pid {
								for _, current := range wallTiles {
									if current.hwnd == hwnd && current.pid == pid {
										backgroundWallWindow(hwnd, pid)
										break
									}
								}
							}
						})
					})
				} else if !backgroundWallWindow(hwnd, t.pid) {
					Diag(fmt.Sprintf("监控墙：pid=%d 后台收起失败，保留系统最小化", t.pid))
				}
				break
			}
		}
	case wallMinimizeEnd:
		settleBackgroundWindow(hwnd)
	case EvSystemForeground:
		if current, _ := ForegroundWindow(); current != hwnd {
			return
		}
		// 前台已经交给别窗后，完成此前可能被前台锁拒绝的自恢复阶段。
		wallBackgroundMu.Lock()
		for h, state := range wallBackground {
			if h != hwnd && state.restoring {
				state.restoring = false
				wallBackground[h] = state
			}
		}
		wallBackgroundMu.Unlock()
		s, ok := backgroundWindowState(hwnd)
		if !ok {
			break
		}
		if current, _ := ForegroundWindow(); current != hwnd {
			break
		}
		if s.restoring {
			if s.foreground != 0 {
				procSetForeground.Call(s.foreground)
			}
		} else {
			revealBackgroundWindow(hwnd)
		}
	}
}

// startWallBackgroundWatch 与墙消息泵同线程安装，事件到来才动作，不增加窗口恢复轮询。
func startWallBackgroundWatch() func() {
	wallBackgroundWatching.Store(true)
	var hooks []uintptr
	for _, bounds := range [][2]uintptr{{wallMinimizeStart, wallMinimizeEnd}, {EvSystemForeground, EvSystemForeground}} {
		h, _, _ := procSetWinEventHook.Call(bounds[0], bounds[1], 0, wallBackgroundEventCallback, 0, 0, 0)
		if h != 0 {
			hooks = append(hooks, h)
		} else {
			Diag("监控墙：后台收起事件监听安装失败")
		}
	}
	return func() {
		wallBackgroundWatching.Store(false)
		for _, h := range hooks {
			wallUnhookEvent.Call(h)
		}
		cleanupWallBackground()
	}
}

// CleanupWallBackgroundPreview 在进程退出前同步清理，不能只投递关墙消息后立即 os.Exit。
func CleanupWallBackgroundPreview() {
	wallBackgroundWatching.Store(false)
	cleanupWallBackground()
}

// cleanupWallBackground 关墙时先回到真正最小化，再恢复透明属性，避免游戏突然铺回桌面。
func cleanupWallBackground() {
	wallBackgroundOpMu.Lock()
	defer wallBackgroundOpMu.Unlock()
	wallBackgroundMu.Lock()
	states := wallBackground
	wallBackground = make(map[uintptr]wallBackgroundWindow)
	wallBackgroundMu.Unlock()
	for hwnd, s := range states {
		if wallWindowPID(hwnd) != s.pid {
			continue
		}
		releaseBackgroundWindow(hwnd, s)
	}
}

func closeBackgroundWindow(hwnd uintptr) {
	wallBackgroundOpMu.Lock()
	defer wallBackgroundOpMu.Unlock()
	s, ok := backgroundWindowState(hwnd)
	if !ok {
		return
	}
	wallBackgroundMu.Lock()
	delete(wallBackground, hwnd)
	wallBackgroundMu.Unlock()
	releaseBackgroundWindow(hwnd, s)
}

func releaseBackgroundWindow(hwnd uintptr, s wallBackgroundWindow) {
	p := s.placement
	p.showCmd = 7
	ok, _, _ := wallSetPlacement.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	iconic, _, _ := procIsIconic.Call(hwnd)
	if ok == 0 || iconic == 0 {
		// 关闭清理不能只排队后立即退出；同步最小化确认，再恢复透明属性。
		procShowWindow.Call(hwnd, 7)
		if iconic, _, _ := procIsIconic.Call(hwnd); iconic == 0 {
			Diag(fmt.Sprintf("监控墙：pid=%d 关闭时最小化失败，恢复可见以免留下不可操作窗口", s.pid))
		}
	}
	restoreBackgroundStyle(hwnd, s)
}
