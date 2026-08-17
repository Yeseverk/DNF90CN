package profile

import (
	"context"
	"fmt"
	"time"
)

type RequestEvent struct {
	AccountID      string            `json:"account_id"`
	SessionID      string            `json:"session_id,omitempty"`
	Route          string            `json:"route,omitempty"`
	RequestID      string            `json:"request_id,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
}

type RequestResult struct {
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

type RequestHook interface {
	BeforeProfileRequest(context.Context, RequestEvent) context.Context
	AfterProfileRequest(context.Context, RequestEvent, RequestResult)
}

type RequestHooks []RequestHook

func (hooks RequestHooks) Run(ctx context.Context, event RequestEvent, handler func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if handler == nil {
		return fmt.Errorf("profile request handler is required")
	}
	if event.StartedAt.IsZero() {
		event.StartedAt = time.Now().UTC()
	} else {
		event.StartedAt = event.StartedAt.UTC()
	}
	event.Metadata = cloneRequestMetadata(event.Metadata)
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		next := hook.BeforeProfileRequest(ctx, event)
		if next != nil {
			ctx = next
		}
	}
	err := handler(ctx)
	result := RequestResult{
		OK:       err == nil,
		Duration: time.Since(event.StartedAt),
	}
	if err != nil {
		result.Error = err.Error()
	}
	for _, hook := range hooks {
		if hook != nil {
			hook.AfterProfileRequest(ctx, event, result)
		}
	}
	return err
}

func cloneRequestMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
