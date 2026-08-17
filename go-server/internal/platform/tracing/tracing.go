package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type contextKey struct{}

type Span struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Error        string            `json:"error,omitempty"`
	StartedAt    time.Time         `json:"started_at"`
	EndedAt      time.Time         `json:"ended_at"`
	DurationMS   int64             `json:"duration_ms"`
}

type Tracer struct {
	enabled bool
	max     int
	logger  *slog.Logger

	mu    sync.RWMutex
	spans []Span
}

type Exporter interface {
	ExportSpan(context.Context, Span)
}

type exporterHolder struct {
	exporter Exporter
}

const (
	CarrierTraceID     = "trace_id"
	CarrierSpanID      = "span_id"
	CarrierTraceParent = "traceparent"
)

var globalExporter atomic.Value

func init() {
	globalExporter.Store(exporterHolder{})
}

func New(enabled bool, maxSpans int, logger *slog.Logger) *Tracer {
	if maxSpans <= 0 {
		maxSpans = 256
	}
	return &Tracer{
		enabled: enabled,
		max:     maxSpans,
		logger:  logger,
		spans:   make([]Span, 0, maxSpans),
	}
}

func SetExporter(exporter Exporter) {
	globalExporter.Store(exporterHolder{exporter: exporter})
}

func (t *Tracer) Enabled() bool {
	if t == nil {
		return false
	}
	t.mu.RLock()
	enabled := t.enabled
	t.mu.RUnlock()
	return enabled
}

func (t *Tracer) Configure(enabled bool, maxSpans int) {
	if t == nil {
		return
	}
	if maxSpans <= 0 {
		maxSpans = 256
	}
	t.mu.Lock()
	t.enabled = enabled
	t.max = maxSpans
	if len(t.spans) > t.max {
		t.spans = append([]Span(nil), t.spans[len(t.spans)-t.max:]...)
	}
	t.mu.Unlock()
}

func (t *Tracer) Start(ctx context.Context, name string, attrs map[string]string) (context.Context, func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !t.Enabled() {
		return ctx, func(error) {}
	}
	parent, _ := SpanFromContext(ctx)
	span := Span{
		TraceID:    parent.TraceID,
		SpanID:     randomID(8),
		Name:       name,
		Attributes: cloneAttrs(attrs),
		StartedAt:  time.Now().UTC(),
	}
	if span.TraceID == "" {
		span.TraceID = randomID(16)
	}
	if parent.SpanID != "" {
		span.ParentSpanID = parent.SpanID
	}
	ctx = context.WithValue(ctx, contextKey{}, span)
	var once sync.Once
	return ctx, func(err error) {
		once.Do(func() {
			if err != nil {
				span.Error = err.Error()
			}
			span.EndedAt = time.Now().UTC()
			span.DurationMS = span.EndedAt.Sub(span.StartedAt).Milliseconds()
			t.record(ctx, span)
		})
	}
}

func (t *Tracer) Snapshot() []Span {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	out := append([]Span(nil), t.spans...)
	t.mu.RUnlock()
	return out
}

func (t *Tracer) record(ctx context.Context, span Span) {
	t.mu.Lock()
	t.spans = append(t.spans, span)
	if len(t.spans) > t.max {
		t.spans = append([]Span(nil), t.spans[len(t.spans)-t.max:]...)
	}
	t.mu.Unlock()
	if t.logger != nil && span.Error != "" {
		t.logger.Warn("trace span recorded error", "name", span.Name, "trace_id", span.TraceID, "span_id", span.SpanID, "error", span.Error)
	}
	if exporter := currentExporter(); exporter != nil {
		exporter.ExportSpan(ctx, span)
	}
}

func SpanFromContext(ctx context.Context) (Span, bool) {
	if ctx == nil {
		return Span{}, false
	}
	span, ok := ctx.Value(contextKey{}).(Span)
	return span, ok
}

func ContextWithSpan(ctx context.Context, span Span) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if span.TraceID == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, span)
}

func InjectCarrier(ctx context.Context, carrier map[string]string) map[string]string {
	span, ok := SpanFromContext(ctx)
	if !ok || span.TraceID == "" {
		return cloneAttrs(carrier)
	}
	out := cloneAttrs(carrier)
	if out == nil {
		out = make(map[string]string, 3)
	}
	out[CarrierTraceID] = span.TraceID
	if span.SpanID != "" {
		out[CarrierSpanID] = span.SpanID
		out[CarrierTraceParent] = "00-" + span.TraceID + "-" + span.SpanID + "-01"
	}
	return out
}

func ExtractCarrier(ctx context.Context, carrier map[string]string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	traceID := strings.TrimSpace(carrier[CarrierTraceID])
	spanID := strings.TrimSpace(carrier[CarrierSpanID])
	if traceID == "" || spanID == "" {
		traceID, spanID = parseTraceParent(carrier[CarrierTraceParent])
	}
	if traceID == "" {
		return ctx
	}
	return ContextWithSpan(ctx, Span{
		TraceID: traceID,
		SpanID:  spanID,
		Name:    "remote-parent",
	})
}

func cloneAttrs(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for key, value := range attrs {
		out[key] = value
	}
	return out
}

func currentExporter() Exporter {
	holder, _ := globalExporter.Load().(exporterHolder)
	return holder.exporter
}

func randomID(bytesLen int) string {
	if bytesLen <= 0 {
		bytesLen = 8
	}
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}

func parseTraceParent(value string) (string, string) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) < 4 {
		return "", ""
	}
	traceID := strings.TrimSpace(parts[1])
	spanID := strings.TrimSpace(parts[2])
	if len(traceID) != 32 || len(spanID) != 16 {
		return "", ""
	}
	return traceID, spanID
}
