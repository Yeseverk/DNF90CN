package profile

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

type OfflineDelivery struct {
	AccountID      string            `json:"account_id"`
	Kind           string            `json:"kind"`
	Payload        []byte            `json:"payload,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

type OfflinePipeline interface {
	EnqueueOffline(context.Context, OfflineDelivery) error
	DrainOffline(context.Context, string, func(context.Context, OfflineDelivery) error) (int, error)
	SnapshotOffline(string) []OfflineDelivery
}

type MemoryOfflinePipeline struct {
	mu            sync.Mutex
	maxPerAccount int
	now           func() time.Time
	deliveries    map[string][]OfflineDelivery
}

func NewMemoryOfflinePipeline(maxPerAccount int, now func() time.Time) *MemoryOfflinePipeline {
	if maxPerAccount <= 0 {
		maxPerAccount = 256
	}
	if now == nil {
		now = time.Now
	}
	return &MemoryOfflinePipeline{
		maxPerAccount: maxPerAccount,
		now:           now,
		deliveries:    make(map[string][]OfflineDelivery),
	}
}

func (p *MemoryOfflinePipeline) EnqueueOffline(ctx context.Context, delivery OfflineDelivery) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	normalized, err := normOfflineDelivery(delivery, p.nowUTC())
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deliveries == nil {
		p.deliveries = make(map[string][]OfflineDelivery)
	}
	queue := slices.Clone(p.deliveries[normalized.AccountID])
	queue = append(queue, cloneOfflineDelivery(normalized))
	if len(queue) > p.maxPerAccount {
		queue = slices.Clone(queue[len(queue)-p.maxPerAccount:])
	}
	p.deliveries[normalized.AccountID] = queue
	return nil
}

func (p *MemoryOfflinePipeline) DrainOffline(ctx context.Context, accountID string, handler func(context.Context, OfflineDelivery) error) (int, error) {
	if err := ctxErr(ctx); err != nil {
		return 0, err
	}
	if handler == nil {
		return 0, fmt.Errorf("offline delivery handler is required")
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || p == nil {
		return 0, nil
	}
	p.mu.Lock()
	queue := cloneOfflineItems(p.deliveries[accountID])
	p.mu.Unlock()
	delivered := 0
	for idx, delivery := range queue {
		if err := ctxErr(ctx); err != nil {
			p.keepUndelivered(accountID, queue[idx:])
			return delivered, err
		}
		if err := handler(ctx, cloneOfflineDelivery(delivery)); err != nil {
			p.keepUndelivered(accountID, queue[idx:])
			return delivered, err
		}
		delivered++
	}
	p.keepUndelivered(accountID, nil)
	return delivered, nil
}

func (p *MemoryOfflinePipeline) SnapshotOffline(accountID string) []OfflineDelivery {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || p == nil {
		return nil
	}
	p.mu.Lock()
	out := cloneOfflineItems(p.deliveries[accountID])
	p.mu.Unlock()
	return out
}

func (p *MemoryOfflinePipeline) keepUndelivered(accountID string, remaining []OfflineDelivery) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(remaining) == 0 {
		delete(p.deliveries, accountID)
		return
	}
	p.deliveries[accountID] = cloneOfflineItems(remaining)
}

func (p *MemoryOfflinePipeline) nowUTC() time.Time {
	if p == nil || p.now == nil {
		return time.Now().UTC()
	}
	return p.now().UTC()
}

func normOfflineDelivery(delivery OfflineDelivery, now time.Time) (OfflineDelivery, error) {
	delivery.AccountID = strings.TrimSpace(delivery.AccountID)
	delivery.Kind = strings.TrimSpace(delivery.Kind)
	delivery.IdempotencyKey = strings.TrimSpace(delivery.IdempotencyKey)
	if delivery.AccountID == "" || delivery.Kind == "" {
		return OfflineDelivery{}, fmt.Errorf("offline delivery account_id and kind are required")
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	} else {
		delivery.CreatedAt = delivery.CreatedAt.UTC()
	}
	delivery.Payload = append([]byte(nil), delivery.Payload...)
	delivery.Metadata = cloneRequestMetadata(delivery.Metadata)
	return delivery, nil
}

func cloneOfflineDelivery(delivery OfflineDelivery) OfflineDelivery {
	delivery.Payload = append([]byte(nil), delivery.Payload...)
	delivery.Metadata = cloneRequestMetadata(delivery.Metadata)
	return delivery
}

func cloneOfflineItems(in []OfflineDelivery) []OfflineDelivery {
	if len(in) == 0 {
		return nil
	}
	out := make([]OfflineDelivery, len(in))
	for idx, delivery := range in {
		out[idx] = cloneOfflineDelivery(delivery)
	}
	return out
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
