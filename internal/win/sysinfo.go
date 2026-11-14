package win

import (
	"strconv"
	"syscall"
)

// ScreenResolution 返回主显示器的分辨率，形如 "2560*1440"。
func ScreenResolution() string {
	w, h := ScreenSize()
	return strconv.FormatUint(uint64(w), 10) + "*" + strconv.FormatUint(uint64(h), 10)
}

// ScreenSize 返回主显示器的物理像素尺寸。
//
// 启动组存窗口坐标时要把它一起记下来：下次屏幕变了（换显示器、改分辨率），
// 那套坐标就作废，启动时得退化成网格排列，不能让窗口跑到屏幕外面去。
func ScreenSize() (int, int) {
	user32 := syscall.NewLazyDLL("user32.dll")
	get := user32.NewProc("GetSystemMetrics")
	if get.Find() != nil {
		return 1920, 1080
	}
	w, _, _ := get.Call(0) // SM_CXSCREEN
	h, _, _ := get.Call(1) // SM_CYSCREEN
	if w == 0 || h == 0 {
		return 1920, 1080
	}
	return int(w), int(h)
}
