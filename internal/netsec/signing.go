package netsec

// X-Client-Sign 的算法与几个随机串工具。
//
// 签名串 = 方法名 + 路径（含 query）+ 请求体，用 log_key 做 HMAC-SHA256。
// 路径要去掉 scheme 和主机，只留第一个 "/" 开始的部分。

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"net/url"
	"strings"
)

// signingText 把一次请求压成待签名的字符串。
func signingText(method, rawurl, body string) string {
	if u, err := url.Parse(rawurl); err == nil {
		return strings.ToUpper(method) + u.RequestURI() + body
	}
	// 解析失败就退回手工截断（和上面的结果保持一致）
	rest := rawurl
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		rest = rest[i:]
	} else {
		rest = ""
	}
	return strings.ToUpper(method) + rest + body
}

// HMACSign 算出 X-Client-Sign 的值（小写十六进制）。
func HMACSign(method, rawurl, body, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(signingText(method, rawurl, body)))
	return hex.EncodeToString(mac.Sum(nil))
}

// randHex 生成 n 位"看起来随机"的十六进制字符（大小写混排）。
func randHex(n int) string {
	const pool = "0123456789abcdefABCDEF"
	out := make([]byte, n)
	for i := range out {
		out[i] = pool[rand.Intn(len(pool))]
	}
	return string(out)
}

// randDigits 生成 n 位数字串。
func randDigits(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte('0' + rand.Intn(10))
	}
	return string(out)
}
