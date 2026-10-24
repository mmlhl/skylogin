package win

import (
	"strconv"
	"syscall"
)

// ScreenResolution 返回主显示器的分辨率，形如 "2560*1440"。
func ScreenResolution() string {
	user32 := syscall.NewLazyDLL("user32.dll")
	get := user32.NewProc("GetSystemMetrics")
	if get.Find() != nil {
		return "1920*1080"
	}
	w, _, _ := get.Call(0) // SM_CXSCREEN
	h, _, _ := get.Call(1) // SM_CYSCREEN
	if w == 0 || h == 0 {
		return "1920*1080"
	}
	return strconv.FormatUint(uint64(w), 10) + "*" + strconv.FormatUint(uint64(h), 10)
}
