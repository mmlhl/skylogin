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
//  3. 公共 DNS 也问不到时（安全软件的"DNS 保护"、加速器接管全部 53 端口），
//     最后走一次 DoH：443 端口的 HTTPS，带证书校验，按 IP 直连。
//
// 只在解析失败时才会多花时间，正常网络下行为完全不变。
package netdns

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

// dohServer 是一个 DoH 端点：host 用来做 SNI 与证书校验，ip 用来拨号。
//
// 为什么不直接请求 https://dns.alidns.com：本机 DNS 已经坏了，这个名字自己
// 都解析不出来，必须先有 IP 才能握手。证书仍按 host 校验，不是"信任任意证书"。
type dohServer struct {
	host string
	ip   string
}

// dohServers 是最后一道兜底。只在 UDP 53 整体问不通时才会用到。
var dohServers = []dohServer{
	{host: "dns.alidns.com", ip: "223.5.5.5:443"},
	{host: "doh.pub", ip: "1.12.12.12:443"},
}

// systemLookup 是系统解析入口。抽成变量是为了让测试能模拟"本机解析不了"，
// 真机上永远指向 net.DefaultResolver。
var systemLookup = net.DefaultResolver.LookupHost

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
	addrs, sysErr := systemLookup(sysCtx, host)
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

	// ③ DoH 兜底：53 端口被安全软件 / 加速器整体接管时，上面那轮也问不到
	for _, s := range dohServers {
		if err := ctx.Err(); err != nil {
			break
		}
		if got := lookupViaDoH(ctx, s, host); len(got) > 0 {
			logf("DNS 兜底：系统与公共 DNS 都没答上来，改用 DoH（%s）解析 %s 成功", s.host, host)
			putCache(host, got, nil, okTTL)
			return got, nil
		}
	}

	if sysErr == nil {
		// 系统解析"成功"但一条记录都没有：这本身也是解析失败，别把 nil 传出去
		sysErr = fmt.Errorf("解析 %s 没有得到任何地址", host)
	}
	// 全挂时把"问了哪些地方"写进日志：用户看到的是 UI 上的一句 no such host，
	// 支持同学需要知道的是"系统 + 三个公共 DNS + DoH 都没答"，这是本地拦截，
	// 不是凭证问题，也不是服务端故障。
	logf("DNS 解析 %s 失败：系统与公共 DNS/DoH 均无结果（%v）；若浏览器能开网页，多半是 hosts、路由器 DNS 或安全软件拦了这个域名", host, sysErr)
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

// lookupViaDoH 用 DoH 解析一次，返回 A 记录（失败返回 nil）。
func lookupViaDoH(ctx context.Context, s dohServer, host string) []string {
	tr := &http.Transport{
		// 兜底路径一次就完，不值得留空闲连接（也可能把进程退出拖慢）。
		DisableKeepAlives: true,
		// 拨的是 IP，URL 里的主机名仍要用来校验证书；显式写 SNI 免得依赖实现细节。
		TLSClientConfig: &tls.Config{ServerName: s.host},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: publicTimeout}
			return d.DialContext(ctx, "tcp", s.ip)
		},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Timeout: publicTimeout, Transport: tr}

	u := "https://" + s.host + "/resolve?name=" + url.QueryEscape(host) + "&type=A"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	// dns-json：阿里 / 腾讯 / Google 都认这个格式。
	req.Header.Set("Accept", "application/dns-json")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil
	}
	return parseDoHAnswer(body)
}

// parseDoHAnswer 从 dns-json 应答里取 A 记录。
//
// 只接受 Status=0；CNAME(5) / AAAA(28) 一律跳过——这里要的是能直接拨的 v4 地址。
func parseDoHAnswer(body []byte) []string {
	var v struct {
		Status int `json:"Status"`
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if json.Unmarshal(body, &v) != nil || v.Status != 0 {
		return nil
	}
	var out []string
	for _, a := range v.Answer {
		if a.Type != 1 {
			continue
		}
		if ip := net.ParseIP(a.Data); ip != nil && ip.To4() != nil {
			out = append(out, a.Data)
		}
	}
	return out
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

// Wrap 给一个自己配好的 Transport 接上 DNS 兜底。
//
// 给"必须保留特殊设置"的调用方用：比如九游的扫码网关强制 HTTP/1.1
// （走 h2 时服务端处理表单会 500），不能直接换成 NewTransport() 的默认配置。
// 只替换 DialContext，其它字段原样保留。
func Wrap(t *http.Transport) *http.Transport {
	t.DialContext = dialContext
	return t
}
