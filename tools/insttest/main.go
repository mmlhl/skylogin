// 实验：用不同启动方式拉起 N 个 sky.exe，看能活下来几个。
//
// 只做"启动期存活"测量，不登录、不发真票：目的是分辨游戏的单实例检查
// 到底是"看启动散布"还是别的东西。跑完会杀掉所有子进程并删掉镜像。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"skylogin/internal/fever"
)

type launch struct {
	pid     uint32
	index   int
	mirror  string
	started time.Time
}

func main() {
	exe := flag.String("exe", `D:\FeverApps\sky\Sky.exe`, "sky.exe 路径")
	n := flag.Int("n", 10, "拉几个")
	mode := flag.String("mode", "plain", "plain | suspended | waves")
	seconds := flag.Int("seconds", 30, "观察多少秒")
	root := flag.String("mirror", `D:\SkyLoginMirrorTest`, "镜像根目录")
	waveSize := flag.Int("wave", 4, "waves 模式每波几个")
	gapMS := flag.Int("gap", 0, "每次启动之间插入多少毫秒（模拟慢机器的 CreateProcess 开销）")
	keep := flag.Bool("keep", false, "保留镜像目录")
	noHost := flag.Bool("no-host", false, "不起平台宿主窗口")
	flag.Parse()

	start := time.Now()
	fmt.Printf("[%s] 模式=%s N=%d 观察=%ds\n", start.Format("15:04:05"), *mode, *n, *seconds)

	// ① 建镜像（和生产一样：同盘硬链接）
	mirrors := make([]string, 0, *n)
	for i := 0; i < *n; i++ {
		name := fmt.Sprintf("sky-test-%d-%d", start.Unix(), i)
		m, err := fever.MirrorInstall(*exe, name, *root)
		if err != nil {
			fmt.Printf("建镜像失败: %v\n", err)
			cleanup(nil, *root, *keep)
			return
		}
		mirrors = append(mirrors, m)
	}
	fmt.Printf("镜像就绪 %d 套，耗时 %v\n", len(mirrors), time.Since(start).Round(time.Millisecond))

	// ② 宿主窗口：让游戏能找到"发烧平台"
	var host *fever.Host
	if !*noHost {
		h, err := startHost()
		if err != nil {
			fmt.Printf("起宿主失败: %v\n", err)
			cleanup(nil, *root, *keep)
			return
		}
		host = h
		defer host.Stop()
		fmt.Println("平台宿主已就绪")
	}

	// ③ 按模式启动
	var list []launch
	var err error
	switch *mode {
	case "plain":
		list, err = launchPlain(*exe, mirrors, *gapMS)
	case "suspended":
		list, err = launchSuspended(*exe, mirrors, *gapMS)
	case "waves":
		list, err = launchWaves(*exe, mirrors, *waveSize)
	default:
		fmt.Println("未知模式")
		return
	}
	if err != nil {
		fmt.Printf("启动失败: %v\n", err)
	}
	fmt.Printf("拉起完成：%d 个，从第一个到最后一个 %v\n", len(list), spread(list))

	// ④ 观察存活
	dead := map[uint32]time.Duration{}
	deadline := time.Now().Add(time.Duration(*seconds) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		for _, l := range list {
			if _, ok := dead[l.pid]; ok {
				continue
			}
			if !fever.ProcessAlive(l.pid) {
				dead[l.pid] = time.Since(l.started)
			}
		}
	}

	// ⑤ 窗口类（看实例序号有没有撞）
	classes := clientWindowClasses()

	alive := 0
	fmt.Println("--- 明细 ---")
	for _, l := range list {
		state := "存活"
		extra := ""
		if d, ok := dead[l.pid]; ok {
			state = fmt.Sprintf("退出 @%.1fs", d.Seconds())
		} else {
			alive++
			if c := classes[l.pid]; c != "" {
				extra = " 窗口类=" + c
			}
		}
		fmt.Printf("  #%d pid=%d %s%s\n", l.index, l.pid, state, extra)
	}
	fmt.Printf("--- 结果：模式=%s N=%d 存活=%d 死亡=%d ---\n", *mode, len(list), alive, len(dead))

	cleanup(list, *root, *keep)
}

func startHost() (*fever.Host, error) {
	var h *fever.Host
	h = fever.NewHostWithClass(fever.DefaultClassName, func(req fever.TicketRequest) bool {
		// 不回真票：只记录，看看有多少个实例走到了取票这一步
		fmt.Printf("  [ipc] 取票请求 pid=%d instance=%d\n", req.ProcessID, req.Instance)
		return true
	})
	if err := h.Start(); err != nil {
		return nil, err
	}
	return h, nil
}

func launchPlain(exe string, mirrors []string, gapMS int) ([]launch, error) {
	out := make([]launch, 0, len(mirrors))
	for i, m := range mirrors {
		cmd := exec.Command(filepath.Join(m, filepath.Base(exe)), "--start_from_launcher=1")
		cmd.Dir = m
		if err := cmd.Start(); err != nil {
			fmt.Printf("  #%d 启动失败: %v\n", i, err)
			continue
		}
		out = append(out, launch{pid: uint32(cmd.Process.Pid), index: i, mirror: m, started: time.Now()})
		if gapMS > 0 {
			time.Sleep(time.Duration(gapMS) * time.Millisecond)
		}
	}
	return out, nil
}

func launchWaves(exe string, mirrors []string, size int) ([]launch, error) {
	if size <= 0 {
		size = 4
	}
	out := make([]launch, 0, len(mirrors))
	for i, m := range mirrors {
		cmd := exec.Command(filepath.Join(m, filepath.Base(exe)), "--start_from_launcher=1")
		cmd.Dir = m
		if err := cmd.Start(); err != nil {
			fmt.Printf("  #%d 启动失败: %v\n", i, err)
			continue
		}
		out = append(out, launch{pid: uint32(cmd.Process.Pid), index: i, mirror: m, started: time.Now()})
		if (i+1)%size == 0 && i+1 < len(mirrors) {
			time.Sleep(4 * time.Second)
		}
	}
	return out, nil
}

// launchSuspended 用 CREATE_SUSPENDED 一次性建完，再统一放行。
func launchSuspended(exe string, mirrors []string, gapMS int) ([]launch, error) {
	type created struct {
		pi  windows.ProcessInformation
		idx int
		m   string
		t   time.Time
	}
	var all []created
	for i, m := range mirrors {
		exePath := filepath.Join(m, filepath.Base(exe))
		appName, _ := windows.UTF16PtrFromString(exePath)
		cmdLine, _ := windows.UTF16PtrFromString(`"` + exePath + `" --start_from_launcher=1`)
		dir, _ := windows.UTF16PtrFromString(m)
		si := new(windows.StartupInfo)
		si.Cb = uint32(unsafe.Sizeof(*si))
		var pi windows.ProcessInformation
		err := windows.CreateProcess(appName, cmdLine, nil, nil, false,
			windows.CREATE_SUSPENDED, nil, dir, si, &pi)
		if err != nil {
			fmt.Printf("  #%d 创建失败: %v\n", i, err)
			continue
		}
		all = append(all, created{pi: pi, idx: i, m: m, t: time.Now()})
		if gapMS > 0 {
			time.Sleep(time.Duration(gapMS) * time.Millisecond)
		}
	}
	// 全部创建完，统一放行
	resumeAt := time.Now()
	out := make([]launch, 0, len(all))
	for _, c := range all {
		_, err := windows.ResumeThread(c.pi.Thread)
		if err != nil {
			fmt.Printf("  #%d ResumeThread 失败: %v\n", c.idx, err)
		}
		out = append(out, launch{pid: c.pi.ProcessId, index: c.idx, mirror: c.m, started: resumeAt})
		windows.CloseHandle(c.pi.Thread)
		windows.CloseHandle(c.pi.Process)
	}
	return out, nil
}

func spread(list []launch) time.Duration {
	if len(list) < 2 {
		return 0
	}
	min, max := list[0].started, list[0].started
	for _, l := range list {
		if l.started.Before(min) {
			min = l.started
		}
		if l.started.After(max) {
			max = l.started
		}
	}
	return max.Sub(min).Round(time.Millisecond)
}

// clientWindowClasses 枚举顶层窗口，返回 pid → 类名（只收游戏的 client 窗口）。
func clientWindowClasses() map[uint32]string {
	out := map[uint32]string{}
	cb := windows.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		cls := fever.WindowClass(windows.Handle(hwnd))
		if strings.HasPrefix(cls, "LHMW_FG_client") {
			pid := fever.WindowPID(windows.Handle(hwnd))
			if pid != 0 {
				out[pid] = cls
			}
		}
		return 1
	})
	user32 := windows.NewLazySystemDLL("user32.dll")
	procEnum := user32.NewProc("EnumWindows")
	procEnum.Call(cb, 0)
	return out
}

// cleanup 杀掉仍然活着的测试进程，并删除镜像。
func cleanup(list []launch, root string, keep bool) {
	for _, l := range list {
		if fever.ProcessAlive(l.pid) {
			_ = fever.KillProcessTree(l.pid)
		}
	}
	// 兜底：把本目录里还挂着的 sky.exe 也清掉（只清镜像目录里的）
	time.Sleep(500 * time.Millisecond)
	if keep {
		fmt.Printf("镜像保留在 %s\n", root)
		return
	}
	entries, err := os.ReadDir(root)
	if err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "sky-test-") {
				names = append(names, e.Name())
			}
		}
		fever.RemoveMirrorDirs(root, names)
	}
	_ = os.Remove(root)
	fmt.Println("已清理测试镜像")
}

var _ = sort.Strings
var _ sync.Mutex
