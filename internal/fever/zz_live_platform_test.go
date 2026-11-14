package fever

import (
	"path/filepath"
	"testing"
)

func TestPlatformExeProbe(t *testing.T) {
	root := launcherRoot()
	t.Logf("launcherRoot = %q", root)
	if root != "" {
		t.Logf("exe = %q", filepath.Join(root, "FeverGamesLauncher.exe"))
	}
	t.Logf("skin = %q", FindMpaySkin())
	t.Logf("PlatformExe = %q", PlatformExe())
}
