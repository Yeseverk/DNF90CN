package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

// ShutdownDumpOptions 控制关停堆栈 dump 的目录、服务名和时间来源。
type ShutdownDumpOptions struct {
	Dir     string
	Service string
	Now     func() time.Time
}

// DumpGoroutineStack 把当前进程所有 goroutine 栈写入文件，用于关停超时排查。
func DumpGoroutineStack(options ShutdownDumpOptions) (string, error) {
	dir := strings.TrimSpace(options.Dir)
	if dir == "" {
		dir = "reports"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	service := sanitizeDumpPart(options.Service)
	if service == "" {
		service = "service"
	}
	filename := fmt.Sprintf("shutdown_%s_%s.stack", service, now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(dir, filename)
	data := goroutineStackDump()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func goroutineStackDump() []byte {
	size := 1 << 20
	for {
		buf := make([]byte, size)
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}
		size *= 2
	}
}

func sanitizeDumpPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return strings.Trim(b.String(), "_")
}
