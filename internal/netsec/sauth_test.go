package netsec

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"skylogin/internal/device"
)

// 固定向量：签名结果必须稳定（用官方客户端的签名行为核对过），
// 用来确保签名字节和线上行为保持一致。
func TestHMACSignMatchesReference(t *testing.T) {
	url := "https://mgbsdk.matrix.netease.com/ma75/sdk/uni_sauth"
	body := `{"gameid": "ma75", "login_channel": "huawei", "sdkuid": "12345678"}`
	key := "lM8jwFDFbrbKABNJr3IAKZHyavfDQfHu"
	const want = "1a218ad72a214326c87b31ba1e0597df9dbc339141c2935af700a81f988920b9"
	if got := HMACSign("POST", url, body, key); got != want {
		t.Fatalf("HMACSign 不一致\n got=%s\nwant=%s", got, want)
	}
}

func TestSigningText(t *testing.T) {
	got := signingText("post", "https://mgbsdk.matrix.netease.com/ma75/sdk/uni_sauth", `{"a":1}`)
	want := `POST/ma75/sdk/uni_sauth{"a":1}`
	if got != want {
		t.Fatalf("signingText 不一致: %q != %q", got, want)
	}
}

func TestNewSAUTHFields(t *testing.T) {
	dev := fakeDevice()
	m := NewSAUTH("123", "session", "ma75", map[string]any{"timestamp": "1"}, dev)
	for _, k := range []string{
		"gameid", "login_channel", "app_channel", "platform", "sdkuid", "udid",
		"sessionid", "sdk_version", "is_unisdk_guest", "ip", "aim_info",
		"source_app_channel", "source_platform", "client_login_sn", "step",
		"step2", "hostid", "sdklog", "timestamp",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("缺少字段 %s", k)
		}
	}
	if m["gameid"] != "ma75" || m["login_channel"] != "huawei" {
		t.Errorf("渠道字段不对: %v %v", m["gameid"], m["login_channel"])
	}
	if m["sessionid"] != "session" || m["sdkuid"] != "123" {
		t.Errorf("会话字段不对")
	}
}

func fakeDevice() *device.ClientProfile {
	return &device.ClientProfile{
		DeviceModel: "M2102K1AC",
		OSName:      "android",
		OSVer:       "12",
		Udid:        "0123456789abcdef",
		AppVer:      "157",
		Imei:        "123456789012345",
		CountryCode: "CN",
	}
}

func TestNewLoginPayload(t *testing.T) {
	sdkJSON := `{"username":"tester"}`
	authResp := map[string]any{
		"unisdk_login_json": base64.StdEncoding.EncodeToString([]byte(sdkJSON)),
	}
	p, err := NewLoginPayload(authResp, "SIGN+base64/==", "10001", "7", "1700000000000", fakeDevice())
	if err != nil {
		t.Fatalf("NewLoginPayload 失败: %v", err)
	}
	for _, k := range []string{"user_id", "token", "login_channel", "udid", "app_channel",
		"sdk_version", "jf_game_id", "pay_channel", "extra_unisdk_data", "gv", "gvn", "cv"} {
		if p.Body[k] == "" {
			t.Errorf("登录包缺少字段 %s", k)
		}
	}
	if p.Body["login_channel"] != "huawei" || p.Body["user_id"] != "10001" {
		t.Errorf("字段值不对: %v", p.Body)
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(p.ExtraUnisdkData), &extra); err != nil {
		t.Fatalf("extra_unisdk_data 不是合法 JSON: %v", err)
	}
	for _, k := range []string{"SAUTH_STR", "SAUTH_JSON"} {
		s, _ := extra[k].(string)
		if s == "" {
			t.Fatalf("缺少 %s", k)
		}
		if _, err := base64.StdEncoding.DecodeString(s); err != nil {
			t.Fatalf("%s 不是合法 base64: %v", k, err)
		}
	}
	// SAUTH_JSON 解出来应当带 username 与 sessionid
	raw, _ := base64.StdEncoding.DecodeString(extra["SAUTH_JSON"].(string))
	var sj map[string]any
	if err := json.Unmarshal(raw, &sj); err != nil {
		t.Fatalf("SAUTH_JSON 内容非法: %v", err)
	}
	if sj["sessionid"] != "SIGN+base64/==" || sj["sdkuid"] != "10001" {
		t.Errorf("SAUTH_JSON 会话字段不对: %v", sj)
	}
	if sj["username"] == "" {
		t.Errorf("SAUTH_JSON 缺少 username")
	}
}

func TestNewLoginPayloadMissingJSON(t *testing.T) {
	if _, err := NewLoginPayload(map[string]any{}, "s", "u", "1", "t", fakeDevice()); err == nil {
		t.Fatal("缺少 unisdk_login_json 时应当报错")
	}
}
