package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var logMu sync.Mutex

// logf 同时写控制台与日志文件，便于用户回报问题。
func logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
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
