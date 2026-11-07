package ucsdk

// 加密信封。
//
// 请求：先把业务 JSON（{id, service, data, game, client}）用随机 AES key/iv
// 加密，再用服务端下发的 RSA 公钥把 key/iv 各加密一份，拼成
//
//	{"k": base64(RSA(key)), "i": base64(RSA(iv)), "d": base64(AES(正文))}
//
// 响应：{"c": 状态码, "d": base64(AES(结果))}。c 为 1 表示成功。
//
// 几个实测确认的点（见 .ai/10）：
//   - 对称算法是 AES/CBC/PKCS7，不是在 Java 里声明的那种 PKCS5
//   - RSA 公钥只有 512 位，格式是「5|」+ base64(DER)
//   - key 与 iv 每次随机，互不相同

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// rsaMinBits 是解析公钥时接受的最小位数。
//
// 九游下发的是 512 位，比现代标准低很多，但这是服务端定的，没有协商余地。
// 只用于校验收到的公钥是否合理，不影响能否运算（加密路径见 rsa_pkcs1.go）。
const rsaMinBits = 512

// parsePublicKey 解析 getSecurityKey 下发的公钥。
//
// 实际格式是「5|MFwwDQYJ…」，竖线前面是密钥版本号，后面才是 base64 的
// PKIX DER。版本号目前只会遇到 5，但解析时不做假设，只按分隔符取后半段。
func parsePublicKey(raw string) (*rsa.PublicKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("公钥为空")
	}
	if i := strings.Index(raw, "|"); i >= 0 {
		raw = raw[i+1:]
	}
	der, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("公钥不是合法 base64: %w", err)
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("公钥解析失败: %w", err)
	}
	pub, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("下发的不是 RSA 公钥")
	}
	if pub.N.BitLen() < rsaMinBits {
		return nil, fmt.Errorf("公钥位数 %d 太小", pub.N.BitLen())
	}
	return pub, nil
}

// pkcs7Pad 按块大小补齐。九游用的是 PKCS7，与 PKCS5 在 16 字节块下等价。
func pkcs7Pad(b []byte, size int) []byte {
	n := size - len(b)%size
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}

// pkcs7Unpad 去掉补齐字节；数据不规整时原样返回，交给上层判断。
func pkcs7Unpad(b []byte) []byte {
	n := len(b)
	if n == 0 || n%16 != 0 {
		return b
	}
	p := int(b[n-1])
	if p <= 0 || p > 16 || p > n {
		return b
	}
	return b[:n-p]
}

// envelope 是一次请求的加密结果，同时保留原始 key/iv 供解密响应用。
type envelope struct {
	K string // base64(RSA(key))
	I string // base64(RSA(iv))
	D string // base64(AES(正文))

	key []byte
	iv  []byte
}

// seal 把明文加密成九游要求的信封。
//
// 字段语义（引导公钥、业务请求两条路一致，不存在交叉）：
//
//	k = RSA(AES key)
//	i = RSA(AES IV)
//	d = AES(正文)
//
// 已用算法日志逐字节核实：createRequestJson(28) 加密 key 并产出 k，
// createRequestJson(29) 加密 iv 并产出 i。写反了外套仍是 c=1，但响应解不开。
func seal(plain []byte, pub *rsa.PublicKey) (*envelope, error) {
	key := make([]byte, 16)
	iv := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)

	// 公钥只有 512 位，标准库会拒算，所以用自己的 PKCS#1 v1.5 实现。
	// 包裹的就是 16 字节原始 key/iv，不做 base64（已用算法日志比对确认）。
	ek, err := encryptPKCS1v15(pub, key)
	if err != nil {
		return nil, fmt.Errorf("RSA 加密 key 失败: %w", err)
	}
	ei, err := encryptPKCS1v15(pub, iv)
	if err != nil {
		return nil, fmt.Errorf("RSA 加密 iv 失败: %w", err)
	}
	return &envelope{
		K:   base64.StdEncoding.EncodeToString(ek),
		I:   base64.StdEncoding.EncodeToString(ei),
		D:   base64.StdEncoding.EncodeToString(ct),
		key: key,
		iv:  iv,
	}, nil
}

// tryDecrypt 用给定 key/iv 解一段密文，成功后返回明文。
//
// 判定标准是「解出来像 JSON」：长度对齐、去填充后是合法 UTF-8、以 { 或 [ 开头。
// 只要不满足就当作密钥或层数不对，交给调用方换一种试。
func tryDecrypt(key, iv, ct []byte) ([]byte, bool) {
	if len(ct) == 0 || len(ct)%16 != 0 {
		return nil, false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, false
	}
	pt := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(pt, ct)
	pt = pkcs7Unpad(pt)
	if len(pt) == 0 || !utf8.Valid(pt) {
		return nil, false
	}
	if pt[0] != '{' && pt[0] != '[' {
		return nil, false
	}
	return pt, true
}

// open 用请求时的 key/iv 解响应正文。
//
// 实测两种形态都存在，且不能统一：
//   - system.getSecurityKey 的 d 是 base64(密文)，解一层即可
//   - 业务接口的 d 是 base64(base64(密文))，比取公钥那条多套了一层
//
// 所以这里不写死层数，两种都试，用「解出来像 JSON」作为验收条件。
func (e *envelope) open(d string) ([]byte, error) {
	if d == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(d)
	if err != nil {
		return nil, fmt.Errorf("响应不是合法 base64: %w", err)
	}
	if pt, ok := tryDecrypt(e.key, e.iv, raw); ok {
		return pt, nil
	}
	// 多套一层的情形：把解出来的字节当成又一段 base64 再试
	if inner, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); err == nil {
		if pt, ok := tryDecrypt(e.key, e.iv, inner); ok {
			return pt, nil
		}
	}
	return nil, fmt.Errorf("响应解密失败（%d 字节，两种层数都不匹配）", len(raw))
}
