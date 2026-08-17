package notification

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrNotifyTargetReq   = errors.New("notification target is required")
	ErrNotifyBodyReq     = errors.New("notification body is required")
	ErrNotifyProviderReq = errors.New("notification provider is required")
)

type PushRequest struct {
	TargetID       string            `json:"target_id"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Title          string            `json:"title,omitempty"`
	Body           string            `json:"body"`
	Data           map[string]string `json:"data,omitempty"`
	At             time.Time         `json:"at,omitempty"`
}

type MailAddress struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

type MailRequest struct {
	To             []MailAddress     `json:"to"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Subject        string            `json:"subject"`
	Body           string            `json:"body"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type Receipt struct {
	Provider  string    `json:"provider,omitempty"`
	MessageID string    `json:"message_id,omitempty"`
	Accepted  bool      `json:"accepted"`
	At        time.Time `json:"at,omitempty"`
}

// PushProvider 必须可并发调用；Accepted=false 表示供应商明确拒收，不是降级成功。
type PushProvider interface {
	SendPush(context.Context, PushRequest) (Receipt, error)
}

// MailProvider 与 PushProvider 共享语义：可重试错误必须通过 error 暴露给调用方。
type MailProvider interface {
	SendMail(context.Context, MailRequest) (Receipt, error)
}

type PushProviderFunc func(context.Context, PushRequest) (Receipt, error)

func (f PushProviderFunc) SendPush(ctx context.Context, request PushRequest) (Receipt, error) {
	if f == nil {
		return Receipt{}, ErrNotifyProviderReq
	}
	return f(ctx, request)
}

type MailProviderFunc func(context.Context, MailRequest) (Receipt, error)

func (f MailProviderFunc) SendMail(ctx context.Context, request MailRequest) (Receipt, error) {
	if f == nil {
		return Receipt{}, ErrNotifyProviderReq
	}
	return f(ctx, request)
}

func SendPush(ctx context.Context, provider PushProvider, request PushRequest) (Receipt, error) {
	if provider == nil {
		return Receipt{}, ErrNotifyProviderReq
	}
	if strings.TrimSpace(request.TargetID) == "" {
		return Receipt{}, ErrNotifyTargetReq
	}
	if strings.TrimSpace(request.Body) == "" {
		return Receipt{}, ErrNotifyBodyReq
	}
	return provider.SendPush(ctx, request)
}

func SendMail(ctx context.Context, provider MailProvider, request MailRequest) (Receipt, error) {
	if provider == nil {
		return Receipt{}, ErrNotifyProviderReq
	}
	if len(request.To) == 0 {
		return Receipt{}, ErrNotifyTargetReq
	}
	for _, to := range request.To {
		if strings.TrimSpace(to.Address) == "" {
			return Receipt{}, ErrNotifyTargetReq
		}
	}
	if strings.TrimSpace(request.Subject) == "" || strings.TrimSpace(request.Body) == "" {
		return Receipt{}, ErrNotifyBodyReq
	}
	return provider.SendMail(ctx, request)
}
