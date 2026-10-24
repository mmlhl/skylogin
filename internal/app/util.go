package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// retry 对易抖动的网络调用做有限重试。
func retry(n int, wait time.Duration, fn func() error) error {
	var err error
	for i := 0; i < n; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i < n-1 {
			time.Sleep(wait)
		}
	}
	return err
}

// truncate 截断过长的字符串，便于放进日志。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func timeSleep(sec int) { time.Sleep(time.Duration(sec) * time.Second) }

// rawGET 发一次 GET 并把响应解析成 JSON（诊断用）。
func rawGET(u string) (map[string]any, int, error) {
	resp, err := http.DefaultClient.Get(u)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("非 JSON: %.200s", string(b))
	}
	return m, resp.StatusCode, nil
}
