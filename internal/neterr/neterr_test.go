package neterr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"
)

func TestDNSFailure(t *testing.T) {
	dns := &net.DNSError{Err: "no such host", Name: "service.mkey.163.com", IsNotFound: true}
	if !IsNetwork(&url.Error{Op: "Get", URL: "https://x", Err: dns}) {
		t.Fatal("DNS 解析失败必须判为网络类")
	}
}

// 用户实际报的那个错：connectex ... actively refused（Windows 10061）
func TestConnectionRefused(t *testing.T) {
	op := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.Errno(10061)}
	if !IsNetwork(&url.Error{Op: "Post", URL: "https://wpg-api.biligame.com", Err: op}) {
		t.Fatal("连接被拒绝必须判为网络类")
	}
}

func TestTimeout(t *testing.T) {
	for _, err := range []error{
		context.DeadlineExceeded,
		&url.Error{Op: "Post", URL: "https://x", Err: context.DeadlineExceeded},
	} {
		if !IsNetwork(err) {
			t.Fatalf("%v 应判为网络类", err)
		}
	}
}

func TestBusinessErrorIsNotNetwork(t *testing.T) {
	for _, err := range []error{
		errors.New("access_key 已失效（code=1）"),
		fmt.Errorf("渠道身份验证失败：登录状态失效"),
		nil,
	} {
		if IsNetwork(err) {
			t.Fatalf("%v 不该判成网络类", err)
		}
	}
}

func TestWrap(t *testing.T) {
	// 认不出来的错误，显式标记后应能被 IsNetwork 认出
	w := Wrap(errors.New("连接失败"))
	if !IsNetwork(w) {
		t.Fatal("Wrap 之后应判为网络类")
	}
	// 已经是网络类的不重复包装
	dns := &net.DNSError{Err: "no such host", IsNotFound: true}
	if got := Wrap(dns); got != dns {
		t.Fatal("已经是网络类时不该再包一层")
	}
	if Wrap(nil) != nil {
		t.Fatal("nil 应返回 nil")
	}
}

func TestWrappedChainSurvives(t *testing.T) {
	// fmt.Errorf 用 %s 会断开错误链，用 %w 才能穿透——channelSAUTH 必须用 %w
	dns := &net.DNSError{Err: "no such host", IsNotFound: true}
	withW := fmt.Errorf("渠道身份验证失败（%w）", dns)
	if !IsNetwork(withW) {
		t.Fatal("%w 包装后应仍能识别网络类")
	}
	withS := fmt.Errorf("渠道身份验证失败（%s）", dns.Error())
	if IsNetwork(withS) {
		// 这条断言是在提醒：用 s 动词包装会断开错误链，网络错误一律用 w
		t.Fatal("用 s 动词包装后不该还能识别（说明错误链被断开了）")
	}
}
