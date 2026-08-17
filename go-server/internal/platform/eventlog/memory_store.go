package eventlog

import (
	"context"
	"sync"
	"time"
)

// MemoryStoreMaxEvents 是内存事件日志默认保留的最大事件数。
const MemoryStoreMaxEvents = 10000

// MemoryStore 是用于本地开发、测试和轻量部署的内存 eventlog store。
type MemoryStore struct {
	name string

	mu          sync.Mutex
	events      map[string]Event
	order       []string
	idempotency map[string]string
	maxEvents   int
}

// NewMemoryStore 使用默认容量创建内存事件日志 store。
func NewMemoryStore(name string) *MemoryStore {
	return NewMemoryStoreWithLimit(name, MemoryStoreMaxEvents)
}

// NewMemoryStoreWithLimit 使用指定容量创建内存事件日志 store。
func NewMemoryStoreWithLimit(name string, maxEvents int) *MemoryStore {
	if maxEvents <= 0 {
		maxEvents = MemoryStoreMaxEvents
	}
	return &MemoryStore{
		name:        name,
		events:      make(map[string]Event),
		idempotency: make(map[string]string),
		maxEvents:   maxEvents,
	}
}

// Append 写入事件，并对相同幂等键的重复写入返回既有事件。
func (s *MemoryStore) Append(ctx context.Context, event Event) (Event, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, err
	}
	if s == nil {
		return Event{}, ErrStoreRequired
	}
	event = cloneEvent(event)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureReadyLocked()
	if event.IdempotencyKey != "" {
		if id, ok := s.idempotency[event.IdempotencyKey]; ok {
			existing, exists := s.events[id]
			if !exists {
				delete(s.idempotency, event.IdempotencyKey)
			} else {
				if !sameIdempotentEvent(existing, event) {
					return Event{}, ErrIdempotencyConflict
				}
				return cloneEvent(existing), nil
			}
		}
	}
	if _, exists := s.events[event.ID]; exists {
		return Event{}, ErrEventExists
	}
	s.events[event.ID] = event
	s.order = append(s.order, event.ID)
	if event.IdempotencyKey != "" {
		s.idempotency[event.IdempotencyKey] = event.ID
	}
	s.evictOverflowLocked()
	return cloneEvent(event), nil
}

func (s *MemoryStore) ensureReadyLocked() {
	if s.events == nil {
		s.events = make(map[string]Event)
	}
	if s.idempotency == nil {
		s.idempotency = make(map[string]string)
	}
	if s.maxEvents <= 0 {
		s.maxEvents = MemoryStoreMaxEvents
	}
}

func (s *MemoryStore) evictOverflowLocked() {
	for s.maxEvents > 0 && len(s.order) > s.maxEvents {
		id := s.order[0]
		s.order = s.order[1:]
		event, ok := s.events[id]
		if !ok {
			continue
		}
		delete(s.events, id)
		if event.IdempotencyKey != "" {
			delete(s.idempotency, event.IdempotencyKey)
		}
	}
}

// Get 按事件 ID 读取事件副本。
func (s *MemoryStore) Get(ctx context.Context, id string) (Event, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, false, err
	}
	if s == nil {
		return Event{}, false, ErrStoreRequired
	}
	s.mu.Lock()
	event, ok := s.events[id]
	s.mu.Unlock()
	if !ok {
		return Event{}, false, nil
	}
	return cloneEvent(event), true, nil
}

// GetByIdempotencyKey 按幂等键读取事件副本。
func (s *MemoryStore) GetByIdempotencyKey(ctx context.Context, key string) (Event, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, false, err
	}
	if s == nil {
		return Event{}, false, ErrStoreRequired
	}
	s.mu.Lock()
	id, ok := s.idempotency[key]
	if ok {
		event := cloneEvent(s.events[id])
		s.mu.Unlock()
		return event, true, nil
	}
	s.mu.Unlock()
	return Event{}, false, nil
}

// ListByStreamAggregate 按事件流和聚合 ID 倒序读取事件。
func (s *MemoryStore) ListByStreamAggregate(ctx context.Context, stream, aggregateID string, limit int) ([]Event, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreRequired
	}
	if limit <= 0 {
		limit = defaultPublishLimit
	}
	s.mu.Lock()
	out := make([]Event, 0, limit)
	for i := len(s.order) - 1; i >= 0 && len(out) < limit; i-- {
		event := s.events[s.order[i]]
		if event.Stream != stream || event.AggregateID != aggregateID {
			continue
		}
		out = append(out, cloneEvent(event))
	}
	s.mu.Unlock()
	return out, nil
}

// ClaimPending 领取一批到期事件用于发布。
func (s *MemoryStore) ClaimPending(ctx context.Context, limit int, now time.Time, claimTimeout time.Duration) ([]Event, error) {
	return s.claimPending(ctx, limit, now, claimTimeout, nil)
}

// ClaimPendingExcept 领取到期事件时排除指定事件流。
func (s *MemoryStore) ClaimPendingExcept(ctx context.Context, limit int, now time.Time, claimTimeout time.Duration, excludeStreams []string) ([]Event, error) {
	excluded := make(map[string]struct{}, len(excludeStreams))
	for _, stream := range normalizeStreamList(excludeStreams) {
		excluded[stream] = struct{}{}
	}
	return s.claimPending(ctx, limit, now, claimTimeout, excluded)
}

func (s *MemoryStore) claimPending(ctx context.Context, limit int, now time.Time, claimTimeout time.Duration, excluded map[string]struct{}) ([]Event, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreRequired
	}
	if limit <= 0 {
		limit = defaultPublishLimit
	}
	now = now.UTC()
	if claimTimeout <= 0 {
		claimTimeout = defaultClaimTimeout
	}
	claimUntil := now.Add(claimTimeout)
	// 内存 store 用 AvailableAt 临时保存 claim 截止时间；worker 更新状态时必须带回同一个 deadline。
	s.mu.Lock()
	out := make([]Event, 0, limit)
	for _, id := range s.order {
		event := s.events[id]
		if event.Status != StatusPending && event.Status != StatusFailed && event.Status != StatusProcessing {
			continue
		}
		if _, skip := excluded[event.Stream]; skip {
			continue
		}
		if event.AvailableAt.After(now) {
			continue
		}
		event.Status = StatusProcessing
		event.AvailableAt = claimUntil
		event.UpdatedAt = now
		s.events[id] = event
		out = append(out, cloneEvent(event))
		if len(out) >= limit {
			break
		}
	}
	s.mu.Unlock()
	return out, nil
}

// MarkPublished 将当前 claim 持有的事件推进为已发布。
func (s *MemoryStore) MarkPublished(ctx context.Context, id string, publishedAt time.Time, claimDeadline time.Time) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	s.mu.Lock()
	event, ok := s.events[id]
	if !ok {
		s.mu.Unlock()
		return ErrEventNotFound
	}
	if !ownsClaim(event, claimDeadline) {
		s.mu.Unlock()
		return ErrClaimLost
	}
	publishedAt = publishedAt.UTC()
	// Published 是终态，只有当前 claim 持有者能写入，避免过期 worker 覆盖新 worker 的结果。
	event.Status = StatusPublished
	event.PublishedAt = publishedAt
	event.UpdatedAt = publishedAt
	event.LastError = ""
	s.events[id] = event
	s.mu.Unlock()
	return nil
}

// MarkFailed 将当前 claim 持有的事件推进为失败并设置下次重试时间。
func (s *MemoryStore) MarkFailed(ctx context.Context, id string, message string, nextAttempt time.Time, updatedAt time.Time, claimDeadline time.Time) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	s.mu.Lock()
	event, ok := s.events[id]
	if !ok {
		s.mu.Unlock()
		return ErrEventNotFound
	}
	if !ownsClaim(event, claimDeadline) {
		s.mu.Unlock()
		return ErrClaimLost
	}
	// MarkFailed 会把 AvailableAt 改成下一次可重试时间；超过最大次数后由上层转 dead_letter。
	event.Status = StatusFailed
	event.Attempts++
	event.LastError = message
	event.AvailableAt = nextAttempt.UTC()
	event.UpdatedAt = updatedAt.UTC()
	s.events[id] = event
	s.mu.Unlock()
	return nil
}

// MarkDeadLetter 将当前 claim 持有的事件推进为死信。
func (s *MemoryStore) MarkDeadLetter(ctx context.Context, id string, message string, updatedAt time.Time, claimDeadline time.Time) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	s.mu.Lock()
	event, ok := s.events[id]
	if !ok {
		s.mu.Unlock()
		return ErrEventNotFound
	}
	if !ownsClaim(event, claimDeadline) {
		s.mu.Unlock()
		return ErrClaimLost
	}
	updatedAt = updatedAt.UTC()
	// DeadLetter 是人工介入边界，事件保留在 store 中，等待 requeue 或审计排查。
	event.Status = StatusDeadLetter
	event.Attempts++
	event.LastError = message
	event.AvailableAt = updatedAt
	event.UpdatedAt = updatedAt
	s.events[id] = event
	s.mu.Unlock()
	return nil
}

// ListDeadLetters 返回当前死信事件列表。
func (s *MemoryStore) ListDeadLetters(ctx context.Context, limit int) ([]Event, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreRequired
	}
	if limit < 0 {
		return nil, ErrInvalidLimit
	}
	if limit == 0 {
		limit = defDeadLetterLimit
	}
	s.mu.Lock()
	out := make([]Event, 0, limit)
	for _, id := range s.order {
		event := s.events[id]
		if event.Status != StatusDeadLetter {
			continue
		}
		out = append(out, cloneEvent(event))
		if len(out) >= limit {
			break
		}
	}
	s.mu.Unlock()
	return out, nil
}

// RequeueDeadLetter 把死信事件重新放回待发布队列。
func (s *MemoryStore) RequeueDeadLetter(ctx context.Context, id string, availableAt time.Time, updatedAt time.Time, preserveAttempts bool) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	s.mu.Lock()
	event, ok := s.events[id]
	if !ok || event.Status != StatusDeadLetter {
		s.mu.Unlock()
		return ErrEventNotFound
	}
	event.Status = StatusPending
	event.AvailableAt = availableAt.UTC()
	event.UpdatedAt = updatedAt.UTC()
	event.PublishedAt = time.Time{}
	event.LastError = ""
	if !preserveAttempts {
		event.Attempts = 0
	}
	s.events[id] = event
	s.mu.Unlock()
	return nil
}

// Snapshot 汇总内存 store 当前各状态数量和最早到期时间。
func (s *MemoryStore) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return Snapshot{}, err
	}
	if s == nil {
		return Snapshot{}, ErrStoreRequired
	}
	s.mu.Lock()
	snapshot := Snapshot{
		Name:     s.name,
		Total:    len(s.events),
		ByStatus: make(map[string]int),
	}
	for _, event := range s.events {
		snapshot.ByStatus[event.Status]++
		if isDueStatus(event.Status) && (snapshot.OldestDue.IsZero() || event.AvailableAt.Before(snapshot.OldestDue)) {
			snapshot.OldestDue = event.AvailableAt
		}
	}
	if len(snapshot.ByStatus) == 0 {
		snapshot.ByStatus = nil
	}
	s.mu.Unlock()
	return snapshot, nil
}
