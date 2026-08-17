package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// AsyncStoreOptions 配置异步写后队列、重试和死信存储。
type AsyncStoreOptions[T any, F comparable] struct {
	FlushInterval   time.Duration
	MaxPending      int
	RetryBackoff    time.Duration
	MaxRetries      int
	DeadLetterLimit int
	DeadLetterStore DeadLetterStore[T, F]

	RecordKey       KeyFunc[T]
	NormalizeKey    func(string) string
	Clone           CloneFunc[T]
	AllFields       func() []F
	NormalizeFields func([]F) []F
	SaveFields      SaveFieldsFunc[T, F]
	SaveBatch       SaveFieldBatchFunc[T, F]
	AutoExpire      bool
	AutoExpireTTL   time.Duration
	Expire          ExpireFunc[T]
	ClosedError     error
}

// AsyncStoreStats 描述异步存储当前运行状态。
type AsyncStoreStats struct {
	Pending              int    `json:"pending"`
	PendingDue           int    `json:"pending_due"`
	DeadLetters          int    `json:"dead_letters"`
	Closed               bool   `json:"closed"`
	FlushInterval        string `json:"flush_interval"`
	RetryBackoff         string `json:"retry_backoff"`
	MaxPending           int    `json:"max_pending"`
	MaxRetries           int    `json:"max_retries"`
	AutoExpireTTL        string `json:"auto_expire_ttl,omitempty"`
	DeadLetterLimit      int    `json:"dead_letter_limit"`
	DeadLetterStore      string `json:"dead_letter_store,omitempty"`
	DeadLetterStoreError string `json:"dead_letter_store_error,omitempty"`
}

const defAsyncFlush = 5 * time.Second

// AsyncStore 用内存队列包装同步 Store，实现异步刷盘和死信兜底。
type AsyncStore[T any, F comparable] struct {
	base          Store[T]
	flushInterval time.Duration
	maxPending    int
	allFields     func() []F
	closedError   error

	mu     sync.Mutex
	closed bool

	queue       *asyncPendingQueue[T, F]
	retry       asyncRetryPolicy[T, F]
	deadLetters *asyncDeadLetterSink[T, F]
	flusher     *asyncFlusher[T, F]

	done     chan struct{}
	flushNow chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

// NewAsyncStore 用写后队列包装同步 Store。
// Load 会先读 pending 和 dead-letter，再读 base store；这样即使后台刷盘还没落到持久层，
// 调用方也能看到自己已被接受的最新 mutation。
func NewAsyncStore[T any, F comparable](base Store[T], options AsyncStoreOptions[T, F]) (*AsyncStore[T, F], error) {
	if base == nil {
		return nil, fmt.Errorf("db.AsyncStore requires a base store")
	}
	if options.AutoExpire && options.AutoExpireTTL <= 0 {
		return nil, fmt.Errorf("db.AsyncStore auto expire ttl must be positive")
	}
	return newAsyncStore(base, options), nil
}

// MustNewAsyncStore 创建异步存储，失败时 panic，适合启动期固定配置。
func MustNewAsyncStore[T any, F comparable](base Store[T], options AsyncStoreOptions[T, F]) *AsyncStore[T, F] {
	store, err := NewAsyncStore(base, options)
	if err != nil {
		panic(err)
	}
	return store
}

func newAsyncStore[T any, F comparable](base Store[T], options AsyncStoreOptions[T, F]) *AsyncStore[T, F] {
	if options.RetryBackoff < 0 {
		options.RetryBackoff = time.Second
	}
	if options.DeadLetterLimit <= 0 {
		options.DeadLetterLimit = 128
	}
	if options.Clone == nil {
		options.Clone = IdentityClone[T]
	}
	if options.SaveFields == nil {
		options.SaveFields = func(ctx context.Context, store Store[T], record T, fields ...F) error {
			return SaveFields(ctx, store, record, options.NormalizeFields, fields...)
		}
	}
	if options.SaveBatch == nil {
		if _, ok := base.(BatchFieldStore[T, F]); ok {
			options.SaveBatch = func(ctx context.Context, store Store[T], saves []FieldSave[T, F]) error {
				return SaveFieldBatch(ctx, store, options.NormalizeFields, saves)
			}
		}
	}
	if options.AutoExpire && options.Expire == nil {
		options.Expire = func(ctx context.Context, store Store[T], key string, ttl time.Duration) error {
			return Expire(ctx, store, key, ttl)
		}
	}
	if options.ClosedError == nil {
		options.ClosedError = ErrAsyncStoreClosed
	}

	queue := newAsyncPendingQueue(options.RecordKey, options.NormalizeKey, options.Clone, options.NormalizeFields)
	retry := asyncRetryPolicy[T, F]{backoff: options.RetryBackoff, maxRetries: options.MaxRetries}
	deadLetters := newAsyncDeadSink(asyncDeadSinkOpts[T, F]{
		Limit:           options.DeadLetterLimit,
		Store:           options.DeadLetterStore,
		RecordKey:       options.RecordKey,
		NormalizeKey:    options.NormalizeKey,
		Clone:           options.Clone,
		NormalizeFields: options.NormalizeFields,
	})
	flusher := newAsyncFlusher(asyncFlusherOptions[T, F]{
		Base:            base,
		Queue:           queue,
		Retry:           retry,
		DeadLetters:     deadLetters,
		Clone:           options.Clone,
		NormalizeFields: options.NormalizeFields,
		SaveFields:      options.SaveFields,
		SaveBatch:       options.SaveBatch,
		AutoExpireTTL:   options.AutoExpireTTL,
		Expire:          options.Expire,
	})

	store := &AsyncStore[T, F]{
		base:          base,
		flushInterval: options.FlushInterval,
		maxPending:    options.MaxPending,
		allFields:     options.AllFields,
		closedError:   options.ClosedError,
		queue:         queue,
		retry:         retry,
		deadLetters:   deadLetters,
		flusher:       flusher,
		done:          make(chan struct{}),
		flushNow:      make(chan struct{}, 1),
		stopped:       make(chan struct{}),
	}
	store.deadLetters.Load()
	go store.run()
	return store
}

// Load 优先读取 pending 和 dead-letter，再读取底层存储。
func (s *AsyncStore[T, F]) Load(ctx context.Context, accountID string) (T, bool, error) {
	ctx = contextOrBackground(ctx)
	accountID = s.queue.NormalizeRecordKey(accountID)
	if pending, ok := s.queue.Get(accountID); ok {
		return s.queue.Clone(pending.profile), true, nil
	}
	if dead, ok := s.deadLetters.Get(accountID); ok {
		return s.queue.Clone(dead.Profile), true, nil
	}
	return s.base.Load(ctx, accountID)
}

// Check 检查异步存储、底层存储和死信存储状态。
func (s *AsyncStore[T, F]) Check(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	s.mu.Lock()
	closed := s.closed
	base := s.base
	closedError := s.closedError
	s.mu.Unlock()

	if closed {
		return closedError
	}
	if deadLetterStoreError := s.deadLetters.LastError(); deadLetterStoreError != "" {
		return fmt.Errorf("dead letter store: %s", deadLetterStoreError)
	}
	if err := Check(ctx, base); err != nil {
		return err
	}
	return s.deadLetters.CheckStore(ctx)
}

// Save 保存完整记录字段。
func (s *AsyncStore[T, F]) Save(ctx context.Context, profile T) error {
	fields := s.allFieldsForSave()
	if len(fields) == 0 {
		return ErrAllFieldsRequired
	}
	return s.SaveFields(ctx, profile, fields...)
}

// SaveFields 把局部字段写入 pending 队列。
func (s *AsyncStore[T, F]) SaveFields(ctx context.Context, profile T, fields ...F) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	accountID, err := s.queue.RecordKey(profile)
	if err != nil {
		return err
	}
	fields = s.queue.NormalizeFields(fields)
	if len(fields) == 0 {
		return nil
	}

	save := pendingSave[T, F]{
		profile: s.queue.Clone(profile),
		fields:  fields,
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.closedError
	}
	pending := s.queue.Add(accountID, save)
	s.mu.Unlock()

	if s.maxPending > 0 && pending >= s.maxPending {
		s.requestFlush()
	}
	return nil
}

// Flush 强制刷出当前可写 pending 队列。
func (s *AsyncStore[T, F]) Flush(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	return s.flusher.Flush(ctx, true)
}

// Stats 返回异步队列、死信和重试状态。
func (s *AsyncStore[T, F]) Stats() AsyncStoreStats {
	now := time.Now().UTC()
	pending, pendingDue := s.queue.Stats(now)

	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()

	return AsyncStoreStats{
		Pending:              pending,
		PendingDue:           pendingDue,
		DeadLetters:          s.deadLetters.Count(),
		Closed:               closed,
		FlushInterval:        s.flushInterval.String(),
		RetryBackoff:         s.retry.backoff.String(),
		MaxPending:           s.maxPending,
		MaxRetries:           s.retry.maxRetries,
		AutoExpireTTL:        asyncDurationString(s.flusher.AutoExpireTTL()),
		DeadLetterLimit:      s.deadLetters.Limit(),
		DeadLetterStore:      s.deadLetters.StoreName(),
		DeadLetterStoreError: s.deadLetters.LastError(),
	}
}

// DeadLetters 返回当前死信快照。
func (s *AsyncStore[T, F]) DeadLetters() []AsyncStoreDeadLetter[T, F] {
	return s.deadLetters.List()
}

// RequeueDeadLetter 把指定死信重新放回 pending 队列。
func (s *AsyncStore[T, F]) RequeueDeadLetter(accountID string) bool {
	accountID = s.queue.NormalizeRecordKey(accountID)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	dead, ok := s.deadLetters.Take(accountID)
	if !ok {
		return false
	}

	fields := s.queue.NormalizeFields(dead.Fields)
	if len(fields) == 0 {
		fields = s.allFieldsForSave()
	}
	save := pendingSave[T, F]{
		profile:       s.queue.Clone(dead.Profile),
		fields:        fields,
		attempts:      dead.Attempts,
		lastError:     dead.Error,
		firstFailedAt: dead.FirstFailedAt,
		lastFailedAt:  dead.LastFailedAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		if err := s.deadLetters.Add(save); err != nil {
			s.deadLetters.restoreMemory(dead)
		}
		return false
	}
	s.queue.Requeue(accountID, save)
	return true
}

// Close 停止后台刷盘并做最后一次 Flush。
func (s *AsyncStore[T, F]) Close(ctx context.Context) error {
	ctx, cancel := asyncCloseCtx(ctx)
	defer cancel()
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	s.stopOnce.Do(func() {
		close(s.done)
	})
	select {
	case <-s.stopped:
	case <-ctx.Done():
		return ctx.Err()
	}
	return errors.Join(s.Flush(ctx), CloseOrFlush(ctx, s.base))
}

func asyncCloseCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defAsyncFlush)
}

func (s *AsyncStore[T, F]) run() {
	defer close(s.stopped)
	if s.flushInterval <= 0 {
		for {
			select {
			case <-s.flushNow:
				s.flushBackground()
			case <-s.done:
				return
			}
		}
	}

	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.flushBackground()
		case <-s.flushNow:
			s.flushBackground()
		case <-s.done:
			return
		}
	}
}

func (s *AsyncStore[T, F]) requestFlush() {
	select {
	case s.flushNow <- struct{}{}:
	default:
		return
	}
}

func (s *AsyncStore[T, F]) flushBackground() {
	ctx, cancel := context.WithTimeout(context.Background(), defAsyncFlush)
	_ = s.flusher.Flush(ctx, false)
	cancel()
}

func (s *AsyncStore[T, F]) allFieldsForSave() []F {
	if s.allFields == nil {
		return nil
	}
	return s.queue.NormalizeFields(s.allFields())
}

func asyncDurationString(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	return duration.String()
}
