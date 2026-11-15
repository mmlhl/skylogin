package win

import "time"

// switchWindowPresentation 的窗口状态探针与样式写入可替换，测试不会操作真实游戏。
// 正常 DWM 窗口改样式后立即可读，不应为每个客户端固定等待近两秒。
func switchWindowPresentation(ready func() bool, apply func(), pause func(time.Duration)) bool {
	if ready() {
		return true
	}
	apply()
	for attempt := 0; attempt < 5; attempt++ {
		if ready() {
			return true
		}
		if attempt < 4 {
			pause(20 * time.Millisecond)
		}
	}
	return false
}
