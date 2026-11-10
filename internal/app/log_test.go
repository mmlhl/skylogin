package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 日志轮转：超限归档，且归档时要把老版本写下的明文凭证一起清掉。

func withTempDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := dataDirFn
	dataDirFn = func() string { return dir }
	t.Cleanup(func() { dataDirFn = old })
	return dir
}

func TestRotateLogArchivesAndRedacts(t *testing.T) {
	dir := withTempDataDir(t)

	// 造一份超过上限的日志，里面带明文 token（形态与真实日志一致）
	line := "查账号显示名失败: Get \"https://x/users/y?app_channel=a50&token=1-FAKEtokenVALUE&updater_cv=c1.0.0\": dial tcp: no such host\n"
	big := strings.Repeat(line, logMaxBytes/len(line)+20)
	cur := filepath.Join(dir, "log.txt")
	if err := os.WriteFile(cur, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}

	rotateLog()

	if _, err := os.Stat(cur); !os.IsNotExist(err) {
		t.Fatal("超限的原日志应被清掉，从新文件开始写")
	}
	b, err := os.ReadFile(filepath.Join(dir, "log.old.txt"))
	if err != nil {
		t.Fatalf("归档没生成: %v", err)
	}
	if strings.Contains(string(b), "FAKEtokenVALUE") {
		t.Fatal("归档里还留着明文 token——老版本写下的日志必须一并脱敏")
	}
	if !strings.Contains(string(b), "token=1-FAKE…") {
		t.Fatal("归档应保留脱敏后的形态，排查时还要能看出是哪个 token")
	}
	// 诊断信息本身不能丢
	if !strings.Contains(string(b), "dial tcp: no such host") {
		t.Fatal("归档里的非敏感内容不该被改动")
	}
}

func TestRotateLogSkipsSmallFile(t *testing.T) {
	dir := withTempDataDir(t)
	cur := filepath.Join(dir, "log.txt")
	content := "光遇账号登录器 1.6.2+dev 启动\n"
	if err := os.WriteFile(cur, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	rotateLog()

	if b, err := os.ReadFile(cur); err != nil || string(b) != content {
		t.Fatal("没超限的日志不该被动")
	}
	if _, err := os.Stat(filepath.Join(dir, "log.old.txt")); !os.IsNotExist(err) {
		t.Fatal("没超限时不该生成归档")
	}
}

func TestRotateLogKeepsOnlyOneArchive(t *testing.T) {
	dir := withTempDataDir(t)

	old := filepath.Join(dir, "log.old.txt")
	if err := os.WriteFile(old, []byte("上一轮的归档\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x\n", logMaxBytes)
	if err := os.WriteFile(filepath.Join(dir, "log.txt"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}

	rotateLog()

	b, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "上一轮的归档") {
		t.Fatal("旧归档应被替换，只保留一份——否则归档自己会越堆越多")
	}
}

func TestRotateLogNoFile(t *testing.T) {
	withTempDataDir(t)
	// 没有日志文件时不该 panic
	rotateLog()
}

// 老版本日志的就地脱敏：每个用户机器上只做一次。

func TestSanitizeLegacyLog(t *testing.T) {
	dir := withTempDataDir(t)
	cur := filepath.Join(dir, "log.txt")
	legacy := "2026-11-10 14:37:15  网易账号 zhangsan@example.com：正在现签票据…\n" +
		"2026-11-10 14:25:29  查账号显示名失败: Get \"https://x?token=1-FAKEtokenVALUE&a=1\": no such host\n" +
		"2026-11-10 14:28:24  多开：一号 凭证已失效，跳过\n"
	if err := os.WriteFile(cur, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	sanitizeLegacyLog()

	b, err := os.ReadFile(cur)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "zhangsan@example.com") {
		t.Fatal("旧日志里的邮箱没被清掉")
	}
	if strings.Contains(s, "FAKEtokenVALUE") {
		t.Fatal("旧日志里的 token 没被清掉")
	}
	if !strings.Contains(s, "z***@example.com") || !strings.Contains(s, "token=1-FAKE…") {
		t.Fatalf("应保留脱敏后的形态，实际: %s", s)
	}
	// 普通行必须原样保留，否则日志就没法用来排查了
	if !strings.Contains(s, "多开：一号 凭证已失效，跳过") {
		t.Fatal("非敏感内容不该被改动")
	}
	if _, err := os.Stat(filepath.Join(dir, "log.sanitized")); err != nil {
		t.Fatal("应留下已处理标记，避免每次启动都重扫")
	}
}

func TestSanitizeLegacyLogOnlyOnce(t *testing.T) {
	dir := withTempDataDir(t)
	cur := filepath.Join(dir, "log.txt")

	// 第一次：带明文
	if err := os.WriteFile(cur, []byte("token=1-FAKEfirstVALUE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sanitizeLegacyLog()

	// 之后用户又手工放了一份旧日志进来，也不该再处理（标记已存在）
	if err := os.WriteFile(cur, []byte("token=1-FAKEsecondVALUE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sanitizeLegacyLog()

	b, _ := os.ReadFile(cur)
	if !strings.Contains(string(b), "FAKEsecondVALUE") {
		t.Fatal("有标记时不该再动日志（每次启动重扫 2 MB 是浪费）")
	}
}

func TestSanitizeLegacyLogNoFile(t *testing.T) {
	withTempDataDir(t)
	// 没有日志文件时不该 panic，且应留下标记
	sanitizeLegacyLog()
}
