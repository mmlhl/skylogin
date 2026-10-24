package huawei

import "testing"

// 固定向量：ST 绑包名的结果必须稳定，防止以后改动破坏已保存的凭证。
func TestBindSTMatchesReference(t *testing.T) {
	st := "AQAAAAEXAMPLEserviceTokenVALUE0123456789"
	pkg := "com.netease.sky.huawei"
	const want = "AQAAAAEXAMPLEserviceeb98a8c92a89672abd9f86e30fd06bdf54647d3b9dd7db4261f7d29d208b1e3e"
	if got := BindST(st, pkg); got != want {
		t.Fatalf("BindST 不一致\n got=%s\nwant=%s", got, want)
	}
}

func TestBindSTEmpty(t *testing.T) {
	if got := BindST("", "x"); got != "" {
		t.Fatalf("空 ST 应返回空串，得到 %q", got)
	}
}

func TestXMLValue(t *testing.T) {
	xml := `<LoginByQrCodeRsp><serviceToken>ABC123</serviceToken><nickName>小明</nickName></LoginByQrCodeRsp>`
	if got := xmlValue(xml, "serviceToken"); got != "ABC123" {
		t.Fatalf("serviceToken=%q", got)
	}
	if got := AccountName(xml); got != "小明" {
		t.Fatalf("AccountName=%q", got)
	}
	if got := xmlValue(xml, "missing"); got != "" {
		t.Fatalf("缺失标签应返回空串，得到 %q", got)
	}
}
