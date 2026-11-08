package app

// 只读检查：寄宿池现在是什么状态——谁能当宿主、槽里是谁、为什么当不了。
//
// 只输出布尔、数量与脱敏名称，不输出 sid/token。
// 开关：HOSTPOOL_READ=1

import (
	"os"
	"testing"
)

func TestZZHostPoolRead(t *testing.T) {
	if os.Getenv("HOSTPOOL_READ") == "" {
		t.Skip("未开启寄宿池只读检查")
	}
	pool := HostPoolStatus()
	t.Logf("能当宿主 %d 个，空槽 %d 个", pool.CanHost, pool.FreeSlots)
	if pool.Note != "" {
		t.Logf("提示：%s", pool.Note)
	}
	for _, h := range pool.Hosts {
		t.Logf("  %-26s 可宿主=%-5v 已查=%-5v 空槽=%-5v 槽内=%q %s",
			h.Name, h.CanHost, h.Checked, h.Available, h.HostedName, h.Reason)
	}
}
