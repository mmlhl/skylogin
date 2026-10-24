package mpay

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"skylogin/internal/fever"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
)

// 网易账号的"持久化"靠 MPay token：
//
//	token → create_ticket → AAAA 票（给游戏用，游戏自己兑换）
//
// 每次 login/ticket 都会返回新的 token，所以用完要把新 token 存回去。

// postForm 发一个表单 POST，返回解析后的 JSON。
func (c *Client) postForm(path string, q url.Values, form url.Values) (map[string]any, error) {
	u := c.Base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("响应不是 JSON: %s", truncate(string(b), 200))
	}
	if code, ok := out["code"].(float64); ok && code != 0 && code != 200 {
		reason, _ := out["reason"].(string)
		return out, &TicketError{Code: int(code), Reason: reason}
	}
	return out, nil
}

// TicketError 表示 MPay 明确拒绝（带错误码）。
type TicketError struct {
	Code   int
	Reason string
}

func (e *TicketError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("MPay 拒绝: %d", e.Code)
	}
	return fmt.Sprintf("MPay 拒绝: %d %s", e.Code, e.Reason)
}

// baseForm 是 MPay PC 端请求的公共参数。
func baseForm(deviceID, longGameID, shortGameID, appChannel, cv, gv string) url.Values {
	form := url.Values{}
	form.Set("app_channel", appChannel)
	form.Set("app_mode", "2")
	form.Set("app_type", "games")
	form.Set("arch", "win_x64")
	form.Set("cv", cv)
	form.Set("device_id", deviceID)
	form.Set("game_id", longGameID)
	form.Set("gv", gv)
	form.Set("jf_game_id", shortGameID)
	form.Set("mcount_app_key", protocol.Get().MpayKey)
	form.Set("mcount_transaction_id", newUUID()+"-2")
	form.Set("process_id", fmt.Sprint(os.Getpid()))
	form.Set("sv", fever.SystemVersion())
	form.Set("updater_cv", "c1.0.0")
	return form
}

// GetUserInfo 用 token 查一个账号的公开资料（显示名、昵称等）。
//
// 为什么需要它：login/ticket 返回的 user 里不一定带 display_username
// （a50 上下文实测会缺），拿不到就只能退回用户 id，界面上就成了
// "别名是一串 id"。官方启动器自己是用下面这个 GET 取用户资料的，
// 返回里有 display_username / username / account / client_username。
func (c *Client) GetUserInfo(token, userID, deviceID, longGameID, shortGameID string) (map[string]any, error) {
	q := baseForm(deviceID, longGameID, shortGameID, "a50_sdk_cn", "c4.20.0", "0.0.0.0")
	q.Set("bill_pid", shortGameID)
	q.Set("is_outer_browser_open", "1")
	q.Set("opt_fields", "nickname,avatar,realname_status,mobile_bind_status")
	q.Set("realname_status", "1")
	q.Set("token", token)

	path := "/mpay/games/" + longGameID + "/devices/" + deviceID + "/users/" + userID
	out, err := c.get(path, q)
	if err != nil {
		return nil, err
	}
	user, _ := out["user"].(map[string]any)
	if user == nil {
		return nil, fmt.Errorf("用户资料里没有 user: %s", truncate(fmt.Sprint(out), 200))
	}
	return user, nil
}

// DisplayNameOf 从用户资料里挑一个能给人看的账号名。
//
// 优先 display_username（网易自己给的就是脱敏后的手机号/邮箱），
// 再退到 username / account / client_username。
func DisplayNameOf(user map[string]any) string {
	for _, k := range []string{"display_username", "username", "account", "client_username"} {
		if v, _ := user[k].(string); strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// LoginTicket 用一张票换用户信息（含新的 MPay token）。
//
// 参数与游戏自己的请求一致：app_channel=a50_sdk_cn、cv=c4.20.0、gv=0.0.0.0。
func (c *Client) LoginTicket(ticket, deviceID, longGameID, shortGameID string) (map[string]any, error) {
	form := baseForm(deviceID, longGameID, shortGameID, "a50_sdk_cn", "c4.20.0", "0.0.0.0")
	form.Set("is_remember_account", "0")
	form.Set("opt_fields", "nickname,avatar,realname_status,mobile_bind_status,mask_related_mobile,related_login_status")
	form.Set("ticket", ticket)
	form.Set("transid", newTransID())
	form.Set("uni_transaction_id", newTransID())

	out, err := c.postForm("/mpay/api/users/login/ticket", nil, form)
	if err != nil {
		return out, err
	}
	if _, ok := out["user"]; !ok {
		return out, fmt.Errorf("login/ticket 没有返回 user: %s", truncate(fmt.Sprint(out), 200))
	}
	return out, nil
}

// CreateTicket 用 MPay token 现签一张票（AAAA 开头），给游戏兑换用。
//
// 参数必须用**光遇自己**的上下文（a50_sdk_cn + ma75 长 id + c4.20.0 + gv=0.0.0.0）。
// 照抄发烧平台的 a50 参数会 403，因为 token 是在光遇上下文里签发的。
func (c *Client) CreateTicket(token, userID, deviceID string) (string, map[string]any, error) {
	return c.CreateTicketFor(token, userID, deviceID, profile.LongGameID, profile.ShortGameID)
}

// CreateTicketFor 用指定游戏上下文签票。
//
// 为什么需要这个：MPay 的 token 是**按应用绑定**的（token 里带 g_i）。
//   - 扫码登录拿到的 token 属于光遇（aecfs5n2y4aaaakd），要用光遇上下文签票
//   - 网页/平台登录拿到的 token 属于发烧平台（aecglf6ee4aaaarz，a50），
//     用光遇上下文签会被拒（1306 用户登录失效），必须用 a50 上下文
//
// 签出来的票游戏都认（平台自己也是用 a50 票启动光遇的）。
func (c *Client) CreateTicketFor(token, userID, deviceID, longGameID, shortGameID string) (string, map[string]any, error) {
	cv := "c4.20.0"
	form := baseForm(deviceID, longGameID, shortGameID, profile.HuaweiChannel, cv, "0.0.0.0")
	form.Set("bill_pid", shortGameID)
	form.Set("is_outer_browser_open", "1")
	form.Set("token", token)
	form.Set("transid", newTransID())
	form.Set("uni_transaction_id", newTransID())
	if userID != "" {
		form.Set("user_id", userID)
	}

	q := url.Values{}
	q.Set("cv", cv)

	out, err := c.postForm("/mpay/api/users/create_ticket", q, form)
	if err != nil {
		return "", out, err
	}
	t, _ := out["ticket"].(string)
	if t == "" {
		return "", out, fmt.Errorf("create_ticket 没有返回 ticket: %s", truncate(fmt.Sprint(out), 200))
	}
	return t, out, nil
}

// newUUID 生成 mcount_transaction_id 需要的 UUID 形态。
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// newTransID 生成 transid：<固定前缀>_<毫秒>_<序号>。
func newTransID() string {
	return fmt.Sprintf("21321A2DD37EFE78212D43A4A02FF362_%d_%d",
		time.Now().UnixMilli(), time.Now().UnixNano()%100000000)
}

// SendSMS 给手机号发登录验证码。
//
// 实测：200 且返回 {}，验证码由网易下发；号码格式不对会返回 400/1330。
func (c *Client) SendSMS(mobile, deviceID string) (map[string]any, error) {
	form := baseForm(deviceID, profile.LongGameID, profile.ShortGameID, "a50_sdk_cn", "c4.20.0", "0.0.0.0")
	form.Set("mobile", mobile)
	out, err := c.postForm("/mpay/api/users/login/mobile/get_sms", nil, form)
	if err != nil {
		return out, err
	}
	return out, nil
}

// VerifySMS 校验验证码，成功时返回一张 16 位票据（AAAA…）。
//
// 这张票可以直接换 token（LoginTicket），也能直接交给游戏。
func (c *Client) VerifySMS(mobile, code, deviceID string) (string, map[string]any, error) {
	form := baseForm(deviceID, profile.LongGameID, profile.ShortGameID, "a50_sdk_cn", "c4.20.0", "0.0.0.0")
	form.Set("mobile", mobile)
	form.Set("smscode", code)
	out, err := c.postForm("/mpay/api/users/login/mobile/verify_sms", nil, form)
	if err != nil {
		return "", out, err
	}
	t, _ := out["ticket"].(string)
	if t == "" {
		return "", out, fmt.Errorf("验证码校验没有返回票据: %s", truncate(fmt.Sprint(out), 200))
	}
	return t, out, nil
}
