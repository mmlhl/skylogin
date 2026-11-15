package playbtn

// 真机自检：对着正在运行的游戏窗口跑一遍"抓图 + 定位"，只打印不点击。
//
// 默认跳过（普通 go test 不依赖游戏在跑）；手动验证时：
//
//	$env:PLAYBTN_PROBE="1"; go test ./internal/playbtn/ -run TestProbeLive -v
//
// 需要先让某个客户端停在登录界面（播放按钮可见），才能看到 ok=true。

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"skylogin/internal/win"
)

func TestProbeLive(t *testing.T) {
	if os.Getenv("PLAYBTN_PROBE") == "" {
		t.Skip("设置 PLAYBTN_PROBE=1 才跑真机自检")
	}
	pids := skyPids(t)
	if len(pids) == 0 {
		t.Skip("没有找到 sky.exe 进程")
	}
	for _, pid := range pids {
		hwnd := win.MainWindowOfPID(pid)
		if hwnd == 0 {
			fmt.Printf("pid=%d 没有主窗口\n", pid)
			continue
		}
		t0 := time.Now()
		img, err := capture(hwnd)
		t1 := time.Now()
		if err != nil {
			fmt.Printf("pid=%d 抓图失败: %v\n", pid, err)
			continue
		}
		x, y, ok := locate(img)
		t2 := time.Now()
		fmt.Printf("pid=%d hwnd=0x%X %dx%d 抓图=%v 定位=%v -> (%d,%d) ok=%v\n",
			pid, hwnd, img.w, img.h, t1.Sub(t0).Round(time.Millisecond),
			t2.Sub(t1).Round(time.Millisecond), x, y, ok)
	}
}

// TestProbeAttempt 真机点一次：走完整的 Enter 流程（会真的点击窗口）。
//
//	$env:PLAYBTN_PROBE="attempt"; $env:PLAYBTN_PID="1234"; go test ./internal/playbtn/ -run TestProbeAttempt -v
//
// PLAYBTN_PID 不填时自动挑第一个"还停在登录界面"的客户端。
func TestProbeAttempt(t *testing.T) {
	if os.Getenv("PLAYBTN_PROBE") != "attempt" {
		t.Skip("设置 PLAYBTN_PROBE=attempt 才会真点")
	}
	pid := uint32(0)
	fmt.Sscan(os.Getenv("PLAYBTN_PID"), &pid)
	if pid == 0 {
		for _, p := range skyPids(t) {
			h := win.MainWindowOfPID(p)
			if h == 0 {
				continue
			}
			img, err := capture(h)
			if err != nil || img == nil {
				continue
			}
			if _, _, ok := locate(img); ok {
				pid = p
				break
			}
		}
	}
	if pid == 0 {
		t.Skip("没有停在登录界面的客户端")
	}
	t.Logf("对 pid=%d 尝试进入", pid)
	ok := Enter(pid, time.Now().Add(8*time.Second), func(f string, a ...any) { t.Logf(f, a...) })
	if !ok {
		t.Fatal("8 秒内没能点进游戏")
	}
}

// skyPids 枚举当前 sky.exe 进程。
func skyPids(t *testing.T) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Logf("枚举进程失败: %v", err)
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []uint32
	if err := windows.Process32First(snap, &e); err != nil {
		t.Logf("Process32First 失败: %v", err)
		return nil
	}
	for {
		name := windows.UTF16ToString(e.ExeFile[:])
		if strings.EqualFold(name, "sky.exe") {
			out = append(out, e.ProcessID)
		}
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}
	if len(out) == 0 {
		t.Logf("进程快照里没有 sky.exe")
	}
	return out
}
