package protocol

import (
	"os"
	"path/filepath"
	"testing"
)

// 空字段不应该覆盖已有值。
func TestMergeKeepsDefaults(t *testing.T) {
	base := Default()
	got := merge(base, Current{LogKey: "new-key", LongGameID: ""})
	if got.LogKey != "new-key" {
		t.Errorf("LogKey = %q，期望被覆盖", got.LogKey)
	}
	if got.LongGameID != base.LongGameID {
		t.Errorf("LongGameID = %q，期望保持默认 %q", got.LongGameID, base.LongGameID)
	}
}

// 本地缓存应该盖住内置默认值。
func TestInitLoadsCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "protocol.json")
	if err := os.WriteFile(cache, []byte(`{"app_channel":"x_sdk_cn","long_game_id":"abc-g-ma75"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	active = Default() // 重置，避免受其它用例影响
	Init(dir, "")

	got := Get()
	if got.AppChannel != "x_sdk_cn" {
		t.Errorf("AppChannel = %q", got.AppChannel)
	}
	if got.LongGameID != "abc-g-ma75" {
		t.Errorf("LongGameID = %q", got.LongGameID)
	}
	if got.LogKey != Default().LogKey {
		t.Errorf("没被覆盖的 LogKey 应该保持默认")
	}
}

// 缓存文件里的未知字段不应该影响结果。
func TestInitIgnoresUnknown(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "protocol.json"),
		[]byte(`{"log_key":"k2","something_else":"ignored"}`), 0o600)

	active = Default()
	Init(dir, "")

	if Get().LogKey != "k2" {
		t.Errorf("LogKey = %q", Get().LogKey)
	}
}
