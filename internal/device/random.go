package device

// 随机值的生成：全部走 crypto/rand，避免同一时刻生成出重复的指纹。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
)

// randomHex 生成 n 字节的十六进制串（长度 2n）。
func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// randomDigits 生成 n 位十进制数字串。
func randomDigits(n int) string {
	buf := make([]byte, n)
	for i := range buf {
		v, _ := rand.Int(rand.Reader, big.NewInt(10))
		buf[i] = byte('0' + v.Int64())
	}
	return string(buf)
}

// randInt 返回 [0, n) 的随机整数。
func randInt(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

// newUUID 生成标准 v4 格式的 uuid 字符串。
func newUUID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
