package fever

import (
	"errors"
	"strings"
	"testing"
)

// TestCheckGameUpdate 用假的下载器验证比对逻辑（不打真网）。
func TestCheckGameUpdate(t *testing.T) {
	const reply = `{"data":{"apps":[{"running_process_name":"sky.exe",` +
		`"version_code":"v3_5219_3f5b5a4230076fd4ae16f9bf6db83aa5",` +
		`"app_id":63,"app_origin_version":null,"update_actions":[]}]},` +
		`"code":200,"msg":"操作成功"}`
	get := func(url string) ([]byte, error) {
		// 参数必须与官方抓包一致：app_ids=[63]
		if !strings.Contains(url, "app_ids=%5B63%5D") {
			t.Fatalf("app_ids 参数不对: %s", url)
		}
		return []byte(reply), nil
	}

	// 本地落后 → 提示更新（本地版本必须原样带给服务器）
	res, err := CheckGameUpdate("v3_old", func(u string) ([]byte, error) {
		if !strings.Contains(u, "origin_version_code%22%3A%22v3_old%22") {
			t.Fatalf("本地版本没带上: %s", u)
		}
		return []byte(reply), nil
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !res.UpdateNeeded || res.LatestVersion == "" || !res.Checked {
		t.Fatalf("落后的版本没被识别: %+v", res)
	}

	// 本地一致 → 不提示
	res, _ = CheckGameUpdate("v3_5219_3f5b5a4230076fd4ae16f9bf6db83aa5", get)
	if res.UpdateNeeded {
		t.Fatalf("版本一致不该提示更新: %+v", res)
	}

	// 没有本地基准（没走平台安装）→ 不能瞎报
	res, _ = CheckGameUpdate("", get)
	if res.UpdateNeeded {
		t.Fatalf("没有本地版本不该报更新: %+v", res)
	}

	// 网络失败 → Checked=false，不算成功比对
	res, err = CheckGameUpdate("v3_old", func(string) ([]byte, error) { return nil, errors.New("timeout") })
	if err == nil || res.Checked {
		t.Fatalf("网络失败应当带错误返回: %+v err=%v", res, err)
	}
}

// TestSkyVersionCode 冒烟：真机上（装了平台）应能读到版本号；没装也不能崩。
func TestSkyVersionCode(t *testing.T) {
	code := SkyVersionCode()
	t.Logf("本地 VersionCode = %q", code)
}
