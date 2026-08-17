package admincmdqueue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"longheng.io/server/internal/platform/eventlog"
)

var (
	ErrWorkerConfig   = errors.New("admin command worker config is invalid")
	ErrWorkerStopping = errors.New("admin command worker is stopping")
)

type WorkerOptions struct {
	Name         string
	Executor     *Executor
	Handler      Handler
	Interval     time.Duration
	Limit        int
	RetryDelay   time.Duration
	ClaimTimeout time.Duration
	MaxAttempts  int
	Logger       *slog.Logger
}

type Worker struct {
	name         string
	executor     *Executor
	handler      Handler
	interval     time.Duration
	limit        int
	retryDelay   time.Duration
	claimTimeout time.Duration
	maxAttempts  int
	logger       *slog.Logger

	mu       sync.Mutex
	running  bool
	stopping bool
	cancel   context.CancelFunc
	done     chan struct{}
	stopped  chan struct{}
}

func NewWorker(options WorkerOptions) (*Worker, error) {
	name := options.Name
	if name == "" {
		name = "admin-command-worker"
	}
	if options.Executor == nil {
		return nil, fmt.Errorf("%w: executor is required", ErrWorkerConfig)
	}
	if options.Handler == nil {
		return nil, fmt.Errorf("%w: handler is required", ErrWorkerConfig)
	}
	interval := options.Interval
	if interval <= 0 {
		interval = time.Second
	}
	retryDelay := options.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Second
	}
	return &Worker{
		name:         name,
		executor:     options.Executor,
		handler:      options.Handler,
		interval:     interval,
		limit:        options.Limit,
		retryDelay:   retryDelay,
		claimTimeout: options.ClaimTimeout,
		maxAttempts:  options.MaxAttempts,
		logger:       options.Logger,
	}, nil
}

func (w *Worker) Name() string {
	if w == nil || w.name == "" {
		return "admin-command-worker"
	}
	return w.name
}

func (w *Worker) Start(ctx context.Context) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if w == nil || w.executor == nil || w.handler == nil {
		return ErrWorkerConfig
	}
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	if w.stopping {
		w.mu.Unlock()
		return ErrWorkerStopping
	}
	runCtx, cancel := context.WithCancel(context.Background())
	w.done = make(chan struct{})
	w.stopped = make(chan struct{})
	w.running = true
	w.cancel = cancel
	done := w.done
	stopped := w.stopped
	w.mu.Unlock()

	go w.run(runCtx, done, stopped)
	return nil
}

func (w *Worker) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if !w.running && !w.stopping {
		w.mu.Unlock()
		return nil
	}
	done := w.done
	stopped := w.stopped
	cancel := w.cancel
	shouldSignal := w.running
	if shouldSignal {
		w.cancel = nil
		w.running = false
		w.stopping = true
	}
	w.mu.Unlock()

	if shouldSignal && done != nil {
		close(done)
	}
	if shouldSignal && cancel != nil {
		cancel()
	}
	if stopped == nil {
		return nil
	}
	select {
	case <-stopped:
		w.clearStopped(stopped)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) RunOnce(ctx context.Context) (eventlog.PublishStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if w == nil || w.executor == nil || w.handler == nil {
		return eventlog.PublishStats{}, ErrWorkerConfig
	}
	stats, err := w.executor.PublishPending(ctx, w.handler, eventlog.PublishOptions{
		Limit:        w.limit,
		RetryDelay:   w.retryDelay,
		ClaimTimeout: w.claimTimeout,
		MaxAttempts:  w.maxAttempts,
	})
	if err != nil && w.logger != nil {
		w.logger.Error("admin command publish failed", "worker", w.Name(), "fetched", stats.Fetched, "published", stats.Published, "failed", stats.Failed, "dead_lettered", stats.DeadLettered, "error", err)
		return stats, err
	}
	if stats.Published > 0 && w.logger != nil {
		w.logger.Info("admin command published", "worker", w.Name(), "published", stats.Published, "failed", stats.Failed, "dead_lettered", stats.DeadLettered)
	}
	return stats, err
}

func (w *Worker) run(ctx context.Context, done <-chan struct{}, stopped chan struct{}) {
	defer func() {
		w.clearStopped(stopped)
		close(stopped)
	}()
	_, _ = w.RunOnce(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_, _ = w.RunOnce(ctx)
		}
	}
}

func (w *Worker) clearStopped(stopped chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped != stopped {
		return
	}
	w.running = false
	w.stopping = false
	w.cancel = nil
	w.done = nil
	w.stopped = nil
}
