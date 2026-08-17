package logiclog

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type FileSink struct {
	memory *MemorySink

	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
}

func NewFileSink(path string, memoryLimit int) (*FileSink, error) {
	if path == "" {
		return nil, fmt.Errorf("logiclog file path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create logiclog directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- 逻辑日志路径来自运维配置，不来自外部请求。
	if err != nil {
		return nil, fmt.Errorf("open logiclog file %q: %w", path, err)
	}
	return &FileSink{
		memory: NewBoundedMemorySink(memoryLimit),
		file:   file,
		writer: bufio.NewWriter(file),
	}, nil
}

func (s *FileSink) WriteLogicLog(ctx context.Context, event Event) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	event = cloneEvent(event)
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode logiclog event: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil || s.writer == nil {
		return fmt.Errorf("logiclog file sink is closed")
	}
	if _, err := s.writer.Write(line); err != nil {
		return fmt.Errorf("write logiclog event: %w", err)
	}
	if err := s.writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("write logiclog newline: %w", err)
	}
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("flush logiclog event: %w", err)
	}
	// 逻辑日志用于业务流水和问题追踪，返回成功前需要推进到磁盘层。
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("sync logiclog event: %w", err)
	}
	if s.memory != nil {
		return s.memory.WriteLogicLog(ctx, event)
	}
	return nil
}

func (s *FileSink) Events() []Event {
	if s == nil || s.memory == nil {
		return nil
	}
	return s.memory.Events()
}

func (s *FileSink) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	if s.writer != nil {
		if err := s.writer.Flush(); err != nil {
			errs = append(errs, err)
		}
		s.writer = nil
	}
	if s.file != nil {
		if err := s.file.Sync(); err != nil {
			errs = append(errs, err)
		}
		if err := s.file.Close(); err != nil {
			errs = append(errs, err)
		}
		s.file = nil
	}
	return errors.Join(errs...)
}
