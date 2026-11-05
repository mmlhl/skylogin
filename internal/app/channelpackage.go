package app

// 渠道整包登录凭证（DLL 回调里 extra_unisdk_data 那一坨）。
//
// 两条渠道链路给的东西不一样：
//
//	华为     sdk_info = {openid, accessToken}，要我们自己调华为接口换 gameAuthSign
//	vivo     SAUTH_JSON / SAUTH_STR —— 已经是**拼好的登录包**：
//	         gameid=ma75&login_channel=nearme_vivo&username=…&sessionid=…&sdk_version=…
//
// 对第二种，最省事也最不容易错的做法是：把整包原样存下来，登录时把里面的字段
// 盖到我们自己的 SAUTH 上。这样以后再来新渠道（小米/OPPO/B站…），只要 DLL 能
// 把它登进来，这边不用改代码。

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// channelPackage 是解析后的渠道登录包。
type channelPackage struct {
	Fields     map[string]any // SAUTH_JSON 的全部字段（或 SAUTH_STR 拆出来的键值）
	Channel    string         // login_channel，例如 nearme_vivo
	UID        string         // sdkuid
	Session    string         // sessionid
	Access     string         // access_token（有的渠道没有）
	SDKVersion string
	Realname   string
	GameID     string
}

// parseChannelPackage 解析 extra_unisdk_data。
//
// 优先用 SAUTH_JSON（字段最全）；没有就退回 SAUTH_STR（查询串形式）。
func parseChannelPackage(raw string) (*channelPackage, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("登录包为空")
	}
	var wrap map[string]any
	if err := json.Unmarshal([]byte(raw), &wrap); err != nil {
		return nil, fmt.Errorf("登录包不是 JSON: %w", err)
	}

	pkg := &channelPackage{}
	// SAUTH_JSON / SAUTH_STR 都是 **base64**（"eyJ…" 就是 "{…" 的 base64），
	// 先解码再按 JSON / 查询串解析。
	if s, _ := wrap["SAUTH_JSON"].(string); s != "" {
		var fields map[string]any
		if err := json.Unmarshal([]byte(decodeBase64Text(s)), &fields); err == nil {
			pkg.Fields = fields
		}
	}
	if pkg.Fields == nil {
		if s, _ := wrap["SAUTH_STR"].(string); s != "" {
			if vals, err := url.ParseQuery(decodeBase64Text(s)); err == nil {
				fields := map[string]any{}
				for k := range vals {
					fields[k] = vals.Get(k)
				}
				pkg.Fields = fields
			}
		}
	}
	if pkg.Fields == nil {
		return nil, errors.New("登录包里既没有 SAUTH_JSON 也没有 SAUTH_STR")
	}

	pkg.Channel = fieldStr(pkg.Fields, "login_channel", "app_channel")
	// 华为那份叫 sdkuid，vivo 那份写的是 sdkuiid（多一个 i），两个都认
	pkg.UID = fieldStr(pkg.Fields, "sdkuid", "sdkuiid", "uid")
	pkg.Session = fieldStr(pkg.Fields, "sessionid", "session_id")
	pkg.Access = fieldStr(pkg.Fields, "access_token", "accessToken")
	pkg.SDKVersion = fieldStr(pkg.Fields, "sdk_version")
	pkg.Realname = fieldStr(pkg.Fields, "realname")
	pkg.GameID = fieldStr(pkg.Fields, "gameid")
	return pkg, nil
}

// decodeBase64Text 能解就解，解不开就原样返回。
//
// 渠道包里的 SAUTH_JSON / SAUTH_STR 是 base64，但**编码方式不固定**：
// 同一个渠道不同次扫码，padding 有时是 "=="、有时被 URL 转义成 "%3D"
// （SDK 会先把这串塞进查询串再取出来）。所以先做一次 URL 反转义，
// 再依次试几种 base64 变体。
func decodeBase64Text(s string) string {
	s = strings.TrimSpace(s)
	if u, err := url.QueryUnescape(s); err == nil {
		s = strings.TrimSpace(u)
	}
	// 标准 base64 的 "+" 在表单/查询串里可能被当成空格
	s = strings.ReplaceAll(s, " ", "+")

	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		b, err := enc.DecodeString(s)
		if err != nil || len(b) == 0 || !looksLikeText(b) {
			continue
		}
		return string(b)
	}
	return s
}

// looksLikeText 判断解出来的字节像不像文本（防止把二进制误当成 base64 解）。
func looksLikeText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	bad := 0
	for _, r := range string(b) {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			bad++
		}
	}
	return bad*10 < len(b)
}

// fieldStr 按顺序取第一个非空字符串字段。
func fieldStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, _ := m[k].(string); strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// channelKeyOf 把服务端的渠道名映射成账号文件里用的渠道 key。
//
// 为什么要映射：同一个玩家可能先用网页登录加过（Channel = "vivo"），
// 后来又用渠道服扫码加了一次（服务端叫 "nearme_vivo"）。如果两边存的
// channel 不一样，saveAccount 会认为是两个账号，列表里就会出现两条
// 一模一样的记录（点一个另一个跟着亮）。
//
// 认不出来的渠道原样返回——整包那条链路本来就不依赖这个 key。
func channelKeyOf(name string) string {
	switch name {
	case "nearme_vivo", "vivo":
		return channelKeyVivo
	case "bilibili", "bilibili_sdk":
		return channelKeyBili
	case "m4399", "4399com":
		return channelKeyM4399
	}
	return name
}

const (
	channelKeyBili  = "bilibili"
	channelKeyM4399 = "m4399"
	channelKeyVivo  = "vivo"
)

// displayChannel 把渠道名变成界面上看得懂的名字。
//
// 渠道清单只在 scanchen.go 的注册表里维护，这里只做别名归一
// （同一个渠道，扫码叫 nearme_vivo、网页登录叫 vivo）。
func displayChannel(name string) string {
	switch name {
	case "vivo":
		return labelOrKey("nearme_vivo")
	case "bilibili":
		return labelOrKey("bilibili_sdk")
	case "m4399":
		return labelOrKey("4399com")
	case "xiaomi", "mi":
		return "小米"
	case "oppo", "nearme_oppo":
		return "OPPO"
	case "huawei":
		return labelOrKey("huawei")
	}
	return labelOrKey(name)
}
