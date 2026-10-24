package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type httpClient struct{ timeout time.Duration }

func (c *httpClient) getJSON(url string, out any) error {
	client := &http.Client{Timeout: c.timeout}
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
