package channel

import (
	"errors"
	"net"
	"net/url"
	"testing"
)

// B站登录失败要能分清"网络不通"和"凭证坏了"。
//
// 2026-11-10 有用户的机器连 baidu / qq / github 全部 10061（连接被拒），
// B站登录因此失败；旧代码把它说成"B站凭证验证失败"，用户拿着好端端的
// access_key 去重扫，白折腾。

func TestIsNetworkDNS(t *testing.T) {
	dns := &net.DNSError{Err: "no such host", Name: "wpg-api.biligame.com", IsNotFound: true}
	if !IsNetwork(&url.Error{Op: "Post", URL: "https://wpg-api.biligame.com", Err: dns}) {
		t.Fatal("DNS 失败应判为网络类")
	}
}

func TestIsNetworkRefused(t *testing.T) {
	// 用户实际报的那个：connectex ... actively refused
	op := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connectex: actively refused")}
	if !IsNetwork(&url.Error{Op: "Post", URL: "https://wpg-api.biligame.com", Err: op}) {
		t.Fatal("连接被拒应判为网络类")
	}
}

func TestIsNetworkBusinessError(t *testing.T) {
	// 服务端说 access_key 失效，这不是网络问题
	err := errors.New("access_key 已失效（code=1）")
	if IsNetwork(err) {
		t.Fatal("业务错误不该判成网络类")
	}
}
