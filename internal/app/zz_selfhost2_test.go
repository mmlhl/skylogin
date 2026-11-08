package app

// ma75 授权变体：复用完整探针，只更换授权凭证的应用上下文。
// REMEMBER_SELFHOST2=1 PROBE_ACCOUNT=<目标> PROBE_HOST_ACCOUNT=<宿主>
// PROBE_PLATFORM=ad|ios|hm|pc；其它实验变量见 zz_final_test.go。

import (
	"os"
	"testing"
)

func TestZZSelfHost2(t *testing.T) {
	if os.Getenv("REMEMBER_SELFHOST2") != "1" {
		t.Skip("未开启 ma75 授权实验")
	}
	runSelfHostProbe(t, false)
}
