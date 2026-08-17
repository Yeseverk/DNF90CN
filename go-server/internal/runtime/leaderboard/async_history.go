package leaderboard

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const (
	histQueueMax      = 4096
	histFlushTimeout  = 3 * time.Second
	histRetryInterval = 200 * time.Millisecond
)

var (
	errHistStoreNil = errors.New("leaderboard history store is nil")
	errHistClosed   = errors.New("leaderboard history queue is closed")
	errHistFull     = errors.New("leaderboard history queue is full")
)

// AsyncHistoryStore 把排行榜历史写入移出热路径，适用于非 strict 的 SQL/outbox 历史。
type AsyncHistoryStore struct {
	next    HistoryStore
	max     int
	timeout time.Duration

	mu        sync.Mutex
	pending   []HistoryEntry
	closed    bool
	flushing  bool
	flushDone chan struct{}
	errs      uint64

	wake chan struct{}
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// NewAsyncHistoryStore 创建异步排行榜历史存储。
func NewAsyncHistoryStore(next HistoryStore) *AsyncHistoryStore {
	store := &AsyncHistoryStore{
		next:    next,
		max:     histQueueMax,
		timeout: histFlushTimeout,
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go store.run()
	return store
}

// Append 只入队历史，后台负责慢存储写入。
func (s *AsyncHistoryStore) Append(ctx context.Context, entry HistoryEntry) error {
	if s == nil || s.next == nil {
		return errHistStoreNil
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	entry = normHistoryEntry(entry)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errHistClosed
	}
	if len(s.pending) >= s.max {
		atomic.AddUint64(&s.errs, 1)
		return errHistFull
	}
	s.pending = append(s.pending, entry)
	s.wakeLocked()
	return nil
}

// List 刷新 pending 后透传底层查询，管理面能读到已排队历史。
func (s *AsyncHistoryStore) List(ctx context.Context, leaderboardID string, limit int) ([]HistoryEntry, error) {
	if s == nil || s.next == nil {
		return nil, errHistStoreNil
	}
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}
	return s.next.List(ctx, leaderboardID, limit)
}

// Flush 同步排空待写历史。
func (s *AsyncHistoryStore) Flush(ctx context.Context) error {
	if s == nil || s.next == nil {
		return errHistStoreNil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		batch, wait := s.beginFlush()
		if wait != nil {
			if err := waitHistFlush(ctx, wait); err != nil {
				return err
			}
			continue
		}
		if len(batch) == 0 {
			return nil
		}
		if err := s.writeBatch(ctx, batch); err != nil {
			return err
		}
	}
}

// Close 停止后台 worker 并排空待写历史。
func (s *AsyncHistoryStore) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	closeCtx, cancel := s.closeContext(ctx)
	defer cancel()
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		close(s.stop)
	})
	select {
	case <-s.done:
	case <-closeCtx.Done():
		return closeCtx.Err()
	}
	return errors.Join(s.Flush(closeCtx), closeHistory(closeCtx, s.next))
}

// HistoryErrors 返回异步写入失败计数。
func (s *AsyncHistoryStore) HistoryErrors() uint64 {
	if s == nil {
		return 0
	}
	return atomic.LoadUint64(&s.errs)
}

// EventLogErrors 透传被包装历史存储的 outbox 错误计数。
func (s *AsyncHistoryStore) EventLogErrors() uint64 {
	if s == nil {
		return 0
	}
	return historyStoreErrors(s.next)
}

func (s *AsyncHistoryStore) run() {
	defer close(s.done)
	retry := time.NewTimer(time.Hour)
	stopHistTimer(retry)
	var retryC <-chan time.Time
	defer stopHistTimer(retry)
	for {
		select {
		case <-s.wake:
			if err := s.flushBackground(); err != nil {
				resetHistTimer(retry, histRetryInterval)
				retryC = retry.C
			} else {
				retryC = nil
			}
		case <-retryC:
			retryC = nil
			if err := s.flushBackground(); err != nil {
				resetHistTimer(retry, histRetryInterval)
				retryC = retry.C
			}
		case <-s.stop:
			return
		}
	}
}

func (s *AsyncHistoryStore) flushBackground() error {
	timeout := s.timeout
	if timeout <= 0 {
		timeout = histFlushTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	err := s.Flush(ctx)
	cancel()
	return err
}

func (s *AsyncHistoryStore) closeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	timeout := s.timeout
	if timeout <= 0 {
		timeout = histFlushTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (s *AsyncHistoryStore) beginFlush() ([]HistoryEntry, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flushing {
		return nil, s.flushDone
	}
	if len(s.pending) == 0 {
		return nil, nil
	}
	batch := cloneHistoryEntries(s.pending)
	s.pending = nil
	s.flushing = true
	s.flushDone = make(chan struct{})
	return batch, nil
}

func (s *AsyncHistoryStore) writeBatch(ctx context.Context, batch []HistoryEntry) error {
	var err error
	failedAt := len(batch)
	for i, entry := range batch {
		if err = ctxErr(ctx); err != nil {
			failedAt = i
			break
		}
		if err = s.next.Append(ctx, entry); err != nil {
			failedAt = i
			break
		}
	}
	s.finishFlush(batch[failedAt:], err)
	return err
}

func (s *AsyncHistoryStore) finishFlush(failed []HistoryEntry, err error) {
	s.mu.Lock()
	if err != nil {
		atomic.AddUint64(&s.errs, 1)
		restored := cloneHistoryEntries(failed)
		restored = append(restored, s.pending...)
		s.pending = restored
	}
	done := s.flushDone
	s.flushing = false
	s.flushDone = nil
	s.mu.Unlock()
	if done != nil {
		close(done)
	}
}

func (s *AsyncHistoryStore) wakeLocked() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func wrapHistoryStore(store HistoryStore, strict bool) HistoryStore {
	if strict {
		return store
	}
	switch store.(type) {
	case nil, *MemoryHistoryStore, *AsyncHistoryStore:
		return store
	case *SQLHistoryStore, *EventLogHistoryStore:
		return NewAsyncHistoryStore(store)
	default:
		return store
	}
}

func flushHistory(ctx context.Context, store HistoryStore) error {
	if flusher, ok := store.(interface {
		Flush(context.Context) error
	}); ok {
		return flusher.Flush(ctx)
	}
	return nil
}

func closeHistory(ctx context.Context, store HistoryStore) error {
	if closer, ok := store.(interface {
		Close(context.Context) error
	}); ok {
		return closer.Close(ctx)
	}
	return flushHistory(ctx, store)
}

func waitHistFlush(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func resetHistTimer(timer *time.Timer, delay time.Duration) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func stopHistTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
