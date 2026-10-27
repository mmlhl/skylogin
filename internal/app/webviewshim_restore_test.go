package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsOurMirror 只有 sky-<数字>-<数字> 这种才是我们建的镜像。
func TestIsOurMirror(t *testing.T) {
	cases := map[string]bool{
		`D:\SkyLoginMirror\sky-1793107448332-1`: true,
		`sky-1-2`:                               true,
		`sky`:                                   false,
		`sky-abc-1`:                             false,
		`sky-1-2-3`:                             false,
		`C:\FeverApps\sky`:                      false,
		`..\evil-1-1`:                           true, // 名字本身合法，但根目录另有校验
	}
	for in, want := range cases {
		if got := isOurMirror(in); got != want {
			t.Errorf("isOurMirror(%q)=%v want %v", in, got, want)
		}
	}
}

// TestInstallRefusesNonMirror 非镜像目录必须拒绝，且不能动里面的文件。
func TestInstallRefusesNonMirror(t *testing.T) {
	root := t.TempDir() // 名字是随机临时目录，不是 sky-x-y
	cef := filepath.Join(root, "webviewsupport.cef904430")
	if err := os.MkdirAll(cef, 0o755); err != nil {
		t.Fatal(err)
	}
	browser := filepath.Join(cef, shimBrowserName)
	if err := os.WriteFile(browser, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installWebviewShim(root); err == nil {
		t.Fatal("非镜像目录应该报错")
	}
	got, err := os.ReadFile(browser)
	if err != nil {
		t.Fatalf("原文件被动了: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("原文件内容被改: %q", got)
	}
	if _, err := os.Stat(filepath.Join(cef, shimRealName)); err == nil {
		t.Fatal("不该产生 .real.exe 备份")
	}
}

// TestRestoreWebviewShim 还原后应该只剩原名文件，没有壳和备份。
func TestRestoreWebviewShim(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sky-123-1")
	cef := filepath.Join(root, "webviewsupport.cef904430")
	if err := os.MkdirAll(cef, 0o755); err != nil {
		t.Fatal(err)
	}
	realExe := filepath.Join(cef, shimRealName)
	if err := os.WriteFile(realExe, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	browser := filepath.Join(cef, shimBrowserName)
	if err := os.WriteFile(browser, []byte("shim"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(cef, shimMarkerName), []byte("x"), 0o644)

	restoreWebviewShim(root)

	if got, err := os.ReadFile(browser); err != nil || string(got) != "real" {
		t.Fatalf("还原失败: %q %v", got, err)
	}
	if _, err := os.Stat(realExe); err == nil {
		t.Fatal(".real.exe 应该已经被改名回去")
	}
	if _, err := os.Stat(filepath.Join(cef, shimMarkerName)); err == nil {
		t.Fatal("标记文件应该被清掉")
	}
}
