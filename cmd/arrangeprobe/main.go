// arrangerange 探针：量正在运行的客户端窗口，检查堆叠，然后实际排一次。
//
// 用法：go run ./cmd/arrangeprobe [cols rows]
// 不带参数默认 2×2。探针会：
//  1. 先在"无 DPI 感知"下量屏幕（拿到的值除以物理值 = 系统缩放倍数）
//  2. 切 permonitorv2 感知，量物理屏幕和工作区
//  3. 枚举 Sky.exe 进程的主窗口：pid / 标题 / 矩形，两两查重叠
//  4. 调 win.ArrangeByGrid 实际排列，1 秒后再量一次，看二次校准的效果
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"skylogin/internal/win"
)

var (
	user32      = windows.NewLazySystemDLL("user32.dll")
	procMetrics = user32.NewProc("GetSystemMetrics")
	procSPI     = user32.NewProc("SystemParametersInfoW")
	procTextW   = user32.NewProc("GetWindowTextW")
	procRect    = user32.NewProc("GetWindowRect")
)

type rect struct{ left, top, right, bottom int32 }

func setDPIAwareV2() {
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4
	user32.NewProc("SetProcessDpiAwarenessContext").Call(^uintptr(3))
}

func metrics() (int, int) {
	w, _, _ := procMetrics.Call(0)
	h, _, _ := procMetrics.Call(1)
	return int(w), int(h)
}

func workArea() rect {
	var r rect
	procSPI.Call(0x0048, 0, uintptr(unsafe.Pointer(&r)), 0)
	return r
}

func pidsOf(exe string) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var pids []uint32
	if err := windows.Process32First(snap, &e); err != nil {
		return nil
	}
	for {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), exe) {
			pids = append(pids, e.ProcessID)
		}
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}
	return pids
}

func title(h uintptr) string {
	buf := make([]uint16, 128)
	n, _, _ := procTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 128)
	return syscall.UTF16ToString(buf[:n])
}

func getRect(h uintptr) (rect, bool) {
	var r rect
	if ret, _, _ := procRect.Call(h, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return r, false
	}
	return r, r.right > r.left && r.bottom > r.top
}

func dump(tag string, cells []win.ClientCell, handles []uintptr) {
	fmt.Printf("── %s ──\n", tag)
	rs := make([]rect, len(cells))
	for i, c := range cells {
		h := handles[i]
		if h == 0 {
			fmt.Printf("  pid=%d 没有可见主窗口\n", c.PID)
			continue
		}
		r, ok := getRect(h)
		rs[i] = r
		if !ok {
			fmt.Printf("  pid=%d hwnd=%#x 标题=%q 矩形无效\n", c.PID, h, title(h))
			continue
		}
		fmt.Printf("  pid=%d hwnd=%#x 矩形=(%d,%d)-(%d,%d) 大小=%dx%d 标题=%q\n",
			c.PID, h, r.left, r.top, r.right, r.bottom, r.right-r.left, r.bottom-r.top, title(h))
	}
	// 两两查重叠
	for i := 0; i < len(cells); i++ {
		for j := i + 1; j < len(cells); j++ {
			a, b := rs[i], rs[j]
			if a.right == 0 || b.right == 0 {
				continue
			}
			ow := min(a.right, b.right) - max(a.left, b.left)
			oh := min(a.bottom, b.bottom) - max(a.top, b.top)
			if ow > 0 && oh > 0 {
				fmt.Printf("  !! 堆叠: #%d 和 #%d 重叠 %dx%d\n", i+1, j+1, ow, oh)
			}
		}
	}
}

func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func main() {
	cols, rows := 2, 2
	if len(osArgs()) >= 3 {
		fmt.Sscanf(osArgs()[1]+" "+osArgs()[2], "%d %d", &cols, &rows)
	}

	// 1) 无 DPI 感知下量屏幕
	w0, h0 := metrics()
	fmt.Printf("无 DPI 感知: 屏幕=%dx%d\n", w0, h0)

	// 2) 切 permonitorv2，再量（物理值）
	setDPIAwareV2()
	time.Sleep(200 * time.Millisecond)
	w1, h1 := metrics()
	wa := workArea()
	fmt.Printf("permonitorv2: 屏幕=%dx%d 工作区=(%d,%d)-(%d,%d)\n", w1, h1, wa.left, wa.top, wa.right, wa.bottom)
	if w0 != w1 {
		fmt.Printf("→ 系统缩放 %.0f%%（缩放会影响非 DPI 感知进程拿到的坐标）\n", float64(w1)/float64(w0)*100)
	} else {
		fmt.Println("→ 无缩放差异（100%）")
	}

	// 3) 找目标进程的窗口（默认 Sky.exe，可用 -name 换成替身进程验证）
	exeName := "Sky.exe"
	args := osArgs()
	for i, a := range args {
		if a == "-name" && i+1 < len(args) {
			exeName = args[i+1]
		}
	}
	pids := pidsOf(exeName)
	if len(pids) == 0 {
		fmt.Println("没找到", exeName, "进程")
		return
	}
	var cells []win.ClientCell
	handles := make([]uintptr, 0)
	for _, pid := range pids {
		h := win.MainWindowOfPID(pid)
		cells = append(cells, win.ClientCell{PID: pid})
		handles = append(handles, h)
	}
	dump("排列前", cells, handles)

	// 4) 实际排一次
	if len(cells) > cols*rows {
		cols, rows = 2, (len(cells)+1)/2
	}
	moved := win.ArrangeByGrid(cells, cols, rows, 0, 0)
	fmt.Printf("ArrangeByGrid(%d×%d): 移动了 %d 个\n", cols, rows, moved)

	time.Sleep(1200 * time.Millisecond)
	dump("排列后（含二次校准）", cells, handles)
}

func osArgs() []string { return os.Args }
