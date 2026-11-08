package netsec

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"

	"skylogin/internal/device"
)

// 把 a50/ios 授权包传给组包函数，确保没有被重建成 ma75/ad 或生成新的登录序号。
func TestProbePayloadPreservesSAUTH(t *testing.T) {
	sauth := map[string]any{
		"gameid": "a50", "sdkuid": "test-user", "sessionid": "original-session",
		"login_channel": "netease", "app_channel": "netease.allysdk3rd",
		"platform": "ios", "source_platform": "ios", "sdk_version": "4.21.0",
		"client_login_sn": "original-sn", "udid": "source-device", "deviceid": "mpay-device",
	}
	encoded, _ := json.Marshal(map[string]any{"username": "test-user"})
	authResp := map[string]any{"unisdk_login_json": base64.StdEncoding.EncodeToString(encoded)}
	lp, err := NewLoginPayloadFromSAUTH(authResp, sauth, "payload-token", "0", "test-time", &device.ClientProfile{}, NeteaseChannelInfo())
	if err != nil {
		t.Fatal(err)
	}
	if lp.Body["jf_game_id"] != "a50" || lp.Body["app_channel"] != sauth["app_channel"] {
		t.Fatal("表单丢失原应用上下文")
	}
	if lp.Body["uuid"] != "" || lp.Body["game_id"] != "" {
		t.Fatal("二维码绑定应由确认步骤完成")
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(lp.ExtraUnisdkData), &extra); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(extra["SAUTH_JSON"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for key, want := range sauth {
		if !reflect.DeepEqual(fields[key], want) {
			t.Errorf("字段 %s 被替换", key)
		}
	}
	if len(sauth) != 11 {
		t.Fatal("修改了原始授权上下文")
	}
}

func TestProbePayloadRejectsMissingContext(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte(`{"username":"test-user"}`))
	_, err := NewLoginPayloadFromSAUTH(map[string]any{"unisdk_login_json": raw}, map[string]any{}, "token", "0", "1", &device.ClientProfile{}, NeteaseChannelInfo())
	if err == nil {
		t.Fatal("缺失授权上下文时不应隐式回退到 ma75")
	}
	if _, err := PostSignedWithKey(nil, "a50", ""); err == nil {
		t.Fatal("空密钥不应发起网络请求")
	}
}
