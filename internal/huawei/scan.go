package huawei

// 扫码：取码 → 轮询 → 确认。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ScanCode 是"取登录二维码"的返回。
type ScanCode struct {
	Content     string  `json:"content"`
	QrCode      string  `json:"qrCode"`
	QrToken     string  `json:"qrToken"`
	ExpiredTime FlexInt `json:"expiredTime"`
	SessionID   string  `json:"sessionID"`
}

// FlexInt 兼容接口里 number / string 两种写法。
type FlexInt int64

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		var fl float64
		if e2 := json.Unmarshal(b, &fl); e2 == nil {
			*f = FlexInt(int64(fl))
			return nil
		}
		return err
	}
	*f = FlexInt(v)
	return nil
}

// ScanReply 是"手机确认扫码"之后的结果。
type ScanReply struct {
	RawQRCode   string `json:"-"`
	RawJSON     string `json:"-"`
	UserID      string `json:"userID"`
	UserAccount string `json:"userAccount"`
	Code        string `json:"code"`
	SiteID      string `json:"siteID"`
	AccountType string `json:"accountType"`
}

// GetScanCode 取一张新的登录二维码。
func (c *Client) GetScanCode() (*ScanCode, error) {
	endpoint := fmt.Sprintf("%s/DimensionalCode/getqrInfo?Version=%s&cVersion=HwID_5.3.0.312&blackScreen=0&appBrand=HUAWEI", qrHost, ver)
	body := "version=53000&appID=" + appID + "&loginChannel=7000700&reqClientType=701&confirmFlag=1&lang=zh_CN"

	status, payload, err := c.send(endpoint, body, "application/x-www-form-urlencoded; charset=UTF-8")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("getqrInfo http %d", status)
	}
	var code ScanCode
	if err := json.Unmarshal([]byte(payload), &code); err != nil {
		return nil, fmt.Errorf("getqrInfo 解析失败: %w", err)
	}
	if code.QrToken == "" || code.Content == "" {
		return nil, errors.New("getqrInfo 返回为空")
	}
	c.qrCode = code.QrCode
	return &code, nil
}

// FetchScanState 轮询一次。还没扫码时返回 (nil, nil)。
//
// 华为不同接口的字段大小写不一致（userID / userId），这里统一大小写不敏感地取。
func (c *Client) FetchScanState(qrToken string) (*ScanReply, error) {
	endpoint := fmt.Sprintf("%s/DimensionalCode/async?Version=%s&cVersion=HwID_5.3.0.312&blackScreen=0&appBrand=HUAWEI", qrHost, ver)
	status, payload, err := c.send(endpoint, "qrToken="+url.QueryEscape(qrToken), "application/x-www-form-urlencoded; charset=UTF-8")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("poll http %d", status)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(payload), &fields); err != nil {
		return nil, nil
	}

	reply := &ScanReply{
		RawQRCode:   lookup(fields, "qrCode", "qr_code"),
		RawJSON:     payload,
		UserID:      lookup(fields, "userID", "user_id"),
		UserAccount: lookup(fields, "userAccount", "user_account", "account"),
		Code:        lookup(fields, "code"),
		SiteID:      lookup(fields, "siteID", "site_id"),
		AccountType: lookup(fields, "accountType", "account_type"),
	}
	if reply.UserID == "" {
		return nil, nil
	}
	return reply, nil
}

// lookup 在 map 里按名字（忽略大小写）取第一个非空值，并转成字符串。
func lookup(fields map[string]any, names ...string) string {
	for _, want := range names {
		for k, v := range fields {
			if !strings.EqualFold(k, want) {
				continue
			}
			switch t := v.(type) {
			case string:
				if t != "" {
					return t
				}
			case float64:
				return strconv.FormatFloat(t, 'f', -1, 64)
			case json.Number:
				return t.String()
			case bool:
				if t {
					return "1"
				}
				return "0"
			}
		}
	}
	return ""
}

// FetchScanStateRaw 返回原始状态码和响应体（排查用）。
func (c *Client) FetchScanStateRaw(qrToken string) (int, string, error) {
	endpoint := fmt.Sprintf("%s/DimensionalCode/async?Version=%s&cVersion=HwID_5.3.0.312&blackScreen=0&appBrand=HUAWEI", qrHost, ver)
	return c.send(endpoint, "qrToken="+url.QueryEscape(qrToken), "application/x-www-form-urlencoded; charset=UTF-8")
}

// WaitForScan 一直轮询到确认、过期或取消。
func (c *Client) WaitForScan(code *ScanCode, onTick func(time.Duration), done <-chan struct{}) (*ScanReply, error) {
	deadline := time.Now().Add(180 * time.Second)
	if v := int64(code.ExpiredTime); v > 0 {
		var exp time.Time
		if v > 1e12 {
			exp = time.UnixMilli(v)
		} else {
			exp = time.Unix(v, 0)
		}
		exp = exp.Add(-5 * time.Second) // 留 5 秒余量
		if exp.Before(deadline) {
			deadline = exp
		}
	}

	start := time.Now()
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return nil, ErrCanceled
		default:
		}
		if reply, err := c.FetchScanState(code.QrToken); err == nil && reply != nil {
			return reply, nil
		}
		if onTick != nil {
			onTick(time.Since(start))
		}
		time.Sleep(700 * time.Millisecond)
	}
	return nil, ErrExpired
}

// SavedScan 是落盘的扫码结果，失败后可以拿它重试换 ST。
type SavedScan struct {
	UserID      string `json:"user_id"`
	UserAccount string `json:"user_account"`
	Code        string `json:"code"`
	SiteID      string `json:"site_id"`
	AccountType string `json:"account_type"`
	QrCode      string `json:"qr_code"`
	RawJSON     string `json:"raw_json,omitempty"`
	SavedAt     int64  `json:"saved_at"`
}

// Snapshot 把扫码结果和当前二维码码值打包成可保存的形态。
func (c *Client) Snapshot(reply *ScanReply) SavedScan {
	qr := c.qrCode
	if reply.RawQRCode != "" {
		qr = reply.RawQRCode
	}
	return SavedScan{
		UserID: reply.UserID, UserAccount: reply.UserAccount, Code: reply.Code,
		SiteID: reply.SiteID, AccountType: reply.AccountType, QrCode: qr,
		RawJSON: reply.RawJSON, SavedAt: time.Now().Unix(),
	}
}

// Restore 还原成 ScanReply。
func (s SavedScan) Restore() *ScanReply {
	return &ScanReply{
		UserID: s.UserID, UserAccount: s.UserAccount, Code: s.Code,
		SiteID: s.SiteID, AccountType: s.AccountType,
	}
}
