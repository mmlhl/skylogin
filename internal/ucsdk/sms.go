package ucsdk

// 短信验证码登录。
//
// 三步（实测，见 .ai/10 第 6.5 节）：
//
//	1. sendSmsCode       {mobile, countryCode}
//	2. loginBySmsCode    {mobile, countryCode, smsCode, loginType:1}
//	     └─ 若该手机号下有多个游戏账号，返回 214 + accountList + serviceTicket
//	3. loginBySmsCode    同上，再带上 serviceTicket 与 accountId
//
// 单账号时第 2 步就能直接登录成功，不会经过第 3 步。

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Account 是登录后拿到的账号信息。
type Account struct {
	AccountID string `json:"accountId"`
	Remark    string `json:"accountRemark"` // 「光·遇_k47h」这种，用户能认出来
	Alias     int64  `json:"accountAlias"`
	Desc      string `json:"description"`
	RoleInfo  string `json:"accountRoleInfo"`
}

// Session 是一次成功登录的结果。
type Session struct {
	SID          string
	RefreshToken string
	AccountID    string
	// Timeout 是服务端声明的有效期（秒）。登录响应里也有这个字段，
	// 续期响应里固定是 86400，即续一次多活 24 小时。
	Timeout       int
	ServiceTicket string
	// Account 是登录到的游戏账号（多账号时是用户选的那个）。
	Account Account
}

// sendSmsData 是发送验证码的请求数据。
//
// 用结构体而不是 map，是因为服务端对明文顺序敏感：
// 抓包里 SDK 发的是 mobile 在前、countryCode 在后。
type sendSmsData struct {
	Mobile      string `json:"mobile"`
	CountryCode string `json:"countryCode"`
}

// SendSmsCode 发送短信验证码。
func (c *Client) SendSmsCode(mobile, countryCode string) error {
	if mobile == "" {
		return fmt.Errorf("手机号不能为空")
	}
	if countryCode == "" {
		countryCode = "86"
	}
	_, err := c.call(HostAccount, ServiceSendSms, sendSmsData{
		Mobile:      mobile,
		CountryCode: countryCode,
	})
	return err
}

// loginData 是 loginBySmsCode 的响应数据。
type loginData struct {
	AccountList []Account `json:"accountList"`
	AccountInfo *Account  `json:"accountInfo"`
	// 会话材料：字段名按实测，sid 在大写 SID 与小写 sid 之间都见过，
	// 所以两种都收，取到非空的那个。
	SID          string `json:"sid"`
	SIDUpper     string `json:"SID"`
	RefreshToken string `json:"refreshToken"`
	ServiceTkt   string `json:"serviceTicket"`
	Timeout      int    `json:"timeout"`
}

func (d loginData) sid() string {
	if d.SID != "" {
		return d.SID
	}
	return d.SIDUpper
}

// loginBySmsData 是验证码登录的请求数据。
//
// 顺序按抓包：mobile, countryCode, smsCode, loginType；
// 选账号阶段再补 serviceTicket 与 accountId（空值不序列化）。
type loginBySmsData struct {
	Mobile        string `json:"mobile"`
	CountryCode   string `json:"countryCode"`
	SmsCode       string `json:"smsCode"`
	LoginType     int    `json:"loginType"`
	ServiceTicket string `json:"serviceTicket,omitempty"`
	AccountID     string `json:"accountId,omitempty"`
}

// LoginBySms 用验证码登录。
//
// 返回值 needsChoice 为真时，说明这个手机号下有多个游戏账号，
// 调用方要用 Accounts 渲染选择界面，然后带 accountID 再调一次。
func (c *Client) LoginBySms(mobile, countryCode, smsCode, serviceTicket, accountID string) (*Session, []Account, error) {
	body := loginBySmsData{
		Mobile:        mobile,
		CountryCode:   countryCode,
		SmsCode:       smsCode,
		LoginType:     1,
		ServiceTicket: serviceTicket,
		AccountID:     accountID,
	}

	raw, err := c.call(HostAccount, ServiceLoginBySms, body)
	return parseLoginResult(raw, err)
}

// parseLoginResult 把两种登录方式共用的响应解析成 Session。
//
// 214 是「该手机号下有多个游戏账号」，不是失败：把账号列表与 serviceTicket
// 交出去，由上层渲染选择界面，再带 accountID 重调一次。
func parseLoginResult(raw json.RawMessage, err error) (*Session, []Account, error) {
	if err != nil {
		if Code(err) != CodeNeedAccount {
			return nil, nil, err
		}
		err = ErrNeedAccountChoice
	}

	var d loginData
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &d); e != nil {
			return nil, nil, fmt.Errorf("登录响应解析失败: %w", e)
		}
	}

	if errors.Is(err, ErrNeedAccountChoice) {
		return &Session{ServiceTicket: d.ServiceTkt}, d.AccountList, ErrNeedAccountChoice
	}

	sid := d.sid()
	if sid == "" {
		return nil, nil, fmt.Errorf("登录成功但没拿到 sid")
	}
	s := &Session{
		SID:           sid,
		RefreshToken:  d.RefreshToken,
		ServiceTicket: d.ServiceTkt,
		Timeout:       d.Timeout,
	}
	if d.AccountInfo != nil {
		s.Account = *d.AccountInfo
		s.AccountID = d.AccountInfo.AccountID
	}
	if s.AccountID == "" {
		s.AccountID = s.Account.AccountID
	}
	return s, nil, nil
}

// ErrNeedAccountChoice 表示需要用户从账号列表里选一个。
var ErrNeedAccountChoice = fmt.Errorf("需要选择游戏账号")
