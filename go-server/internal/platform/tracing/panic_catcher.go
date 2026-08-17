package tracing

import (
	"context"
	"fmt"
	"log/slog"
)

type PanicInfo struct {
	Operation  string            `json:"operation,omitempty"`
	Recovered  any               `json:"recovered,omitempty"`
	TraceID    string            `json:"trace_id,omitempty"`
	SpanID     string            `json:"span_id,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func CapturePanic(ctx context.Context, operation string, recovered any, attrs map[string]string) PanicInfo {
	info := PanicInfo{
		Operation:  operation,
		Recovered:  recovered,
		Attributes: cloneAttrs(attrs),
	}
	if span, ok := SpanFromContext(ctx); ok {
		info.TraceID = span.TraceID
		info.SpanID = span.SpanID
	}
	return info
}

func PanicError(ctx context.Context, operation string, recovered any, attrs map[string]string) error {
	info := CapturePanic(ctx, operation, recovered, attrs)
	if info.TraceID == "" {
		return fmt.Errorf("%s panic: %v", info.Operation, info.Recovered)
	}
	if info.SpanID == "" {
		return fmt.Errorf("%s panic trace_id=%s: %v", info.Operation, info.TraceID, info.Recovered)
	}
	return fmt.Errorf("%s panic trace_id=%s span_id=%s: %v", info.Operation, info.TraceID, info.SpanID, info.Recovered)
}

func LogPanic(ctx context.Context, logger *slog.Logger, operation string, recovered any, attrs map[string]string) PanicInfo {
	info := CapturePanic(ctx, operation, recovered, attrs)
	if logger == nil {
		return info
	}
	args := []any{"operation", info.Operation, "panic", info.Recovered}
	if info.TraceID != "" {
		args = append(args, "trace_id", info.TraceID)
	}
	if info.SpanID != "" {
		args = append(args, "span_id", info.SpanID)
	}
	for key, value := range info.Attributes {
		args = append(args, key, value)
	}
	logger.Error("panic recovered", args...)
	return info
}
