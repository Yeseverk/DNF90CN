package logiclog

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Event struct {
	EventID    string         `json:"event_id,omitempty"`
	AccountID  string         `json:"account_id,omitempty"`
	RoleID     string         `json:"role_id,omitempty"`
	ServerID   string         `json:"server_id,omitempty"`
	ReasonCode string         `json:"reason_code"`
	Action     string         `json:"action"`
	Fields     map[string]any `json:"fields,omitempty"`
	TraceID    string         `json:"trace_id,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Sink interface {
	WriteLogicLog(context.Context, Event) error
}

type eventSnapshotter interface {
	Events() []Event
}

type sinkCloser interface {
	Close() error
}

type Logger struct {
	catalog *Catalog
	sink    Sink
	now     func() time.Time
}

func NewLogger(catalog *Catalog, sink Sink) *Logger {
	return NewLoggerWithClock(catalog, sink, time.Now)
}

func NewLoggerWithClock(catalog *Catalog, sink Sink, now func() time.Time) *Logger {
	if now == nil {
		now = time.Now
	}
	return &Logger{catalog: catalog, sink: sink, now: now}
}

func (l *Logger) Write(ctx context.Context, event Event) error {
	if l == nil || l.sink == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if event.Action == "" {
		return fmt.Errorf("logiclog action is required")
	}
	if err := l.catalog.MustAllow(event.ReasonCode); err != nil {
		return err
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = l.now().UTC()
	}
	event.Fields = cloneFields(event.Fields)
	return l.sink.WriteLogicLog(ctx, event)
}

func (l *Logger) Events() []Event {
	if l == nil || l.sink == nil {
		return nil
	}
	snapshotter, ok := l.sink.(eventSnapshotter)
	if !ok {
		return nil
	}
	return snapshotter.Events()
}

func (l *Logger) Close() error {
	if l == nil || l.sink == nil {
		return nil
	}
	closer, ok := l.sink.(sinkCloser)
	if !ok {
		return nil
	}
	return closer.Close()
}

type MemorySink struct {
	limit int

	mu     sync.Mutex
	events []Event
}

func NewMemorySink() *MemorySink {
	return &MemorySink{}
}

func NewBoundedMemorySink(limit int) *MemorySink {
	if limit <= 0 {
		limit = 256
	}
	return &MemorySink{limit: limit, events: make([]Event, 0, limit)}
}

func (s *MemorySink) WriteLogicLog(ctx context.Context, event Event) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.events = append(s.events, cloneEvent(event))
	if s.limit > 0 && len(s.events) > s.limit {
		s.events = append([]Event(nil), s.events[len(s.events)-s.limit:]...)
	}
	s.mu.Unlock()
	return nil
}

func (s *MemorySink) Events() []Event {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	for i, event := range s.events {
		out[i] = cloneEvent(event)
	}
	return out
}

func cloneEvent(event Event) Event {
	event.Fields = cloneFields(event.Fields)
	return event
}

func cloneFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out
}
