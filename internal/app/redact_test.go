package app

import (
	"strings"
	"testing"
)

// 日志会被用户贴到群里，凭证不能原样出去。
// 这里的样本全部是构造的假值，别把真实 token 写进测试。

func TestRedactMPayTokenInURL(t *testing.T) {
	in := `查账号显示名失败: Get "https://service.mkey.163.com/mpay/games/x/devices/y/users/z?app_channel=a50_sdk_cn&token=1-FAKEtokenVALUE&updater_cv=c1.0.0": dial tcp: no such host`
	out := redactLogLine(in)
	if strings.Contains(out, "FAKEtokenVALUE") {
		t.Fatalf("token 没被脱敏: %s", out)
	}
	if !strings.Contains(out, "token=1-FAKE…") {
		t.Fatalf("应保留前 6 位便于区分，实际: %s", out)
	}
	// 其余诊断信息必须完整保留，否则日志就没用了
	if !strings.Contains(out, "dial tcp: no such host") || !strings.Contains(out, "app_channel=a50_sdk_cn") {
		t.Fatalf("非敏感内容不该被改动: %s", out)
	}
}

func TestRedactAccessKey(t *testing.T) {
	in := `B站凭证验证失败（Post "https://wpg-api.biligame.com/api/pcg/auto.login?accesskey=FAKEACCESSKEY0000&gameid=183": ...）`
	out := redactLogLine(in)
	if strings.Contains(out, "FAKEACCESSKEY0000") {
		t.Fatalf("accesskey 没被脱敏: %s", out)
	}
	if !strings.Contains(out, "accesskey=FAKE…") {
		t.Fatalf("应保留前 4 位，实际: %s", out)
	}
}

func TestRedactAccessKeyWithUnderscore(t *testing.T) {
	// 参数名两种写法都出现过
	out := redactLogLine("?access_key=FAKEACCESSKEY0000&x=1")
	if strings.Contains(out, "FAKEACCESSKEY0000") {
		t.Fatalf("access_key 没被脱敏: %s", out)
	}
}

func TestRedactSign(t *testing.T) {
	out := redactLogLine("sign=0000000000000000&isgovver=1")
	if strings.Contains(out, "0000000000000000") {
		t.Fatalf("sign 没被脱敏: %s", out)
	}
	if !strings.Contains(out, "isgovver=1") {
		t.Fatalf("其他参数不该被改动: %s", out)
	}
}

func TestRedactEmail(t *testing.T) {
	out := redactLogLine("网易账号 zhangsan@example.com：正在现签票据…")
	if strings.Contains(out, "zhangsan") {
		t.Fatalf("邮箱没被脱敏: %s", out)
	}
	if !strings.Contains(out, "z***@example.com") {
		t.Fatalf("应保留首字符和域名，实际: %s", out)
	}
}

func TestRedactLeavesNormalLinesAlone(t *testing.T) {
	lines := []string{
		"光遇账号登录器 1.6.2+dev 启动（dev-c0bcba5，2026-11-10 19:42）",
		"账号: 一号 (玩家ID DEMOPLAYER000001)",
		"已更新平台身份凭证（a50，326 字符）",
		"多开：一号 凭证状态暂未确认，跳过",
		"mcount_app_key=EEkEEXLymcNjM42yLY3Bn6AO15aGy4yq",
	}
	for _, s := range lines {
		if got := redactLogLine(s); got != s {
			t.Errorf("不该改动：%q → %q", s, got)
		}
	}
}

func TestRedactShortValueFullyMasked(t *testing.T) {
	// 值比保留位数还短时，整个抹掉——否则"前 6 位"就是原文
	out := redactLogLine("token=abc")
	if strings.Contains(out, "abc") {
		t.Fatalf("短值应整体抹掉: %s", out)
	}
}

func TestRedactMultipleSecretsInOneLine(t *testing.T) {
	in := "token=1-FAKEsecondVALUE&sign=0000000000000000&mail=a@example.com"
	out := redactLogLine(in)
	for _, leaked := range []string{"FAKEsecondVALUE", "0000000000000000", "a@example.com"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("同一行里的 %q 没被脱敏: %s", leaked, out)
		}
	}
}
