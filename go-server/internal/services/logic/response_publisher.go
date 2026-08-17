package logic

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/bus"
	"longheng.io/server/pkg/contracts"
)

const (
	respQueueMax      = 4096
	respPubTimeout    = 3 * time.Second
	respRetryInterval = 200 * time.Millisecond
)

var (
	errRespMissing = errors.New("logic response publisher is missing")
	errRespClosed  = errors.New("logic response queue is closed")
	errRespFull    = errors.New("logic response queue is full")
)

type respPubItem struct {
	topic    string
	response contracts.LogicPlayerResponse
}

// respPublisher 把 logic 到 gateway 的回包 publish 移出玩家队列热路径。
type respPublisher struct {
	bus     bus.Bus
	logger  *slog.Logger
	max     int
	timeout time.Duration

	mu        sync.Mutex
	pending   []respPubItem
	closed    bool
	flushing  bool
	flushDone chan struct{}
	errs      uint64

	wake chan struct{}
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func newRespPublisher(eventBus bus.Bus, logger *slog.Logger) *respPublisher {
	if eventBus == nil {
		return nil
	}
	publisher := &respPublisher{
		bus:     eventBus,
		logger:  logger,
		max:     respQueueMax,
		timeout: respPubTimeout,
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go publisher.run()
	return publisher
}

func (p *respPublisher) Publish(ctx context.Context, topic string, response contracts.LogicPlayerResponse) error {
	if p == nil || p.bus == nil {
		return errRespMissing
	}
	if err := respCtxErr(ctx); err != nil {
		return err
	}
	item := respPubItem{
		topic:    topic,
		response: clonePlayerResp(response),
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errRespClosed
	}
	if len(p.pending) >= p.max {
		atomic.AddUint64(&p.errs, 1)
		return errRespFull
	}
	p.pending = append(p.pending, item)
	p.wakeLocked()
	return nil
}

func (p *respPublisher) Flush(ctx context.Context) error {
	if p == nil || p.bus == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		batch, wait := p.beginFlush()
		if wait != nil {
			if err := waitRespFlush(ctx, wait); err != nil {
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

func (p *respPublisher) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	ctx, cancel := p.closeContext(ctx)
	defer cancel()

	p.mu.Lock()
	p.closed = true
	stop := p.stop
	done := p.done
	p.mu.Unlock()
	if stop != nil {
		p.once.Do(func() {
			close(stop)
		})
	}
	if stop != nil && done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return p.Flush(ctx)
}

func (p *respPublisher) Errors() uint64 {
	if p == nil {
		return 0
	}
	return atomic.LoadUint64(&p.errs)
}

func (p *respPublisher) run() {
	defer close(p.done)
	retry := time.NewTimer(time.Hour)
	stopRespTimer(retry)
	var retryC <-chan time.Time
	defer stopRespTimer(retry)
	for {
		select {
		case <-p.wake:
			if err := p.flushBackground(); err != nil {
				resetRespTimer(retry, respRetryInterval)
				retryC = retry.C
			} else {
				retryC = nil
			}
		case <-retryC:
			retryC = nil
			if err := p.flushBackground(); err != nil {
				resetRespTimer(retry, respRetryInterval)
				retryC = retry.C
			}
		case <-p.stop:
			return
		}
	}
}

func (p *respPublisher) flushBackground() error {
	timeout := p.timeout
	if timeout <= 0 {
		timeout = respPubTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	err := p.Flush(ctx)
	cancel()
	return err
}

func (p *respPublisher) beginFlush() ([]respPubItem, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.flushing {
		return nil, p.flushDone
	}
	if len(p.pending) == 0 {
		return nil, nil
	}
	batch := cloneRespPubItems(p.pending)
	p.pending = nil
	p.flushing = true
	p.flushDone = make(chan struct{})
	return batch, nil
}

func (p *respPublisher) writeBatch(ctx context.Context, batch []respPubItem) error {
	for idx, item := range batch {
		if err := respCtxErr(ctx); err != nil {
			p.finishFlush(batch[idx:])
			return err
		}
		if err := p.bus.Publish(ctx, item.topic, item.response); err != nil {
			atomic.AddUint64(&p.errs, 1)
			if p.logger != nil {
				p.logger.Error("logic response publish failed", "topic", item.topic, "account_id", item.response.AccountID, "msg_id", item.response.MsgID, "error", err)
			}
			p.finishFlush(batch[idx:])
			return err
		}
	}
	p.finishFlush(nil)
	return nil
}

func (p *respPublisher) finishFlush(failed []respPubItem) {
	p.mu.Lock()
	if len(failed) > 0 {
		restored := cloneRespPubItems(failed)
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

func (p *respPublisher) wakeLocked() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func cloneRespPubItems(in []respPubItem) []respPubItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]respPubItem, len(in))
	for idx, item := range in {
		out[idx] = respPubItem{
			topic:    item.topic,
			response: clonePlayerResp(item.response),
		}
	}
	return out
}

func waitRespFlush(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *respPublisher) closeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	timeout := respPubTimeout
	if p != nil && p.timeout > 0 {
		timeout = p.timeout
	}
	return context.WithTimeout(ctx, timeout)
}

func respCtxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func stopRespTimer(timer *time.Timer) {
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

func resetRespTimer(timer *time.Timer, d time.Duration) {
	if d <= 0 {
		d = respRetryInterval
	}
	stopRespTimer(timer)
	timer.Reset(d)
}
