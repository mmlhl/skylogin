package mpayhost

import (
	"encoding/base64"
	"testing"
)

// makeToken 造一条 "1-" + base64(JSON) 形态的 token，用来测身份判定。
func makeToken(t *testing.T, gi string) string {
	t.Helper()
	body := `{"g_i":"` + gi + `","sa":3,"t":1}`
	return "1-" + base64.RawURLEncoding.EncodeToString([]byte(body))
}

func TestTokenOwnerIs(t *testing.T) {
	game := makeToken(t, "aecfs5n2y4aaaakd")
	platform := makeToken(t, "aecglf6ee4aaaarz")

	if !tokenOwnerIs(game, "aecfs5n2y4aaaakd-g-ma75") {
		t.Fatal("游戏身份凭证应判为游戏")
	}
	if tokenOwnerIs(game, "aecglf6ee4aaaarz-g-a50") {
		t.Fatal("游戏身份凭证不该判成平台")
	}
	if !tokenOwnerIs(platform, "aecglf6ee4aaaarz-g-a50") {
		t.Fatal("平台身份凭证应判为平台")
	}
	if tokenOwnerIs(platform, "aecfs5n2y4aaaakd-g-ma75") {
		t.Fatal("平台身份凭证不该判成游戏")
	}
	// 脏数据不能崩，也不能误判
	if tokenOwner("") != "" {
		t.Fatal("空 token 应返回空 g_i")
	}
	if tokenOwner("1-not-base64!!") != "" {
		t.Fatal("坏的 token 应返回空 g_i")
	}
	if tokenOwnerIs("", "aecfs5n2y4aaaakd-g-ma75") {
		t.Fatal("空 token 不该判为任何应用")
	}
}

// TestIdentityFromResult 验证结果槽位到身份结构的映射：
// slot0=用户 id、slot1=设备号、slot2=token。
func TestIdentityFromResult(t *testing.T) {
	r := Result{
		OK:  true,
		Slots: []string{"aebdemo0000000000", "amdemo0000000000-d", makeToken(t, "aecfs5n2y4aaaakd")},
	}
	id := r.Identity("aecglf6ee4aaaarz-g-a50")
	if id.UserID != "aebdemo0000000000" || id.DeviceID != "amdemo0000000000-d" {
		t.Fatalf("槽位映射错了: %+v", id)
	}
	if id.Platform {
		t.Fatal("游戏身份凭证不该标成平台")
	}
	if id.Token == "" {
		t.Fatal("token 没取到")
	}
}
