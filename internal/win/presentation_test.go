package win

import (
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestPresentationSwitchFastPathDoesNotWaitOrReapply(t *testing.T) {
	applied, pauses := 0, 0
	ready := false
	pause := func(time.Duration) { pauses++ }
	if !switchWindowPresentation(func() bool { return ready }, func() { applied++; ready = true }, pause) {
		t.Fatal("直接样式切换应成功")
	}
	if applied != 1 || pauses != 0 {
		t.Fatalf("成功后不能逐窗固定等待：apply=%d pause=%d", applied, pauses)
	}
	if !switchWindowPresentation(func() bool { return ready }, func() { applied++ }, pause) || applied != 1 || pauses != 0 {
		t.Fatal("已有目标状态应直接返回")
	}
}

func TestSevenPresentationSwitchesDoNotAccumulateFixedDelays(t *testing.T) {
	waited := time.Duration(0)
	for i := 0; i < 7; i++ {
		ready := false
		if !switchWindowPresentation(func() bool { return ready }, func() { ready = true }, func(d time.Duration) { waited += d }) {
			t.Fatal("窗口切换失败")
		}
	}
	if waited != 0 {
		t.Fatalf("七窗成功切换不应积累等待：%v", waited)
	}
}

func TestPresentationSwitchFailureHasBoundedPolling(t *testing.T) {
	applied, waited := 0, time.Duration(0)
	if switchWindowPresentation(func() bool { return false }, func() { applied++ }, func(d time.Duration) { waited += d }) {
		t.Fatal("样式未生效不能报成功")
	}
	if applied != 1 || waited != 80*time.Millisecond {
		t.Fatalf("失败探测应有界且不能循环模拟按键：apply=%d waited=%v", applied, waited)
	}
}

// 隐藏原生窗口可验证样式路径与真实边框，无需切换用户游戏或输入全局快捷键。
func TestSevenNativeHiddenPresentationRoundTrips(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	g, _ := enterPhysicalPixels()
	defer g.restore()
	create := user32dll.NewProc("CreateWindowExW")
	destroy := user32dll.NewProc("DestroyWindow")
	class, _ := syscall.UTF16PtrFromString("STATIC")
	var handles []uintptr
	for i := 0; i < 7; i++ {
		h, _, err := create.Call(0, uintptr(unsafe.Pointer(class)), 0, wsCaption|wsThickFrame|wsSysMenu, 0, 0, 1024, 720, 0, 0, 0, 0)
		if h == 0 {
			t.Fatalf("创建隐藏样式测试窗失败：%v", err)
		}
		defer destroy.Call(h)
		handles = append(handles, h)
	}
	start := time.Now()
	for _, h := range handles {
		if !enterFullscreen(h) || !exitFullscreen(h) {
			t.Fatal("隐藏原生窗全屏/窗口往返失败")
		}
	}
	elapsed := time.Since(start)
	t.Logf("七个隐藏原生窗口全屏/窗口往返耗时：%v", elapsed)
	if elapsed > 4*time.Second {
		t.Fatalf("不应再积累旧的逐窗快捷键等待：%v", elapsed)
	}
}
