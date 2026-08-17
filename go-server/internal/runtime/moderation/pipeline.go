package moderation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Evaluator interface {
	Evaluate(context.Context, Request) (Decision, error)
}

type ExternalProvider interface {
	EvaluateText(context.Context, Request, TextResult) (ProviderDecision, error)
}

type ProviderDecision struct {
	Rejected      bool          `json:"rejected,omitempty"`
	SanitizedText string        `json:"sanitized_text,omitempty"`
	Reasons       []string      `json:"reasons,omitempty"`
	Tags          []string      `json:"tags,omitempty"`
	CacheTTL      time.Duration `json:"-"`
}

type PipelineOptions struct {
	Engine        *Engine
	Provider      ExternalProvider
	CacheTTL      time.Duration
	FailOpen      bool
	ErrorReason   string
	RejectReason  string
	ProviderScope string
	Now           func() time.Time
}

type Pipeline struct {
	engine        *Engine
	provider      ExternalProvider
	cacheTTL      time.Duration
	failOpen      bool
	errorReason   string
	rejectReason  string
	providerScope string
	now           func() time.Time
	mu            sync.Mutex
	cache         map[string]cachedProvDecision
}

type cachedProvDecision struct {
	decision  ProviderDecision
	expiresAt time.Time
}

func NewPipeline(options PipelineOptions) *Pipeline {
	engine := options.Engine
	if engine == nil {
		engine = NewEngine(nil, nil)
	}
	errorReason := normalizeToken(options.ErrorReason)
	if errorReason == "" {
		errorReason = "external_unavailable"
	}
	rejectReason := normalizeToken(options.RejectReason)
	if rejectReason == "" {
		rejectReason = "external_rejected"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Pipeline{
		engine:        engine,
		provider:      options.Provider,
		cacheTTL:      options.CacheTTL,
		failOpen:      options.FailOpen,
		errorReason:   errorReason,
		rejectReason:  rejectReason,
		providerScope: normalizeToken(options.ProviderScope),
		now:           now,
		cache:         make(map[string]cachedProvDecision),
	}
}

func (p *Pipeline) Evaluate(ctx context.Context, request Request) (Decision, error) {
	if p == nil {
		return Decision{}, fmt.Errorf("%w: pipeline is nil", ErrInvalidRequest)
	}
	if p.engine == nil {
		return Decision{}, fmt.Errorf("%w: engine is required", ErrInvalidRequest)
	}
	decision, err := p.engine.Evaluate(ctx, request)
	if err != nil {
		return Decision{}, err
	}
	if p.provider == nil || !decision.Allowed || strings.TrimSpace(request.Text) == "" {
		return decision, nil
	}
	providerDecision, err := p.evaluateProvider(ctx, request, decision.Text)
	if err != nil {
		if p.failOpen {
			return decision, nil
		}
		decision.Allowed = false
		decision.Reasons = appendReasons(decision.Reasons, p.errorReason)
		return decision, nil
	}
	return applyProviderChoice(decision, providerDecision, p.rejectReason), nil
}

func (p *Pipeline) ClearCache() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.cache = make(map[string]cachedProvDecision)
	p.mu.Unlock()
}

func (p *Pipeline) Engine() *Engine {
	if p == nil {
		return nil
	}
	return p.engine
}

func (p *Pipeline) evaluateProvider(ctx context.Context, request Request, local TextResult) (ProviderDecision, error) {
	key := p.cacheKey(request)
	now := p.nowUTC()
	if key != "" {
		if decision, ok := p.cachedDecision(key, now); ok {
			return decision, nil
		}
	}
	decision, err := p.provider.EvaluateText(ctx, request, local)
	if err != nil {
		return ProviderDecision{}, err
	}
	decision.Reasons = normalizeList(decision.Reasons)
	decision.Tags = normalizeList(decision.Tags)
	if key != "" {
		p.storeCachedDecision(key, decision, now)
	}
	return cloneProviderChoice(decision), nil
}

func (p *Pipeline) cachedDecision(key string, now time.Time) (ProviderDecision, bool) {
	p.mu.Lock()
	item, ok := p.cache[key]
	if !ok {
		p.mu.Unlock()
		return ProviderDecision{}, false
	}
	if !item.expiresAt.IsZero() && !item.expiresAt.After(now) {
		delete(p.cache, key)
		p.mu.Unlock()
		return ProviderDecision{}, false
	}
	p.mu.Unlock()
	return cloneProviderChoice(item.decision), true
}

func (p *Pipeline) storeCachedDecision(key string, decision ProviderDecision, now time.Time) {
	ttl := decision.CacheTTL
	if ttl <= 0 {
		ttl = p.cacheTTL
	}
	if ttl <= 0 {
		return
	}
	item := cachedProvDecision{
		decision:  cloneProviderChoice(decision),
		expiresAt: now.Add(ttl),
	}
	p.mu.Lock()
	p.cache[key] = item
	p.mu.Unlock()
}

func (p *Pipeline) cacheKey(request Request) string {
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return ""
	}
	scope := normalizeToken(request.Scope)
	if p.providerScope != "" {
		scope = encodeModerationKey(p.providerScope, scope)
	}
	return encodeModerationKey(scope, text)
}

func (p *Pipeline) nowUTC() time.Time {
	if p == nil || p.now == nil {
		return time.Now().UTC()
	}
	return p.now().UTC()
}

func applyProviderChoice(decision Decision, provider ProviderDecision, rejectReason string) Decision {
	if provider.SanitizedText != "" {
		decision.SanitizedText = provider.SanitizedText
		decision.Text.Sanitized = provider.SanitizedText
	}
	if provider.Rejected {
		decision.Allowed = false
		reasons := provider.Reasons
		if len(reasons) == 0 {
			reasons = []string{rejectReason}
		}
		decision.Reasons = appendReasons(decision.Reasons, reasons...)
	}
	return decision
}

func appendReasons(existing []string, values ...string) []string {
	if len(values) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(values))
	out := make([]string, 0, len(existing)+len(values))
	for _, value := range existing {
		key := normalizeToken(value)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	for _, value := range values {
		key := normalizeToken(value)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func cloneProviderChoice(decision ProviderDecision) ProviderDecision {
	decision.Reasons = append([]string(nil), decision.Reasons...)
	decision.Tags = append([]string(nil), decision.Tags...)
	return decision
}
