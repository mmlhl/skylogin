package huawei

// 从 ST 一路换到"游戏会话"：
//   ST →（按包名绑定）→ access_token →（getGameAuthSign）→ playerId / gameAuthSign

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"skylogin/internal/device"
)

// BindST 把 ST 变换成绑定包名的 secret token。
//
// 算法：ST 前 20 位 + SHA256(ST + ":" + 包名) 的十六进制。
func BindST(st, packageName string) string {
	if st == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(st + ":" + packageName))
	head := st
	if len(head) > 20 {
		head = head[:20]
	}
	return head + hex.EncodeToString(sum[:])
}

// GetAccessToken 用 ST 为指定包名换 access_token。
func GetAccessToken(st, packageName, clientID, deviceID string) (map[string]any, error) {
	query := url.Values{}
	query.Set("client_id", clientID)
	query.Set("sdkVersion", "61400300")
	endpoint := "https://oauth-login.platform.hicloud.com/oauth2/v3/silent_token?" + query.Encode()

	form := url.Values{}
	form.Set("grant_type", "service_token")
	form.Set("service_token", BindST(st, packageName))
	form.Set("scope", "openid")
	form.Set("device_type", "6")
	form.Set("package_name", packageName)
	form.Set("siteId", "1")
	form.Set("device_id", deviceID)
	form.Set("need_code", "true")
	form.Set("client_id", clientID)

	req, err := http.NewRequest(http.MethodPost, endpoint+"&ctrID="+traceID(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Connection", "Keep-Alive")
	req.Header.Set("Authorization", strconv.FormatInt(time.Now().UnixMilli(), 10))
	req.Header.Set("User-Agent", ua)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("silent_token 解析失败: %s", string(raw))
	}
	return out, nil
}

// GetGameSession 用 access_token 换玩家会话（playerId / gameAuthSign / ts / playerLevel）。
func GetGameSession(accessToken, appID, phoneType string, dev *device.AndroidProfile) (map[string]any, error) {
	endpoint := "https://jgw-drcn.jos.dbankcloud.cn/gameservice/api/gbClientApi"

	form := url.Values{}
	add := func(k, v string) { form.Set(k, v) }
	add("gamePopTime", "0")
	add("idType", "2")
	add("hmsSdkVersionCode", "60100302")
	add("hmsSdkVersionName", "6.1.0.302")
	add("hmsApkVersionName", "6.14.0.302")
	add("hmsApkVersionCode", "61400302")
	add("kitver", "61400300")
	add("thirdAppVersion", "1.5.99")
	add("clientPackage", "com.huawei.gamebox")
	add("locale", "zh_CN")
	add("timeZone", "Asia/Shanghai")
	add("serviceType", "47")
	add("odm", "0")
	add("countryCode", "CN")
	add("deviceIdType", strconv.Itoa(dev.DeviceIDType))
	add("deviceId", dev.DeviceID)
	add("brand", dev.Brand)
	add("romVersion", dev.RomVersion)
	add("androidVersion", dev.AndroidVersion)
	add("manufacturer", dev.Manufacturer)
	add("phoneType", dev.PhoneType)
	add("method", "client.hms.gs.getGameAuthSign")
	add("extraBody", `json={"appId":"`+appID+`"}`)
	add("accessToken", accessToken)

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", fmt.Sprintf(
		"com.huawei.hms.game/6.14.0.300 (Linux; Android 12; %s) RestClient/7.0.6.300", phoneType))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("getGameAuthSign 解析失败: %s", string(raw))
	}
	return out, nil
}
