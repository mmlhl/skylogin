package huawei

// 华为 HMS 扫码登录：会话、请求发送与几个小工具。
//
// 所有请求都带 ctrID（毫秒时间戳 + 19 位数字）和 Authorization 头，
// 这是华为客户端侧的自检；服务端不校验其正确性，但缺失会被风控。

import (
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"time"
)

const (
	qrHost = "https://id1.cloud.huawei.com"
	asHost = "https://setting1.hicloud.com"
	appID  = "com.huawei.hwid"
	ver    = "53000"
	ua     = "com.huawei.hwid/5.3.0.312 (Linux; Android 12; SM-G9910) RestClient/5.3.0.312"
)

var (
	// ErrExpired 表示二维码过期，调用方应换一张新的。
	ErrExpired = errors.New("二维码已过期")
	// ErrCanceled 表示用户主动取消。
	ErrCanceled = errors.New("已取消")
)

// Client 保存扫码会话的 cookie 和当前二维码码值。
type Client struct {
	http   *http.Client
	qrCode string
}

// NewClient 建一个带 cookie jar 的会话。
func NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}

// send 发一个带统一头的 POST，返回状态码和响应体。
func (c *Client) send(rawurl, body, contentType string) (int, string, error) {
	sep := "?"
	if strings.Contains(rawurl, "?") {
		sep = "&"
	}
	req, err := http.NewRequest(http.MethodPost, rawurl+sep+"ctrID="+traceID(), strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Connection", "Keep-Alive")
	req.Header.Set("Authorization", strconv.FormatInt(time.Now().UnixMilli(), 10))
	req.Header.Set("User-Agent", ua)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw), err
}

// traceID 是每次请求的流水号：毫秒时间 + 19 位数字。
func traceID() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 10) + digits(19)
}

// digits 生成 n 位数字串（用线性同余推进，够随机且不依赖全局种子）。
func digits(n int) string {
	out := make([]byte, n)
	seed := time.Now().UnixNano()
	for i := range out {
		seed = seed*6364136223846793005 + 1442695040888963407
		v := seed >> 33
		if v < 0 {
			v = -v
		}
		out[i] = byte('0' + v%10)
	}
	return string(out)
}

// xmlValue 取第一个 <tag> 的内容（华为返回的是结构很简单的 XML）。
func xmlValue(s, tag string) string {
	open := "<" + tag + ">"
	start := strings.Index(s, open)
	if start < 0 {
		return ""
	}
	rest := s[start+len(open):]
	end := strings.Index(rest, "</"+tag+">")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// AccountName 从登录响应里取昵称。
func AccountName(xmlResp string) string { return xmlValue(xmlResp, "nickName") }
