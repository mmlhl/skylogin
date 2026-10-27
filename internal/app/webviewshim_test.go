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
	arg := buildWebviewArg(t, "h5_payment")
	in := []string{"Mpay_IPC_123_456_abc_", arg, "--no-sandbox"}

	out := rewriteChannel(in, "h5_sky1")
	if len(out) != 3 {
		t.Fatalf("参数个数变了: %v", out)
	}
	if out[0] != in[0] || out[2] != "--no-sandbox" {
		t.Fatalf("非配置参数被改动: %v", out)
	}
	if got := channelOf(t, out[1]); got != "h5_sky1" {
		t.Fatalf("channel 没改成功: %q", got)
	}
	if cfg, _ := decodeWebviewConfig(out[1]); cfg["webview_type"] == nil {
		t.Fatal("webview_type 丢了")
	}
}

// TestRewriteChannelIgnoresForeignArgs 不是配置的参数不能动。
func TestRewriteChannelIgnoresForeignArgs(t *testing.T) {
	in := []string{"Mpay_IPC_1_2_x_", "not-a-config", "--flag=1"}
	out := rewriteChannel(in, "h5_x")
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
