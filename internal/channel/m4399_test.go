package channel

import "testing"

// UDID 必须是 62 位十六进制（时间戳 + md5 + "00" + 校验尾），格式错了
// 4399 侧的设备身份就废了。
func TestNewM4399Device(t *testing.T) {
	d := NewM4399Device()
	if len(d.Udid) != 62 {
		t.Fatalf("UDID 应为 62 位，实际 %d：%q", len(d.Udid), d.Udid)
	}
	for _, r := range d.Udid {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			t.Fatalf("UDID 应全为十六进制字符：%q", d.Udid)
		}
	}
	if NewM4399Device().Udid == d.Udid {
		t.Fatal("两次生成的 UDID 不应相同")
	}
	if d.DeviceIdentifier == "" {
		t.Fatal("设备标识不应为空")
	}
}

func TestM4399Code(t *testing.T) {
	if got := m4399Code(map[string]any{"code": "100"}); got != "100" {
		t.Fatalf("字符串 code: %q", got)
	}
	if got := m4399Code(map[string]any{"code": float64(100)}); got != "100" {
		t.Fatalf("数字 code: %q", got)
	}
	if got := m4399Code(nil); got != "" {
		t.Fatalf("nil 响应: %q", got)
	}
}

func TestM4399Parse(t *testing.T) {
	ok := map[string]any{
		"code":   float64(100),
		"result": map[string]any{"uid": "123", "state": "st", "access_token": "at", "nick": "昵称"},
	}
	res, complete := m4399Parse(ok)
	if !complete || res.UID != "123" || res.State != "st" || res.AccessToken != "at" || res.Nick != "昵称" {
		t.Fatalf("成功响应解析错误: %+v complete=%v", res, complete)
	}

	// code 非 100/200 不算成功
	if _, complete := m4399Parse(map[string]any{"code": float64(601), "result": map[string]any{"uid": "1"}}); complete {
		t.Fatal("code=601 不应判为成功")
	}
	// 缺 access_token 不算完整（刷新接口也走这个判定）
	partial := map[string]any{"code": "200", "result": map[string]any{"uid": "1", "state": "s"}}
	if _, complete := m4399Parse(partial); complete {
		t.Fatal("缺 access_token 不应判为完整")
	}
}
