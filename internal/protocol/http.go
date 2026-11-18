package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"skylogin/internal/netdns"
)

type httpClient struct{ timeout time.Duration }

func (c *httpClient) getJSON(url string, out any) error {
	// 远程协议参数也是网易的域名，同样可能被本机 DNS 拦掉；
	// 拉不到只是退回内置默认值，但不该因为解析失败而静默降级。
	client := netdns.NewClient(c.timeout)
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
