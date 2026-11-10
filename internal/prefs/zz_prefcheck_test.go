package prefs

// 临时探针：验证 ApplyMultiDefaults 对**真实文件**的改写是否无损。
// 开关：ZZ_PREFCHECK=<preferences.sav 路径>

import (
	"os"
	"testing"
)

func zzDump(t *testing.T, tag string, b []byte) map[string]string {
	t.Helper()
	out := map[string]string{}
	base := int(u32(b, 24))
	t.Logf("== %s：%d 字节 version=%d stringsBase=%d", tag, len(b), u32(b, 4), base)
	for pos := entries; pos+pairSize <= base; pos += pairSize {
		name, err := cstring(b, base+int(u32(b, pos)))
		if err != nil {
			continue
		}
		val := u32(b, pos+4)
		if s, serr := cstring(b, base+int(val)); serr == nil && s != "" && len(s) < 64 && isPrintable(s) {
			out[name] = s
			t.Logf("   %-36s = %q", name, s)
		} else {
			out[name] = ""
			t.Logf("   %-36s = %d", name, val)
		}
	}
	t.Logf("   共 %d 个键", len(out))
	return out
}

func isPrintable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

func TestZZPrefCheck(t *testing.T) {
	path := os.Getenv("ZZ_PREFCHECK")
	if path == "" {
		t.Skip("未设置 ZZ_PREFCHECK")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读失败: %v", err)
	}
	before := zzDump(t, "原始", b)

	out, err := ApplyMultiDefaults(b, false, "LowPower", 30)
	if err != nil {
		t.Fatalf("ApplyMultiDefaults 失败: %v", err)
	}
	after := zzDump(t, "预写后（窗口 + LowPower + 30）", out)

	// 键集合必须一模一样：只该改值，不该多键少键
	if len(before) != len(after) {
		t.Fatalf("键数量变了：%d → %d", len(before), len(after))
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			t.Fatalf("键丢了: %s", k)
		}
	}
	if v, _ := ReadValue(out, KeyFullscreen); v != 0 {
		t.Fatalf("Fullscreen 没写进去: %d", v)
	}
	if v, _ := ReadValue(out, KeyQualityFps); v != 30 {
		t.Fatalf("quality_fps 没写进去: %d", v)
	}
	if s, _ := ReadString(out, KeyQualityMode); s != "LowPower" {
		t.Fatalf("quality_mode 没写进去: %q", s)
	}
	t.Logf("OK：键集合不变，三个值都写进去了（文件 %d → %d 字节）", len(b), len(out))
}
