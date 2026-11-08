package netsec

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"skylogin/internal/device"
	"skylogin/internal/profile"
)

// NewLoginPayloadFromSAUTH 供跨应用实验复用实际提交给 uni_sauth 的授权上下文。
// 旧组包方法会重新生成 ma75/ad 的 SAUTH；实验中不能把 a50/ios 等上下文悄悄替换掉。
// 这里仅校验字段并复制，是否允许跨应用授权仍由服务端决定。
func NewLoginPayloadFromSAUTH(authResp, sauth map[string]any, payloadToken, playerLevel, ts string, dev *device.ClientProfile, ch ChannelInfo) (*LoginPayload, error) {
	userName, err := DecodeUnisdkUserName(authResp)
	if err != nil {
		return nil, fmt.Errorf("授权响应缺少有效的 unisdk_login_json")
	}
	if userName == "" || dev == nil || payloadToken == "" {
		return nil, fmt.Errorf("授权组包缺少用户名、设备或凭证")
	}
	required := []string{"gameid", "sdkuid", "sessionid", "login_channel", "app_channel", "platform", "source_platform", "sdk_version"}
	for _, key := range required {
		if value, ok := sauth[key].(string); !ok || value == "" {
			return nil, fmt.Errorf("授权上下文缺少 %s", key)
		}
	}
	if sauth["login_channel"] != ch.Login {
		return nil, fmt.Errorf("授权渠道与组包渠道不一致")
	}
	extra := buildExtraUnisdk(sauth, userName, playerLevel, ts, dev, ch)
	extraJSON, err := json.Marshal(extra)
	if err != nil {
		return nil, fmt.Errorf("编码授权上下文失败")
	}
	body := map[string]string{
		"user_id":       sauth["sdkuid"].(string),
		"token":         base64.StdEncoding.EncodeToString([]byte(payloadToken)),
		"login_channel": ch.Login, "app_channel": sauth["app_channel"].(string),
		"sdk_version": sauth["sdk_version"].(string),
		"udid":        dev.Udid, "jf_game_id": sauth["gameid"].(string),
		"pay_channel": ch.Login, "extra_data": "", "extra_unisdk_data": string(extraJSON),
		"gv": profile.MpayGV, "gvn": profile.MpayGvn, "cv": profile.MpayCv,
	}
	return &LoginPayload{Body: body, ExtraUnisdkData: string(extraJSON), UserName: userName}, nil
}
