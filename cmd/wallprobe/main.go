// wallprobe 探针：用两个记事本窗口当替身客户端，验证监控墙。
//
// 用法：go run ./cmd/wallprobe 或构建后直接跑。流程：
//  1. 起两个记事本，拿到各自主窗口
//  2. 打开监控墙，把两个窗口当"客户端"喂进去（带标签）
//  3. 截全屏存 PNG（切物理像素，125% 缩放下不缺角）
//  4. 模拟鼠标点击第 1 个格子的中心，验证前台切到了替身一
//  5. 收尾：关墙、关记事本
//
// 输出全是判定行，非交互；截图路径见最后输出。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"skylogin/internal/win"
)

var u32 = syscall.NewLazyDLL("user32.dll")

var (
	procForeground = u32.NewProc("GetForegroundWindow")
	procCursorPos  = u32.NewProc("SetCursorPos")
	procMouseEvent = u32.NewProc("mouse_event")
	procFindWin    = u32.NewProc("FindWindowW")
	procWinLong    = u32.NewProc("GetWindowLongW")
)

const (
	mouseLeftDown = 0x0002
	mouseLeftUp   = 0x0004
)

func main() {
	// 探针线程切物理像素感知：墙是 PMv2 窗口，格子中心是物理坐标，
	// 不切的话 SetCursorPos 会被系统再放大 1.25 倍点到隔壁格子
	u32.NewProc("SetProcessDpiAwarenessContext").Call(^uintptr(3))

	shotPath := `E:\devtools-local\tools\wall-shot.png`

	// 1) 两个替身客户端
	p1 := startNotepad()
	p2 := startNotepad()
	defer func() {
		p1.Kill()
		p2.Kill()
	}()
	time.Sleep(1200 * time.Millisecond)
	h1 := win.MainWindowOfPID(uint32(p1.Pid))
	h2 := win.MainWindowOfPID(uint32(p2.Pid))
	if h1 == 0 || h2 == 0 {
		fmt.Println("FAIL 没找到记事本窗口", h1, h2)
		os.Exit(1)
	}

	// 2) 开墙 + 喂客户端
	if err := win.OpenWall(); err != nil {
		fmt.Println("FAIL 开墙:", err)
		os.Exit(1)
	}
	// 置顶断言：墙的扩展样式应带 WS_EX_TOPMOST(0x8)
	cls, _ := syscall.UTF16PtrFromString("SkyLoginWallWnd")
	fh, _, _ := procFindWin.Call(uintptr(unsafe.Pointer(cls)), 0)
	ex, _, _ := procWinLong.Call(fh, ^uintptr(19)) // GWL_EXSTYLE
	fmt.Printf("topmost: hwnd=%#x -> %v\n", fh, ex&0x8 != 0)
	win.WallUpdate([]win.WallClient{
		{Hwnd: h1, Label: "甲 · 替身一"},
		{Hwnd: h2, Label: "乙 · 替身二"},
	})
	time.Sleep(2500 * time.Millisecond)
	win.RaiseWall()
	time.Sleep(400 * time.Millisecond)

	// 3) 截图（独立 PowerShell 进程，切 DPI 感知后全屏截取）
	ps := fmt.Sprintf(`Add-Type -TypeDefinition 'using System.Runtime.InteropServices; public class W { [DllImport("user32.dll")] public static extern bool SetProcessDPIAware(); }'; [W]::SetProcessDPIAware(); Add-Type -AssemblyName System.Windows.Forms,System.Drawing; $b=[System.Windows.Forms.SystemInformation]::VirtualScreen; $bmp=New-Object System.Drawing.Bitmap $b.Width, $b.Height; $g=[System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($b.Left, $b.Top, 0, 0, $bmp.Size); $bmp.Save('%s'); 'saved'`, shotPath)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput()
	fmt.Printf("screenshot: %s (%v) %s\n", shotPath, err, string(out))

	// 4) 格子数量与点击切换
	_, _, ok0 := win.WallTileCenter(0)
	_, _, ok1 := win.WallTileCenter(1)
	_, _, ok2 := win.WallTileCenter(2)
	fmt.Printf("tiles: [0]=%v [1]=%v [2](应为false)=%v\n", ok0, ok1, ok2)

	cx, cy, ok := win.WallTileCenter(0)
	if !ok {
		fmt.Println("FAIL 拿不到格子 0 中心")
		os.Exit(1)
	}
	procCursorPos.Call(uintptr(cx), uintptr(cy))
	time.Sleep(150 * time.Millisecond)
	procMouseEvent.Call(mouseLeftDown, 0, 0, 0, 0)
	procMouseEvent.Call(mouseLeftUp, 0, 0, 0, 0)
	time.Sleep(900 * time.Millisecond)
	fg, _, _ := procForeground.Call()
	fmt.Printf("click tile0 @(%d,%d) → 前台=%#x 源=%#x 切换成功=%v\n", cx, cy, fg, h1, fg == h1)

	// 5) 收尾
	win.CloseWall()
	time.Sleep(400 * time.Millisecond)
	fmt.Println("done")
}

func startNotepad() *os.Process {
	cmd := exec.Command("notepad")
	if err := cmd.Start(); err != nil {
		fmt.Println("FAIL 起记事本:", err)
		os.Exit(1)
	}
	return cmd.Process
}
