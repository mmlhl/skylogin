package app

// 版本号与构建号的解析规则。
//
// 这里出过一次线上问题：CI 注入的构建号被 vcs 信息覆盖成 "dev-xxxx"，
// 更新检查把它当成开发版（build=0）直接跳过，用户装了新版也收不到提示。

import (
	"runtime/debug"
	"testing"
)

// buildInfoWithVCS 造一份带 vcs 信息的构建描述，模拟"在 git 仓库里构建"。
func buildInfoWithVCS() *debug.BuildInfo {
	return &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "3ae2e51f134db3ad14ee480372c84f3f86973fa3"},
			{Key: "vcs.time", Value: "2026-11-07T20:33:13Z"},
		},
	}
}

// TestInjectedBuildSurvivesVCS 发布构建的构建号不能被 vcs 覆盖。
func TestInjectedBuildSurvivesVCS(t *testing.T) {
	_, build, _ := applyBuildInfo("1.4.0", "21", "", buildInfoWithVCS())
	if build != "21" {
		t.Fatalf("注入的构建号被改成了 %q，更新检查会因此失效", build)
	}
	// 注入的构建时间同样优先
	_, _, bt := applyBuildInfo("1.4.0", "21", "2026-11-08T00:00Z", buildInfoWithVCS())
	if bt != "2026-11-08T00:00Z" {
		t.Fatalf("注入的构建时间被改成了 %q", bt)
	}
}

// TestLocalBuildFallsBackToVCS 本地构建没有注入，才用提交哈希兜底。
func TestLocalBuildFallsBackToVCS(t *testing.T) {
	version, build, bt := applyBuildInfo("1.0.0", "0", "", buildInfoWithVCS())
	if build != "dev-3ae2e51" {
		t.Fatalf("本地构建的兜底值不对: %q", build)
	}
	if bt != "2026-11-07T20:33:13Z" {
		t.Fatalf("构建时间没取到: %q", bt)
	}
	// 没有模块版本时不改版本号
	if version != "1.0.0" {
		t.Fatalf("版本号不该变: %q", version)
	}
}

// TestBuildNumber 构建号只有纯数字才算有效（更新检查靠它比较）。
func TestBuildNumber(t *testing.T) {
	old := Build
	defer func() { Build = old }()

	Build = "21"
	if n := buildNumber(); n != 21 {
		t.Fatalf("buildNumber() = %d，期望 21", n)
	}
	Build = "dev-3ae2e51"
	if n := buildNumber(); n != 0 {
		t.Fatalf("开发版构建号应为 0，实际 %d", n)
	}
}

// TestNormalizeVersion 本地构建的版本号要能给人看。
//
// Go 在 tag 之后构建时给的是伪版本 v1.6.2-0.<时间>-<哈希>，
// 直接显示又长又难读，而且 commit 在 Build 里已经有了。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.6.1", "1.6.1"},
		{"1.6.1", "1.6.1"},
		{"v1.6.2-0.20261110110731-f342dcfbcae5", "1.6.2+dev"},
		{"v0.0.0-20261110110731-f342dcfbcae5", "dev"},
		{"v1.6.2-0.20261110110731-f342dcfbcae5+dirty", "1.6.2+dev.dirty"},
		{"v0.0.0-20261110110731-f342dcfbcae5+dirty", "dev.dirty"},
		{"v1.6.1+dirty", "1.6.1+dirty"},
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
