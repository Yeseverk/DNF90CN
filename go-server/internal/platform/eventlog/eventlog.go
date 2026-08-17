package eventlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

var (
	// ErrStoreRequired 表示 eventlog 缺少底层 store。
	ErrStoreRequired = errors.New("eventlog store is required")

	// ErrPublisherRequired 表示发布流程缺少 Publisher。
	ErrPublisherRequired = errors.New("eventlog publisher is required")

	// ErrEventExists 表示事件 ID 已经存在。
	ErrEventExists = errors.New("event already exists")

	// ErrIdempotencyConflict 表示同一幂等键对应的事件内容不一致。
	ErrIdempotencyConflict = errors.New("eventlog idempotency conflict")

	// ErrEventNotFound 表示目标事件不存在。
	ErrEventNotFound = errors.New("event not found")

	// ErrInvalidEvent 表示事件字段或状态不满足 eventlog 约束。
	ErrInvalidEvent = errors.New("event is invalid")

	// ErrInvalidLimit 表示查询或发布批量限制非法。
	ErrInvalidLimit = errors.New("eventlog limit is invalid")

	// ErrClaimLost 表示当前 worker 已不再持有事件 claim。
	ErrClaimLost = errors.New("eventlog claim is no longer owned")

	// ErrListUnsupported 表示 store 不支持按事件流聚合查询。
	ErrListUnsupported = errors.New("eventlog store does not support stream listing")

	// ErrPublisherPanic 表示外部 Publisher 发布时发生 panic。
	ErrPublisherPanic = errors.New("eventlog publisher panic")
)

const (
	// StatusPending 表示事件等待发布。
	StatusPending = "pending"

	// StatusProcessing 表示事件已被 worker claim。
	StatusProcessing = "processing"

	// StatusFailed 表示事件发布失败并等待重试。
	StatusFailed = "failed"

	// StatusPublished 表示事件已经发布成功。
	StatusPublished = "published"

	// StatusDeadLetter 表示事件进入死信队列等待人工处理。
	StatusDeadLetter = "dead_letter"

	defaultPublishLimit = 100
	defDeadLetterLimit  = 100
	defaultRetryDelay   = time.Second
	defaultClaimTimeout = 30 * time.Second
	defPubStateTimeout  = 5 * time.Second
)

// Event 是可靠事件日志中的一条可持久化 outbox 事件。
type Event struct {
	ID             string            `json:"id"`
	Stream         string            `json:"stream"`
	Type           string            `json:"type"`
	AggregateID    string            `json:"aggregate_id,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Payload        json.RawMessage   `json:"payload,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Status         string            `json:"status"`
	Attempts       int               `json:"attempts,omitempty"`
	AvailableAt    time.Time         `json:"available_at"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	PublishedAt    time.Time         `json:"published_at,omitempty"`
	LastError      string            `json:"last_error,omitempty"`
}

// Store 定义 eventlog 对持久化、claim、死信和快照能力的最小要求。
type Store interface {
	Append(context.Context, Event) (Event, error)
	Get(context.Context, string) (Event, bool, error)
	GetByIdempotencyKey(context.Context, string) (Event, bool, error)
	ClaimPending(context.Context, int, time.Time, time.Duration) ([]Event, error)
	MarkPublished(context.Context, string, time.Time, time.Time) error
	MarkFailed(context.Context, string, string, time.Time, time.Time, time.Time) error
	MarkDeadLetter(context.Context, string, string, time.Time, time.Time) error
	ListDeadLetters(context.Context, int) ([]Event, error)
	RequeueDeadLetter(context.Context, string, time.Time, time.Time, bool) error
	Snapshot(context.Context) (Snapshot, error)
}

// Publisher 是事件发布扩展点，负责把已 claim 的事件投递到下游。
type Publisher interface {
	Publish(context.Context, Event) error
}

// PublisherFunc 允许用函数快速实现 Publisher。
type PublisherFunc func(context.Context, Event) error

// Publish 调用底层函数并保护 nil 函数场景。
func (fn PublisherFunc) Publish(ctx context.Context, event Event) error {
	if fn == nil {
		return ErrPublisherRequired
	}
	return fn(ctx, event)
}

// IDGenerator 为未显式指定 ID 的事件生成稳定 ID。
type IDGenerator func() string

// Options 描述 eventlog 实例的存储、时钟和 ID 生成策略。
type Options struct {
	Name        string
	Store       Store
	Now         func() time.Time
	IDGenerator IDGenerator
}

// Log 提供事件追加、发布推进、死信处理和查询聚合能力。
type Log struct {
	name  string
	store Store
	now   func() time.Time
	gen   IDGenerator

	mu   sync.Mutex
	next uint64
}

// PublishOptions 控制一次待发布扫描的批量、重试和排除流策略。
type PublishOptions struct {
	Limit          int
	RetryDelay     time.Duration
	ClaimTimeout   time.Duration
	MaxAttempts    int
	ExcludeStreams []string
}

// PublishStats 记录一次发布扫描的领取、成功、失败和死信数量。
type PublishStats struct {
	Fetched      int `json:"fetched"`
	Published    int `json:"published"`
	Failed       int `json:"failed"`
	DeadLettered int `json:"dead_lettered,omitempty"`
}

type streamAggLister interface {
	ListByStreamAggregate(context.Context, string, string, int) ([]Event, error)
}

// Snapshot 是 eventlog 当前堆积和各状态数量的观测结果。
type Snapshot struct {
	Name      string         `json:"name,omitempty"`
	Total     int            `json:"total"`
	ByStatus  map[string]int `json:"by_status,omitempty"`
	OldestDue time.Time      `json:"oldest_due,omitempty"`
}

// RequeueOptions 控制死信重新入队后的可用时间和尝试次数处理。
type RequeueOptions struct {
	AvailableAt      time.Time
	PreserveAttempts bool
}

// New 创建 eventlog 实例；缺省名称、时钟和 ID 生成器会在内部补齐。
func New(options Options) *Log {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "eventlog"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Log{
		name:  name,
		store: options.Store,
		now:   now,
		gen:   options.IDGenerator,
	}
}

// Append 校验并追加事件，幂等键相同且内容一致时返回既有事件。
func (l *Log) Append(ctx context.Context, event Event) (Event, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, err
	}
	if l == nil || l.store == nil {
		return Event{}, ErrStoreRequired
	}
	normalized, err := l.normalize(event)
	if err != nil {
		return Event{}, err
	}
	return l.store.Append(ctx, normalized)
}

// Close 关闭底层支持关闭的 store。
func (l *Log) Close() error {
	if l == nil || l.store == nil {
		return nil
	}
	if closer, ok := l.store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// Get 按事件 ID 查询事件。
func (l *Log) Get(ctx context.Context, id string) (Event, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, false, err
	}
	if l == nil || l.store == nil {
		return Event{}, false, ErrStoreRequired
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Event{}, false, nil
	}
	return l.store.Get(ctx, id)
}

// GetByIdempotencyKey 按幂等键查询已存在的事件。
func (l *Log) GetByIdempotencyKey(ctx context.Context, key string) (Event, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, false, err
	}
	if l == nil || l.store == nil {
		return Event{}, false, ErrStoreRequired
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return Event{}, false, nil
	}
	return l.store.GetByIdempotencyKey(ctx, key)
}

// PublishPending 领取到期事件、调用 publisher，并按结果推进状态。
func (l *Log) PublishPending(ctx context.Context, publisher Publisher, options PublishOptions) (PublishStats, error) {
	if err := ctxErr(ctx); err != nil {
		return PublishStats{}, err
	}
	if l == nil || l.store == nil {
		return PublishStats{}, ErrStoreRequired
	}
	if publisher == nil {
		return PublishStats{}, ErrPublisherRequired
	}
	limit := options.Limit
	if limit <= 0 {
		limit = defaultPublishLimit
	}
	retryDelay := options.RetryDelay
	if retryDelay <= 0 {
		retryDelay = defaultRetryDelay
	}
	claimTimeout := options.ClaimTimeout
	if claimTimeout <= 0 {
		claimTimeout = defaultClaimTimeout
	}
	// PublishPending 先 claim 一批事件，再逐条发布并更新状态；claim lease 防止多个 worker 同时提交同一事件。
	now := l.now().UTC()
	events, err := l.claimPending(ctx, limit, now, claimTimeout, options.ExcludeStreams)
	if err != nil {
		return PublishStats{}, err
	}
	stats := PublishStats{Fetched: len(events)}
	var errs []error
	for _, event := range events {
		if err := ctxErr(ctx); err != nil {
			return stats, err
		}
		if err := safePublish(ctx, publisher, event); err != nil {
			stats.Failed++
			if errors.Is(err, ErrPermanentPublishFailure) || (options.MaxAttempts > 0 && event.Attempts+1 >= options.MaxAttempts) {
				stats.DeadLettered++
				markCtx, cancel := publishStateContext(ctx)
				markErr := l.store.MarkDeadLetter(markCtx, event.ID, err.Error(), now, event.AvailableAt)
				cancel()
				if markErr != nil {
					errs = append(errs, fmt.Errorf("mark event %s dead letter: %w", event.ID, markErr))
					continue
				}
			} else {
				next := now.Add(retryDelay)
				markCtx, cancel := publishStateContext(ctx)
				markErr := l.store.MarkFailed(markCtx, event.ID, err.Error(), next, now, event.AvailableAt)
				cancel()
				if markErr != nil {
					errs = append(errs, fmt.Errorf("mark event %s failed: %w", event.ID, markErr))
					continue
				}
			}
			errs = append(errs, fmt.Errorf("publish event %s: %w", event.ID, err))
			continue
		}
		markCtx, cancel := publishStateContext(ctx)
		err := l.store.MarkPublished(markCtx, event.ID, now, event.AvailableAt)
		cancel()
		if err != nil {
			stats.Failed++
			errs = append(errs, fmt.Errorf("mark event %s published: %w", event.ID, err))
			continue
		}
		stats.Published++
	}
	return stats, errors.Join(errs...)
}

func safePublish(ctx context.Context, publisher Publisher, event Event) (err error) {
	// publisher 是外部扩展点，panic 必须转换成发布失败，避免 worker 被第三方代码打穿。
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", ErrPublisherPanic, recovered)
		}
	}()
	return publisher.Publish(ctx, event)
}

func publishStateContext(parent context.Context) (context.Context, context.CancelFunc) {
	// 发布后状态写入不能继承调用方取消；否则下游已收到事件时，本地 outbox 仍可能停在 processing 并重复投递。
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	return context.WithTimeout(base, defPubStateTimeout)
}

type streamExcludingStore interface {
	ClaimPendingExcept(context.Context, int, time.Time, time.Duration, []string) ([]Event, error)
}

func (l *Log) claimPending(ctx context.Context, limit int, now time.Time, claimTimeout time.Duration, excludeStreams []string) ([]Event, error) {
	excludeStreams = normalizeStreamList(excludeStreams)
	if len(excludeStreams) == 0 {
		return l.store.ClaimPending(ctx, limit, now, claimTimeout)
	}
	store, ok := l.store.(streamExcludingStore)
	if !ok {
		return nil, fmt.Errorf("%w: store does not support excluded streams", ErrInvalidEvent)
	}
	return store.ClaimPendingExcept(ctx, limit, now, claimTimeout, excludeStreams)
}

func normalizeStreamList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// Snapshot 返回底层 store 的状态快照并补齐日志名称。
func (l *Log) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return Snapshot{}, err
	}
	if l == nil || l.store == nil {
		return Snapshot{}, ErrStoreRequired
	}
	snapshot, err := l.store.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Name == "" {
		snapshot.Name = l.name
	}
	return snapshot, nil
}

// ListByStreamAggregate 查询指定事件流和聚合 ID 的最近事件。
func (l *Log) ListByStreamAggregate(ctx context.Context, stream, aggregateID string, limit int) ([]Event, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if l == nil || l.store == nil {
		return nil, ErrStoreRequired
	}
	stream = strings.TrimSpace(stream)
	aggregateID = strings.TrimSpace(aggregateID)
	if stream == "" {
		return nil, fmt.Errorf("%w: stream is required", ErrInvalidEvent)
	}
	if limit < 0 {
		return nil, ErrInvalidLimit
	}
	if limit == 0 {
		limit = defaultPublishLimit
	}
	lister, ok := l.store.(streamAggLister)
	if !ok {
		return nil, ErrListUnsupported
	}
	return lister.ListByStreamAggregate(ctx, stream, aggregateID, limit)
}

// DeadLetters 查询当前死信队列中的事件。
func (l *Log) DeadLetters(ctx context.Context, limit int) ([]Event, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if l == nil || l.store == nil {
		return nil, ErrStoreRequired
	}
	if limit < 0 {
		return nil, ErrInvalidLimit
	}
	if limit == 0 {
		limit = defDeadLetterLimit
	}
	return l.store.ListDeadLetters(ctx, limit)
}

// RequeueDeadLetter 把死信事件重新置为待发布状态。
func (l *Log) RequeueDeadLetter(ctx context.Context, id string, options RequeueOptions) (Event, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, err
	}
	if l == nil || l.store == nil {
		return Event{}, ErrStoreRequired
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Event{}, ErrEventNotFound
	}
	now := l.now().UTC()
	availableAt := options.AvailableAt
	if availableAt.IsZero() {
		availableAt = now
	} else {
		availableAt = availableAt.UTC()
	}
	if err := l.store.RequeueDeadLetter(ctx, id, availableAt, now, options.PreserveAttempts); err != nil {
		return Event{}, err
	}
	event, ok, err := l.store.Get(ctx, id)
	if err != nil {
		return Event{}, err
	}
	if !ok {
		return Event{}, ErrEventNotFound
	}
	return event, nil
}

// MarkPublished 手动把事件标记为已发布，主要用于管理修复和测试夹具。
func (l *Log) MarkPublished(ctx context.Context, id string) (Event, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, err
	}
	if l == nil || l.store == nil {
		return Event{}, ErrStoreRequired
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Event{}, ErrEventNotFound
	}
	now := l.now().UTC()
	if err := l.store.MarkPublished(ctx, id, now, time.Time{}); err != nil {
		return Event{}, err
	}
	event, ok, err := l.store.Get(ctx, id)
	if err != nil {
		return Event{}, err
	}
	if !ok {
		return Event{}, ErrEventNotFound
	}
	return event, nil
}

// MarkFailed 手动把事件标记为失败并设置下一次可发布时间。
func (l *Log) MarkFailed(ctx context.Context, id string, message string, nextAttempt time.Time) (Event, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, err
	}
	if l == nil || l.store == nil {
		return Event{}, ErrStoreRequired
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Event{}, ErrEventNotFound
	}
	now := l.now().UTC()
	if nextAttempt.IsZero() {
		nextAttempt = now
	} else {
		nextAttempt = nextAttempt.UTC()
	}
	if err := l.store.MarkFailed(ctx, id, message, nextAttempt, now, time.Time{}); err != nil {
		return Event{}, err
	}
	event, ok, err := l.store.Get(ctx, id)
	if err != nil {
		return Event{}, err
	}
	if !ok {
		return Event{}, ErrEventNotFound
	}
	return event, nil
}

func (l *Log) normalize(event Event) (Event, error) {
	event.ID = strings.TrimSpace(event.ID)
	event.Stream = strings.TrimSpace(event.Stream)
	event.Type = strings.TrimSpace(event.Type)
	event.AggregateID = strings.TrimSpace(event.AggregateID)
	event.IdempotencyKey = strings.TrimSpace(event.IdempotencyKey)
	event.Status = strings.TrimSpace(event.Status)
	if event.Stream == "" {
		return Event{}, fmt.Errorf("%w: stream is required", ErrInvalidEvent)
	}
	if event.Type == "" {
		return Event{}, fmt.Errorf("%w: type is required", ErrInvalidEvent)
	}
	if event.ID == "" {
		event.ID = l.nextID()
	}
	if event.Status == "" {
		event.Status = StatusPending
	}
	if event.Status != StatusPending && event.Status != StatusProcessing && event.Status != StatusFailed && event.Status != StatusPublished && event.Status != StatusDeadLetter {
		return Event{}, fmt.Errorf("%w: unsupported status %q", ErrInvalidEvent, event.Status)
	}
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage("null")
	}
	if !json.Valid(event.Payload) {
		return Event{}, fmt.Errorf("%w: payload must be valid json", ErrInvalidEvent)
	}
	now := l.now().UTC()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	} else {
		event.CreatedAt = event.CreatedAt.UTC()
	}
	if event.UpdatedAt.IsZero() {
		event.UpdatedAt = event.CreatedAt
	} else {
		event.UpdatedAt = event.UpdatedAt.UTC()
	}
	if event.AvailableAt.IsZero() {
		event.AvailableAt = event.CreatedAt
	} else {
		event.AvailableAt = event.AvailableAt.UTC()
	}
	if !event.PublishedAt.IsZero() {
		event.PublishedAt = event.PublishedAt.UTC()
	}
	if event.Attempts < 0 {
		event.Attempts = 0
	}
	event.Headers = cloneStringMap(event.Headers)
	event.Payload = cloneRawMessage(event.Payload)
	return event, nil
}

func (l *Log) nextID() string {
	if l != nil && l.gen != nil {
		if id := strings.TrimSpace(l.gen()); id != "" {
			return id
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	return fmt.Sprintf("event-%d", l.next)
}

func cloneEvent(event Event) Event {
	event.Payload = cloneRawMessage(event.Payload)
	event.Headers = cloneStringMap(event.Headers)
	return event
}

func sameIdempotentEvent(left Event, right Event) bool {
	return left.Stream == right.Stream &&
		left.Type == right.Type &&
		left.AggregateID == right.AggregateID &&
		left.IdempotencyKey == right.IdempotencyKey &&
		bytes.Equal(left.Payload, right.Payload) &&
		reflect.DeepEqual(left.Headers, right.Headers)
}

func cloneRawMessage(data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), data...)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func isDueStatus(status string) bool {
	return status == StatusPending || status == StatusFailed || status == StatusProcessing
}

func ownsClaim(event Event, claimDeadline time.Time) bool {
	// MarkPublished/MarkFailed 必须确认仍持有本次 claim，过期后可能已被其他 worker 重新领取。
	if claimDeadline.IsZero() {
		return true
	}
	return event.Status == StatusProcessing && event.AvailableAt.Equal(claimDeadline.UTC())
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
