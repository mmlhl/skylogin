package fever

import (
	"os"
	"path/filepath"
	"testing"
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
