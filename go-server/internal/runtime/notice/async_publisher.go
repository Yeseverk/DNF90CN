package notice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const (
	liveQueueMax      = 4096
	liveFlushTimeout  = 3 * time.Second
	liveRetryInterval = 200 * time.Millisecond
)

var (
	errLiveClosed = errors.New("notice live publisher queue is closed")
	errLiveFull   = errors.New("notice live publisher queue is full")
)

// AsyncLivePublisher 把公告在线推送移出发布热路径。
// Store 和 EventLog 仍由 Service.Publish 同步完成，后台只处理可丢弃的 live fanout。
type AsyncLivePublisher struct {
	next    LivePublisher
	max     int
	timeout time.Duration

	mu        sync.Mutex
	pending   []PublishResult
	closed    bool
	flushing  bool
	flushDone chan struct{}
	errs      uint64

	wake chan struct{}
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// NewAsyncLivePublisher 创建异步 live publisher。
func NewAsyncLivePublisher(next LivePublisher) *AsyncLivePublisher {
	publisher := &AsyncLivePublisher{
		next:    next,
		max:     liveQueueMax,
		timeout: liveFlushTimeout,
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go publisher.run()
	return publisher
}

// WrapAsyncLivePublisher 按需把同步 live publisher 包成异步队列。
func WrapAsyncLivePublisher(publisher LivePublisher) LivePublisher {
	switch publisher.(type) {
	case nil, *AsyncLivePublisher:
		return publisher
	default:
		return NewAsyncLivePublisher(publisher)
	}
}

// PublishNotice 只做入队，后台 worker 负责真正推送。
func (p *AsyncLivePublisher) PublishNotice(ctx context.Context, result PublishResult) error {
	if p == nil || p.next == nil {
		return ErrLivePublisherMissing
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	result = clonePublishResult(result)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errLiveClosed
	}
	if len(p.pending) >= p.max {
		atomic.AddUint64(&p.errs, 1)
		return errLiveFull
	}
	p.pending = append(p.pending, result)
	p.wakeLocked()
	return nil
}

// Flush 同步排空当前待推送队列，主要给测试、管理面和服务停止使用。
func (p *AsyncLivePublisher) Flush(ctx context.Context) error {
	if p == nil || p.next == nil {
		return ErrLivePublisherMissing
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		batch, wait := p.beginFlush()
		if wait != nil {
			if err := waitLiveFlush(ctx, wait); err != nil {
				return err
			}
			continue
		}
		if len(batch) == 0 {
			return nil
		}
		if err := p.writeBatch(ctx, batch); err != nil {
			return err
		}
	}
}

// Close 停止后台 worker，并在返回前尽量排空 live 队列。
func (p *AsyncLivePublisher) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	closeCtx, cancel := p.closeContext(ctx)
	defer cancel()
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		close(p.stop)
	})
	select {
	case <-p.done:
	case <-closeCtx.Done():
		return closeCtx.Err()
	}
	return errors.Join(p.Flush(closeCtx), closeLivePublisher(closeCtx, p.next))
}

func (p *AsyncLivePublisher) closeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	timeout := liveFlushTimeout
	if p.timeout > 0 {
		timeout = p.timeout
	}
	return context.WithTimeout(ctx, timeout)
}

// Errors 返回后台 live fanout 失败次数。
func (p *AsyncLivePublisher) Errors() uint64 {
	if p == nil {
		return 0
	}
	return atomic.LoadUint64(&p.errs)
}

func (p *AsyncLivePublisher) run() {
	defer close(p.done)
	retry := time.NewTimer(time.Hour)
	stopLiveTimer(retry)
	var retryC <-chan time.Time
	defer stopLiveTimer(retry)
	for {
		select {
		case <-p.wake:
			if err := p.flushBackground(); err != nil {
				resetLiveTimer(retry, liveRetryInterval)
				retryC = retry.C
			} else {
				retryC = nil
			}
		case <-retryC:
			retryC = nil
			if err := p.flushBackground(); err != nil {
				resetLiveTimer(retry, liveRetryInterval)
				retryC = retry.C
			}
		case <-p.stop:
			return
		}
	}
}

func (p *AsyncLivePublisher) flushBackground() error {
	timeout := p.timeout
	if timeout <= 0 {
		timeout = liveFlushTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	err := p.Flush(ctx)
	cancel()
	return err
}

func (p *AsyncLivePublisher) beginFlush() ([]PublishResult, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.flushing {
		return nil, p.flushDone
	}
	if len(p.pending) == 0 {
		return nil, nil
	}
	batch := clonePublishResults(p.pending)
	p.pending = nil
	p.flushing = true
	p.flushDone = make(chan struct{})
	return batch, nil
}

func (p *AsyncLivePublisher) writeBatch(ctx context.Context, batch []PublishResult) error {
	for idx, result := range batch {
		if err := ctxErr(ctx); err != nil {
			p.finishFlush(batch[idx:], err)
			return err
		}
		if err := p.next.PublishNotice(ctx, result); err != nil {
			atomic.AddUint64(&p.errs, 1)
			p.finishFlush(batch[idx:], err)
			return err
		}
	}
	p.finishFlush(nil, nil)
	return nil
}

func (p *AsyncLivePublisher) finishFlush(failed []PublishResult, _ error) {
	p.mu.Lock()
	if len(failed) > 0 {
		restored := clonePublishResults(failed)
		restored = append(restored, p.pending...)
		p.pending = restored
	}
	done := p.flushDone
	p.flushing = false
	p.flushDone = nil
	p.mu.Unlock()
	if done != nil {
		close(done)
	}
}

func (p *AsyncLivePublisher) wakeLocked() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func clonePublishResults(in []PublishResult) []PublishResult {
	if len(in) == 0 {
		return nil
	}
	out := make([]PublishResult, len(in))
	for idx, result := range in {
		out[idx] = clonePublishResult(result)
	}
	return out
}

func waitLiveFlush(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func closeLivePublisher(ctx context.Context, publisher LivePublisher) error {
	if closer, ok := publisher.(interface {
		Close(context.Context) error
	}); ok {
		return closer.Close(ctx)
	}
	if flusher, ok := publisher.(interface {
		Flush(context.Context) error
	}); ok {
		return flusher.Flush(ctx)
	}
	return nil
}

func stopLiveTimer(timer *time.Timer) {
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

func resetLiveTimer(timer *time.Timer, d time.Duration) {
	if d <= 0 {
		d = liveRetryInterval
	}
	stopLiveTimer(timer)
	timer.Reset(d)
}
