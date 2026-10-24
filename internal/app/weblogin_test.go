package app

// 测试：网页登录的会话目录必须按账号 id 区分，而不是账号名。
//
// 因为两个账号可以重名（比如都叫"光之子"），用名字做目录会把会话混在一起，
// 之后刷新凭证就可能拿到别人的登录态。

import (
	"strings"
	"testing"
)

func TestWebLoginProfileDirUsesUserID(t *testing.T) {
	// 注意：t.TempDir() 每次调用都会给新目录，必须先取一次存起来，
	// 否则"同一账号目录稳定"这条断言会因为基准目录变化而失败
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	a := webLoginProfileDir("aebdemo0000000000")
	b := webLoginProfileDir("aedemo0000000000")

	if a == b {
		t.Fatalf("不同账号应该有不同的会话目录，实际都是 %q", a)
	}
	if !strings.Contains(a, "aebdemo0000000000") {
		t.Fatalf("目录名应该包含账号 id，实际 %q", a)
	}
	if !strings.Contains(b, "aedemo0000000000") {
		t.Fatalf("目录名应该包含账号 id，实际 %q", b)
	}

	// 同一个 id 必须稳定，否则每次登录都会换目录、丢掉登录态
	if webLoginProfileDir("aebdemo0000000000") != a {
		t.Fatal("同一个账号 id 的目录必须稳定")
	}
}

func TestSanitizeDirNameKeepsSafeChars(t *testing.T) {
	// 账号 id 里出现路径分隔符这类字符时必须剥掉，避免目录穿越
	got := sanitizeDirName("../../evil/id")
	if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
		t.Fatalf("目录名里不该有路径成分，实际 %q", got)
	}
	if sanitizeDirName("") != "unknown" {
		t.Fatal("空 id 应该退化成一个固定值")
	}
	// 下划线和连字符是合法 id 字符，要保留
	if sanitizeDirName("a_b-c") != "a_b-c" {
		t.Fatalf("合法字符不该被过滤，实际 %q", sanitizeDirName("a_b-c"))
	}
}
