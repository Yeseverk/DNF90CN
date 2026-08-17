package dnfbridge

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const packetLogHexLimit = 512

type packetLogger struct {
	mu   sync.Mutex
	file *os.File
}

func openPacketLogger(path string) (*packetLogger, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640) //nolint:gosec // G304：路径来自部署环境变量，只用于本地包日志。
	if err != nil {
		return nil, err
	}
	logger := &packetLogger{file: file}
	logger.writeLine("=== LongHeng Go dnfbridge packet log started " + time.Now().UTC().Format(time.RFC3339) + " ===")
	return logger, nil
}

func (l *packetLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *packetLogger) Log(direction string, kind string, data []byte, fields ...any) {
	if l == nil || l.file == nil {
		return
	}
	hexData := data
	truncated := ""
	if len(hexData) > packetLogHexLimit {
		hexData = hexData[:packetLogHexLimit]
		truncated = " truncated=true"
	}
	line := fmt.Sprintf("%s kind=%s", direction, kind)
	for i := 0; i+1 < len(fields); i += 2 {
		line += fmt.Sprintf(" %v=%v", fields[i], fields[i+1])
	}
	line += fmt.Sprintf(" len=%d%s hex=%s", len(data), truncated, hex.EncodeToString(hexData))
	l.writeLine(line)
}

func (l *packetLogger) Event(kind string, fields ...any) {
	if l == nil || l.file == nil {
		return
	}
	line := "EVENT kind=" + kind
	for i := 0; i+1 < len(fields); i += 2 {
		line += fmt.Sprintf(" %v=%v", fields[i], fields[i+1])
	}
	l.writeLine(line)
}

func (l *packetLogger) writeLine(line string) {
	if l == nil || l.file == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.file.WriteString(line + "\n")
}
