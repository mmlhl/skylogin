package channel

import (
	"strings"
	"testing"
)

// uid 必须是无精度损失的整数字符串。JSON 数字解出来是 float64，
// 走 fmt.Sprint 会变成科学计数法（413918109 → "4.13918109e+08"），
// 游戏服务器不认。
func TestBiliUID(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{float64(413918109), "413918109"},
		{float64(123), "123"},
		{"413918109", "413918109"},
		{nil, "<nil>"},
	}
	for _, c := range cases {
		if got := BiliUID(c.in); got != c.want {
			t.Fatalf("BiliUID(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// 签名：按 key 排序拼值 + app_key 后 MD5。
func TestBiliSign(t *testing.T) {
	got := biliSign(map[string]string{"b": "2", "a": "1"}, "KEY")
	want := biliSign(map[string]string{"a": "1", "b": "2"}, "KEY")
	if got != want {
		t.Fatal("签名不应受参数顺序影响")
	}
	if len(got) != 32 {
		t.Fatalf("签名应为 32 位 MD5 hex：%q", got)
	}
}

// 捕获脚本：只认 pcg 系接口；auto.login 的 access_key 从 URL 上取。
func TestBiliLoginScriptRules(t *testing.T) {
	s := BiliLoginScript()
	for _, fragment := range []string{
		`"/api/pcg/"`,
		`"/auto.login"`,
		"access_key=([^&]+)",
		`kind: "login"`,
	} {
		if !strings.Contains(s, fragment) {
			t.Fatalf("脚本缺少关键规则片段: %q", fragment)
		}
	}
	// 结果统一走 uid + secret schema
	if !strings.Contains(s, "secret:") {
		t.Fatal("脚本应回传统一 schema 的 secret 字段")
	}
}
