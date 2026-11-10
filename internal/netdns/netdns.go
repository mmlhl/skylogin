// Package netdns 给 HTTP 请求加一层 DNS 兜底。
//
// 为什么需要它
//
// 2026-11-10 有用户反馈：他那台机器上 service.mkey.163.com 直接解析不出来
// （日志里满屏 "lookup service.mkey.163.com: no such host"），但同一个域名在
// 别的网络里正常；更关键的是**加载湾域名还能通**（返回了真实的 code=6008），
// 说明不是断网，而是单个域名的解析出了问题——路由器 DNS、hosts、运营商
// 拦截、安全软件按域名拦，都会长这样。
//
// 这种故障以前会被上层当成"凭证过期"，让玩家白刷一遍凭证，其实凭证是好的。
// 所以这里做两件事：
//
//  1. 系统解析加 4 秒上限。坏掉的 DNS 有时不是立刻 NXDOMAIN，而是一直不答，
//     不设上限会把整个签票流程拖死。
//  2. 系统解析失败时依次问公共 DNS（阿里 / 腾讯 / 百度），任意一个答上来就用。
//
// 只在解析失败时才会多花时间，正常网络下行为完全不变。
package netdns

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// systemTimeout 系统解析最多等这么久。超时说明本机 DNS 不可用，直接转兜底。
	systemTimeout = 4 * time.Second
	// publicTimeout 单个公共 DNS 的查询上限。
	publicTimeout = 3 * time.Second
	// okTTL / failTTL 解析结果缓存。失败也缓存一小会儿，
	// 否则一次签票流程里几个请求会各自把整套兜底重跑一遍。
	okTTL   = 5 * time.Minute
	failTTL = 30 * time.Second
)

// publicDNS 是兜底用的公共 DNS，按顺序试。
var publicDNS = []string{
	"223.5.5.5:53",    // 阿里
	"119.29.29.29:53", // 腾讯
	"180.76.76.76:53", // 百度
}

type cacheEntry struct {
	addrs  []string
	err    error
	expire time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}

	// 日志函数由上层注入。用 setter + 读写锁而不是导出变量：
	// 它会在请求 goroutine 里被读，直接赋值属于数据竞争。
	logfMu  sync.RWMutex
	logfVal = func(format string, args ...any) {}
)

// SetLogf 注入日志函数（传 nil 表示不记录）。
func SetLogf(f func(format string, args ...any)) {
	if f == nil {
		return
	}
	logfMu.Lock()
	logfVal = f
	logfMu.Unlock()
}

// logf 内部用：取当前日志函数再调用，避免持锁执行外部代码。
func logf(format string, args ...any) {
	logfMu.RLock()
	f := logfVal
	logfMu.RUnlock()
	f(format, args...)
}

// LookupHost 解析主机名：先系统，失败再公共 DNS。
func LookupHost(ctx context.Context, host string) ([]string, error) {
	if host == "" {
		return nil, fmt.Errorf("主机名为空")
	}
	if ip := net.ParseIP(host); ip != nil {
		return []string{host}, nil
	}
	if addrs, err, ok := fromCache(host); ok {
		return addrs, err
	}

	// ① 系统解析，带 4 秒上限
	sysCtx, cancel := context.WithTimeout(ctx, systemTimeout)
	addrs, sysErr := net.DefaultResolver.LookupHost(sysCtx, host)
	cancel()
	if sysErr == nil && len(addrs) > 0 {
		putCache(host, addrs, nil, okTTL)
		return addrs, nil
	}

	// ② 公共 DNS 兜底
	for _, server := range publicDNS {
		if err := ctx.Err(); err != nil {
			break
		}
		if got := lookupVia(ctx, server, host); len(got) > 0 {
			logf("DNS 兜底：系统解析 %s 失败（%v），改用 %s 解析成功", host, sysErr, server)
			putCache(host, got, nil, okTTL)
			return got, nil
		}
	}

	if sysErr == nil {
		// 系统解析"成功"但一条记录都没有：这本身也是解析失败，别把 nil 传出去
		sysErr = fmt.Errorf("解析 %s 没有得到任何地址", host)
	}
	putCache(host, nil, sysErr, failTTL)
	return nil, sysErr
}

// lookupVia 用指定的 DNS 服务器解析一次。
func lookupVia(ctx context.Context, server, host string) []string {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: publicTimeout}
			return d.DialContext(ctx, "udp", server)
		},
	}
	cctx, cancel := context.WithTimeout(ctx, publicTimeout)
	defer cancel()
	addrs, err := r.LookupHost(cctx, host)
	if err != nil {
		return nil
	}
	return addrs
}

func fromCache(host string) ([]string, error, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	e, ok := cache[host]
	if !ok || time.Now().After(e.expire) {
		return nil, nil, false
	}
	return e.addrs, e.err, true
}

func putCache(host string, addrs []string, err error, ttl time.Duration) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[host] = cacheEntry{addrs: addrs, err: err, expire: time.Now().Add(ttl)}
}

// dialContext 是带兜底的拨号：自己解析域名，再按解析结果拨 IP。
//
// 只替换"域名 → IP"这一步，TLS 的 SNI 仍由 http.Transport 按 URL 里的
// 主机名设置，所以证书校验不受影响。
func dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// 没有端口（极少见）：交给标准拨号处理
		return d.DialContext(ctx, network, addr)
	}
	if net.ParseIP(host) != nil {
		return d.DialContext(ctx, network, addr)
	}

	addrs, lerr := LookupHost(ctx, host)
	if lerr != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: lerr}
	}
	var lastErr error
	for _, ip := range addrs {
		conn, derr := d.DialContext(ctx, network, net.JoinHostPort(ip, port))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("解析 %s 没有得到可用地址", host)
	}
	return nil, lastErr
}

// NewClient 返回一个带 DNS 兜底的 http.Client。
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: NewTransport()}
}

var (
	sharedOnce      sync.Once
	sharedTransport *http.Transport
)

// NewTransport 返回带 DNS 兜底的 Transport。
//
// 全局共享一个实例：http.Client 本身很轻，真正值钱的是 Transport 里的
// 连接池。每次新建 Transport 等于每次重开 TCP + TLS，签票这种连续请求
// 会明显变慢（原始代码用 http.DefaultTransport 就是共享的，别退化掉）。
func NewTransport() *http.Transport {
	sharedOnce.Do(func() {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.DialContext = dialContext
		sharedTransport = t
	})
	return sharedTransport
}
