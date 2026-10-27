package channel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 注入脚本：刷新指定小号时脚本里必须带 target；包名必须是光遇 vivo 包；
// 结果统一走 uid + secret schema。
func TestVivoLoginScriptTarget(t *testing.T) {
	withTarget := VivoLoginScript("900001")
	if !strings.Contains(withTarget, `"900001"`) {
		t.Fatal("脚本应嵌入目标小号 id")
	}
	if !strings.Contains(withTarget, VivoGamePackage) {
		t.Fatalf("脚本应包含光遇 vivo 包名 %q", VivoGamePackage)
	}
	if strings.Contains(withTarget, "TARGET = 900001") {
		t.Fatal("target 必须是 JS 字符串而不是裸数字拼接（防注入）")
	}
	empty := VivoLoginScript("")
	if !strings.Contains(empty, `TARGET = ""`) {
		t.Fatal("无目标时 TARGET 应为空字符串")
	}
	if !strings.Contains(withTarget, "secret:") {
		t.Fatal("脚本应回传统一 schema 的 secret 字段")
	}
}

// passport 登录页地址必须指向 vivo 通行证并回跳 joint。
func TestVivoPassportURL(t *testing.T) {
	if !strings.HasPrefix(VivoPassportURL, "https://passport.vivo.com.cn/") {
		t.Fatalf("登录页地址不对: %q", VivoPassportURL)
	}
	if !strings.Contains(VivoPassportURL, "joint.vivo.com.cn") {
		t.Fatal("登录页应回跳 joint.vivo.com.cn")
	}
}

// 渠道身份参数：渠道名必须与账号文件 / uni_sauth 用的取值一致。
func TestInfoOf(t *testing.T) {
	if InfoOf(KeyBili).Login != "bilibili_sdk" {
		t.Fatalf("B站渠道名错误: %q", InfoOf(KeyBili).Login)
	}
	if InfoOf(KeyM4399).Login != "4399com" {
		t.Fatalf("4399 渠道名错误: %q", InfoOf(KeyM4399).Login)
	}
	if InfoOf(KeyVivo).Login != "nearme_vivo" {
		t.Fatalf("vivo 渠道名错误: %q", InfoOf(KeyVivo).Login)
	}
	// 不认识的渠道回落华为默认值
	if InfoOf("whatever").Login != "huawei" {
		t.Fatalf("未知渠道应回落华为: %q", InfoOf("whatever").Login)
	}
}

// 生成注入后的真实脚本（含 target 替换），供 node --check 做 JS 语法验证：
// SKYLOGIN_DUMP_SCRIPT=输出目录 go test -run TestDumpScripts ./internal/channel/
// 三个渠道各写一份 <key>.js。
func TestDumpScripts(t *testing.T) {
	dir := os.Getenv("SKYLOGIN_DUMP_SCRIPT")
	if dir == "" {
		t.Skip("未设置 SKYLOGIN_DUMP_SCRIPT，跳过")
	}
	dumps := map[string]string{
		"bilibili": BiliLoginScript(),
		"m4399":    M4399LoginScript(),
		"vivo":     VivoLoginScript("900001"),
	}
	for key, script := range dumps {
		if strings.Contains(script, "Go 拼接") || strings.Contains(script, "jsonJSString") {
			t.Fatalf("%s 脚本不应残留 Go 拼接占位", key)
		}
		if err := os.WriteFile(filepath.Join(dir, key+".js"), []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
