package fever

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestMirrorsRemaining 只数 <游戏目录名>- 开头的子目录，别的目录不算。
func TestMirrorsRemaining(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"sky-1-1", "sky-2-2", "other-1-1", "sky"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	skyExe := filepath.Join(`D:\games\sky`, "sky.exe")
	if got := MirrorsRemaining(skyExe, root); got != 2 {
		t.Fatalf("MirrorsRemaining=%d want 2", got)
	}
	// 清空后应该是 0，但根目录仍然存在
	_ = os.RemoveAll(filepath.Join(root, "sky-1-1"))
	_ = os.RemoveAll(filepath.Join(root, "sky-2-2"))
	if got := MirrorsRemaining(skyExe, root); got != 0 {
		t.Fatalf("清空后=%d want 0", got)
	}
	if !MirrorRootExists(root) {
		t.Fatal("根目录应该保留")
	}
}

// RemoveMirrorDirs 要返回"没删掉的那些"：调用方靠它决定要不要还原浏览器壳
// （删不掉的镜像里不能留一个被换过的浏览器），返回空就一笔带过。
func TestRemoveMirrorDirsReportsLeftovers(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "sky-1-1")
	if err := os.MkdirAll(filepath.Join(good, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "data", "x.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	left := RemoveMirrorDirs(root, []string{"sky-1-1"})
	if len(left) != 0 {
		t.Fatalf("正常删完不该有剩余: %v", left)
	}
	if _, err := os.Stat(good); !os.IsNotExist(err) {
		t.Fatalf("目录应当已删除: %v", err)
	}

	// 不存在的名字不需要回报：没什么可删的，也不算"没删掉"
	left = RemoveMirrorDirs(root, []string{"sky-9-9"})
	if len(left) != 0 {
		t.Fatalf("不存在的目录不该算剩余: %v", left)
	}
}

// 有进程占着目录里的文件时（浏览器/崩溃上报还没退干净），删除会失败，
// 必须把目录名回报出去，调用方才会去还原浏览器壳。
func TestRemoveMirrorDirsKeepsLockedDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sky-2-2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "held.dll")
	if err := os.WriteFile(locked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 不共享删除权限地占住文件，模拟子进程还没释放映像
	p, err := windows.UTF16PtrFromString(locked)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Skipf("打不开独占句柄，跳过: %v", err)
	}
	defer windows.CloseHandle(h)

	if ok := removeDirWith(dir, 2, time.Millisecond); ok {
		t.Fatal("文件被独占时不该报告删除成功")
	}
	if left := RemoveMirrorDirs(root, []string{"sky-2-2"}); len(left) != 1 || left[0] != "sky-2-2" {
		t.Fatalf("被占用的目录应当回报剩余: %v", left)
	}
}
