package player

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/eventlog"
)

const (
	defProfileEventQueue = 1024
	profileEventTimeout  = 3 * time.Second
)

var (
	errEventClosed = errors.New("profile event queue is closed")
	errEventFull   = errors.New("profile event queue is full")
)

type asyncProfileEvents struct {
	next    ProfileEventAppender
	logger  *slog.Logger
	errs    *uint64
	timeout time.Duration
	queue   chan eventlog.Event

	mu      sync.Mutex
	closed  bool
	once    sync.Once
	stopped chan struct{}
}

func newAsyncEvents(next ProfileEventAppender, logger *slog.Logger, errs *uint64) *asyncProfileEvents {
	events := &asyncProfileEvents{
		next:    next,
		logger:  logger,
		errs:    errs,
		timeout: profileEventTimeout,
		queue:   make(chan eventlog.Event, defProfileEventQueue),
		stopped: make(chan struct{}),
	}
	go events.run()
	return events
}

func (a *asyncProfileEvents) Append(ctx context.Context, event eventlog.Event) (eventlog.Event, error) {
	if a == nil || a.next == nil {
		return eventlog.Event{}, errEventClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return eventlog.Event{}, err
	}
	event = cloneProfileEvent(event)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return eventlog.Event{}, errEventClosed
	}
	select {
	case a.queue <- event:
		return event, nil
	default:
		return eventlog.Event{}, errEventFull
	}
}

func (a *asyncProfileEvents) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
	closeCtx, cancel := a.closeContext(ctx)
	defer cancel()
	a.once.Do(func() {
		a.mu.Lock()
		a.closed = true
		close(a.queue)
		a.mu.Unlock()
	})
	select {
	case <-a.stopped:
		return nil
	case <-closeCtx.Done():
		return closeCtx.Err()
	}
}

func (a *asyncProfileEvents) run() {
	defer close(a.stopped)
	for event := range a.queue {
		ctx, cancel := context.WithTimeout(context.Background(), a.timeoutOrDefault())
		_, err := a.next.Append(ctx, event)
		cancel()
		if err != nil {
			a.recordError(event, err)
		}
	}
}

func (a *asyncProfileEvents) closeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, a.timeoutOrDefault())
}

func (a *asyncProfileEvents) timeoutOrDefault() time.Duration {
	if a == nil || a.timeout <= 0 {
		return profileEventTimeout
	}
	return a.timeout
}

func (a *asyncProfileEvents) recordError(event eventlog.Event, err error) {
	if a.errs != nil {
		atomic.AddUint64(a.errs, 1)
	}
	if a.logger != nil {
		a.logger.Error("player profile async eventlog append failed", "account_id", event.AggregateID, "event_type", event.Type, "error", err)
	}
}

func (m *Module) closeProfileEvents(ctx context.Context) error {
	if m == nil || m.eventAsync == nil {
		return nil
	}
	return m.eventAsync.Close(ctx)
}

func cloneProfileEvent(event eventlog.Event) eventlog.Event {
	if event.Payload != nil {
		event.Payload = append(json.RawMessage(nil), event.Payload...)
	}
	if event.Headers != nil {
		headers := make(map[string]string, len(event.Headers))
		for key, value := range event.Headers {
			headers[key] = value
		}
		event.Headers = headers
	}
	return event
}
