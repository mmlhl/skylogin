package huawei

// 用扫码结果换 serviceToken（ST）。
//
// 华为对 userAccount 的处理在不同版本/风控下不一致，所以留了几种尝试组合：
// 原样、置空、等于 userID。

import (
	"fmt"
	"strings"
)

// Tweak 是一种换 ST 的参数组合。
type Tweak struct {
	Label       string
	UserAccount string
}

// ExchangeST 用当前会话里的二维码码值换 ST。
func (c *Client) ExchangeST(reply *ScanReply, deviceUUID string) (string, string, error) {
	return c.ExchangeSTRaw(reply, c.qrCode, deviceUUID)
}

// ExchangeSTFallback 依次尝试几种 userAccount 组合，返回第一个成功的。
func (c *Client) ExchangeSTFallback(reply *ScanReply, qrCode, deviceUUID string, log func(string, ...any)) (string, string, error) {
	candidates := []Tweak{
		{Label: "原始 userAccount", UserAccount: reply.UserAccount},
		{Label: "空 userAccount", UserAccount: ""},
		{Label: "userAccount=userID", UserAccount: reply.UserID},
	}
	var lastErr error
	var lastRaw string
	for _, candidate := range candidates {
		attempt := *reply
		attempt.UserAccount = candidate.UserAccount
		token, raw, err := c.ExchangeSTRaw(&attempt, qrCode, deviceUUID)
		if err == nil {
			return token, raw, nil
		}
		lastErr, lastRaw = err, raw
		if log != nil {
			log("换 ST 变体[%s]失败: %s", candidate.Label, strings.SplitN(err.Error(), "\n", 2)[0])
		}
		// 70002014 是"事务创建失败"这类可重试错误，其余直接放弃
		if !strings.Contains(raw, "70002014") {
			break
		}
	}
	return "", lastRaw, lastErr
}

// ExchangeSTRaw 用指定的二维码码值换 ST（也用于拿保存的记录重试）。
func (c *Client) ExchangeSTRaw(reply *ScanReply, qrCode, deviceUUID string) (string, string, error) {
	site := reply.SiteID
	if site == "" {
		site = "1"
	}
	accountType := reply.AccountType
	if accountType == "" {
		accountType = "0"
	}

	reqXML := `<?xml version="1.0" encoding="UTF-8"?>` +
		"<LoginByQrCodeReq>" +
		"<version>" + ver + "</version>" +
		"<accountType>" + accountType + "</accountType>" +
		"<userAccount>" + reply.UserAccount + "</userAccount>" +
		"<userID>" + reply.UserID + "</userID>" +
		"<qrCode>" + qrCode + "</qrCode>" +
		"<qrSiteID>" + site + "</qrSiteID>" +
		"<code>" + reply.Code + "</code>" +
		"<appID>" + appID + "</appID>" +
		"<reqClientType>7</reqClientType>" +
		"<loginChannel>7000000</loginChannel>" +
		"<osVersion>12</osVersion>" +
		"<plmn></plmn>" +
		"<uuid>" + deviceUUID + "</uuid>" +
		"<languageCode>zh_CN</languageCode>" +
		"<deviceSecure>0</deviceSecure>" +
		"<mainAcctLogin>1</mainAcctLogin>" +
		"<clientID></clientID>" +
		"<riskToken></riskToken>" +
		"<deviceInfo><deviceID>" + deviceUUID + "</deviceID><deviceType>6</deviceType>" +
		"<terminalType>Android</terminalType></deviceInfo>" +
		"<loginType>1</loginType>" +
		"</LoginByQrCodeReq>"

	endpoint := fmt.Sprintf("%s/AccountServer/IDM/loginByQrCode?Version=%s&cVersion=HwID_5.3.0.312&blackScreen=0&appBrand=HUAWEI", asHost, ver)
	status, payload, err := c.send(endpoint, reqXML, "text/html; charset=UTF-8")
	if err != nil {
		return "", "", err
	}
	if status != 200 {
		return "", payload, fmt.Errorf("loginByQrCode http %d", status)
	}

	st := xmlValue(payload, "serviceToken")
	if st == "" {
		snippet := strings.TrimSpace(payload)
		if len(snippet) > 500 {
			snippet = snippet[:500] + "…"
		}
		return "", payload, fmt.Errorf("响应中缺少 serviceToken；原始响应: %s", snippet)
	}
	return st, payload, nil
}
