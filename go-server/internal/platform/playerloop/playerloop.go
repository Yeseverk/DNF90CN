package playerloop

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/tracing"
)

var (
	// ErrStopped 表示玩家循环管理器未启动或已经停止。
	ErrStopped = errors.New("player loop manager is stopped")

	// ErrMissingAccount 表示提交事件时缺少账号 ID。
	ErrMissingAccount = errors.New("account id is required")

	// ErrQueueFull 表示目标玩家循环队列已经写满。
	ErrQueueFull = errors.New("player loop queue is full")
)

// Event 是按账号串行处理的玩家事件。
type Event struct {
	AccountID  string
	Payload    any
	ReceivedAt time.Time
}

// Handler 处理单个玩家事件，框架保证同一账号事件串行进入。
type Handler func(context.Context, Event) error

// Options 描述玩家循环的空闲回收和 handler 超时策略。
type Options struct {
	IdleTTL        time.Duration
	SweepInterval  time.Duration
	HandlerTimeout time.Duration
}

// Snapshot 是单个账号玩家循环的队列快照。
type Snapshot struct {
	AccountID string `json:"account_id"`
	Queued    int    `json:"queued"`
}

// Manager 按账号维护独立事件队列，保证同一账号状态修改串行执行。
type Manager struct {
	name    string
	queue   int
	handler Handler
	logger  *slog.Logger

	idleTTL        time.Duration
	sweepInterval  time.Duration
	handlerTimeout time.Duration

	mu      sync.RWMutex
	started bool
	loops   map[string]*loop
	active  *managerGeneration

	sweepCount        atomic.Uint64
	handlerErrorCount atomic.Uint64
}

// managerGeneration 持有一次 Start 创建的全部运行资源，避免旧 Stop 干扰新一代循环。
type managerGeneration struct {
	ctx         context.Context
	cancel      context.CancelFunc
	loops       map[string]*loop
	wg          sync.WaitGroup
	sweepCancel context.CancelFunc
	sweepDone   chan struct{}
}

type loop struct {
	accountID string
	ch        chan Event
	closeOnce sync.Once

	mu     sync.RWMutex
	closed bool
	// processing 防止 idle sweep 在 handler 已取出事件、但仍在修改账号状态时关闭 loop。
	processing bool
	lastActive time.Time
}

// New 使用默认选项创建玩家循环管理器。
func New(name string, queue int, handler Handler, logger *slog.Logger) *Manager {
	return NewWithOptions(name, queue, handler, logger, Options{})
}

// NewWithOptions 创建带空闲回收和 handler 超时配置的玩家循环管理器。
func NewWithOptions(name string, queue int, handler Handler, logger *slog.Logger, options Options) *Manager {
	if queue <= 0 {
		queue = 64
	}
	if options.SweepInterval <= 0 && options.IdleTTL > 0 {
		options.SweepInterval = options.IdleTTL / 2
		if options.SweepInterval <= 0 {
			options.SweepInterval = time.Second
		}
	}
	return &Manager{
		name:           name,
		queue:          queue,
		handler:        handler,
		logger:         logger,
		idleTTL:        options.IdleTTL,
		sweepInterval:  options.SweepInterval,
		handlerTimeout: options.HandlerTimeout,
		loops:          make(map[string]*loop),
	}
}

// Name 返回玩家循环管理器名称。
func (m *Manager) Name() string {
	return m.name
}

// Start 启动玩家循环管理器和可选空闲回收任务。
func (m *Manager) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	generation := &managerGeneration{
		ctx:    runCtx,
		cancel: runCancel,
		loops:  make(map[string]*loop),
	}
	m.started = true
	m.active = generation
	m.loops = generation.loops
	if m.idleTTL > 0 {
		sweepCtx, cancel := context.WithCancel(runCtx)
		generation.sweepCancel = cancel
		generation.sweepDone = make(chan struct{})
		go m.sweepIdleLoops(sweepCtx, generation)
	}
	return nil
}

// Stop 关闭所有账号循环并等待已取出的事件处理完成。
func (m *Manager) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var stopErr error
	recordCtxErr := func() {
		if stopErr == nil {
			stopErr = ctx.Err()
		}
	}
	m.mu.Lock()
	if !m.started || m.active == nil {
		m.mu.Unlock()
		return nil
	}
	generation := m.active
	loops := make([]*loop, 0, len(generation.loops))
	for _, playerLoop := range generation.loops {
		loops = append(loops, playerLoop)
	}
	m.started = false
	m.active = nil
	m.loops = make(map[string]*loop)
	m.mu.Unlock()

	if generation.sweepCancel != nil {
		generation.sweepCancel()
	}
	if generation.sweepDone != nil {
		select {
		case <-generation.sweepDone:
		case <-ctx.Done():
			recordCtxErr()
		}
	}

	for _, playerLoop := range loops {
		playerLoop.close()
	}

	done := make(chan struct{})
	go func() {
		generation.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		generation.cancel()
		return stopErr
	case <-ctx.Done():
		generation.cancel()
		recordCtxErr()
		return stopErr
	}
}

// Submit 将事件提交到指定账号的串行队列。
func (m *Manager) Submit(ctx context.Context, accountID string, payload any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return ErrMissingAccount
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	if !m.started || m.active == nil {
		m.mu.Unlock()
		return ErrStopped
	}
	generation := m.active
	playerLoop := generation.loops[accountID]
	if playerLoop == nil {
		playerLoop = &loop{
			accountID:  accountID,
			ch:         make(chan Event, m.queue),
			lastActive: time.Now().UTC(),
		}
		generation.loops[accountID] = playerLoop
		generation.wg.Add(1)
		go m.run(generation, playerLoop)
	}
	m.mu.Unlock()

	event := Event{
		AccountID:  accountID,
		Payload:    payload,
		ReceivedAt: time.Now().UTC(),
	}
	return playerLoop.submit(ctx, event)
}

// Snapshot 返回所有账号循环的队列长度快照。
func (m *Manager) Snapshot() []Snapshot {
	m.mu.RLock()
	out := make([]Snapshot, 0, len(m.loops))
	for accountID, playerLoop := range m.loops {
		out = append(out, Snapshot{
			AccountID: accountID,
			Queued:    len(playerLoop.ch),
		})
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].AccountID < out[j].AccountID
	})
	return out
}

func (m *Manager) run(generation *managerGeneration, playerLoop *loop) {
	defer generation.wg.Done()
	for event := range playerLoop.ch {
		m.handle(generation.ctx, playerLoop, event)
	}
}

func (m *Manager) handle(ctx context.Context, playerLoop *loop, event Event) {
	playerLoop.markProcessing(true)
	defer playerLoop.markProcessing(false)

	if m.handler == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	handlerCtx := ctx
	var cancel context.CancelFunc
	if m.handlerTimeout > 0 {
		handlerCtx, cancel = context.WithTimeout(ctx, m.handlerTimeout)
	}
	if cancel != nil {
		defer cancel()
	}
	defer func() {
		if rec := recover(); rec != nil {
			m.handlerErrorCount.Add(1)
			if m.logger != nil {
				tracing.LogPanic(handlerCtx, m.logger, "playerloop.handler", rec, map[string]string{"account_id": event.AccountID})
			}
		}
	}()
	if err := m.handler(handlerCtx, event); err != nil {
		m.handlerErrorCount.Add(1)
		if m.logger != nil && !errors.Is(err, context.Canceled) {
			m.logger.Error("player loop handler failed", "account_id", event.AccountID, "error", err)
		}
	}
}

func (m *Manager) sweepIdleLoops(ctx context.Context, generation *managerGeneration) {
	defer close(generation.sweepDone)
	ticker := time.NewTicker(m.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.reclaimGenLoops(generation, time.Now().UTC())
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) reclaimIdleLoops(now time.Time) {
	m.mu.RLock()
	generation := m.active
	m.mu.RUnlock()
	m.reclaimGenLoops(generation, now)
}

func (m *Manager) reclaimGenLoops(generation *managerGeneration, now time.Time) {
	if m.idleTTL <= 0 {
		return
	}
	m.sweepCount.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started || m.active != generation {
		return
	}
	for accountID, playerLoop := range generation.loops {
		if !playerLoop.canReclaim(now, m.idleTTL) {
			continue
		}
		delete(generation.loops, accountID)
		playerLoop.close()
	}
}

func (l *loop) close() {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		close(l.ch)
		l.mu.Unlock()
	})
}

func (l *loop) submit(ctx context.Context, event Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrStopped
	}
	select {
	case l.ch <- event:
		if event.ReceivedAt.After(l.lastActive) {
			l.lastActive = event.ReceivedAt
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrQueueFull
	}
}

func (l *loop) markProcessing(processing bool) {
	l.mu.Lock()
	l.processing = processing
	if !processing {
		l.lastActive = time.Now().UTC()
	}
	l.mu.Unlock()
}

func (l *loop) canReclaim(now time.Time, idleTTL time.Duration) bool {
	l.mu.RLock()
	processing := l.processing
	lastActive := l.lastActive
	l.mu.RUnlock()
	if processing || len(l.ch) > 0 || lastActive.IsZero() {
		return false
	}
	return now.Sub(lastActive) >= idleTTL
}
