package ucsdk

// 用 refreshToken 续期换新 sid。
//
// 实测（2026-11-08，见 .ai/10 第 7.11 节）确认三件事：
//
//	host       sdk.9game.cn（不是 sdk-account.9game.cn，发错永远回 99；见下）
//	参数       {refreshToken, sid}——两个都要，少 sid 就回 code=11「用户未登录」
//	响应       {sid, timeout}，timeout=86400 即新 sid 又有 24 小时
//
// 关于 code=99：sdk-account 域名对任何不存在或未开放的服务名都回
// 「99 系统繁忙」，很容易被误判成限流。真正的「服务名不存在」提示
// （code=10）只有 sdknc 域名才会给。所以看业务码前先确认域名对不对。

import (
	"encoding/json"
	"fmt"
)

// refreshData 是 ucid.user.refreshLogin 的请求数据。
//
// 字段顺序按实测：refreshToken 在前、sid 在后。
// 两个都是必填——sid 是"续的是哪条会话"，refreshToken 是"凭什么续"。
type refreshData struct {
	RefreshToken string `json:"refreshToken"`
	SID          string `json:"sid"`
}

// RefreshLogin 用 refreshToken 把当前 sid 续期，换一个新的回来。
//
// 返回的 Session 里 Timeout 是服务端给的有效期（秒），
// RefreshToken 可能为空：服务端不轮换时沿用旧值，由调用方决定是否覆盖。
func (c *Client) RefreshLogin(refreshToken, sid string) (*Session, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("refreshToken 为空")
	}
	if sid == "" {
		return nil, fmt.Errorf("sid 为空")
	}
	raw, err := c.call(HostSDK, ServiceRefreshLogin, refreshData{
		RefreshToken: refreshToken,
		SID:          sid,
	})
	if err != nil {
		return nil, err
	}

	// 续期响应比登录响应简单得多，只有 sid 和 timeout
	var d struct {
		SID          string `json:"sid"`
		SIDUpper     string `json:"SID"`
		RefreshToken string `json:"refreshToken"`
		Timeout      int    `json:"timeout"`
	}
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &d); e != nil {
			return nil, fmt.Errorf("续期响应解析失败: %w", e)
		}
	}
	newSID := d.SID
	if newSID == "" {
		newSID = d.SIDUpper
	}
	if newSID == "" {
		return nil, fmt.Errorf("续期成功但没拿到 sid: %s", trim(string(raw), 300))
	}
	return &Session{
		SID:          newSID,
		RefreshToken: d.RefreshToken,
		Timeout:      d.Timeout,
	}, nil
}
