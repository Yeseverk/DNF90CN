package bus

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"longheng.io/server/internal/platform/tracing"
	"longheng.io/server/pkg/contracts"
)

// ErrClosed 表示 bus 或订阅已经关闭，调用方应停止继续投递或订阅。
var ErrClosed = errors.New("bus is closed")

// ErrCloseTimeout 表示关闭阶段仍有 handler 未在预算内收敛。
var ErrCloseTimeout = errors.New("bus close timed out waiting for handlers")

// ErrHandlerRequired 表示订阅入口缺少有效 handler，避免消息到达后才 panic。
var ErrHandlerRequired = errors.New("bus handler is required")

// ErrUnsupported 表示当前适配器不支持该能力，例如 Redis Pub/Sub 不支持 queue group。
var ErrUnsupported = errors.New("bus operation is not supported by this adapter")

type Envelope struct {
	Topic       string            `json:"topic"`
	Payload     any               `json:"-"`
	PublishedAt time.Time         `json:"published_at"`
	Reply       string            `json:"reply,omitempty"`
	RequestID   string            `json:"request_id,omitempty"`
	Trace       map[string]string `json:"trace,omitempty"`
}

type Handler func(context.Context, Envelope) error

type Subscription interface {
	Close() error
}

// Bus 是实时发布订阅抽象。内置 memory、Redis Pub/Sub 和 NATS core 适配器都不是持久化消息。
// 关键状态变更必须走 EventLog/Outbox，或使用带显式确认语义的 stream 适配器。
type Bus interface {
	Publish(context.Context, string, any) error
	Request(context.Context, string, any, time.Duration) ([]byte, error)
	Subscribe(string, Handler) (Subscription, error)
	QueueSubscribe(string, string, Handler) (Subscription, error)
	Close() error
}

const defHandlerTimeout = 5 * time.Second
const defaultCloseTimeout = 5 * time.Second

type MemoryOptions struct {
	HandlerTimeout time.Duration
	CloseTimeout   time.Duration
	Metrics        *Metrics
}

type Memory struct {
	logger  *slog.Logger
	options MemoryOptions
	metrics *Metrics

	mu     sync.RWMutex
	closed bool
	nextID int
	subs   map[string]map[int]Handler
	queues map[string]map[string]map[int]Handler
	next   map[string]map[string]int

	deliveries sync.WaitGroup
}

func NewMemory(logger *slog.Logger) *Memory {
	return NewMemoryWithOptions(logger, MemoryOptions{HandlerTimeout: defHandlerTimeout})
}

func NewMemoryWithOptions(logger *slog.Logger, options MemoryOptions) *Memory {
	if options.HandlerTimeout < 0 {
		options.HandlerTimeout = 0
	}
	if options.CloseTimeout == 0 {
		options.CloseTimeout = defaultCloseTimeout
	}
	if options.CloseTimeout < 0 {
		options.CloseTimeout = 0
	}
	return &Memory{
		logger:  logger,
		options: options,
		metrics: options.Metrics,
		subs:    make(map[string]map[int]Handler),
		queues:  make(map[string]map[string]map[int]Handler),
		next:    make(map[string]map[string]int),
	}
}

// SetMetrics 在构造后替换指标发射器。平台装配先创建 bus、后创建 metrics 时会用到。
// 该方法可与 Publish/Subscribe 并发调用，指针替换受 bus 锁保护。
func (b *Memory) SetMetrics(m *Metrics) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.metrics = m
	b.options.Metrics = m
	b.mu.Unlock()
}

func (b *Memory) Publish(ctx context.Context, topic string, payload any) error {
	return b.publishEnvelope(ctx, Envelope{
		Topic:       topic,
		Payload:     payload,
		PublishedAt: time.Now().UTC(),
	})
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (b *Memory) publishEnvelope(ctx context.Context, env Envelope) error {
	if err := ctxErr(ctx); err != nil {
		if b != nil {
			b.mu.Lock()
			b.ensureReadyLocked()
			metricsRef := b.metrics
			b.mu.Unlock()
			metricsRef.recordPublish(classifyHandlerError(err))
		}
		return err
	}
	if b == nil {
		return ErrClosed
	}
	topic := env.Topic
	b.mu.Lock()
	b.ensureReadyLocked()
	if b.closed {
		metricsRef := b.metrics
		b.mu.Unlock()
		metricsRef.recordPublish(ResultClosed)
		return ErrClosed
	}
	rawHandlers := b.subs[topic]
	handlers := make([]Handler, 0, len(rawHandlers))
	for _, handler := range rawHandlers {
		handlers = append(handlers, handler)
	}
	handlers = append(handlers, b.queueHandlersLocked(topic)...)
	if len(handlers) > 0 {
		// Add 在释放锁前完成，Close 等待投递收敛时不会漏掉已经选中的 handler。
		b.deliveries.Add(len(handlers))
	}
	metricsRef := b.metrics
	logger := b.logger
	handlerTimeout := b.options.HandlerTimeout
	b.mu.Unlock()

	if env.Topic == "" {
		env.Topic = topic
	}
	if env.PublishedAt.IsZero() {
		env.PublishedAt = time.Now().UTC()
	}
	env = envelopeWithTrace(ctx, env)
	env.Payload = cloneBusPayload(env.Payload)
	deliveryParent := context.Background()
	var deliveryDeadline time.Time
	var hasDeliveryDeadline bool
	if ctx != nil {
		// 发布取消不直接杀掉订阅者；deadline 仍保留，避免慢 handler 脱离调用预算。
		deliveryParent = context.WithoutCancel(ctx)
		deliveryDeadline, hasDeliveryDeadline = ctx.Deadline()
	}
	deliveryParent = contextWithTrace(deliveryParent, env)

	for _, handler := range handlers {
		go func(h Handler) {
			defer b.deliveries.Done()
			handlerEnv := cloneEnvelope(env)
			parent := deliveryParent
			cancel := func() {}
			if hasDeliveryDeadline {
				parent, cancel = context.WithDeadline(parent, deliveryDeadline)
			}
			defer cancel()
			deliverWithDeadline(parent, logger, topic, handlerTimeout, handlerEnv, h, metricsRef)
		}(handler)
	}
	metricsRef.recordPublish(ResultOK)
	metricsRef.recordPublishBytes(payloadSize(env.Payload))
	return nil
}

func (b *Memory) queueHandlersLocked(topic string) []Handler {
	topicQueues := b.queues[topic]
	if len(topicQueues) == 0 {
		return nil
	}
	out := make([]Handler, 0, len(topicQueues))
	for queue, queueHandlers := range topicQueues {
		if len(queueHandlers) == 0 {
			continue
		}
		ids := make([]int, 0, len(queueHandlers))
		for id := range queueHandlers {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		if _, ok := b.next[topic]; !ok {
			b.next[topic] = make(map[string]int)
		}
		// 同一 queue 内轮询选择一个订阅者，模拟工作队列语义，而不是广播给所有实例。
		idx := b.next[topic][queue] % len(ids)
		b.next[topic][queue]++
		out = append(out, queueHandlers[ids[idx]])
	}
	return out
}

func cloneEnvelope(env Envelope) Envelope {
	env.Payload = cloneBusPayload(env.Payload)
	if len(env.Trace) > 0 {
		traceCopy := make(map[string]string, len(env.Trace))
		for key, value := range env.Trace {
			traceCopy[key] = value
		}
		env.Trace = traceCopy
	}
	return env
}

func cloneBusPayload(payload any) any {
	switch value := payload.(type) {
	case []byte:
		return cloneBytes(value)
	case json.RawMessage:
		return json.RawMessage(cloneBytes(value))
	case []string:
		return cloneStrings(value)
	case []any:
		return cloneAnySlice(value)
	case map[string]string:
		return cloneStringMap(value)
	case map[string]any:
		return cloneAnyMap(value)
	case contracts.GatewayClientPacket:
		value.Body = cloneBytes(value.Body)
		value.Metadata = cloneStringMap(value.Metadata)
		return value
	case *contracts.GatewayClientPacket:
		if value == nil {
			return value
		}
		cloned := cloneBusPayload(*value).(contracts.GatewayClientPacket)
		return &cloned
	case contracts.LogicPlayerResponse:
		value.Body = cloneBytes(value.Body)
		value.Metadata = cloneStringMap(value.Metadata)
		return value
	case *contracts.LogicPlayerResponse:
		if value == nil {
			return value
		}
		cloned := cloneBusPayload(*value).(contracts.LogicPlayerResponse)
		return &cloned
	case contracts.GatewayPush:
		value.Body = cloneBytes(value.Body)
		value.Metadata = cloneStringMap(value.Metadata)
		return value
	case *contracts.GatewayPush:
		if value == nil {
			return value
		}
		cloned := cloneBusPayload(*value).(contracts.GatewayPush)
		return &cloned
	case contracts.ClusterServiceEvent:
		value.Meta = cloneStringMap(value.Meta)
		return value
	case *contracts.ClusterServiceEvent:
		if value == nil {
			return value
		}
		cloned := cloneBusPayload(*value).(contracts.ClusterServiceEvent)
		return &cloned
	case contracts.ChatBroadcast:
		value.Metadata = cloneStringMap(value.Metadata)
		return value
	case *contracts.ChatBroadcast:
		if value == nil {
			return value
		}
		cloned := cloneBusPayload(*value).(contracts.ChatBroadcast)
		return &cloned
	case contracts.NoticePublished:
		value.Recipients = cloneStrings(value.Recipients)
		value.DeliveryIDs = cloneStrings(value.DeliveryIDs)
		value.Meta = cloneStringMap(value.Meta)
		return value
	case *contracts.NoticePublished:
		if value == nil {
			return value
		}
		cloned := cloneBusPayload(*value).(contracts.NoticePublished)
		return &cloned
	case contracts.RPCRequest:
		value.Metadata = cloneStringMap(value.Metadata)
		value.Payload = cloneBytes(value.Payload)
		return value
	case *contracts.RPCRequest:
		if value == nil {
			return value
		}
		cloned := cloneBusPayload(*value).(contracts.RPCRequest)
		return &cloned
	case contracts.RPCResponse:
		value.Metadata = cloneStringMap(value.Metadata)
		value.Payload = cloneBytes(value.Payload)
		return value
	case *contracts.RPCResponse:
		if value == nil {
			return value
		}
		cloned := cloneBusPayload(*value).(contracts.RPCResponse)
		return &cloned
	default:
		return payload
	}
}

func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	return append([]byte(nil), in...)
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneAnySlice(in []any) []any {
	if len(in) == 0 {
		return nil
	}
	out := make([]any, len(in))
	for idx, value := range in {
		out[idx] = cloneBusPayload(value)
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneBusPayload(value)
	}
	return out
}

func (b *Memory) Request(ctx context.Context, topic string, payload any, timeout time.Duration) ([]byte, error) {
	return requestWithPubSub(ctx, topic, payload, timeout, b.Subscribe, b.publishEnvelope)
}

// ensureReadyLocked 补齐手工构造 Memory 时缺失的内部索引。
// 调用方必须已经持有 b.mu 写锁，因为 queue 轮询状态会在发布快照阶段推进。
func (b *Memory) ensureReadyLocked() {
	if b.subs == nil {
		b.subs = make(map[string]map[int]Handler)
	}
	if b.queues == nil {
		b.queues = make(map[string]map[string]map[int]Handler)
	}
	if b.next == nil {
		b.next = make(map[string]map[string]int)
	}
	if b.options.HandlerTimeout < 0 {
		b.options.HandlerTimeout = 0
	}
	if b.options.CloseTimeout < 0 {
		b.options.CloseTimeout = 0
	}
	if b.metrics == nil && b.options.Metrics != nil {
		b.metrics = b.options.Metrics
	}
}

func deliveryContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

func deliverWithDeadline(parent context.Context, logger *slog.Logger, topic string, timeout time.Duration, env Envelope, handler Handler, m *Metrics) {
	handlerCtx, cancel := deliveryContext(parent, timeout)
	defer cancel()
	if timeout <= 0 {
		err, panicked := callHandler(handlerCtx, handler, env)
		logHandlerError(logger, topic, err)
		m.recordHandler(handlerOutcome(err, panicked))
		return
	}
	type resultWithPanic struct {
		err      error
		panicked bool
	}
	done := make(chan resultWithPanic, 1)
	go func() {
		err, panicked := callHandler(handlerCtx, handler, env)
		done <- resultWithPanic{err: err, panicked: panicked}
	}()
	// 超时只释放 bus 投递流程，不能强杀业务 goroutine。
	// 因此订阅 handler 必须主动检查 ctx，避免慢任务在后台堆积。
	select {
	case result := <-done:
		logHandlerError(logger, topic, result.err)
		m.recordHandler(handlerOutcome(result.err, result.panicked))
	case <-handlerCtx.Done():
		logHandlerError(logger, topic, handlerCtx.Err())
		m.recordHandler(handlerOutcome(handlerCtx.Err(), false))
	}
}

func callHandler(ctx context.Context, handler Handler, env Envelope) (err error, panicked bool) {
	defer func() {
		if rec := recover(); rec != nil {
			err = tracing.PanicError(ctx, "bus.handler", rec, map[string]string{"topic": env.Topic})
			panicked = true
		}
	}()
	return handler(ctx, env), false
}

func handlerOutcome(err error, panicked bool) string {
	if panicked {
		return ResultPanic
	}
	return classifyHandlerError(err)
}

func logHandlerError(logger *slog.Logger, topic string, err error) {
	if err == nil || logger == nil || errors.Is(err, context.Canceled) {
		return
	}
	logger.Error("bus handler failed", "topic", topic, "error", err)
}

func (b *Memory) Check(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil {
		return ErrClosed
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrClosed
	}
	return nil
}

func (b *Memory) Subscribe(topic string, handler Handler) (Subscription, error) {
	if handler == nil {
		return nil, ErrHandlerRequired
	}
	if b == nil {
		return nil, ErrClosed
	}
	b.mu.Lock()
	b.ensureReadyLocked()
	if b.closed {
		metricsRef := b.metrics
		b.mu.Unlock()
		metricsRef.recordSubscribe(ResultClosed)
		return nil, ErrClosed
	}

	b.nextID++
	id := b.nextID
	if _, ok := b.subs[topic]; !ok {
		b.subs[topic] = make(map[int]Handler)
	}
	b.subs[topic][id] = handler
	metricsRef := b.metrics
	b.mu.Unlock()

	metricsRef.recordSubscribe(ResultOK)
	metricsRef.trackSubscription(1)

	return &memorySubscription{
		closeFn: func() error {
			b.mu.Lock()
			if topicSubs, ok := b.subs[topic]; ok {
				if _, hasSub := topicSubs[id]; hasSub {
					delete(topicSubs, id)
					if len(topicSubs) == 0 {
						delete(b.subs, topic)
					}
					trackRef := b.metrics
					b.mu.Unlock()
					trackRef.trackSubscription(-1)
					return nil
				}
			}
			b.mu.Unlock()
			return nil
		},
	}, nil
}

func (b *Memory) QueueSubscribe(topic string, queue string, handler Handler) (Subscription, error) {
	queue = normalizeQueueName(queue)
	if queue == "" {
		return b.Subscribe(topic, handler)
	}
	if handler == nil {
		return nil, ErrHandlerRequired
	}
	if b == nil {
		return nil, ErrClosed
	}
	b.mu.Lock()
	b.ensureReadyLocked()
	if b.closed {
		metricsRef := b.metrics
		b.mu.Unlock()
		metricsRef.recordSubscribe(ResultClosed)
		return nil, ErrClosed
	}

	b.nextID++
	id := b.nextID
	if _, ok := b.queues[topic]; !ok {
		b.queues[topic] = make(map[string]map[int]Handler)
	}
	if _, ok := b.queues[topic][queue]; !ok {
		b.queues[topic][queue] = make(map[int]Handler)
	}
	b.queues[topic][queue][id] = handler
	metricsRef := b.metrics
	b.mu.Unlock()

	metricsRef.recordSubscribe(ResultOK)
	metricsRef.trackSubscription(1)

	return &memorySubscription{
		closeFn: func() error {
			b.mu.Lock()
			if topicQueues, ok := b.queues[topic]; ok {
				if queueSubs, ok := topicQueues[queue]; ok {
					if _, hasSub := queueSubs[id]; hasSub {
						delete(queueSubs, id)
						if len(queueSubs) == 0 {
							delete(topicQueues, queue)
							if b.next[topic] != nil {
								delete(b.next[topic], queue)
							}
						}
						if len(topicQueues) == 0 {
							delete(b.queues, topic)
							delete(b.next, topic)
						}
						trackRef := b.metrics
						b.mu.Unlock()
						trackRef.trackSubscription(-1)
						return nil
					}
				}
			}
			b.mu.Unlock()
			return nil
		},
	}, nil
}

func (b *Memory) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	b.ensureReadyLocked()
	b.closed = true
	b.subs = make(map[string]map[int]Handler)
	b.queues = make(map[string]map[string]map[int]Handler)
	b.next = make(map[string]map[string]int)
	closeTimeout := b.options.CloseTimeout
	b.mu.Unlock()

	done := make(chan struct{})
	go func() {
		b.deliveries.Wait()
		close(done)
	}()
	// Close 不再接受新发布，并等待已开始的投递收敛；超时由上层关停策略处理。
	if closeTimeout <= 0 {
		select {
		case <-done:
		default:
		}
		return nil
	}
	timer := time.NewTimer(closeTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return ErrCloseTimeout
	}
}

type memorySubscription struct {
	closeFn func() error
}

func (s *memorySubscription) Close() error {
	if s == nil {
		return nil
	}
	if s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}
