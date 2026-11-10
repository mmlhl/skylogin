// Package neterr 判断一个错误是不是"没能问到服务端"。
//
// 为什么单独成包：mpay 和 channel 两条链路都要做这个判断，各写一份迟早会
// 走样，而走样的代价很具体——2026-11-10 的用户日志里 110 次"token 已失效"
// 全部来自 DNS 解析失败，凭证是好的，误判引发了一轮重登风暴。
//
// 判据只有一条：错误发生在连接层（DNS、拨号、TLS、超时），而不是服务端
// 给了业务应答。连接层失败说明**我们没问到**，对凭证好坏一无所知；
// 只有服务端明确回了错误码，才有资格说凭证有问题。
package neterr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// ErrNetwork 是"网络不可达"的哨兵错误，供调用方用 errors.Is 判断。
var ErrNetwork = errors.New("网络不可达")

// IsNetwork 判断这次失败是不是网络原因。
func IsNetwork(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNetwork) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	// url.Error 只包 RoundTrip 阶段的问题，都是连接层
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return false
}

// Wrap 把错误显式标记成网络类。
//
// 只在"错误本身认不出来、但业务上确定是网络原因"时用（比如某个渠道把
// 连接失败翻译成了自定义文案）。已经是网络类时原样返回，不会重复包装。
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	if IsNetwork(err) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrNetwork, err)
}
