package netdns

// 这里测的是"系统解析失败之后"的那条链：公共 DNS 兜底、DoH 兜底、缓存。
// 真机上系统 DNS 是好的，兜底永远不被触发，所以必须把系统解析换成假的才能测到。

import (
	"context"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// 假 DNS 只答 A 记录；AAAA 回空（NODATA），Go 的解析器会当成"没有 v6"。
func dnsReply(q []byte, ip net.IP) []byte {
	if len(q) < 12 {
		return nil
	}
	i := 12
	for i < len(q) && q[i] != 0 {
		i += int(q[i]) + 1
	}
	if i+5 > len(q) {
		return nil
	}
	qtype := binary.BigEndian.Uint16(q[i+1:])
	end := i + 5 // QNAME 的 0x00 + QTYPE + QCLASS
	out := make([]byte, end, end+16)
	copy(out, q[:end])
	out[2] = 0x81                           // QR=1, RD=1
	out[3] = 0x80                           // RA=1, RCODE=0
	binary.BigEndian.PutUint16(out[6:], 0)  // ANCOUNT
	binary.BigEndian.PutUint16(out[8:], 0)  // NSCOUNT
	binary.BigEndian.PutUint16(out[10:], 0) // ARCOUNT：请求里的 EDNS OPT 不抄回来
	if qtype == 1 {
		if v4 := ip.To4(); v4 != nil {
			out = append(out, 0xC0, 0x0C, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x3C, 0x00, 0x04)
			out = append(out, v4...)
			binary.BigEndian.PutUint16(out[6:], 1)
		}
	}
	return out
}

func startFakeDNS(t *testing.T, ip net.IP) (string, *atomic.Int32) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起假 DNS 失败: %v", err)
	}
	var hits atomic.Int32
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			hits.Add(1)
			if resp := dnsReply(buf[:n], ip); resp != nil {
				_, _ = pc.WriteTo(resp, from)
			}
		}
	}()
	t.Cleanup(func() { _ = pc.Close() })
	return pc.LocalAddr().String(), &hits
}

// isolate 把整条解析链换掉：系统解析用传入的函数，公共 DNS 指向假服务器，
// DoH 关掉（否则测试会真连外网）。
func isolate(t *testing.T, dnsAddr string, sys func(context.Context, string) ([]string, error)) {
	t.Helper()
	oldDNS, oldSys, oldDoH := publicDNS, systemLookup, dohServers
	publicDNS = []string{dnsAddr}
	systemLookup = sys
	dohServers = nil
	clearCache()
	t.Cleanup(func() {
		publicDNS, systemLookup, dohServers = oldDNS, oldSys, oldDoH
		clearCache()
	})
}

func clearCache() {
	cacheMu.Lock()
	cache = map[string]cacheEntry{}
	cacheMu.Unlock()
}

func systemFails(_ context.Context, host string) ([]string, error) {
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func TestLookupHostFallsBackToPublicDNS(t *testing.T) {
	addr, hits := startFakeDNS(t, net.IPv4(203, 0, 113, 7))
	isolate(t, addr, systemFails)

	addrs, err := LookupHost(context.Background(), "fallback.test")
	if err != nil {
		t.Fatalf("系统解析失败时应当由公共 DNS 兜底: %v", err)
	}
	if len(addrs) != 1 || addrs[0] != "203.0.113.7" {
		t.Fatalf("兜底解析结果不对: %v", addrs)
	}
	if hits.Load() == 0 {
		t.Fatal("没有真的去问公共 DNS")
	}

	// 一次签票流程里同一个域名会被问好几次，每次都重跑 4 秒系统超时会把流程拖死。
	before := hits.Load()
	if _, err := LookupHost(context.Background(), "fallback.test"); err != nil {
		t.Fatalf("第二次解析失败: %v", err)
	}
	if hits.Load() != before {
		t.Fatalf("成功结果没有缓存，又问了一次 DNS（%d → %d）", before, hits.Load())
	}
}

func TestLookupHostPrefersSystem(t *testing.T) {
	addr, hits := startFakeDNS(t, net.IPv4(203, 0, 113, 8))
	isolate(t, addr, func(context.Context, string) ([]string, error) {
		return []string{"192.0.2.1"}, nil
	})

	addrs, err := LookupHost(context.Background(), "system.test")
	if err != nil {
		t.Fatalf("系统解析成功时不该报错: %v", err)
	}
	if len(addrs) != 1 || addrs[0] != "192.0.2.1" {
		t.Fatalf("应当用系统解析结果: %v", addrs)
	}
	if hits.Load() != 0 {
		t.Fatalf("系统解析已经成功，不该再去问公共 DNS（问了 %d 次）", hits.Load())
	}
}

func TestLookupHostAllFailCachesError(t *testing.T) {
	// 127.0.0.1:1 上没有 DNS 服务，一拨就回端口不可达
	isolate(t, "127.0.0.1:1", systemFails)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	if _, err := LookupHost(ctx, "fail.test"); err == nil {
		t.Fatal("系统和公共 DNS 都失败时应当报错")
	}

	// 失败也要缓存：否则同一次启动里每个请求都要重付一遍 4 秒系统超时
	start := time.Now()
	if _, err := LookupHost(ctx, "fail.test"); err == nil {
		t.Fatal("第二次也应当报错")
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("失败结果没有被缓存，第二次耗时 %v", d)
	}
}

func TestParseDoHAnswer(t *testing.T) {
	body := []byte(`{"Status":0,"Answer":[
		{"name":"mgb.example.","type":5,"data":"target.example."},
		{"name":"target.example.","type":1,"data":"203.0.113.9"},
		{"name":"target.example.","type":28,"data":"::1"}]}`)
	got := parseDoHAnswer(body)
	if len(got) != 1 || got[0] != "203.0.113.9" {
		t.Fatalf("只该取 A 记录: %v", got)
	}

	// NXDOMAIN 不能当成功
	if got := parseDoHAnswer([]byte(`{"Status":3}`)); len(got) != 0 {
		t.Fatalf("NXDOMAIN 不该返回地址: %v", got)
	}
	// 坏 JSON 不能 panic
	if got := parseDoHAnswer([]byte(`not json`)); len(got) != 0 {
		t.Fatalf("坏 JSON 不该返回地址: %v", got)
	}
}
