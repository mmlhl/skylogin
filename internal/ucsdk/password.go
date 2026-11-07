package ucsdk

// 手机号 + 密码登录。
//
// 实测（2026-11-08 抓包，见 .ai/10 第 7.8 节）：
//
//	POST sdk-account.9game.cn/ng/client/unifiedAccount.loginByMobilePassword
//	data = {mobile, password, areaCode, encrypt, silent, loginType}
//
// 与短信登录的三处差别，任何一处写错都会被判「密码错误」：
//
//  1. 字段叫 areaCode，不是 countryCode
//  2. password = "5|" + base64(RSA(公钥, "<前缀>||<明文密码>"))
//  3. 多了 encrypt=1、silent=false、loginType=2
//
// 注意 password 送进 RSA 的不是密码本身，而是「16 位十六进制前缀 + || + 密码」。

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// passwordPrefix 是明文密码前面那 16 位十六进制串。
//
// 实测两次登录（首次 + 带 serviceTicket 重发）取值完全相同。
// 它到底是 md5(某个字段) 的前 16 位、还是写死的常量，目前没确认；
// 唯一确定的是必须原样拼在密码前、用 "||" 隔开，否则服务端判密码错。
const passwordPrefix = "41d8cd98f00b204e"

// passwordData 是 loginByMobilePassword 的请求数据。
//
// 字段顺序按抓包；选账号阶段再补 serviceTicket 与 accountId（空值不序列化）。
type passwordData struct {
	Mobile        string `json:"mobile"`
	Password      string `json:"password"`
	AreaCode      string `json:"areaCode"`
	Encrypt       int    `json:"encrypt"`
	Silent        bool   `json:"silent"`
	LoginType     int    `json:"loginType"`
	ServiceTicket string `json:"serviceTicket,omitempty"`
	AccountID     string `json:"accountId,omitempty"`
}

// LoginByPassword 用手机号 + 密码登录。
//
// 返回值与 LoginBySms 一致：返回 ErrNeedAccountChoice 时要带 accountID 再调一次。
func (c *Client) LoginByPassword(mobile, password, areaCode, serviceTicket, accountID string) (*Session, []Account, error) {
	if mobile == "" || password == "" {
		return nil, nil, fmt.Errorf("手机号与密码都不能为空")
	}
	if areaCode == "" {
		areaCode = "86"
	}
	pub, err := c.ensureKey()
	if err != nil {
		return nil, nil, err
	}
	// 送进 RSA 的是「前缀 + || + 明文密码」，不是密码本身
	sealed, err := encryptPKCS1v15(pub, []byte(passwordPrefix+"||"+password))
	if err != nil {
		return nil, nil, fmt.Errorf("密码加密失败: %w", err)
	}

	raw, err := c.call(HostAccount, ServiceLoginByPassword, passwordData{
		Mobile:        mobile,
		Password:      c.keyVersion() + "|" + base64.StdEncoding.EncodeToString(sealed),
		AreaCode:      areaCode,
		Encrypt:       1,
		Silent:        false,
		LoginType:     2,
		ServiceTicket: serviceTicket,
		AccountID:     accountID,
	})
	return parseLoginResult(raw, err)
}

// keyVersion 取当前会话公钥的版本号（「5|MFww…」里的 5）。
//
// password 字段的前缀必须与公钥版本一致，所以从公钥串里取，不写死。
func (c *Client) keyVersion() string {
	c.mu.Lock()
	raw := c.publicKey
	c.mu.Unlock()
	if i := strings.Index(raw, "|"); i > 0 {
		return raw[:i]
	}
	return "5"
}
