package game

// 档案与既有常量的一致性检查。
//
// 迁移期有两份数据：profile 包的常量（线上在用）和本包的档案（未来唯一来源）。
// 两份数据漂移是最隐蔽的事故——某天改了 archive 却没改常量，登录签名就对不上了。
// 这里把关键字段钉死：任一不一致，测试立刻失败。

import (
	"testing"

	"skylogin/internal/profile"
)

func TestSkyMatchesProfileConstants(t *testing.T) {
	sky := Sky()
	if sky.ID != profile.ShortGameID {
		t.Fatalf("ID = %q，profile.ShortGameID = %q", sky.ID, profile.ShortGameID)
	}
	if sky.LongID != profile.LongGameID {
		t.Fatalf("LongID = %q，profile.LongGameID = %q", sky.LongID, profile.LongGameID)
	}
	if sky.LogKey != profile.LogKey {
		t.Fatalf("LogKey 与 profile.LogKey 不一致")
	}
	if sky.Exe != profile.SkyExeName {
		t.Fatalf("Exe = %q，profile.SkyExeName = %q", sky.Exe, profile.SkyExeName)
	}
	if sky.LauncherArg != profile.LauncherArg {
		t.Fatalf("LauncherArg 与 profile.LauncherArg 不一致")
	}
	if sky.SdkVersion != profile.SdkVersion {
		t.Fatalf("SdkVersion = %q，profile.SdkVersion = %q", sky.SdkVersion, profile.SdkVersion)
	}
}

func TestSkyChannelParams(t *testing.T) {
	sky := Sky()
	if c, ok := sky.Channel("uc_platform"); !ok || c.AppID != "918836" {
		t.Fatalf("九游参数缺失或 app_id 不是 918836: %+v", c)
	}
	if c, ok := sky.Channel("huawei"); !ok || c.AppID != profile.HuaweiAppID {
		t.Fatalf("华为参数缺失或 app_id 与 profile 不一致: %+v", c)
	}
	if _, ok := sky.Channel("nonexistent"); ok {
		t.Fatal("未配置的渠道不应返回参数")
	}
}
