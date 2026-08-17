package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/tracing"
)

var ErrActionRequired = errors.New("audit action is required")

type Event struct {
	Time           time.Time         `json:"time"`
	Service        string            `json:"service"`
	NodeID         string            `json:"node_id"`
	Actor          string            `json:"actor,omitempty"`
	Action         string            `json:"action"`
	Target         string            `json:"target,omitempty"`
	TraceID        string            `json:"trace_id,omitempty"`
	RequestID      string            `json:"request_id,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	ReasonCode     string            `json:"reason_code,omitempty"`
	Fields         map[string]string `json:"fields,omitempty"`
}

type Sink interface {
	Write(context.Context, Event) error
	Close() error
	Snapshot() []Event
}

type Logger struct {
	service string
	nodeID  string
	sink    Sink
}

func NewLogger(service, nodeID string, sink Sink) *Logger {
	return &Logger{
		service: strings.TrimSpace(service),
		nodeID:  strings.TrimSpace(nodeID),
		sink:    sink,
	}
}

func (l *Logger) Record(ctx context.Context, event Event) error {
	if l == nil || l.sink == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	event.Action = strings.TrimSpace(event.Action)
	if event.Action == "" {
		return ErrActionRequired
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if event.Service == "" {
		event.Service = l.service
	}
	if event.NodeID == "" {
		event.NodeID = l.nodeID
	}
	if event.TraceID == "" {
		if span, ok := tracing.SpanFromContext(ctx); ok {
			event.TraceID = span.TraceID
		}
	}
	event = cloneEvent(event)
	return l.sink.Write(ctx, event)
}

func (l *Logger) Snapshot() []Event {
	if l == nil || l.sink == nil {
		return nil
	}
	return l.sink.Snapshot()
}

func (l *Logger) Close() error {
	if l == nil || l.sink == nil {
		return nil
	}
	return l.sink.Close()
}

type MemorySink struct {
	limit int

	mu     sync.Mutex
	events []Event
}

func NewMemory(limit int) *MemorySink {
	if limit <= 0 {
		limit = 256
	}
	return &MemorySink{limit: limit, events: make([]Event, 0, limit)}
}

func (s *MemorySink) Write(ctx context.Context, event Event) error {
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
	s.mu.Lock()
	s.events = append(s.events, event)
	if len(s.events) > s.limit {
		s.events = append([]Event(nil), s.events[len(s.events)-s.limit:]...)
	}
	s.mu.Unlock()
	return nil
}

func (s *MemorySink) Close() error {
	return nil
}

func (s *MemorySink) Snapshot() []Event {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	out := cloneEvents(s.events)
	s.mu.Unlock()
	return out
}

type FileSink struct {
	memory *MemorySink

	mu         sync.Mutex
	path       string
	file       *os.File
	writer     *bufio.Writer
	syncEvery  int
	writes     int
	bytes      int64
	maxBytes   int64
	maxAge     time.Duration
	maxBackups int
	openedAt   time.Time
	now        func() time.Time
}

type FileOptions struct {
	MemoryLimit int
	SyncEvery   int
	MaxBytes    int64
	MaxAge      time.Duration
	MaxBackups  int
	Now         func() time.Time
}

func NewFile(path string, memoryLimit int) (*FileSink, error) {
	return NewFileWithOptions(path, FileOptions{MemoryLimit: memoryLimit})
}

func NewFileWithOptions(path string, options FileOptions) (*FileSink, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("audit file path is required")
	}
	if options.SyncEvery <= 0 {
		options.SyncEvery = 16
	}
	if options.MaxBackups <= 0 {
		options.MaxBackups = 3
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create audit directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- 审计日志路径来自运维配置，不来自外部请求。
	if err != nil {
		return nil, fmt.Errorf("open audit file %q: %w", path, err)
	}
	var size int64
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	return &FileSink{
		memory:     NewMemory(options.MemoryLimit),
		path:       path,
		file:       file,
		writer:     bufio.NewWriter(file),
		syncEvery:  options.SyncEvery,
		bytes:      size,
		maxBytes:   options.MaxBytes,
		maxAge:     options.MaxAge,
		maxBackups: options.MaxBackups,
		openedAt:   now(),
		now:        now,
	}, nil
}

func (s *FileSink) Write(ctx context.Context, event Event) error {
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
		return fmt.Errorf("encode audit event: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil || s.writer == nil {
		return fmt.Errorf("audit file sink is closed")
	}
	if err := s.rotateIfNeededLocked(int64(len(line) + 1)); err != nil {
		return err
	}
	if _, err := s.writer.Write(line); err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	if err := s.writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("write audit newline: %w", err)
	}
	s.bytes += int64(len(line) + 1)
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("flush audit event: %w", err)
	}
	s.writes++
	if s.syncEvery <= 1 || s.writes%s.syncEvery == 0 {
		if err := s.file.Sync(); err != nil {
			return fmt.Errorf("sync audit event: %w", err)
		}
	}
	return s.memory.Write(ctx, event)
}

func (s *FileSink) rotateIfNeededLocked(nextBytes int64) error {
	if s.path == "" || s.file == nil || s.writer == nil {
		return nil
	}
	ageExceeded := s.maxAge > 0 && !s.openedAt.IsZero() && s.now().Sub(s.openedAt) >= s.maxAge
	sizeExceeded := s.maxBytes > 0 && s.bytes > 0 && s.bytes+nextBytes > s.maxBytes
	if !ageExceeded && !sizeExceeded {
		return nil
	}
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("flush audit before rotate: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("sync audit before rotate: %w", err)
	}
	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close audit before rotate: %w", err)
	}
	if err := rotateAuditBackups(s.path, s.maxBackups); err != nil {
		s.reopenRotateFail()
		return err
	}
	rotated := s.path + ".1"
	if err := os.Rename(s.path, rotated); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.reopenRotateFail()
		return fmt.Errorf("rotate audit file: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		s.reopenRotateFail()
		return fmt.Errorf("open rotated audit file: %w", err)
	}
	s.file = file
	s.writer = bufio.NewWriter(file)
	s.bytes = 0
	s.openedAt = s.now()
	return nil
}

func (s *FileSink) reopenRotateFail() {
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		s.file = nil
		s.writer = nil
		return
	}
	s.file = file
	s.writer = bufio.NewWriter(file)
	if info, statErr := file.Stat(); statErr == nil {
		s.bytes = info.Size()
	}
	s.openedAt = s.now()
}

func rotateAuditBackups(path string, maxBackups int) error {
	if maxBackups <= 0 {
		maxBackups = 3
	}
	oldest := fmt.Sprintf("%s.%d", path, maxBackups)
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old audit backup: %w", err)
	}
	for idx := maxBackups - 1; idx >= 1; idx-- {
		from := fmt.Sprintf("%s.%d", path, idx)
		to := fmt.Sprintf("%s.%d", path, idx+1)
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rotate audit backup: %w", err)
		}
	}
	return nil
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

func (s *FileSink) Snapshot() []Event {
	if s == nil || s.memory == nil {
		return nil
	}
	return s.memory.Snapshot()
}

func cloneEvents(events []Event) []Event {
	if len(events) == 0 {
		return nil
	}
	out := make([]Event, len(events))
	for idx, event := range events {
		out[idx] = cloneEvent(event)
	}
	return out
}

func cloneEvent(event Event) Event {
	event.Actor = strings.TrimSpace(event.Actor)
	event.Action = strings.TrimSpace(event.Action)
	event.Target = strings.TrimSpace(event.Target)
	event.TraceID = strings.TrimSpace(event.TraceID)
	event.RequestID = strings.TrimSpace(event.RequestID)
	event.IdempotencyKey = strings.TrimSpace(event.IdempotencyKey)
	event.ReasonCode = strings.TrimSpace(event.ReasonCode)
	event.Fields = cloneFields(event.Fields)
	return event
}

func cloneFields(fields map[string]string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		out[key] = value
	}
	return out
}
