package app

// 多开启动失败的判定、补启规则与镜像清理登记的回归。
// 这些逻辑直接决定"只起来一部分"时用户看到什么、工具会不会自己救一次，
// 所以不依赖真游戏也要能测。

import (
	"testing"
)

func TestShouldRetryCrash(t *testing.T) {
	cases := []struct {
		name       string
		ticketSent bool
		retried    bool
		multi      bool
		want       bool
	}{
		// 多开、还没发过票、没补过 —— 补一次
		{"首次闪退", false, false, true, true},
		// 单开不补：用户就在旁边，自动重开反而吓人
		{"单开不补", false, false, false, false},
		// 发过票说明凭证链路通了，再拉会顶号
		{"发过票不补", true, false, true, false},
		// 补过还退 = 不是偶发竞争
		{"补过不再补", false, true, true, false},
	}
	for _, c := range cases {
		if got := shouldRetryCrash(c.ticketSent, c.retried, c.multi); got != c.want {
			t.Errorf("%s: shouldRetryCrash(%v,%v,%v)=%v want %v",
				c.name, c.ticketSent, c.retried, c.multi, got, c.want)
		}
	}
}

// 用户手动停掉的客户端不许补启。
func TestMayRetryCrashRespectsStop(t *testing.T) {
	if mayRetryCrash(true, false, false, true) {
		t.Fatal("用户手动停掉的不该补启")
	}
	if !mayRetryCrash(false, false, false, true) {
		t.Fatal("正常闪退应当补启")
	}
}

func TestExitHint(t *testing.T) {	cases := map[uint32]string{
		0:          "正常退出，多半是游戏的重复实例检测或自身放弃",
		1:          "正常退出，多半是游戏的重复实例检测或自身放弃",
		0xC0000005: "访问冲突，进程崩了",
		0xC0000135: "缺少 DLL",
		0xC0000142: "DLL 初始化失败",
		0xC0000409: "快速失败（栈溢出/安全检查）",
		0x00000002: "正常退出，多半是游戏的重复实例检测或自身放弃",
		0xDEADBEEF: "异常退出",
	}
	for code, want := range cases {
		if got := exitHint(code); got != want {
			t.Errorf("exitHint(0x%X)=%q want %q", code, got, want)
		}
	}
}

// takeMirrorNames 必须"取走并清空"：清理只跑一次，重复清理会把下一批
// 刚建好的镜像当成自己的删掉。
func TestTakeMirrorNames(t *testing.T) {
	clientsMu.Lock()
	mirrorNames = nil
	clientsMu.Unlock()

	trackMirror("sky-1-1")
	trackMirror("") // 空名不登记
	trackMirror("sky-1-2")

	got := takeMirrorNames()
	if len(got) != 2 || got[0] != "sky-1-1" || got[1] != "sky-1-2" {
		t.Fatalf("登记的镜像名不对: %v", got)
	}
	if again := takeMirrorNames(); len(again) != 0 {
		t.Fatalf("第二次取应该是空的，实际 %v", again)
	}
}

// 观察者只能有一个：N 个客户端启动时各 spawn 一个，就会在"全部退出"后
// 同时删同一批目录、各打一行日志。
func TestMirrorWatcherSingleInstance(t *testing.T) {
	clientsMu.Lock()
	mirrorWatcherOn = false
	clientsMu.Unlock()

	if !startMirrorWatcher() {
		t.Fatal("第一个观察者应当能启动")
	}
	if startMirrorWatcher() {
		t.Fatal("第二个观察者不该启动")
	}
	stopMirrorWatcher()
	if !startMirrorWatcher() {
		t.Fatal("退出后应当能再次启动")
	}
	stopMirrorWatcher()
}
