package app

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// buildWebviewArg 造一个和游戏一样的浏览器参数（base64 json）。
func buildWebviewArg(t *testing.T, channel string) string {
	t.Helper()
	ext, _ := json.Marshal(map[string]any{
		"cache_path_channel_name": channel,
		"enable_media_stream":     1,
	})
	cfg := map[string]any{
		"configs.configs_extension_json":    string(ext),
		"configs.force_device_scale_factor": 1.25,
		"webview_type":                      2,
	}
	raw, _ := json.Marshal(cfg)
	return base64.StdEncoding.EncodeToString(raw)
}

func channelOf(t *testing.T, arg string) string {
	t.Helper()
	cfg, ok := decodeWebviewConfig(arg)
	if !ok {
		t.Fatalf("解不出来: %.40s", arg)
	}
	var ext map[string]any
	if err := json.Unmarshal([]byte(cfg["configs.configs_extension_json"].(string)), &ext); err != nil {
		t.Fatal(err)
	}
	name, _ := ext["cache_path_channel_name"].(string)
	return name
}

// TestRewriteChannel 只改 channel，别的参数原样保留。
func TestRewriteChannel(t *testing.T) {
	arg := buildWebviewArg(t, "h5_copyabc")
	in := []string{"Mpay_IPC_123_456_abc_", arg, "--no-sandbox"}

	out, note := rewriteChannel(in, "h5_sky1", false)
	if len(out) != 3 {
		t.Fatalf("参数个数变了: %v", out)
	}
	if out[0] != in[0] || out[2] != "--no-sandbox" {
		t.Fatalf("非配置参数被改动: %v", out)
	}
	if got := channelOf(t, out[1]); got != "h5_sky1" {
		t.Fatalf("channel 没改成功: %q", got)
	}
	if note != "（原 h5_copyabc）" {
		t.Fatalf("日志备注不对: %q", note)
	}
	if cfg, _ := decodeWebviewConfig(out[1]); cfg["webview_type"] == nil {
		t.Fatal("webview_type 丢了")
	}
}

// TestPickChannelKeepsKindsSeparate 三类固定 profile 都要各自独立。
//
// 实测一个客户端会起三种浏览器：h5_copy<账号 id>（登录）、h5_payment（支付/
// 精灵）、ngwebview（活动）、gmbridge（游戏桥）。后三种是所有实例共用的，
// 以前都被改成登录 channel，于是它们和登录页、以及彼此之间都挤进同一个
// CEF profile——独占锁抢不到，活动页就会报 DNS_PROBE_FINISHED_NXDOMAIN。
func TestPickChannelKeepsKindsSeparate(t *testing.T) {
	login := "h5_copyaebghqhjtbzebtoj"
	suffix := "copyaebghqhjtbzebtoj"

	// 登录 profile 本来就是一行一份，直接换成 channel
	if got := pickChannel(login, login); got != login {
		t.Fatalf("登录 profile 不该变: %q", got)
	}
	// 三类固定 profile：保留类别、只接账号键
	for _, orig := range []string{"h5_payment", "ngwebview", "gmbridge"} {
		want := orig + "_" + suffix
		if got := pickChannel(orig, login); got != want {
			t.Fatalf("pickChannel(%q)=%q，期望 %q", orig, got, want)
		}
		if got := pickChannel(orig, login); got == login {
			t.Fatalf("%q 和登录 profile 撞了", orig)
		}
	}
	// 三类之间也不能撞
	seen := map[string]string{}
	for _, orig := range []string{"h5_payment", "ngwebview", "gmbridge"} {
		got := pickChannel(orig, login)
		if prev, ok := seen[got]; ok {
			t.Fatalf("%q 和 %q 撞到同一个 profile %q", orig, prev, got)
		}
		seen[got] = orig
	}
	// 多开之间也要分开
	if pickChannel("ngwebview", "h5_sky1") == pickChannel("ngwebview", "h5_sky2") {
		t.Fatal("两个实例的活动页 profile 撞了")
	}
	// 空的原值就直接用 channel
	if got := pickChannel("", "h5_sky1793"); got != "h5_sky1793" {
		t.Fatalf("空原值应当直接用 channel，得到 %q", got)
	}
}

// TestRewriteChannelIgnoresForeignArgs 不是配置的参数不能动。
func TestRewriteChannelIgnoresForeignArgs(t *testing.T) {
	in := []string{"Mpay_IPC_1_2_x_", "not-a-config", "--flag=1"}
	out, _ := rewriteChannel(in, "h5_x", false)
	if strings.Join(out, "|") != strings.Join(in, "|") {
		t.Fatalf("不该改动任何东西: %v", out)
	}
}

// TestShimChannel 目录名要能被压成合法 channel。
func TestShimChannel(t *testing.T) {
	got := shimChannel("sky-1793107448332-1")
	if got != "h5_sky17931074483321" {
		t.Fatalf("shimChannel=%q", got)
	}
	if strings.ContainsAny(got, ".-\\ ") {
		t.Fatalf("channel 里有非法字符: %q", got)
	}
}

// TestDecodeWebviewConfigRejectsGarbage 普通 base64 不能被当成浏览器配置。
func TestDecodeWebviewConfigRejectsGarbage(t *testing.T) {
	if _, ok := decodeWebviewConfig("aGVsbG8="); ok { // "hello"
		t.Fatal("普通 base64 不该被当成配置")
	}
}
