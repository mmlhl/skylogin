package win

import "testing"

// 读前台必须和系统一致，且不能有副作用。
// 曾经的实现误用 SetForegroundWindow 读前台：返回值是 BOOL，
// 拿它当窗口句柄解析出 pid 恒为 0，且每次调用都可能把前台挪走。
func TestForegroundWindowMatchesSystem(t *testing.T) {
	gotH, _ := ForegroundWindow()
	wantH, _, _ := procGetForegroundW.Call()
	if gotH != wantH {
		t.Fatalf("ForegroundWindow() = %#x，系统 GetForegroundWindow = %#x", gotH, wantH)
	}
}
