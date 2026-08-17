package eventlog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var (
	// ErrPublishWorkerConfig 表示发布 worker 配置缺少必要字段。
	ErrPublishWorkerConfig = errors.New("eventlog publish worker config is invalid")

	// ErrPublishWorkerStopping 表示 worker 正在停机，不能再次启动。
	ErrPublishWorkerStopping = errors.New("eventlog publish worker is stopping")
)

// PublishWorkerOptions 描述后台发布 worker 的日志、发布器和重试节奏。
type PublishWorkerOptions struct {
	Name           string
	Log            *Log
	Publisher      Publisher
	Interval       time.Duration
	Limit          int
	RetryDelay     time.Duration
	ClaimTimeout   time.Duration
	MaxAttempts    int
	ExcludeStreams []string
	Logger         *slog.Logger
}

// PublishWorker 周期性领取 eventlog 待发布事件并调用 Publisher。
type PublishWorker struct {
	name           string
	log            *Log
	publisher      Publisher
	interval       time.Duration
	limit          int
	retryDelay     time.Duration
	claimTimeout   time.Duration
	maxAttempts    int
	excludeStreams []string
	logger         *slog.Logger

	mu       sync.Mutex
	running  bool
	stopping bool
	cancel   context.CancelFunc
	done     chan struct{}
	stopped  chan struct{}
}

// NewPublishWorker 创建后台发布 worker 并补齐默认名称、间隔和重试延迟。
func NewPublishWorker(options PublishWorkerOptions) (*PublishWorker, error) {
	name := options.Name
	if name == "" {
		name = "eventlog-publisher"
	}
	if options.Log == nil {
		return nil, fmt.Errorf("%w: log is required", ErrPublishWorkerConfig)
	}
	if options.Publisher == nil {
		return nil, fmt.Errorf("%w: publisher is required", ErrPublishWorkerConfig)
	}
	interval := options.Interval
	if interval <= 0 {
		interval = time.Second
	}
	retryDelay := options.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Second
	}
	return &PublishWorker{
		name:           name,
		log:            options.Log,
		publisher:      options.Publisher,
		interval:       interval,
		limit:          options.Limit,
		retryDelay:     retryDelay,
		claimTimeout:   options.ClaimTimeout,
		maxAttempts:    options.MaxAttempts,
		excludeStreams: normalizeStreamList(options.ExcludeStreams),
		logger:         options.Logger,
	}, nil
}

// Name 返回 worker 名称，空实例返回默认名称。
func (w *PublishWorker) Name() string {
	if w == nil || w.name == "" {
		return "eventlog-publisher"
	}
	return w.name
}

// Start 启动后台发布循环；重复启动保持幂等。
func (w *PublishWorker) Start(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if w == nil || w.log == nil || w.publisher == nil {
		return ErrPublishWorkerConfig
	}
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	if w.stopping {
		w.mu.Unlock()
		return ErrPublishWorkerStopping
	}
	// Worker 使用独立后台 context，不继承启动请求的生命周期。
	// 停机时必须走 Stop，确保当前发布循环有机会收敛。
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

// Stop 请求后台发布循环退出并等待收敛。
func (w *PublishWorker) Stop(ctx context.Context) error {
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
	if stopped == nil {
		return nil
	}
	// Stop 等待 run 退出；超时后取消后台 context，避免进程关停被永久卡住。
	select {
	case <-stopped:
		if cancel != nil {
			cancel()
		}
		w.clearStopped(stopped)
		return nil
	case <-ctx.Done():
		if cancel != nil {
			cancel()
		}
		return ctx.Err()
	}
}

func (w *PublishWorker) run(ctx context.Context, done <-chan struct{}, stopped chan struct{}) {
	defer func() {
		close(stopped)
		w.clearStopped(stopped)
	}()
	// 启动后先跑一轮，避免 outbox 事件必须等到下一个 tick 才发布。
	w.publishOnce(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			w.publishOnce(ctx)
		}
	}
}

func (w *PublishWorker) clearStopped(stopped chan struct{}) {
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

func (w *PublishWorker) publishOnce(ctx context.Context) {
	// PublishPending 内部负责 claim、发布、重试、死信；Worker 只负责节拍和日志。
	stats, err := w.log.PublishPending(ctx, w.publisher, PublishOptions{
		Limit:          w.limit,
		RetryDelay:     w.retryDelay,
		ClaimTimeout:   w.claimTimeout,
		MaxAttempts:    w.maxAttempts,
		ExcludeStreams: w.excludeStreams,
	})
	if err != nil && w.logger != nil {
		w.logger.Error("eventlog publish failed", "worker", w.Name(), "fetched", stats.Fetched, "published", stats.Published, "failed", stats.Failed, "dead_lettered", stats.DeadLettered, "error", err)
		return
	}
	if stats.Published > 0 && w.logger != nil {
		w.logger.Info("eventlog published events", "worker", w.Name(), "published", stats.Published, "failed", stats.Failed, "dead_lettered", stats.DeadLettered)
	}
}
