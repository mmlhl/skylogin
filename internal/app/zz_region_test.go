package app

// 本地探针（默认跳过）：给某个账号设置所选渠道，方便真机验证。
//
//	REGION_KEY=aebdemo0000000000 REGION_VALUE=ios go test ./internal/app -run TestZZSetRegion -v
//
// 只改本地账号记录，不发网络请求。

import (
	"os"
	"testing"
)

func TestZZSetRegion(t *testing.T) {
	key, value := os.Getenv("REGION_KEY"), os.Getenv("REGION_VALUE")
	if key == "" {
		t.Skip("未设置 REGION_KEY")
	}
	if err := setAccountRegion(key, value); err != nil {
		t.Fatalf("设置渠道失败: %v", err)
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	for i := range list {
		if accountKey(list[i]) == key || list[i].UserID == key {
			t.Logf("已设置：%s → 渠道=%q（平台声明=%q）", displayName(list[i]), list[i].Region, platformCodeOf(list[i].Region))
			return
		}
	}
	t.Logf("已设置 %s → %q（未回读到记录）", key, value)
}
