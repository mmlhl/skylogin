package mpay

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"skylogin/internal/netsec"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
)

// Client 是极简的 MPay 二维码客户端。
type Client struct {
	Base string
	http *http.Client
	cv   string
}

func New() *Client {
	return &Client{
		Base: profile.MpayBaseURL,
		http: &http.Client{Timeout: 25 * time.Second},
		cv:   protocol.Get().MpayAppCv,
	}
}

func (c *Client) get(path string, q url.Values) (map[string]any, error) {
	u := c.Base + path + "?" + q.Encode()
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s 返回非 JSON: %s", path, truncate(string(b), 300))
	}
	return out, nil
}

// CreateLogin 申请一个二维码会话，返回 uuid。
func (c *Client) CreateLogin(gameID string) (string, map[string]any, error) {
	q := url.Values{}
	q.Set("game_id", gameID)
	q.Set("process_id", fmt.Sprint(os.Getpid()))
	q.Set("cv", c.cv)
	q.Set("app_channel", profile.HuaweiChannel)
	q.Set("_cloud_extra_base64", "e30=")
	data, err := c.get("/mpay/api/qrcode/create_login", q)
	if err != nil {
		return "", nil, err
	}
	uuid, _ := data["uuid"].(string)
	if uuid == "" {
		return "", data, fmt.Errorf("create_login 未返回 uuid: %s", truncate(fmt.Sprint(data), 400))
	}
	return uuid, data, nil
}

// Scan 把二维码标记为"已扫描"。
func (c *Client) Scan(uuid, gameID string) (map[string]any, error) {
	q := url.Values{}
	q.Set("uuid", uuid)
	q.Set("login_channel", profile.HuaweiChannel)
	q.Set("app_channel", profile.HuaweiChannel)
	q.Set("pay_channel", profile.HuaweiChannel)
	q.Set("game_id", gameID)
	q.Set("gv", profile.MpayGV)
	q.Set("gvn", profile.MpayGvn)
	q.Set("cv", profile.MpayCv)
	return c.get("/mpay/api/qrcode/scan", q)
}

// Confirm 确认登录，body 为渠道登录包 + uuid + game_id。
func (c *Client) Confirm(uuid, gameID string, payload map[string]string) (map[string]any, error) {
	form := url.Values{}
	for k, v := range payload {
		form.Set(k, v)
	}
	form.Set("uuid", uuid)
	form.Set("game_id", gameID)

	u := c.Base + "/mpay/api/qrcode/confirm_login"
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
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("confirm_login 返回非 JSON: %s", truncate(string(b), 300))
	}
	return out, nil
}

// Query 轮询二维码状态。
func (c *Client) Query(uuid, gameID string) (map[string]any, error) {
	q := url.Values{}
	q.Set("uuid", uuid)
	q.Set("game_id", gameID)
	q.Set("process_id", fmt.Sprint(os.Getpid()))
	q.Set("cv", c.cv)
	return c.get("/mpay/api/qrcode/query", q)
}

// WaitTicket 轮询直到二维码被确认，返回 login_info.code（一次性票据）。
func (c *Client) WaitTicket(uuid, gameID string, timeout time.Duration) (string, map[string]any, error) {
	deadline := time.Now().Add(timeout)
	var last map[string]any
	for time.Now().Before(deadline) {
		data, err := c.Query(uuid, gameID)
		if err == nil {
			last = data
			if qr, ok := data["qrcode"].(map[string]any); ok {
				if st, _ := qr["status"].(float64); st == 2 {
					if info, ok := data["login_info"].(map[string]any); ok {
						if code, _ := info["code"].(string); code != "" {
							return code, data, nil
						}
					}
				}
			}
			if code, ok := data["code"].(float64); ok && code != -1 && code != 0 {
				return "", data, fmt.Errorf("query 返回错误 code=%v reason=%v", code, data["reason"])
			}
		}
		time.Sleep(600 * time.Millisecond)
	}
	return "", last, fmt.Errorf("等待确认超时（%s）", timeout)
}

// Login 完整走一遍：create_login → scan → confirm → 等票据。
func (c *Client) Login(gameID string, payload map[string]string) (string, map[string]any, error) {
	uuid, createData, err := c.CreateLogin(gameID)
	if err != nil {
		return "", createData, err
	}
	if _, err := c.Scan(uuid, gameID); err != nil {
		return "", nil, fmt.Errorf("scan 失败: %w", err)
	}
	conf, err := c.Confirm(uuid, gameID, payload)
	if err != nil {
		return "", nil, fmt.Errorf("confirm 失败: %w", err)
	}
	if code, ok := conf["code"].(float64); ok && code != 0 && code != 200 {
		return "", conf, fmt.Errorf("confirm 被拒绝: %s", truncate(fmt.Sprint(conf), 400))
	}
	return c.WaitTicket(uuid, gameID, 30*time.Second)
}

// SignPayload 保留一个签名工具入口，便于调试。
func SignPayload(body string) string {
	return netsec.HMACSign("POST", profile.MpayBaseURL+"/mpay/api/qrcode/confirm_login", body, protocol.Get().LogKey)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// CreateLoginHosted 用"发烧平台托管 MPay"的参数申请二维码。
func (c *Client) CreateLoginHosted(deviceID, gameID, dstJFGame, appChannel, appCv string) (string, map[string]any, error) {
	q := url.Values{}
	q.Set("app_channel", appChannel)
	q.Set("qrcode_channel_type", "2")
	q.Set("dst_jf_game_id", dstJFGame)
	q.Set("cv", appCv)
	q.Set("is_remember", "2")
	q.Set("device_id", deviceID)
	q.Set("game_id", gameID)
	data, err := c.get("/mpay/api/qrcode/create_login", q)
	if err != nil {
		return "", data, err
	}
	uuid, _ := data["uuid"].(string)
	if uuid == "" {
		return "", data, fmt.Errorf("create_login 未返回 uuid: %s", truncate(fmt.Sprint(data), 300))
	}
	return uuid, data, nil
}

// ScanHosted 以托管身份标记二维码已扫描。
func (c *Client) ScanHosted(uuid, deviceID, gameID string) (string, map[string]any, error) {
	q := url.Values{}
	q.Set("uuid", uuid)
	q.Set("login_channel", profile.HuaweiChannel)
	q.Set("app_channel", profile.HuaweiChannel)
	q.Set("pay_channel", profile.HuaweiChannel)
	q.Set("game_id", gameID)
	q.Set("gv", profile.MpayGV)
	q.Set("gvn", profile.MpayGvn)
	q.Set("cv", profile.MpayCv)
	data, err := c.get("/mpay/api/qrcode/scan", q)
	return gameID, data, err
}

// ConfirmForm 原样提交 confirm_login 表单。
func (c *Client) ConfirmForm(form map[string]string) (map[string]any, error) {
	v := url.Values{}
	for k, val := range form {
		v.Set(k, val)
	}
	u := c.Base + "/mpay/api/qrcode/confirm_login"
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("confirm_login 返回非 JSON: %s", truncate(string(b), 200))
	}
	return out, nil
}

// CreateLoginHostedQCT 指定 qrcode_channel_type 的托管建码。
func (c *Client) CreateLoginHostedQCT(deviceID, gameID, dstJFGame, appChannel, appCv, qct string) (string, map[string]any, error) {
	q := url.Values{}
	q.Set("app_channel", appChannel)
	q.Set("qrcode_channel_type", qct)
	q.Set("dst_jf_game_id", dstJFGame)
	q.Set("cv", appCv)
	q.Set("is_remember", "2")
	q.Set("device_id", deviceID)
	q.Set("game_id", gameID)
	data, err := c.get("/mpay/api/qrcode/create_login", q)
	if err != nil {
		return "", data, err
	}
	uuid, _ := data["uuid"].(string)
	if uuid == "" {
		return "", data, fmt.Errorf("无 uuid: %s", truncate(fmt.Sprint(data), 200))
	}
	return uuid, data, nil
}

// LoginHosted 走"发烧平台托管"完整链路：建码 → 扫码 → 确认 → 取票据。
func (c *Client) LoginHosted(deviceID string, payload map[string]string, gameID, dstJFGame, appChannel, appCv, qct string) (string, map[string]any, error) {
	uuid, createData, err := c.CreateLoginHostedQCT(deviceID, gameID, dstJFGame, appChannel, appCv, qct)
	if err != nil {
		return "", createData, err
	}
	scanGame, scanData, err := c.ScanHosted(uuid, deviceID, gameID)
	if err != nil {
		return "", scanData, fmt.Errorf("scan 失败: %w", err)
	}
	if scanData != nil {
		if code, _ := scanData["code"].(float64); code == 1424 {
			if g, ok := scanData["game"].(map[string]any); ok {
				if id, _ := g["id"].(string); id != "" {
					scanGame = id
					if _, sd2, err2 := c.ScanHosted(uuid, deviceID, scanGame); err2 == nil {
						scanData = sd2
					}
				}
			}
		}
	}
	form := map[string]string{}
	for k, v := range payload {
		form[k] = v
	}
	form["uuid"] = uuid
	form["game_id"] = scanGame
	conf, err := c.ConfirmForm(form)
	if err != nil {
		return "", conf, fmt.Errorf("confirm 失败: %w", err)
	}
	if code, ok := conf["code"].(float64); ok && code != 0 {
		return "", conf, fmt.Errorf("confirm 被拒绝: code=%v reason=%v", code, conf["reason"])
	}
	return c.WaitTicket(uuid, scanGame, 30*time.Second)
}

// LoginGame 用"游戏自身"的参数建码（game_id 用目标游戏自己的长 id），
// 这是抓包确认过的真实参数组合。
func (c *Client) LoginGame(deviceID string, payload map[string]string, longGameID, shortGameID, appChannel, appCv, gv, sv, arch string) (string, map[string]any, error) {
	q := url.Values{}
	q.Set("app_channel", appChannel)
	q.Set("app_mode", "2")
	q.Set("app_type", "games")
	q.Set("arch", arch)
	q.Set("cv", appCv)
	q.Set("device_id", deviceID)
	q.Set("game_id", longGameID)
	q.Set("gv", gv)
	q.Set("is_remember", "2")
	q.Set("jf_game_id", shortGameID)
	q.Set("mcount_app_key", protocol.Get().MpayKey)
	q.Set("mcount_transaction_id", "1")
	q.Set("process_id", fmt.Sprint(os.Getpid()))
	q.Set("qrcode_channel_type", "2")
	q.Set("sv", sv)
	q.Set("updater_cv", "c1.0.0")

	data, err := c.get("/mpay/api/qrcode/create_login", q)
	if err != nil {
		return "", data, err
	}
	uuid, _ := data["uuid"].(string)
	if uuid == "" {
		return "", data, fmt.Errorf("create_login 未返回 uuid: %s", truncate(fmt.Sprint(data), 200))
	}
	if _, sd, err := c.ScanHosted(uuid, deviceID, longGameID); err != nil {
		return "", sd, fmt.Errorf("scan 失败: %w", err)
	}
	form := map[string]string{}
	for k, v := range payload {
		form[k] = v
	}
	form["uuid"] = uuid
	form["game_id"] = longGameID
	conf, err := c.ConfirmForm(form)
	if err != nil {
		return "", conf, fmt.Errorf("confirm 失败: %w", err)
	}
	if code, ok := conf["code"].(float64); ok && code != 0 {
		return "", conf, fmt.Errorf("confirm 被拒绝: code=%v reason=%v", code, conf["reason"])
	}
	return c.WaitTicket(uuid, longGameID, 30*time.Second)
}

// DeviceInfo 是本机的设备信息，用于向 MPay 注册一个新设备。
type DeviceInfo struct {
	Name       string // 计算机名
	Mac        string // 形如 8C-32-23-14-06-2F
	Resolution string // 形如 2560*1440
	UniqueID   string // 32 位十六进制，本机唯一即可
	ProcessID  int
}

// RegisterDevice 向 MPay 登记一台"新设备"，返回分配到的 id 与 key。
//
// 这是首次启动游戏时客户端自己也会走的一步：请求里**不带 device_id**，
// 服务器按设备信息下发一个（以及一把 key）。
// 注意：每次调用都会分配新的 id，所以调用方必须把结果存下来复用。
func (c *Client) RegisterDevice(longGameID, shortGameID string, info DeviceInfo) (string, string, error) {
	form := url.Values{}
	form.Set("app_channel", "a50_sdk_cn")
	form.Set("app_mode", "2")
	form.Set("app_type", "games")
	form.Set("brand", "Microsoft")
	form.Set("cv", "a5.10.0")
	form.Set("device_model", "pc_mode")
	form.Set("device_name", info.Name)
	form.Set("device_type", "Computer")
	form.Set("game_id", longGameID)
	form.Set("gv", "0.0.0.0")
	form.Set("init_urs_device", "0")
	form.Set("jf_game_id", shortGameID)
	form.Set("mac", info.Mac)
	form.Set("mcount_app_key", protocol.Get().MpayKey)
	form.Set("mcount_transaction_id", "1")
	form.Set("process_id", fmt.Sprint(info.ProcessID))
	form.Set("resolution", info.Resolution)
	form.Set("sv", "10.0.19045")
	form.Set("system_name", "windows")
	form.Set("system_version", "10.0.19045")
	form.Set("transid", "")
	form.Set("udid", "")
	form.Set("uni_transaction_id", "")
	form.Set("unique_id", info.UniqueID)
	form.Set("updater_cv", "c1.0.0")

	u := c.Base + "/mpay/games/" + longGameID + "/devices"
	resp, err := c.http.PostForm(u, form)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)

	var out struct {
		Device struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"device"`
		Code   float64 `json:"code"`
		Reason string  `json:"reason"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", "", fmt.Errorf("注册设备返回非 JSON: %s", truncate(string(b), 200))
	}
	if out.Device.ID == "" {
		if out.Reason == "" {
			out.Reason = truncate(string(b), 200)
		}
		return "", "", fmt.Errorf("注册设备失败: %s", out.Reason)
	}
	return out.Device.ID, out.Device.Key, nil
}

// CreateScanSession 只创建二维码，不做扫码/确认 —— 交给手机去扫（光遇手游/网易大神）。
// 返回 uuid 与 create_login 的响应。
func (c *Client) CreateScanSession(deviceID, longGameID, shortGameID, appChannel, appCv, gv, sv, arch string) (string, map[string]any, error) {
	q := url.Values{}
	q.Set("app_channel", appChannel)
	q.Set("app_mode", "2")
	q.Set("app_type", "games")
	q.Set("arch", arch)
	q.Set("cv", appCv)
	q.Set("device_id", deviceID)
	q.Set("game_id", longGameID)
	q.Set("gv", gv)
	q.Set("is_remember", "2")
	q.Set("jf_game_id", shortGameID)
	q.Set("mcount_app_key", protocol.Get().MpayKey)
	q.Set("mcount_transaction_id", "1")
	q.Set("process_id", fmt.Sprint(os.Getpid()))
	q.Set("qrcode_channel_type", "2")
	q.Set("sv", sv)
	q.Set("updater_cv", "c1.0.0")

	data, err := c.get("/mpay/api/qrcode/create_login", q)
	if err != nil {
		return "", data, err
	}
	uuid, _ := data["uuid"].(string)
	if uuid == "" {
		if c, _ := data["code"].(float64); c != 0 {
			return "", data, fmt.Errorf("建码被拒: code=%v reason=%v", c, data["reason"])
		}
		return "", data, errors.New("建码未返回 uuid")
	}
	return uuid, data, nil
}

// QRImage 取二维码图片（PNG）。
func (c *Client) QRImage(uuid, longGameID, appCv string) ([]byte, error) {
	q := url.Values{}
	q.Set("uuid", uuid)
	q.Set("game_id", longGameID)
	q.Set("cv", appCv)
	u := c.Base + "/mpay/api/qrcode/image?" + q.Encode()
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ScanStatus 查询二维码状态；confirmed 为 true 时同时返回票据与渠道。
func (c *Client) ScanStatus(uuid, longGameID string) (status int, code, channel string, err error) {
	data, qerr := c.Query(uuid, longGameID)
	if qerr != nil {
		return 0, "", "", qerr
	}
	if q, ok := data["qrcode"].(map[string]any); ok {
		if st, _ := q["status"].(float64); st >= 0 {
			status = int(st)
		}
	}
	if info, ok := data["login_info"].(map[string]any); ok {
		code, _ = info["code"].(string)
		channel, _ = info["login_channel"].(string)
	}
	return status, code, channel, nil
}
