package device

// 设备档案的落盘行为：同一账号复用同一台，不同渠道互不影响。

import (
	"path/filepath"
	"testing"

	"skylogin/internal/phone"
)

func TestLoadOrCreatePhoneStable(t *testing.T) {
	// 注意 t.TempDir() 每次调用都返回新目录，必须先算好再闭包捕获
	path := filepath.Join(t.TempDir(), "phones.json")
	phoneStorePathFn = func() string { return path }

	a1, err := LoadOrCreatePhone("uc", "13800000000")
	if err != nil {
		t.Fatal(err)
	}
	a2, err := LoadOrCreatePhone("uc", "13800000000")
	if err != nil {
		t.Fatal(err)
	}
	if a1.AndroidID != a2.AndroidID || a1.Model != a2.Model {
		t.Fatalf("同一账号分到两台设备: %+v vs %+v", a1, a2)
	}

	b, err := LoadOrCreatePhone("uc", "13900000000")
	if err != nil {
		t.Fatal(err)
	}
	if b.AndroidID == a1.AndroidID {
		t.Fatal("不同账号分到同一台设备")
	}

	// 换个渠道，同一个 key 也应该是另一台（渠道之间不该串号）
	c, err := LoadOrCreatePhone("huawei", "13800000000")
	if err != nil {
		t.Fatal(err)
	}
	if c.AndroidID == a1.AndroidID {
		t.Fatal("不同渠道共用了同一台设备")
	}
}

// TestAndroidProfileSelfConsistent 华为档案里的品牌、型号、系统版本要自洽。
func TestAndroidProfileSelfConsistent(t *testing.T) {
	p, err := LoadOrCreateAndroid(filepath.Join(t.TempDir(), "android.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Brand != "HUAWEI" && p.Brand != "HONOR" {
		t.Fatalf("华为渠道不该报 %q 品牌", p.Brand)
	}
	if p.Manufacturer != p.Brand {
		t.Fatalf("厂商与品牌不一致: %q vs %q", p.Manufacturer, p.Brand)
	}

	// 型号必须来自共享机型库，且属于该品牌的机器
	var ok bool
	for _, d := range phone.Catalog {
		if d.Model == p.PhoneType {
			ok = d.Vendor == p.Brand && d.OSRelease == p.AndroidVersion
			break
		}
	}
	if !ok {
		t.Fatalf("型号 %q / 版本 %q 不是该品牌在机型库里的组合", p.PhoneType, p.AndroidVersion)
	}
}
