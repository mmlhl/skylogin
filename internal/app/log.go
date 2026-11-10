package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var logMu sync.Mutex

// logMaxBytes 单个日志文件的上限（2 MB）。
//
// 超过就归档成 log.old.txt，且只保留一份：日志里有账号名（邮箱）这类隐私，
// 不该无限累积。本机实测 886 KB 的日志里就有 1747 处邮箱明文。
const logMaxBytes = 2 << 20

// maintainLogs 在程序启动时调一次：清理老版本的明文日志，必要时滚动。
//
// 刻意不放在 logf 里：logf 在测试中也会被调用，而测试不该去动用户真实
// 数据目录里的日志。RunUI 是唯一的启动入口，放这里最干净。
func maintainLogs() {
	sanitizeLegacyLog()
	rotateLog()
}

// sanitizeLegacyLog 把老版本写下的明文日志就地脱敏一次。
//
// 为什么需要它：实时脱敏只对"这次运行新写的行"生效，而用户机器上早就
// 躺着一份旧日志（本机 886 KB 里有 1747 处邮箱明文，反馈日志里还有 27 个
// 完整 token）。光靠轮转也不行——轮转只处理超过 2 MB 的，小文件永远不会被碰。
//
// 只做一次，用 log.sanitized 标记。之后靠 logf 实时脱敏 + rotateLog 兜底。
func sanitizeLegacyLog() {
	dir := dataDir()
	marker := filepath.Join(dir, "log.sanitized")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	cur := filepath.Join(dir, "log.txt")
	if b, err := os.ReadFile(cur); err == nil {
		lines := strings.Split(string(b), "\n")
		changed := false
		for i := range lines {
			if r := redactLogLine(lines[i]); r != lines[i] {
				lines[i] = r
				changed = true
			}
		}
		// 只有真的改动了才回写，避免无谓地重写整个文件
		if changed {
			writeFileAtomic(cur, []byte(strings.Join(lines, "\n")))
		}
	}
	// 标记放在最后：中途失败时下次启动会重来一遍
	_ = os.WriteFile(marker, []byte("1"), 0o600)
}

// writeFileAtomic 先写临时文件再改名，避免中途失败把原文件截断。
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// rotateLog 在进程内第一次写日志时跑一次：文件超限就滚动。
//
// 归档前会把内容重新脱敏——老版本写下的日志是明文 token / 邮箱，
// 光滚动不清理，明文照样躺在磁盘上。
func rotateLog() {
	dir := dataDir()
	cur := filepath.Join(dir, "log.txt")
	st, err := os.Stat(cur)
	if err != nil || st.Size() < logMaxBytes {
		return
	}
	b, err := os.ReadFile(cur)
	if err != nil {
		return
	}
	lines := strings.Split(string(b), "\n")
	for i := range lines {
		lines[i] = redactLogLine(lines[i])
	}
	old := filepath.Join(dir, "log.old.txt")
	// 只留一份归档，否则归档本身会越堆越多，等于没限制
	_ = os.Remove(old)
	if err := os.WriteFile(old, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		return
	}
	// 原文件清掉，从头开始写；失败也不影响归档已经落盘
	_ = os.Remove(cur)
}

// logf 同时写控制台与日志文件，便于用户回报问题。
//
// 写出去之前统一脱敏：日志会被用户贴到群里，里面的 token 是能直接签票的
// 账号级凭证（2026-11-10 那份反馈日志里完整出现过 27 次）。见 redact.go。
func logf(format string, args ...any) {

	line := redactLogLine(fmt.Sprintf(format, args...))
	fmt.Println(line)

	logMu.Lock()
	defer logMu.Unlock()
	f, err := os.OpenFile(filepath.Join(dataDir(), "log.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + "  " + line + "\n")
}
