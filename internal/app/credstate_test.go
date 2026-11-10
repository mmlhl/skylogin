package app

import (
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"skylogin/internal/mpay"
)

// 凭证判定的分界线：网络类失败只能说"暂时无法确认"，不许说"已失效"。

func TestCredErrFromNetworkIsUnverified(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "service.mkey.163.com", IsNotFound: true}
	err := credErrFrom(&url.Error{Op: "Get", URL: "https://x", Err: dnsErr})
	if err == nil {
		t.Fatal("不应返回 nil")
	}
	if credExpired(err) {
		t.Fatal("DNS 失败被判成凭证失效——这正是日志里 110 次误判的来源")
	}
	if !credUnverified(err) {
		t.Fatal("DNS 失败应归为暂时无法确认")
	}
}

func TestCredErrFromAuthIsExpired(t *testing.T) {
	err := credErrFrom(&mpay.TicketError{Code: 1306})
	if !credExpired(err) {
		t.Fatal("1306 应判为凭证已失效")
	}
	if credUnverified(err) {
		t.Fatal("1306 不该同时是暂时无法确认")
	}
}

func TestCredErrFrom6008IsUnverified(t *testing.T) {
	// 记住槽未命中：既不是网络问题，也绝不能判凭证失效
	err := credErrFrom(&mpay.TicketError{Code: 6008})
	if credExpired(err) {
		t.Fatal("6008 不能判凭证失效")
	}
}

func TestCredErrFromNil(t *testing.T) {
	if err := credErrFrom(nil); err != nil {
		t.Fatalf("nil 应返回 nil，实际 %v", err)
	}
}

// 重登节流：冷却窗口内同一账号只允许一次，避免同设备反复顶号。
func TestAllowReloginCooldown(t *testing.T) {
	key := "test-cooldown"
	resetReloginThrottle(key)
	defer resetReloginThrottle(key)

	if !allowRelogin(key) {
		t.Fatal("首次应允许重登")
	}
	if allowRelogin(key) {
		t.Fatal("冷却窗口内应拒绝，否则会重演日志里的重登风暴")
	}
	// 用户手动刷新时清掉记录，应立刻又能重登
	resetReloginThrottle(key)
	if !allowRelogin(key) {
		t.Fatal("重置后应允许重登")
	}
}

func TestAllowReloginDifferentAccounts(t *testing.T) {
	a, b := "test-acc-a", "test-acc-b"
	resetReloginThrottle(a)
	resetReloginThrottle(b)
	defer resetReloginThrottle(a)
	defer resetReloginThrottle(b)

	if !allowRelogin(a) || !allowRelogin(b) {
		t.Fatal("不同账号之间不应互相影响")
	}
}

func TestReloginCooldownValue(t *testing.T) {
	// 冷却时间要够长：日志里同一账号 52 秒内重登 5 次，窗口太短挡不住
	if reloginCooldown < 5*time.Minute {
		t.Fatalf("冷却窗口 %v 太短，挡不住日志里的重登风暴", reloginCooldown)
	}
}

func TestCredErrChain(t *testing.T) {
	// 上层用 errors.Is 判断，包装链必须保持完整
	err := credErrFrom(errors.New("某种说不清的错误"))
	if !errors.Is(err, errCredUnverified) {
		t.Fatal("包装后 errors.Is 应仍能认出哨兵错误")
	}
}
