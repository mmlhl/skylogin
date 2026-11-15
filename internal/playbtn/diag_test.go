package playbtn

// 诊断用：对着真机窗口走一遍"激活 → 命中测试"，只打印不点击。
//
//	$env:PLAYBTN_DIAG="1"; go test -count=1 ./internal/playbtn/ -run TestDiagHit -v

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"

	"skylogin/internal/win"
)

func TestDiagHit(t *testing.T) {
	if os.Getenv("PLAYBTN_DIAG") == "" {
		t.Skip("设置 PLAYBTN_DIAG=1 才跑")
	}
	probe := user32.NewProc("GetSystemMetrics")
	w, _, _ := probe.Call(0)
	h, _, _ := probe.Call(1)
	fmt.Printf("屏幕(GetSystemMetrics): %dx%d\n", w, h)
	for _, pt := range [][2]int{{684, 374}, {300, 300}, {1000, 700}} {
		fmt.Printf("  点(%d,%d) 不做 DPI 切换命中=%s\n", pt[0], pt[1], hitDetail(pt[0], pt[1]))
	}
	for _, pid := range skyPids(t) {
		hwnd := win.MainWindowOfPID(pid)
		if hwnd == 0 {
			continue
		}
		img, err := capture(hwnd)
		if err != nil || img == nil {
			fmt.Printf("pid=%d 抓图失败: %v\n", pid, err)
			continue
		}
		x, y, ok := locate(img)
		if !ok {
			fmt.Printf("pid=%d 没有播放按钮\n", pid)
			continue
		}
		sx, sy := screenPointOf(hwnd, x, y)
		before := pointHitsProcess(pid, sx, sy)

		activate(hwnd)
		after := pointHitsProcess(pid, sx, sy)

		fmt.Printf("pid=%d 按钮=窗内(%d,%d) 屏幕(%d,%d) 激活前命中=%v 激活后命中=%v 前台是本窗=%v 命中详情=%s\n",
			pid, x, y, sx, sy, before, after, isForeground(hwnd), hitDetail(sx, sy))
	}
}

// hitDetail 打印该点真正的落点窗口信息。
func hitDetail(x, y int) string {
	var out string
	physicalPixels(func() {
		h := windowFromPoint(x, y)
		if h == 0 {
			out = "WindowFromPoint=0"
			return
		}
		var wpid uint32
		procGetWindowThreadPID.Call(h, uintptr(unsafe.Pointer(&wpid)))
		buf := make([]uint16, 128)
		procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 128)
		out = fmt.Sprintf("hwnd=0x%X pid=%d title=%q", h, wpid, syscall.UTF16ToString(buf))
	})
	return out
}

// isForeground 仅诊断用。
func isForeground(hwnd uintptr) bool {
	procGetForegroundWindow := user32.NewProc("GetForegroundWindow")
	fg, _, _ := procGetForegroundWindow.Call()
	return fg == hwnd
}
