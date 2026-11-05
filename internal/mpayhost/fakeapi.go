package mpayhost

// 诊断：把 mpay.dll 的服务地址指到本地，看它到底发什么请求。
//
// 为什么这条路最省事
//
//	mpay.dll 是静态链接 libcurl + OpenSSL 的，没有 WinHTTP 导入表可 hook，
//	直接抓 HTTPS 要装证书（那就违反"不装证书"的原则了）。
//	但它自己导出了这些 SetOption 键：
//
//	  mpay_option_custom_service_url
//	  mpay_option_custom_httpdns_service_url
//	  mpay_option_custom_bind_mobile_service_url
//
//	也就是说官方就留了"把服务地址换成别的"的口子。换成 http://127.0.0.1:端口
//	之后，DLL 的请求会**明文**发到我们的本地服务器——既不用证书，也不用改
//	hosts、不用开代理，更不用注入游戏进程（这一切都在我们自己的子进程里）。
//
// 这个文件只负责：起一个记录一切的本地 HTTP 服务 + 把 DLL 指过去。
//
// 实测结论（2026-11-06）：**不行**。
//   SetOption 被 DLL 接受了（日志里有 "setOption params : key[...] value[...]"），
//   但核心接口（config / login/ticket）依然打到 service.mkey.163.com，
//   本地假服务器一条请求都没收到。
//   也就是说这个选项只影响 bind-mobile / realname 这类外围服务，
//   不改变主 API 的地址。

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// FakeAPI 是一个"照单全收"的本地服务：记录每个请求，回一个空 JSON。
type FakeAPI struct {
	Addr   string
	srv    *http.Server
	mu     sync.Mutex
	hits   []string // 每个请求一行：方法 + 路径 + body 摘要
	logf   func(string, ...any)
	closed chan struct{}
}

// StartFakeAPI 在随机端口起服务并把收到的请求写进日志。
func StartFakeAPI(log func(string, ...any)) (*FakeAPI, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f := &FakeAPI{Addr: ln.Addr().String(), logf: log, closed: make(chan struct{})}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		line := fmt.Sprintf("%s %s  body=%d字节  %s",
			r.Method, r.URL.Path, len(body), summarize(string(body)))
		f.mu.Lock()
		f.hits = append(f.hits, line)
		f.mu.Unlock()
		log("%s", line)

		// 先统一回一个"成功"的空壳，让流程继续往下走
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"status":"ok","reason":"","data":null}`))
	})

	f.srv = &http.Server{Handler: mux}
	go func() {
		_ = f.srv.Serve(ln)
		close(f.closed)
	}()
	return f, nil
}

// Close 关掉服务。
func (f *FakeAPI) Close() {
	if f == nil || f.srv == nil {
		return
	}
	_ = f.srv.Close()
	select {
	case <-f.closed:
	case <-time.After(2 * time.Second):
	}
}

// Hits 返回记录到的请求（排查用）。
func (f *FakeAPI) Hits() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

// summarize 把 body 压成一行，只留结构和少量明文。
func summarize(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
