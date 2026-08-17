package logic

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"longheng.io/server/internal/platform/bus"
	"longheng.io/server/internal/services/asyncpayload"
)

const (
	evtQMax          = 4096
	evtRetryInterval = 200 * time.Millisecond
)

var (
	errEvtMissing = errors.New("logic event publisher is missing")
	errEvtClosed  = errors.New("logic event queue is closed")
	errEvtFull    = errors.New("logic event queue is full")
)

type evtItem struct {
	topic   string
	payload any
}

// eventPub 把 logic 的非关键通知 publish 移出调用方热路径。
type eventPub struct {
	bus     bus.Bus
	logger  *slog.Logger
	timeout time.Duration

	mu        sync.Mutex
	pending   []evtItem
	closed    bool
	flushing  bool
	flushDone chan struct{}

	wake chan struct{}
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func newEventPub(eventBus bus.Bus, logger *slog.Logger) *eventPub {
	if eventBus == nil {
		return nil
	}
	pub := &eventPub{
		bus:     eventBus,
		logger:  logger,
		timeout: asyncPublishTimeout,
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go pub.run()
	return pub
}

func (p *eventPub) Publish(topic string, payload any) error {
	if p == nil || p.bus == nil {
		return errEvtMissing
	}
	item := evtItem{topic: topic, payload: asyncpayload.Clone(payload)}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errEvtClosed
	}
	if len(p.pending) >= evtQMax {
		return errEvtFull
	}
	p.pending = append(p.pending, item)
	p.wakeLocked()
	return nil
}

func (p *eventPub) Flush(ctx context.Context) error {
	if p == nil || p.bus == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		batch, wait := p.beginFlush()
		if wait != nil {
			if err := waitEvtFlush(ctx, wait); err != nil {
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

func (p *eventPub) Close(ctx context.Context) error {
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

func (p *eventPub) run() {
	defer close(p.done)
	retry := time.NewTimer(time.Hour)
	stopEvtTimer(retry)
	var retryC <-chan time.Time
	defer stopEvtTimer(retry)
	for {
		select {
		case <-p.wake:
			if err := p.flushBackground(); err != nil {
				resetEvtTimer(retry, evtRetryInterval)
				retryC = retry.C
			} else {
				retryC = nil
			}
		case <-retryC:
			retryC = nil
			if err := p.flushBackground(); err != nil {
				resetEvtTimer(retry, evtRetryInterval)
				retryC = retry.C
			}
		case <-p.stop:
			return
		}
	}
}

func (p *eventPub) flushBackground() error {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeoutOrDefault())
	err := p.Flush(ctx)
	cancel()
	return err
}

func (p *eventPub) beginFlush() ([]evtItem, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.flushing {
		return nil, p.flushDone
	}
	if len(p.pending) == 0 {
		return nil, nil
	}
	batch := cloneEvtItems(p.pending)
	p.pending = nil
	p.flushing = true
	p.flushDone = make(chan struct{})
	return batch, nil
}

func (p *eventPub) writeBatch(ctx context.Context, batch []evtItem) error {
	for idx, item := range batch {
		if err := ctxErr(ctx); err != nil {
			p.finishFlush(batch[idx:])
			return err
		}
		if err := p.bus.Publish(ctx, item.topic, asyncpayload.Clone(item.payload)); err != nil {
			if p.logger != nil {
				p.logger.Error("logic async publish failed", "topic", item.topic, "error", err)
			}
			p.finishFlush(batch[idx:])
			return err
		}
	}
	p.finishFlush(nil)
	return nil
}

func (p *eventPub) finishFlush(failed []evtItem) {
	p.mu.Lock()
	if len(failed) > 0 {
		restored := cloneEvtItems(failed)
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

func (p *eventPub) wakeLocked() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func cloneEvtItems(in []evtItem) []evtItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]evtItem, len(in))
	for idx, item := range in {
		out[idx] = evtItem{
			topic:   item.topic,
			payload: asyncpayload.Clone(item.payload),
		}
	}
	return out
}

func (p *eventPub) timeoutOrDefault() time.Duration {
	if p == nil || p.timeout <= 0 {
		return asyncPublishTimeout
	}
	return p.timeout
}

func (p *eventPub) closeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, p.timeoutOrDefault())
}

func stopEvtTimer(timer *time.Timer) {
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

func resetEvtTimer(timer *time.Timer, d time.Duration) {
	if d <= 0 {
		d = evtRetryInterval
	}
	stopEvtTimer(timer)
	timer.Reset(d)
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func waitEvtFlush(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
