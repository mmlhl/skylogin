package ucsdk

// 加密层的自检。
//
// 这里不连网：用抓包得到的真实样本验证本地实现能不能还原出同样的东西。
// 判据是「我们自己加密 → 用同样 key/iv 解开 → 内容和原文一致」，
// 以及「RSA 包裹出来的长度与格式符合 512 位公钥的预期」。

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
)

// 实测抓到的公钥（与 builtin.go 里的一致，这里独立写一份做双保险）
const sampleKey = "5|MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJBAMFxR3NxXU5Mf3U6wykuXYOU+OgXZgr/VyfNaXw4vOGImx0f7a9s9ASrHQAGsev8nqn7C5uLzTo8tiHWd5Cb/YsCAwEAAQ=="

func TestParsePublicKey(t *testing.T) {
	pub, err := parsePublicKey(sampleKey)
	if err != nil {
		t.Fatalf("解析公钥失败: %v", err)
	}
	if got := pub.N.BitLen(); got != 512 {
		t.Fatalf("公钥位数 = %d，期望 512", got)
	}
	if pub.E != 65537 {
		t.Fatalf("指数 = %d，期望 65537", pub.E)
	}
}

func TestParsePublicKeyRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "5|", "5|not-base64!!", "5|aGVsbG8="} {
		if _, err := parsePublicKey(s); err == nil {
			t.Fatalf("%q 应当解析失败", s)
		}
	}
}

// TestSealOpenRoundTrip 验证「加密 → 解密」自洽。
func TestSealOpenRoundTrip(t *testing.T) {
	pub, err := parsePublicKey(sampleKey)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"service":"unifiedAccount.sendSmsCode","data":{"mobile":"13800000000"}}`)

	env, err := seal(plain, pub)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	// RSA 包裹后的 key/iv 都应该是 64 字节（512 位模数）
	for name, s := range map[string]string{"k": env.K, "i": env.I} {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Fatalf("%s 不是合法 base64: %v", name, err)
		}
		if len(b) != 64 {
			t.Fatalf("%s 长度 = %d，期望 64（512 位）", name, len(b))
		}
	}
	if env.K == env.I {
		t.Fatal("key 与 iv 的密文不应相同（说明用了同一份随机数）")
	}

	// 用同一把 key/iv 解回来
	back, err := env.open(env.D)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if string(back) != string(plain) {
		t.Fatalf("往返不一致:\n原文 %s\n回来 %s", plain, back)
	}
}

// TestSealRandomisesEveryCall 每次的 key/iv 必须不同。
func TestSealRandomisesEveryCall(t *testing.T) {
	pub, err := parsePublicKey(sampleKey)
	if err != nil {
		t.Fatal(err)
	}
	a, err := seal([]byte("same-plaintext"), pub)
	if err != nil {
		t.Fatal(err)
	}
	b, err := seal([]byte("same-plaintext"), pub)
	if err != nil {
		t.Fatal(err)
	}
	if a.D == b.D {
		t.Fatal("两次加密同一明文得到了相同密文，随机数没有生效")
	}
	if string(a.key) == string(b.key) {
		t.Fatal("两次用了同一把 AES key")
	}
}

// TestPKCS7Unpad 校验去填充逻辑对边界值的处理。
func TestPKCS7Unpad(t *testing.T) {
	full := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	// 整块都是填充
	if got := pkcs7Unpad(make16(16)); len(got) != 0 {
		t.Fatalf("整块填充应解出空，实际 %d 字节", len(got))
	}
	// 填充值为 1
	padded := append([]byte{}, full[:15]...)
	padded = append(padded, 1)
	if got := pkcs7Unpad(padded); len(got) != 15 {
		t.Fatalf("去掉 1 字节填充后应为 15，实际 %d", len(got))
	}
	// 非法：填充值大于块长，应原样返回
	if got := pkcs7Unpad(make16(0x20)); len(got) != 16 {
		t.Fatalf("非法填充应原样返回，实际 %d", len(got))
	}
}

func make16(v byte) []byte {
	b := make([]byte, 16)
	for i := range b {
		b[i] = v
	}
	return b
}

// generateTestKey 生成一把测试用的 RSA 密钥（2048 位，标准库能直接处理）。
func generateTestKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

// rsaDecryptForTest 用私钥解开 PKCS#1 v1.5 密文，返回整段 EM（含填充）。
//
// 这里不用标准库的 DecryptPKCS1v15（它会直接去掉填充），
// 而是自己模幂后取回原始字节，好检查填充结构是否符合 RFC 8017。
func rsaDecryptForTest(priv *rsa.PrivateKey, ct []byte) ([]byte, error) {
	k := priv.Size()
	if len(ct) != k {
		return nil, fmt.Errorf("密文长度 %d，期望 %d", len(ct), k)
	}
	c := new(big.Int).SetBytes(ct)
	m := new(big.Int).Exp(c, priv.D, priv.N)
	out := make([]byte, k)
	m.FillBytes(out)
	return out, nil
}

// TestOpenRejectsBadLength 密文长度不是块大小整数倍时要报错而不是panic。
func TestOpenRejectsBadLength(t *testing.T) {
	env := &envelope{key: make([]byte, 16), iv: make([]byte, 16)}
	bad := base64.StdEncoding.EncodeToString([]byte("123456789"))
	if _, err := env.open(bad); err == nil {
		t.Fatal("长度不合法的密文应当报错")
	}
}

// TestEncryptPKCS1v15Shape 检查手写的 PKCS#1 v1.5 是否产出合法结构。
//
// 用私钥解回来验证：0x00 0x02 || 非零填充 || 0x00 || 明文。
func TestEncryptPKCS1v15Shape(t *testing.T) {
	priv, err := generateTestKey()
	if err != nil {
		t.Skip("生成测试密钥失败，跳过:", err)
	}
	msg := []byte("0123456789abcdef")
	ct, err := encryptPKCS1v15(&priv.PublicKey, msg)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	// 解开（用模幂运算的逆过程：这里直接调标准库的解密，密钥够大）
	plain, err := rsaDecryptForTest(priv, ct)
	if err != nil {
		t.Fatalf("解出失败: %v", err)
	}
	if plain[0] != 0 || plain[1] != 2 {
		t.Fatalf("填充头不对: %02x %02x", plain[0], plain[1])
	}
	sep := -1
	for i := 2; i < len(plain); i++ {
		if plain[i] == 0 {
			sep = i
			break
		}
	}
	if sep < 10 {
		t.Fatalf("填充分隔位位置异常: %d", sep)
	}
	if string(plain[sep+1:]) != string(msg) {
		t.Fatalf("明文不匹配: %q", plain[sep+1:])
	}
}

// TestRequestJSONShape 请求体字段名必须与抓包一致。
func TestRequestJSONShape(t *testing.T) {
	b, err := json.Marshal(request{
		ID:      1,
		Service: ServiceSendSms,
		Data:    map[string]any{"mobile": "1"},
		Game:    SkyGame(),
		Client:  bootstrapSegment(RandomDevice(), randomUUID(), ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "service", "data", "game", "client"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("请求体缺少字段 %s", k)
		}
	}
	g := m["game"].(map[string]any)
	for _, k := range []string{"gameId", "channelId", "apkChannelId", "versionCode", "versionName", "brandId", "packType"} {
		if _, ok := g[k]; !ok {
			t.Fatalf("game 段缺少字段 %s", k)
		}
	}
}

// 保证 crypto/rand 被用到（生成测试密钥需要）
var _ = rand.Read
