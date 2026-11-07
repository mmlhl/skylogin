package ucsdk

// 用真实抓包样本对账。
//
// 这些样本来自 2026-11-07 的实机抓包（见 .ai/10）：hook 拿到了每次加解密
// 的 key/iv 和明文，网络侧拿到了密文。如果本地实现能把抓包里的密文
// 还原成抓包里的明文，就说明解密逻辑是对的。

import (
	"encoding/base64"
	"strings"
	"testing"
)

// 抓包样本：一次响应解密（CRYPT#8）
//
//	key = 2uJbjaBHvbO0rFOH5wTMsw==（16 字节随机）
//	iv  = dyKte243+6ahswkzsdVvkQ==
//	密文 408 个 base64 字符，明文 293 字节
const (
	sampleKeyB64 = "2uJbjaBHvbO0rFOH5wTMsw=="
	sampleIVB64  = "dyKte243+6ahswkzsdVvkQ=="
	// 密文取 CRYPT#8 的 IN（响应密文）
	sampleCipherB64 = "BHEpVyc2GZrbM3GnvuLZT/0TUW4CLtT1rqmLSr6EjhpgA2iFQYsuMPIA5ffwIarJrpII7k4ElV/O0VbetOdje6RhPU33P9FSbz8xKpCrmza/t4sV8zxUDaM1RhvF7qr8RiQ74YISB3RGMICVDZNeNcSC/PJy1NQmK0pRq33wSYl2mWyKKnXI6boN2SYL6wkbw1zmRqZYSLWMboQF48smXR+vUtiQARZXOT0yRjsmqMZtoFqhqZMAKA2eC5QfP2zuRVBJJv+OPB4nxF8uKQG8sIyLFH75+dSdidSDNRDGldPzgcYmrxo97ut2iieJ3Z0O98PJD9OplwcJbhL0fyKXVexM9Mkx5iX5zu9bBP5vJOcU8nROJrYQBgbNoZBsagngIwfEHvbYUTiHV6s5GHKrcw=="
)

func TestDecryptCapturedResponse(t *testing.T) {
	key, err := base64.StdEncoding.DecodeString(sampleKeyB64)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := base64.StdEncoding.DecodeString(sampleIVB64)
	if err != nil {
		t.Fatal(err)
	}
	env := &envelope{key: key, iv: iv}

	plain, err := env.open(sampleCipherB64)
	if err != nil {
		t.Fatalf("解密抓包样本失败: %v", err)
	}
	got := string(plain)
	// 抓包里这段明文是响应 JSON 的开头
	if !strings.HasPrefix(got, `{"state":{"code":1`) {
		t.Fatalf("明文与抓包不一致，实际开头: %q", got[:min(60, len(got))])
	}
	if !strings.Contains(got, `"msg":"操作成功"`) {
		t.Fatalf("明文里没找到预期内容: %q", got[:min(120, len(got))])
	}
}

// TestSealMatchesCapturedShape 检查我们加密出来的结构与抓包的密文同构：
// k/i 都是 64 字节（512 位 RSA 的输出），d 长度是 16 的整数倍。
func TestSealMatchesCapturedShape(t *testing.T) {
	pub, err := parsePublicKey(builtinPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	env, err := seal([]byte(`{"service":"unifiedAccount.sendSmsCode"}`), pub)
	if err != nil {
		t.Fatal(err)
	}
	for name, field := range map[string]string{"k": env.K, "i": env.I} {
		b, err := base64.StdEncoding.DecodeString(field)
		if err != nil {
			t.Fatalf("%s 不是合法 base64: %v", name, err)
		}
		if len(b) != 64 {
			t.Fatalf("%s 长度 %d，抓包里的都是 64 字节", name, len(b))
		}
	}
	d, _ := base64.StdEncoding.DecodeString(env.D)
	if len(d)%16 != 0 {
		t.Fatalf("d 长度 %d 不是 16 的倍数", len(d))
	}
}
