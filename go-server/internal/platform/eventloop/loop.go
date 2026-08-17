package eventloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrLoopRequired 表示事件循环实例为空。
	ErrLoopRequired = errors.New("eventloop: loop is required")
	// ErrNoProcessor 表示事件循环缺少处理器。
	ErrNoProcessor = errors.New("eventloop: processor is required")
	// ErrNoSliceStep 表示时间片执行缺少 step 回调。
	ErrNoSliceStep = errors.New("eventloop: slice step is required")
	// ErrInvalidSliceRun 表示时间片执行缺少最大处理数量或截止时间。
	ErrInvalidSliceRun = errors.New("eventloop: slice run requires max items or deadline")
	// ErrNotStarted 表示事件循环尚未启动。
	ErrNotStarted = errors.New("eventloop: loop is not started")
	// ErrClosed 表示事件循环已经关闭。
	ErrClosed = errors.New("eventloop: loop is closed")
	// ErrDraining 表示事件循环正在排空队列。
	ErrDraining = errors.New("eventloop: loop is draining")
	// ErrQueueFull 表示目标优先级队列已满。
	ErrQueueFull = errors.New("eventloop: queue is full")
	// ErrUnknownPriority 表示事件优先级不在框架支持范围内。
	ErrUnknownPriority = errors.New("eventloop: unknown priority")
)

const (
	// DefaultFrameInterval 是帧驱动事件循环的默认帧间隔。
	DefaultFrameInterval = 50 * time.Millisecond
	// DefaultFrameBudget 是单帧可用于处理事件的默认预算。
	DefaultFrameBudget = 25 * time.Millisecond
	// DefaultMaxLowTime 是单帧处理低优先级事件的默认时间上限。
	DefaultMaxLowTime = 2 * time.Millisecond
	// DefaultSlowThreshold 是慢事件告警的默认阈值。
	DefaultSlowThreshold = time.Millisecond
)

// Priority 描述事件队列优先级。
type Priority int

const (
	// PriorityMedium 是默认优先级，适合普通业务事件。
	PriorityMedium Priority = iota
	// PriorityHigh 适合登录、支付回调等需要优先处理的事件。
	PriorityHigh
	// PriorityLow 适合可延后处理的后台事件。
	PriorityLow
)

// String 返回优先级的稳定标签。
func (p Priority) String() string {
	switch p {
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	case PriorityLow:
		return "low"
	default:
		return "unknown"
	}
}

// Result 表示事件处理器返回的数据或错误。
type Result struct {
	Data any
	Err  error
}

// Event 是提交到事件循环中的最小调度单元。
type Event struct {
	Type     string
	Data     any
	Metadata map[string]string
	Priority Priority
	Callback chan Result
	Context  context.Context
	SentAt   time.Time
}

// Processor 是事件循环消费事件时调用的处理器接口。
type Processor interface {
	Process(context.Context, Event) Result
}

// ProcessorFunc 允许普通函数直接作为事件处理器使用。
type ProcessorFunc func(context.Context, Event) Result

// Process 调用底层函数处理事件。
func (f ProcessorFunc) Process(ctx context.Context, event Event) Result {
	return f(ctx, event)
}

// Options 配置事件循环的队列容量、帧预算、回调和日志行为。
type Options struct {
	QueueSize         int
	CallbackQueueSize int
	CallbackTimeout   time.Duration
	FrameDriven       bool
	FrameInterval     time.Duration
	FrameBudget       time.Duration
	MaxLowTime        time.Duration
	HighBurst         int
	MediumBurst       int
	SlowThreshold     time.Duration
	Logger            *slog.Logger
}

func (o Options) normalized() Options {
	if o.QueueSize <= 0 {
		o.QueueSize = 1024
	}
	if o.CallbackQueueSize <= 0 {
		o.CallbackQueueSize = 1024
	}
	if o.CallbackTimeout <= 0 {
		o.CallbackTimeout = 500 * time.Millisecond
	}
	if o.FrameInterval <= 0 {
		o.FrameInterval = DefaultFrameInterval
	}
	if o.FrameBudget <= 0 {
		o.FrameBudget = o.FrameInterval / 2
		if o.FrameBudget <= 0 {
			o.FrameBudget = DefaultFrameBudget
		}
	}
	if o.MaxLowTime <= 0 {
		o.MaxLowTime = DefaultMaxLowTime
		if o.MaxLowTime > o.FrameBudget {
			o.MaxLowTime = o.FrameBudget / 4
			if o.MaxLowTime <= 0 {
				o.MaxLowTime = o.FrameBudget
			}
		}
	}
	if o.HighBurst <= 0 {
		o.HighBurst = 64
	}
	if o.MediumBurst <= 0 {
		o.MediumBurst = 32
	}
	if o.SlowThreshold <= 0 {
		o.SlowThreshold = DefaultSlowThreshold
	}
	return o
}

// QueueLengths 描述各优先级队列和回调队列的当前长度。
type QueueLengths struct {
	High     int
	Medium   int
	Low      int
	Callback int
}

// MetricsSnapshot 是事件循环内部计数器的瞬时快照。
type MetricsSnapshot struct {
	Submitted       map[Priority]uint64
	Dropped         map[Priority]uint64
	Processed       map[Priority]uint64
	Failed          map[Priority]uint64
	Panicked        map[Priority]uint64
	Slow            map[Priority]uint64
	QueueWaitNanos  map[Priority]uint64
	ProcessingNanos map[Priority]uint64
	CallbackDropped uint64
}

type metricSet struct {
	submitted       [3]atomic.Uint64
	dropped         [3]atomic.Uint64
	processed       [3]atomic.Uint64
	failed          [3]atomic.Uint64
	panicked        [3]atomic.Uint64
	slow            [3]atomic.Uint64
	queueWaitNanos  [3]atomic.Uint64
	processingNanos [3]atomic.Uint64
	callbackDropped atomic.Uint64
}

type callbackItem struct {
	ctx      context.Context
	callback chan Result
	result   Result
}

type Loop struct {
	name      string
	processor Processor
	options   Options
	logger    *slog.Logger

	high      chan Event
	medium    chan Event
	low       chan Event
	callbacks chan callbackItem
	wake      chan struct{}

	submitMu  sync.Mutex
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	started   bool
	closed    bool
	wg        sync.WaitGroup
	running   atomic.Bool
	accepting atomic.Bool
	draining  atomic.Bool
	metrics   metricSet
}

// New 创建一个事件循环实例，调用 Start 后才会开始消费事件。
func New(name string, processor Processor, options Options) *Loop {
	options = options.normalized()
	if name == "" {
		name = "eventloop"
	}
	return &Loop{
		name:      name,
		processor: processor,
		options:   options,
		logger:    options.Logger,
		high:      make(chan Event, options.QueueSize),
		medium:    make(chan Event, options.QueueSize),
		low:       make(chan Event, options.QueueSize),
		callbacks: make(chan callbackItem, options.CallbackQueueSize),
		wake:      make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
}

// Start 启动事件循环和回调派发协程。
func (l *Loop) Start(ctx context.Context) error {
	if l == nil {
		return ErrLoopRequired
	}
	if l.processor == nil {
		return ErrNoProcessor
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}
	if l.started {
		return nil
	}
	l.ctx, l.cancel = context.WithCancel(ctx)
	l.done = make(chan struct{})
	l.wake = make(chan struct{}, 1)
	l.started = true
	l.running.Store(true)
	l.accepting.Store(true)
	l.draining.Store(false)

	l.wg.Add(2)
	if l.options.FrameDriven {
		// 帧驱动模式按固定 tick 频率消费事件。
		go l.runFrameDriven()
	} else {
		// 优先级驱动模式用于控制面和异步任务。
		go l.runPriorityDriven()
	}
	go l.dispatchCallbacks()
	go func() {
		l.wg.Wait()
		l.running.Store(false)
		l.accepting.Store(false)
		l.mu.Lock()
		l.closed = true
		l.mu.Unlock()
		close(l.done)
	}()
	return nil
}

// Stop 立即停止事件循环，并拒绝仍在队列中的事件。
func (l *Loop) Stop(ctx context.Context) error {
	if l == nil {
		return ErrLoopRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	if !l.started {
		l.mu.Unlock()
		return ErrNotStarted
	}
	done := l.done
	l.mu.Unlock()
	l.submitMu.Lock()
	l.mu.Lock()
	cancel := l.cancel
	l.closed = true
	l.mu.Unlock()
	// Stop 会立即停止；如果队列事件必须执行完，应使用 Drain。
	l.accepting.Store(false)
	l.draining.Store(false)
	l.rejectQueued(ErrClosed)
	if cancel != nil {
		cancel()
	}
	l.submitMu.Unlock()
	select {
	case <-done:
		l.drainCallbacks()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Drain 停止接收新事件，并等待已排队事件处理完成。
func (l *Loop) Drain(ctx context.Context) error {
	if l == nil {
		return ErrLoopRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	if !l.started {
		l.mu.Unlock()
		return ErrNotStarted
	}
	done := l.done
	l.mu.Unlock()
	l.submitMu.Lock()
	l.accepting.Store(false)
	l.draining.Store(true)
	l.submitMu.Unlock()
	l.wakeLoop()

	// Drain 停止接收新事件，并处理完已排队事件。
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Submit 快速提交事件；队列满时立即返回 ErrQueueFull。
func (l *Loop) Submit(event Event) error {
	if l == nil {
		return ErrLoopRequired
	}
	l.submitMu.Lock()
	defer l.submitMu.Unlock()
	if err := l.prepareEvent(&event); err != nil {
		return err
	}
	queue := l.queueFor(event.Priority)
	select {
	case queue <- event:
		l.metrics.submitted[event.Priority].Add(1)
		return nil
	default:
		// Submit 是快速失败入口；调用方自行选择丢弃、降级或改用 SubmitBlocking。
		l.metrics.dropped[event.Priority].Add(1)
		return ErrQueueFull
	}
}

// SubmitBlocking 等待队列容量，直到提交成功、ctx 取消或事件循环关闭。
func (l *Loop) SubmitBlocking(ctx context.Context, event Event) error {
	if l == nil {
		return ErrLoopRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	retryTimer := time.NewTimer(time.Millisecond)
	if !retryTimer.Stop() {
		<-retryTimer.C
	}
	defer retryTimer.Stop()
	// SubmitBlocking 会等待队列容量、ctx 取消或 loop 关闭。
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.submitMu.Lock()
		if err := l.prepareEvent(&event); err != nil {
			l.submitMu.Unlock()
			return err
		}
		queue := l.queueFor(event.Priority)
		select {
		case queue <- event:
			l.metrics.submitted[event.Priority].Add(1)
			l.submitMu.Unlock()
			return nil
		default:
			l.submitMu.Unlock()
		}
		select {
		case <-ctx.Done():
			if validPriority(event.Priority) {
				l.metrics.dropped[event.Priority].Add(1)
			}
			return ctx.Err()
		case <-l.contextDone():
			if validPriority(event.Priority) {
				l.metrics.dropped[event.Priority].Add(1)
			}
			return ErrClosed
		case <-resetTimer(retryTimer, time.Millisecond):
		}
	}
}

// QueueLengths 返回当前队列长度。
func (l *Loop) QueueLengths() QueueLengths {
	if l == nil {
		return QueueLengths{}
	}
	return QueueLengths{
		High:     len(l.high),
		Medium:   len(l.medium),
		Low:      len(l.low),
		Callback: len(l.callbacks),
	}
}

// MetricsSnapshot 返回事件循环累计指标快照。
func (l *Loop) MetricsSnapshot() MetricsSnapshot {
	if l == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		Submitted:       l.loadPriorityCounters(&l.metrics.submitted),
		Dropped:         l.loadPriorityCounters(&l.metrics.dropped),
		Processed:       l.loadPriorityCounters(&l.metrics.processed),
		Failed:          l.loadPriorityCounters(&l.metrics.failed),
		Panicked:        l.loadPriorityCounters(&l.metrics.panicked),
		Slow:            l.loadPriorityCounters(&l.metrics.slow),
		QueueWaitNanos:  l.loadPriorityCounters(&l.metrics.queueWaitNanos),
		ProcessingNanos: l.loadPriorityCounters(&l.metrics.processingNanos),
		CallbackDropped: l.metrics.callbackDropped.Load(),
	}
}

func (l *Loop) prepareEvent(event *Event) error {
	if l.isClosed() {
		return ErrClosed
	}
	if !l.running.Load() {
		return ErrNotStarted
	}
	if l.draining.Load() {
		return ErrDraining
	}
	if !l.accepting.Load() {
		return ErrClosed
	}
	if !validPriority(event.Priority) {
		return ErrUnknownPriority
	}
	if event.SentAt.IsZero() {
		event.SentAt = time.Now()
	}
	return nil
}

func (l *Loop) queueFor(priority Priority) chan Event {
	switch priority {
	case PriorityHigh:
		return l.high
	case PriorityMedium:
		return l.medium
	default:
		return l.low
	}
}

func (l *Loop) contextDone() <-chan struct{} {
	l.mu.Lock()
	ctx := l.ctx
	l.mu.Unlock()
	if ctx == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return ctx.Done()
}

func (l *Loop) runPriorityDriven() {
	defer l.wg.Done()
	for {
		if l.shouldStopAfterDrain() {
			return
		}
		highProcessed := l.processBurst(l.high, l.options.HighBurst)
		mediumProcessed := l.processBurst(l.medium, l.options.MediumBurst)
		if event, ok := l.tryReceive(l.low); ok {
			l.processEvent(event)
			continue
		}
		if highProcessed || mediumProcessed {
			continue
		}
		select {
		case <-l.ctx.Done():
			return
		case <-l.wake:
			continue
		case event := <-l.high:
			l.processEvent(event)
		case event := <-l.medium:
			_ = l.processBurst(l.high, l.options.HighBurst)
			l.processEvent(event)
		case event := <-l.low:
			_ = l.processBurst(l.high, l.options.HighBurst)
			_ = l.processBurst(l.medium, l.options.MediumBurst)
			l.processEvent(event)
		}
	}
}
func (l *Loop) runFrameDriven() {
	defer l.wg.Done()
	ticker := time.NewTicker(l.options.FrameInterval)
	defer ticker.Stop()
	for {
		if l.shouldStopAfterDrain() {
			return
		}
		select {
		case <-l.ctx.Done():
			return
		case <-l.wake:
			continue
		case <-ticker.C:
			l.processFrame()
		}
	}
}

func (l *Loop) processFrame() {
	frameStart := time.Now()
	frameDeadline := frameStart.Add(l.options.FrameBudget)
	lowDeadline := frameStart.Add(l.options.MaxLowTime)
	// 每一帧只在配置的预算内消费事件。
	for time.Now().Before(frameDeadline) {
		if event, ok := l.tryReceive(l.high); ok {
			l.processEvent(event)
			continue
		}
		if event, ok := l.tryReceive(l.medium); ok {
			l.processEvent(event)
			continue
		}
		if time.Now().Before(lowDeadline) {
			if event, ok := l.tryReceive(l.low); ok {
				l.processEvent(event)
				continue
			}
		}
		return
	}
}

func (l *Loop) processBurst(queue chan Event, limit int) bool {
	processed := false
	for i := 0; i < limit; i++ {
		event, ok := l.tryReceive(queue)
		if !ok {
			return processed
		}
		processed = true
		l.processEvent(event)
	}
	return processed
}

func (l *Loop) tryReceive(queue chan Event) (Event, bool) {
	select {
	case event := <-queue:
		return event, true
	default:
		return Event{}, false
	}
}

func (l *Loop) processEvent(event Event) {
	ctx := event.Context
	if ctx == nil {
		ctx = l.ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		l.recordFailure(event.Priority)
		l.deliverCallback(event, Result{Err: err})
		return
	}

	start := time.Now()
	if !event.SentAt.IsZero() {
		if wait := start.Sub(event.SentAt); wait > 0 {
			l.metrics.queueWaitNanos[event.Priority].Add(uint64(wait))
		}
	}
	result := Result{}
	panicked := false
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				panicked = true
				result.Err = fmt.Errorf("eventloop: processor panic: %v", recovered)
				l.logWarn("event processor panic recovered", slog.Any("panic", recovered), slog.String("stack", string(debug.Stack())))
			}
		}()
		result = l.processor.Process(ctx, event)
	}()
	duration := time.Since(start)
	l.recordProcessed(event.Priority, duration, result.Err, panicked)
	l.deliverCallback(event, result)
}

func (l *Loop) deliverCallback(event Event, result Result) {
	if event.Callback == nil {
		return
	}
	if l.isClosed() {
		l.deliverCallbackNow(event, result)
		return
	}
	ctx := event.Context
	if ctx == nil {
		ctx = l.ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	item := callbackItem{ctx: ctx, callback: event.Callback, result: result}
	select {
	case l.callbacks <- item:
	default:
		l.metrics.callbackDropped.Add(1)
		l.logWarn("event callback queue is full; callback dropped", slog.String("event", event.Type))
	}
}

func (l *Loop) dispatchCallbacks() {
	defer l.wg.Done()
	for {
		select {
		case <-l.ctx.Done():
			l.drainCallbacks()
			return
		case item := <-l.callbacks:
			l.sendCallback(item)
		}
	}
}

func (l *Loop) drainCallbacks() {
	for {
		select {
		case item := <-l.callbacks:
			l.deliverItemNow(item)
		default:
			return
		}
	}
}

func (l *Loop) deliverItemNow(item callbackItem) {
	select {
	case item.callback <- item.result:
	default:
		l.metrics.callbackDropped.Add(1)
	}
}

func (l *Loop) sendCallback(item callbackItem) {
	ctx := item.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if l.options.CallbackTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.options.CallbackTimeout)
		defer cancel()
	}
	select {
	case item.callback <- item.result:
		return
	default:
	}
	select {
	case item.callback <- item.result:
	case <-ctx.Done():
		l.metrics.callbackDropped.Add(1)
	}
}

func (l *Loop) shouldStopAfterDrain() bool {
	if !l.draining.Load() {
		return false
	}
	if len(l.high) > 0 || len(l.medium) > 0 || len(l.low) > 0 {
		return false
	}
	if len(l.callbacks) > 0 {
		time.Sleep(time.Millisecond)
		return false
	}
	// 队列清空后再 cancel，让 run loop 自然退出。
	if l.cancel != nil {
		l.cancel()
	}
	return true
}

func (l *Loop) wakeLoop() {
	if l == nil || l.wake == nil {
		return
	}
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

func (l *Loop) rejectQueued(err error) {
	reject := func(queue chan Event) {
		for {
			select {
			case event := <-queue:
				l.deliverCallbackNow(event, Result{Err: err})
			default:
				return
			}
		}
	}
	reject(l.high)
	reject(l.medium)
	reject(l.low)
}

func (l *Loop) deliverCallbackNow(event Event, result Result) {
	if event.Callback == nil {
		return
	}
	select {
	case event.Callback <- result:
	default:
		l.metrics.callbackDropped.Add(1)
	}
}

func (l *Loop) recordProcessed(priority Priority, duration time.Duration, err error, panicked bool) {
	if !validPriority(priority) {
		return
	}
	l.metrics.processed[priority].Add(1)
	if duration > 0 {
		l.metrics.processingNanos[priority].Add(uint64(duration)) //nolint:gosec // G115：duration 已确认非负，纳秒值按 uint64 累加。
	}
	if err != nil {
		l.metrics.failed[priority].Add(1)
	}
	if panicked {
		l.metrics.panicked[priority].Add(1)
	}
	if l.options.SlowThreshold > 0 && duration >= l.options.SlowThreshold {
		l.metrics.slow[priority].Add(1)
		l.logWarn("event processing exceeded slow threshold", slog.String("priority", priority.String()), slog.Duration("duration", duration))
	}
}

func (l *Loop) recordFailure(priority Priority) {
	if !validPriority(priority) {
		return
	}
	l.metrics.processed[priority].Add(1)
	l.metrics.failed[priority].Add(1)
}

func (l *Loop) loadPriorityCounters(counters *[3]atomic.Uint64) map[Priority]uint64 {
	return map[Priority]uint64{
		PriorityHigh:   counters[PriorityHigh].Load(),
		PriorityMedium: counters[PriorityMedium].Load(),
		PriorityLow:    counters[PriorityLow].Load(),
	}
}

func (l *Loop) logWarn(message string, attrs ...slog.Attr) {
	if l.logger == nil {
		return
	}
	args := make([]any, 0, len(attrs)*2+2)
	args = append(args, slog.String("loop", l.name))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	l.logger.Warn(message, args...)
}

func (l *Loop) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

func validPriority(priority Priority) bool {
	return priority == PriorityHigh || priority == PriorityMedium || priority == PriorityLow
}

func resetTimer(timer *time.Timer, duration time.Duration) <-chan time.Time {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
	return timer.C
}
