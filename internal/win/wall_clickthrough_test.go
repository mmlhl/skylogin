package win

// 真机自检：监控墙的鼠标穿透开关。
//
// 普通的 go test 会跳过（不依赖桌面上的墙）；手动验证：
//
//	$env:WALL_PROBE="1"; go test -count=1 ./internal/win/ -run TestProbeWallClickThrough -v
//
// 需要先打开监控墙（2 个以上客户端运行时自动开，或界面里手动开）。
// 注意：墙属于正在运行的主程序，不是测试进程自己的窗口，所以这里按类名
// 找到桌面上那个墙再操作。

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func TestProbeWallClickThrough(t *testing.T) {
	if os.Getenv("WALL_PROBE") == "" {
		t.Skip("设置 WALL_PROBE=1 才跑监控墙自检")
	}
	name, _ := syscall.UTF16PtrFromString("SkyLoginWallWnd")
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(name)), 0)
	if h == 0 {
		t.Skip("监控墙没开")
	}

	ex := func() uintptr { v, _, _ := procGetWindowLongW.Call(h, gwlExStyle); return v }

	before := ex()
	if before&wsExTransparent != 0 {
		t.Fatalf("初始状态不该是穿透的：0x%X", before)
	}
	wasLayered := before&wsExLayered != 0

	setWindowClickThrough(h, true)
	after := ex()
	if after&wsExTransparent == 0 {
		t.Fatalf("开启后应该有 WS_EX_TRANSPARENT：0x%X", after)
	}
	// 只加 TRANSPARENT 不够：必须同时是分层窗口，命中测试才会跳过它
	if after&wsExLayered == 0 {
		t.Fatalf("开启后应该同时带 WS_EX_LAYERED：0x%X", after)
	}

	setWindowClickThrough(h, false)
	back := ex()
	if back&wsExTransparent != 0 {
		t.Fatalf("还原后不该还带穿透：0x%X", back)
	}
	if !wasLayered && back&wsExLayered != 0 {
		t.Fatalf("LAYERED 是我们临时加的，还原时要摘掉：0x%X", back)
	}
	t.Logf("穿透开关正常：0x%X -> 0x%X -> 0x%X", before, after, back)
}
