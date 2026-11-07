package ucsdk

// 自己实现 RSA 的 PKCS#1 v1.5 加密。
//
// 为什么不用 crypto/rsa：Go 1.24 起给 RSA 加了最小位数门槛（1024 位），
// 而九游下发的公钥只有 512 位。标准库会直接拒绝运算，且没有提供
// 「只对这一处放开」的接口——唯一的口子是 GODEBUG 环境变量，
// 那是进程级的，而且必须由外部设置，程序内改不了。
//
// 512 位 RSA 的加密本身很直接：
//
//	EM = 0x00 || 0x02 || 随机非零填充 || 0x00 || 明文
//	C  = EM^e mod n
//
// 这里只做「加密」这一个方向（公钥运算），不涉及私钥、不涉及签名，
// 因此不存在侧信道风险；填充校验也按 RFC 8017 的规则来。

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"math/big"
)

// errMessageTooLong 明文超过可加密长度。
var errMessageTooLong = errors.New("明文过长，超出该公钥可加密的长度")

// encryptPKCS1v15 用公钥做 PKCS#1 v1.5 加密，与 crypto/rsa.EncryptPKCS1v15 等价。
func encryptPKCS1v15(pub *rsa.PublicKey, msg []byte) ([]byte, error) {
	k := (pub.N.BitLen() + 7) / 8
	// 至少要有 11 字节开销：0x00、0x02、一个分隔 0x00，以及至少 8 字节随机数
	if len(msg) > k-11 {
		return nil, errMessageTooLong
	}

	em := make([]byte, k)
	em[0] = 0
	em[1] = 2
	psLen := k - len(msg) - 3
	ps := em[2 : 2+psLen]
	if err := fillNonZero(ps); err != nil {
		return nil, err
	}
	em[2+psLen] = 0
	copy(em[3+psLen:], msg)

	m := new(big.Int).SetBytes(em)
	if m.Cmp(pub.N) >= 0 {
		// 理论上不会发生（EM 首位是 0），保险起见还是要挡一下
		return nil, errors.New("待加密数据超出模数")
	}
	c := new(big.Int).Exp(m, big.NewInt(int64(pub.E)), pub.N)

	out := make([]byte, k)
	c.FillBytes(out)
	return out, nil
}

// fillNonZero 用随机非零字节填满填充串。PKCS#1 v1.5 要求填充字节不能为 0，
// 否则接收方会把填充提前截断。
func fillNonZero(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	// 一次多取一些，非零的留下，零的丢弃后继续取，避免逐字节读随机数
	buf := make([]byte, len(b)*2)
	need := len(b)
	off := 0
	for need > 0 {
		if off >= len(buf) {
			buf = make([]byte, need*2)
			off = 0
		}
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		off = 0
		for off < len(buf) && need > 0 {
			if buf[off] != 0 {
				b[len(b)-need] = buf[off]
				need--
			}
			off++
		}
	}
	return nil
}
