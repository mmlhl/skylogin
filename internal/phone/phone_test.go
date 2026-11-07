package phone

// 机型库的约束：每一行都必须自洽。
//
// 这些断言的价值是「加机型时被挡住」——表迟早要继续加，加错一行
// （比如 API 和版本号对不上、分辨率写反）人眼很难发现。

import (
	"regexp"
	"testing"
)

// apiOfRelease 是 Android 大版本 → API Level 的标准映射。
var apiOfRelease = map[string]int{
	"11": 30,
	"12": 31,
	"13": 33,
	"14": 34,
	"15": 35,
	"16": 36,
}

func TestCatalogConsistent(t *testing.T) {
	if len(Catalog) < 20 {
		t.Fatalf("机型库只有 %d 条，太少", len(Catalog))
	}

	seen := map[string]bool{}
	for _, d := range Catalog {
		name := d.Vendor + " " + d.Model
		if d.Model == "" || d.Vendor == "" || d.OSRelease == "" || d.APILevel == 0 {
			t.Fatalf("%s：字段不全", name)
		}
		if seen[d.Model] {
			t.Fatalf("%s：机型重复", name)
		}
		seen[d.Model] = true

		if want := apiOfRelease[d.OSRelease]; want != d.APILevel {
			t.Fatalf("%s：Android %s 应对应 API %d，实际写的 %d", name, d.OSRelease, want, d.APILevel)
		}
		if d.ScreenLong <= d.ScreenShort || d.ScreenShort < 720 {
			t.Fatalf("%s：分辨率不合理 %dx%d", name, d.ScreenLong, d.ScreenShort)
		}
		if d.ScreenLong%2 != 0 || d.ScreenShort%2 != 0 {
			t.Fatalf("%s：分辨率不是偶数 %dx%d", name, d.ScreenLong, d.ScreenShort)
		}
	}
}

// TestCatalogHardwareFactsStayClean 保证机型条目里不夹带标识。
//
// 标识必须由 New / Random 现生成：如果哪天有人图省事，把 android_id
// 直接写进 Catalog，所有账号就会共用同一个 id。
func TestCatalogHardwareFactsStayClean(t *testing.T) {
	for _, d := range Catalog {
		if d.AndroidID != "" || d.InstallUUID != "" || d.OAID != "" || d.InstalledAt != 0 {
			t.Fatalf("%s：机型条目里不该有标识，应该由 New() 生成", d.Model)
		}
	}
}

func TestNewFillsIdentifiers(t *testing.T) {
	d := New(Catalog[0])
	if !d.Valid() {
		t.Fatalf("生成的档案不完整: %+v", d)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(d.AndroidID) {
		t.Fatalf("android_id 形态不对: %q", d.AndroidID)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(d.InstallUUID) {
		t.Fatalf("install uuid 不是 v4: %q", d.InstallUUID)
	}
	if !regexp.MustCompile(`^[0-9A-F]{64}$`).MatchString(d.OAID) {
		t.Fatalf("OAID 形态不对: %q", d.OAID)
	}
	if d.InstalledAt <= 0 {
		t.Fatal("安装时间没生成")
	}

	// 两次生成的标识不能撞
	if e := New(Catalog[0]); e.AndroidID == d.AndroidID {
		t.Fatal("两台设备的 android_id 相同")
	}
}

// TestNoOAIDDevice 覆盖「这台机器取不到广告标识」的情况。
func TestNoOAIDDevice(t *testing.T) {
	spec := Device{Model: "X", Vendor: "Y", OSRelease: "14", APILevel: 34, ScreenLong: 2400, ScreenShort: 1080, NoOAID: true}
	d := New(spec)
	if d.OAID != "" {
		t.Fatal("标了 NoOAID 却生成了 OAID")
	}
	if !d.Valid() {
		t.Fatal("没有 OAID 的档案也应该有效")
	}
}

// TestFrameFormat 校验 fr 串的形态，九游按这个格式上报。
func TestFrameFormat(t *testing.T) {
	d := New(Catalog[0])
	want := "API Level-" + itoa(d.APILevel) + " - " + d.Vendor + " " + d.Model + "-" + d.OSRelease
	if got := d.Frame(); got != want {
		t.Fatalf("Frame() = %q, want %q", got, want)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
