package fever

import (
	"os"
	"testing"
)

// TestGameUpdateLive 手动跑真网（默认跳过：设 SKYLOGIN_LIVE=1 才执行），
// 打印本机版本与服务器最新包的比对结果，排查版本提示问题时用。
func TestGameUpdateLive(t *testing.T) {
	if os.Getenv("SKYLOGIN_LIVE") == "" {
		t.Skip("需要 SKYLOGIN_LIVE=1（会访问网易接口）")
	}
	local := SkyVersionCode()
	t.Logf("本地 VersionCode = %q", local)
	res, err := CheckGameUpdate(local, nil)
	if err != nil {
		t.Fatalf("CheckGameUpdate: %v", err)
	}
	t.Logf("latest=%q checked=%v updateNeeded=%v", res.LatestVersion, res.Checked, res.UpdateNeeded)
}
