package app

// 日志里的构建标识。
//
// 2026-11-10 的反馈日志里，同一台机器上先后跑了 1.6.0、1.1.1、1.6.1 三个版本，
// 其中有的判断对、有的判错，光看版本号分不清是"版本混淆"还是"代码退化"。
// 把 commit 一起打进日志就没这个歧义了。
//
// commit 由 tools.go 的 applyBuildInfo 填进 Build（本地构建是 dev-<短哈希>，
// 发布构建是 CI 注入的构建号），这里只负责拼成一行、并把时间转成本地时区。

import (
	"strings"
	"time"
)

// buildStamp 返回日志用的构建标识，形如"（dev-f342dcf，2026-11-10 19:07）"。
// 取不到任何信息时返回空串，日志保持原样。
func buildStamp() string {
	var parts []string
	if Build != "" && Build != "0" {
		parts = append(parts, Build)
	}
	if t := localBuildTime(); t != "" {
		parts = append(parts, t)
	}
	if len(parts) == 0 {
		return ""
	}
	return "（" + strings.Join(parts, "，") + "）"
}

// localBuildTime 把 BuildTime 转成本地时间。
//
// BuildTime 来自 vcs.time，是 UTC（形如 2026-11-10T11:04:03Z）。
// 直接打出来会让人以为是早上 11 点，实际是北京时间 19:04。
func localBuildTime() string {
	if BuildTime == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, BuildTime); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return BuildTime
}
