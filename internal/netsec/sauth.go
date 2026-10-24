package netsec

// uni_sauth：把渠道登录结果换成 MPay 认的登录包。
//
// 分两步：先用 SAUTH 参数请求 uni_sauth 拿到 unisdk_login_json，
// 再把它拼成 confirm_login 需要的 body。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
)

// NewSAUTH 生成 uni_sauth 的请求体。custom 里的字段会覆盖同名默认值。
func NewSAUTH(uid, session, gameID string, custom map[string]any, dev *device.ClientProfile) map[string]any {
	sdklog, _ := json.Marshal(dev)
	ip := OutboundIP()
	data := map[string]any{
		"gameid":             gameID,
		"login_channel":      profile.HuaweiChannel,
		"app_channel":        profile.HuaweiChannel,
		"platform":           "ad",
		"sdkuid":             uid,
		"udid":               dev.Udid,
		"sessionid":          session,
		"sdk_version":        protocol.Get().SdkVersion,
		"is_unisdk_guest":    0,
		"ip":                 ip,
		"aim_info":           fmt.Sprintf(`{"tz":"+0800","tzid":"Asia/Shanghai","aim":"%s","country":"CN"}`, ip),
		"source_app_channel": profile.HuaweiChannel,
		"source_platform":    "ad",
		"client_login_sn":    randHex(16),
		"step":               randDigits(10),
		"step2":              randDigits(9),
		"hostid":             0,
		"sdklog":             string(sdklog),
	}
	for k, v := range custom {
		data[k] = v
	}
	return data
}

// OutboundIP 查一下出口公网 IP（拿不到就空着，不影响主流程）。
func OutboundIP() string {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://who.nie.netease.com/")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var v struct {
		IP string `json:"ip"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return ""
	}
	return v.IP
}

// PostSigned 请求 uni_sauth（带 X-Client-Sign）。
func PostSigned(data map[string]any, shortGameID string) (map[string]any, error) {
	endpoint := profile.UniSdkBaseURL + "/" + shortGameID + "/sdk/uni_sauth"
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Client-Sign", HMACSign(http.MethodPost, endpoint, string(payload), protocol.Get().LogKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Dalvik/2.1.0 (Linux; U; Android 12; M2102K1AC Build/V417IR)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("uni_sauth 解析失败: %s", string(raw))
	}
	return out, nil
}

// LoginPayload 是 confirm_login 需要的渠道登录包。
type LoginPayload struct {
	Body            map[string]string
	ExtraUnisdkData string
	UserName        string
}

// NewLoginPayload 把 uni_sauth 的结果组装成 MPay 认的登录包。
func NewLoginPayload(authResp map[string]any, gameAuthSign, playerID, playerLevel, ts string, dev *device.ClientProfile) (*LoginPayload, error) {
	userName, err := decodeUnisdkUserName(authResp)
	if err != nil {
		return nil, err
	}

	custom := map[string]any{
		"anonymous":        "",
		"get_access_token": "0",
		"extra_data":       playerLevel,
		"timestamp":        ts,
		"realname":         `{"realname_type": 0, "duration": 0}`,
	}
	extraUnisdk := buildExtraUnisdk(NewSAUTH(playerID, gameAuthSign, profile.ShortGameID, custom, dev), userName, playerLevel, ts, dev)
	extraJSON, _ := json.Marshal(extraUnisdk)

	body := map[string]string{
		"user_id":           playerID,
		"token":             base64.StdEncoding.EncodeToString([]byte(gameAuthSign)),
		"login_channel":     profile.HuaweiChannel,
		"udid":              dev.Udid,
		"app_channel":       profile.HuaweiChannel,
		"sdk_version":       protocol.Get().SdkVersion,
		"jf_game_id":        profile.ShortGameID,
		"pay_channel":       profile.HuaweiChannel,
		"extra_data":        "",
		"extra_unisdk_data": string(extraJSON),
		"gv":                profile.MpayGV,
		"gvn":               profile.MpayGvn,
		"cv":                profile.MpayCv,
	}
	return &LoginPayload{Body: body, ExtraUnisdkData: string(extraJSON), UserName: userName}, nil
}

// decodeUnisdkUserName 从 uni_sauth 响应里解出 unisdk_login_json 里的用户名。
func decodeUnisdkUserName(resp map[string]any) (string, error) {
	encoded, ok := resp["unisdk_login_json"].(string)
	if !ok || encoded == "" {
		return "", fmt.Errorf("uni_sauth 未返回 unisdk_login_json: %v", resp)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	var sdk map[string]any
	if err := json.Unmarshal(raw, &sdk); err != nil {
		return "", err
	}
	name, _ := sdk["username"].(string)
	return name, nil
}

// buildExtraUnisdk 组装 confirm_login 里的 extra_unisdk_data。
//
// 里面有两份"同一份 SAUTH"：SAUTH_JSON 是原样的 JSON，
// SAUTH_STR 则是 k=v&k=v 拼接后再 base64（顺序无所谓）。
func buildExtraUnisdk(sauth map[string]any, userName, playerLevel, ts string, dev *device.ClientProfile) map[string]any {
	fields := map[string]any{
		"extra_data":       playerLevel,
		"get_access_token": "0",
		"sdk_udid":         dev.Udid,
		"realname":         `{"realname_type": 0, "duration": 0}`,
	}
	for k, v := range sauth {
		fields[k] = v
	}
	fields["username"] = userName

	var pairs strings.Builder
	first := true
	for k, v := range fields {
		if !first {
			pairs.WriteByte('&')
		}
		first = false
		pairs.WriteString(k)
		pairs.WriteByte('=')
		pairs.WriteString(url.QueryEscape(fmt.Sprint(v)))
	}
	strBlob := base64.StdEncoding.EncodeToString([]byte(pairs.String()))

	jsonBlob, _ := json.Marshal(fields)
	jsonStr := base64.StdEncoding.EncodeToString(jsonBlob)

	return map[string]any{
		"SAUTH_STR":        strBlob,
		"SAUTH_JSON":       jsonStr,
		"anonymous":        "",
		"get_access_token": "0",
		"extra_data":       playerLevel,
		"timestamp":        ts,
		"realname":         `{"realname_type": 0, "duration": 0}`,
	}
}
