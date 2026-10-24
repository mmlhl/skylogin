package fever

import "testing"

// 临时探针：验证只靠注册表也能找到光遇。
func TestFindSkyProbe(t *testing.T) {
	p, err := FindSky()
	if err != nil {
		// CI 上没有装光遇，这种情况跳过而不是失败
		t.Skipf("本机没装光遇，跳过: %v", err)
	}
	t.Logf("找到: %s", p)
}
