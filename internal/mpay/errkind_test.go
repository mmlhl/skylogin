package mpay

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
)

// 这一组测试盯着一件事：**只有服务端明确说凭证失效，才能判凭证失效**。
// 2026-11-10 的日志里 110 次"token 已失效"全部来自 DNS 解析失败，
// 凭证本身是好的，误判引发了一轮重登风暴。

func TestClassifyDNSFailureIsNetwork(t *testing.T) {
	// 日志里真实的形态：url.Error 包着 net.DNSError
	dnsErr := &net.DNSError{Err: "no such host", Name: "service.mkey.163.com", IsNotFound: true}
	err := &url.Error{Op: "Get", URL: "https://service.mkey.163.com/x", Err: dnsErr}
	if got := Classify(err); got != KindNetwork {
		t.Fatalf("DNS 解析失败应判为网络类，实际 %v", got)
	}
	if IsAuth(err) {
		t.Fatal("DNS 失败绝不能被当成凭证失效")
	}
}

func TestClassifyTimeoutIsNetwork(t *testing.T) {
	for _, err := range []error{
		context.DeadlineExceeded,
		&url.Error{Op: "Post", URL: "https://x", Err: context.DeadlineExceeded},
	} {
		if got := Classify(err); got != KindNetwork {
			t.Fatalf("%v 应判为网络类，实际 %v", err, got)
		}
	}
}

func TestClassifyNonJSONIsNetwork(t *testing.T) {
	err := &NotJSONError{Path: "/mpay/api/users/create_ticket", Body: "<html>502</html>"}
	if got := Classify(err); got != KindNetwork {
		t.Fatalf("非 JSON 响应应判为网络类，实际 %v", got)
	}
}

func TestClassify1306IsAuth(t *testing.T) {
	err := &TicketError{Code: 1306, Reason: "用户登录失效"}
	if !IsAuth(err) {
		t.Fatal("1306 是服务端明确说凭证失效，必须判为鉴权失败")
	}
}

// 6008 是加载湾"记住槽没命中"的正常回退信号，不是鉴权失败。
// 每个没走过扫码写槽的账号都会拿到它，当成失效会把这类账号一次性全误标。
func TestClassify6008IsNotAuth(t *testing.T) {
	err := &TicketError{Code: 6008, Reason: "登录已失效，重新扫码授权"}
	if IsAuth(err) {
		t.Fatal("6008 不能判为凭证失效（它是记住槽未命中，属正常回退）")
	}
	if got := Classify(err); got != KindRejected {
		t.Fatalf("6008 应判为服务端拒绝，实际 %v", got)
	}
}

func TestClassifyWrappedTicketError(t *testing.T) {
	inner := &TicketError{Code: 1306}
	err := errors.Join(errors.New("签票失败"), inner)
	if !IsAuth(err) {
		t.Fatal("包了一层之后仍应认出 1306")
	}
}

func TestClassifyNilAndUnknown(t *testing.T) {
	if got := Classify(nil); got != KindUnknown {
		t.Fatalf("nil 应判为未知，实际 %v", got)
	}
	if got := Classify(errors.New("用户资料里没有 user")); got != KindUnknown {
		t.Fatalf("说不清的错误应判为未知，实际 %v", got)
	}
}
