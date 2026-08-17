package moderation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Request struct {
	Subject string    `json:"subject"`
	Scope   string    `json:"scope,omitempty"`
	Text    string    `json:"text,omitempty"`
	Now     time.Time `json:"now,omitempty"`
}

type Decision struct {
	Allowed       bool       `json:"allowed"`
	Subject       string     `json:"subject"`
	Scope         string     `json:"scope,omitempty"`
	OriginalText  string     `json:"original_text,omitempty"`
	SanitizedText string     `json:"sanitized_text,omitempty"`
	Text          TextResult `json:"text"`
	Sanctions     []Sanction `json:"sanctions,omitempty"`
	Reasons       []string   `json:"reasons,omitempty"`
}

type Engine struct {
	mu     sync.RWMutex
	filter *Filter
	store  SanctionStore
	now    func() time.Time
}

func NewEngine(filter *Filter, store SanctionStore) *Engine {
	if filter == nil {
		filter, _ = NewFilter()
	}
	if store == nil {
		store = NewMemorySanctionStore()
	}
	return &Engine{
		filter: filter,
		store:  store,
		now:    time.Now,
	}
}

func (e *Engine) SetNow(now func() time.Time) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.now = now
	e.mu.Unlock()
	if store, ok := e.store.(*MemorySanctionStore); ok {
		store.SetNow(now)
	}
}

func (e *Engine) Evaluate(ctx context.Context, request Request) (Decision, error) {
	if err := ctxErr(ctx); err != nil {
		return Decision{}, err
	}
	if e == nil {
		return Decision{}, fmt.Errorf("%w: engine is nil", ErrInvalidRequest)
	}
	subject := normalizeToken(request.Subject)
	if subject == "" {
		return Decision{}, fmt.Errorf("%w: subject is required", ErrInvalidRequest)
	}
	scope := normalizeToken(request.Scope)
	now := request.Now
	if now.IsZero() {
		now = e.nowUTC()
	} else {
		now = now.UTC()
	}

	decision := Decision{
		Allowed:       true,
		Subject:       subject,
		Scope:         scope,
		OriginalText:  request.Text,
		SanitizedText: request.Text,
		Text:          TextResult{Original: request.Text, Sanitized: request.Text},
	}

	if e.store != nil {
		sanctions, err := e.store.Active(ctx, SanctionQuery{Subject: subject, Scope: scope, Now: now})
		if err != nil {
			return Decision{}, err
		}
		decision.Sanctions = sanctions
		for _, sanction := range sanctions {
			switch sanction.Kind {
			case SanctionBan:
				decision.Allowed = false
				decision.Reasons = append(decision.Reasons, reasonForSanction(sanction))
			case SanctionMute:
				if strings.TrimSpace(request.Text) != "" {
					decision.Allowed = false
					decision.Reasons = append(decision.Reasons, reasonForSanction(sanction))
				}
			}
		}
	}

	if e.filter != nil && request.Text != "" {
		text := e.filter.Evaluate(scope, request.Text)
		decision.Text = text
		decision.SanitizedText = text.Sanitized
		if text.Rejected {
			decision.Allowed = false
			decision.Reasons = append(decision.Reasons, "text_rejected")
		}
	}
	return decision, nil
}

func (e *Engine) Filter() *Filter {
	if e == nil {
		return nil
	}
	return e.filter
}

func (e *Engine) Sanctions() SanctionStore {
	if e == nil {
		return nil
	}
	return e.store
}

func reasonForSanction(sanction Sanction) string {
	if sanction.Scope == "" {
		return string(sanction.Kind)
	}
	return string(sanction.Kind) + ":" + sanction.Scope
}

func (e *Engine) nowUTC() time.Time {
	if e == nil {
		return time.Now().UTC()
	}
	e.mu.RLock()
	now := e.now
	e.mu.RUnlock()
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}
