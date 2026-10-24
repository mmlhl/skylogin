package launcher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 加载湾（发烧平台自己的服务）的换票接口。
//
// 关键发现：二维码只是"凭据编号"，游戏要的是 `AAAA...` 那样的票。
// 发烧平台拿到 code 之后，会调这个接口把它换成票：
//
//	GET /app/v1/third_party_account/ticket?code=<10位code>&app_id=63&device_id=<设备>
//	→ {"data":{"ticket":"AAAA...","login_channel":"netease"},"code":200,"msg":"操作成功"}
//
// 这个接口不需要登录态（实测无效 code 也返回 200，只是内容报错）。
const (
	// TicketURL 换票接口。
	TicketURL = "https://loadingbaycn.webapp.163.com/app/v1/third_party_account/ticket"
	// SkyAppID 是发烧平台里光遇的 app_id（注册表里 gameid=63）。
	SkyAppID = "63"
)

// TicketResult 是换票成功后的结果。
type TicketResult struct {
	Ticket  string
	Channel string
}

// ExchangeTicket 用二维码 code 换一张可兑换的票。
func ExchangeTicket(code, deviceID string) (*TicketResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("code 为空")
	}

	q := url.Values{}
	q.Set("code", code)
	q.Set("app_id", SkyAppID)
	if deviceID != "" {
		q.Set("device_id", deviceID)
	}

	c := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, TicketURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Ticket  string `json:"ticket"`
			Channel string `json:"login_channel"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("换票响应解析失败: %s", truncate(string(body), 200))
	}
	if out.Data.Ticket == "" {
		return nil, fmt.Errorf("换票失败: code=%d msg=%s", out.Code, out.Msg)
	}
	ch := out.Data.Channel
	if ch == "" {
		ch = "netease"
	}
	return &TicketResult{Ticket: out.Data.Ticket, Channel: ch}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
