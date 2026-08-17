package rpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/bus"
	"longheng.io/server/internal/platform/discovery"
	"longheng.io/server/internal/platform/metrics"
	"longheng.io/server/internal/platform/registry"
	"longheng.io/server/internal/platform/tracing"
	"longheng.io/server/pkg/contracts"
)

var (
	// ErrInvalidEndpoint 表示 RPC endpoint 为空或没有完成基础初始化。
	ErrInvalidEndpoint = errors.New("rpc endpoint is invalid")
	// ErrNotStarted 表示 RPC endpoint 还没有启动或已经停止。
	ErrNotStarted = errors.New("rpc endpoint is not started")
	// ErrInvalidNodeID 表示当前节点 ID 或来源节点 ID 为空。
	ErrInvalidNodeID = errors.New("rpc node id is required")
	// ErrInvalidTarget 表示目标节点 ID 为空。
	ErrInvalidTarget = errors.New("rpc target node id is required")
	// ErrInvalidService 表示目标服务名为空。
	ErrInvalidService = errors.New("rpc target service is required")
	// ErrInvalidRoute 表示 RPC 路由为空。
	ErrInvalidRoute = errors.New("rpc route is required")
	// ErrInvalidRequestID 表示请求 ID 为空。
	ErrInvalidRequestID = errors.New("rpc request id is required")
	// ErrNoTargetNodes 表示服务发现或路由解析没有可用目标节点。
	ErrNoTargetNodes = errors.New("rpc target service has no nodes")
	// ErrTooManyPending 表示等待响应的调用数量已经达到上限。
	ErrTooManyPending = errors.New("rpc pending call limit exceeded")
	// ErrPayloadTooLarge 表示请求或响应 payload 超过配置上限。
	ErrPayloadTooLarge = errors.New("rpc payload exceeds max size")
)

// Handler 是 endpoint 收到业务 RPC 请求后的处理函数。
type Handler func(context.Context, contracts.RPCRequest) (contracts.RPCResponse, error)

// Resolver 根据服务名和分片 key 解析目标节点 ID。
type Resolver func(context.Context, string, string) (string, bool, error)

// Options 控制 endpoint 的调用超时、并发等待上限和 payload 大小预算。
type Options struct {
	CallTimeout     time.Duration
	MaxPending      int
	MaxPayloadBytes int
}

// RetryOptions 控制可靠调用的重试次数、退避和远端错误重试策略。
type RetryOptions struct {
	Attempts          int
	Backoff           time.Duration
	MaxBackoff        time.Duration
	IdempotencyKey    string
	RetryRemoteErrors bool
}

// Snapshot 是 endpoint 当前运行状态的只读快照。
type Snapshot struct {
	ServiceName        string   `json:"service_name"`
	NodeID             string   `json:"node_id"`
	Started            bool     `json:"started"`
	Pending            int      `json:"pending"`
	Handlers           []string `json:"handlers"`
	CallTimeoutSeconds float64  `json:"call_timeout_seconds"`
	MaxPending         int      `json:"max_pending"`
	MaxPayloadBytes    int      `json:"max_payload_bytes"`
}

// RemoteError 表示目标节点 handler 返回的远端业务错误。
type RemoteError struct {
	RequestID    string
	TargetNodeID string
	Route        string
	Message      string
}

// Error 返回包含目标节点和路由的远端错误文本。
func (e RemoteError) Error() string {
	if e.Route == "" {
		return fmt.Sprintf("rpc remote error from %s: %s", e.TargetNodeID, e.Message)
	}
	return fmt.Sprintf("rpc remote error from %s route %s: %s", e.TargetNodeID, e.Route, e.Message)
}

// Endpoint 是服务内 RPC 的注册、调用、响应等待和节点路由入口。
type Endpoint struct {
	serviceName  string
	nodeID       string
	transport    Transport
	logger       *slog.Logger
	resolver     Resolver
	discovery    *discovery.Resolver
	tracer       *tracing.Tracer
	options      Options
	metrics      *metrics.Registry
	metricLabels map[string]string

	seq uint64

	mu       sync.RWMutex
	started  bool
	handlers map[string]Handler
	pending  map[string]chan contracts.RPCResponse
	subs     []TransportSubscription
}

// NewEndpoint 使用 bus transport 创建 endpoint。
func NewEndpoint(serviceName, nodeID string, eventBus bus.Bus, logger *slog.Logger) *Endpoint {
	return NewEndpointWithTransport(serviceName, nodeID, NewBusTransport(eventBus), logger)
}

// NewEndpointWithTransport 使用指定 transport 创建 endpoint。
func NewEndpointWithTransport(serviceName, nodeID string, transport Transport, logger *slog.Logger) *Endpoint {
	return &Endpoint{
		serviceName: strings.TrimSpace(serviceName),
		nodeID:      strings.TrimSpace(nodeID),
		transport:   transport,
		logger:      logger,
		handlers:    make(map[string]Handler),
		pending:     make(map[string]chan contracts.RPCResponse),
		options: Options{
			CallTimeout:     3 * time.Second,
			MaxPending:      1024,
			MaxPayloadBytes: 1024 * 1024,
		},
	}
}

// Name 返回生命周期组件名。
func (e *Endpoint) Name() string {
	return "rpc-endpoint"
}

// SetResolver 设置轻量服务到节点解析函数。
func (e *Endpoint) SetResolver(resolver Resolver) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.resolver = resolver
	e.mu.Unlock()
}

// SetDiscoveryResolver 设置带健康反馈的 discovery resolver。
func (e *Endpoint) SetDiscoveryResolver(resolver *discovery.Resolver) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.discovery = resolver
	e.mu.Unlock()
}

// SetTracer 设置 RPC 调用和处理链路使用的 tracer。
func (e *Endpoint) SetTracer(tracer *tracing.Tracer) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.tracer = tracer
	e.mu.Unlock()
}

// Deprecated: 使用 SetOptionsExact 明确表达零值配置；该方法保留零值跳过语义以兼容早期热更新调用。
func (e *Endpoint) SetOptions(options Options) {
	if e == nil {
		return
	}
	e.mu.Lock()
	next := e.options
	if options.CallTimeout != 0 {
		next.CallTimeout = options.CallTimeout
	}
	if options.MaxPending != 0 {
		next.MaxPending = options.MaxPending
	}
	if options.MaxPayloadBytes != 0 {
		next.MaxPayloadBytes = options.MaxPayloadBytes
	}
	e.options = normalizeOptions(next)
	e.mu.Unlock()
}

// SetOptionsExact 设置完整配置，零值也会按规范化规则生效。
func (e *Endpoint) SetOptionsExact(options Options) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.options = normalizeOptions(options)
	e.mu.Unlock()
}

// Snapshot 返回 endpoint 的稳定状态快照。
func (e *Endpoint) Snapshot() Snapshot {
	if e == nil {
		return Snapshot{}
	}
	e.mu.RLock()
	handlers := make([]string, 0, len(e.handlers))
	for route := range e.handlers {
		handlers = append(handlers, route)
	}
	snapshot := Snapshot{
		ServiceName:        e.serviceName,
		NodeID:             e.nodeID,
		Started:            e.started,
		Pending:            len(e.pending),
		Handlers:           handlers,
		CallTimeoutSeconds: e.options.CallTimeout.Seconds(),
		MaxPending:         e.options.MaxPending,
		MaxPayloadBytes:    e.options.MaxPayloadBytes,
	}
	e.mu.RUnlock()
	sort.Strings(snapshot.Handlers)
	return snapshot
}

// Register 注册或移除指定 RPC 路由的 handler。
func (e *Endpoint) Register(route string, handler Handler) error {
	if e == nil {
		return ErrInvalidEndpoint
	}
	route = strings.TrimSpace(route)
	if route == "" {
		return ErrInvalidRoute
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureReadyLocked()
	if handler == nil {
		delete(e.handlers, route)
		return nil
	}
	e.handlers[route] = handler
	return nil
}

// Start 订阅请求和响应主题，使 endpoint 开始接收 RPC 流量。
func (e *Endpoint) Start(ctx context.Context) error {
	if e == nil {
		return ErrInvalidEndpoint
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(e.nodeID) == "" {
		return ErrInvalidNodeID
	}
	if e.transport == nil {
		return bus.ErrClosed
	}

	e.mu.Lock()
	e.ensureReadyLocked()
	if e.started {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	requestSub, err := e.transport.SubscribeRequests(ctx, e.nodeID, e.handleRequest)
	if err != nil {
		return err
	}
	responseSub, err := e.transport.SubscribeResponses(ctx, e.nodeID, e.handleResponse)
	if err != nil {
		_ = requestSub.Close()
		return err
	}

	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		_ = requestSub.Close()
		_ = responseSub.Close()
		return nil
	}
	e.subs = []TransportSubscription{requestSub, responseSub}
	e.started = true
	e.mu.Unlock()
	return nil
}

// Stop 关闭订阅并唤醒所有等待中的调用。
func (e *Endpoint) Stop(context.Context) error {
	if e == nil {
		return ErrInvalidEndpoint
	}
	e.mu.Lock()
	e.ensureReadyLocked()
	subs := append([]TransportSubscription(nil), e.subs...)
	pending := e.pending
	e.subs = nil
	e.pending = make(map[string]chan contracts.RPCResponse)
	e.started = false
	e.mu.Unlock()

	var firstErr error
	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if err := sub.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// 停机时唤醒所有等待响应的调用方，避免业务 goroutine 卡到超时才退出。
	for requestID, ch := range pending {
		select {
		case ch <- contracts.RPCResponse{
			RequestID:    requestID,
			TargetNodeID: e.nodeID,
			Error:        ErrNotStarted.Error(),
			SentAt:       time.Now().UTC(),
		}:
		default:
		}
		close(ch)
	}
	return firstErr
}

// CallNode 向指定节点发起一次 RPC 调用并等待响应。
func (e *Endpoint) CallNode(ctx context.Context, targetNodeID, route string, payload []byte, metadata map[string]string) (contracts.RPCResponse, error) {
	if e == nil {
		return contracts.RPCResponse{}, ErrInvalidEndpoint
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := e.withCallTimeout(ctx)
	defer cancel()
	targetNodeID = strings.TrimSpace(targetNodeID)
	route = strings.TrimSpace(route)
	var callErr error
	defer func() {
		e.recordCallMetric(route, callErr)
	}()
	if err := ctx.Err(); err != nil {
		callErr = err
		return contracts.RPCResponse{}, callErr
	}
	if targetNodeID == "" {
		callErr = ErrInvalidTarget
		return contracts.RPCResponse{}, callErr
	}
	if route == "" {
		callErr = ErrInvalidRoute
		return contracts.RPCResponse{}, callErr
	}
	if err := e.checkPayload(payload); err != nil {
		callErr = err
		return contracts.RPCResponse{}, callErr
	}

	ctx, finish := e.startTrace(ctx, "rpc.call_node", map[string]string{
		"target_node_id": targetNodeID,
		"route":          route,
	})
	defer func() { finish(callErr) }()

	requestID := e.nextRequestID()
	responseCh := make(chan contracts.RPCResponse, 1)
	requestMetadata := tracing.InjectCarrier(ctx, cloneMetadata(metadata))
	request := contracts.RPCRequest{
		RequestID:     requestID,
		SourceService: e.serviceName,
		SourceNodeID:  e.nodeID,
		TargetNodeID:  targetNodeID,
		Route:         route,
		Metadata:      requestMetadata,
		Payload:       append([]byte(nil), payload...),
		SentAt:        time.Now().UTC(),
	}

	e.mu.Lock()
	e.ensureReadyLocked()
	if !e.started {
		e.mu.Unlock()
		callErr = ErrNotStarted
		return contracts.RPCResponse{}, callErr
	}
	if e.options.MaxPending > 0 && len(e.pending) >= e.options.MaxPending {
		e.mu.Unlock()
		callErr = ErrTooManyPending
		return contracts.RPCResponse{}, callErr
	}
	// pending 先登记再发布请求，保证极快返回的响应不会先到而找不到等待者。
	e.pending[requestID] = responseCh
	e.mu.Unlock()

	if err := e.transport.PublishRequest(ctx, targetNodeID, request); err != nil {
		e.removePending(requestID)
		callErr = err
		return contracts.RPCResponse{}, callErr
	}

	select {
	case <-ctx.Done():
		e.removePending(requestID)
		callErr = ctx.Err()
		return contracts.RPCResponse{}, callErr
	case resp, ok := <-responseCh:
		if !ok {
			callErr = ErrNotStarted
			return contracts.RPCResponse{}, callErr
		}
		if resp.Error != "" {
			callErr = RemoteError{
				RequestID:    resp.RequestID,
				TargetNodeID: resp.SourceNodeID,
				Route:        route,
				Message:      resp.Error,
			}
			return resp, callErr
		}
		return resp, nil
	}
}

// CallService 先解析服务目标节点，再发起一次 RPC 调用。
func (e *Endpoint) CallService(ctx context.Context, service, key, route string, payload []byte, metadata map[string]string) (contracts.RPCResponse, error) {
	if e == nil {
		return contracts.RPCResponse{}, ErrInvalidEndpoint
	}
	if ctx == nil {
		ctx = context.Background()
	}
	service = strings.TrimSpace(service)
	key = strings.TrimSpace(key)
	route = strings.TrimSpace(route)
	if service == "" {
		err := ErrInvalidService
		e.recordCallMetric(route, err)
		return contracts.RPCResponse{}, err
	}
	if route == "" {
		err := ErrInvalidRoute
		e.recordCallMetric(route, err)
		return contracts.RPCResponse{}, err
	}

	e.mu.RLock()
	discoveryResolver := e.discovery
	resolver := e.resolver
	e.mu.RUnlock()
	if discoveryResolver != nil {
		// 优先使用带健康反馈的 discovery resolver；失败会回写节点健康，影响后续选路。
		node, ok, err := discoveryResolver.ResolveRoute(ctx, service, route, key)
		if err != nil {
			e.recordCallMetric(route, err)
			return contracts.RPCResponse{}, err
		}
		targetNodeID := strings.TrimSpace(node.NodeID)
		if !ok || targetNodeID == "" {
			err := ErrNoTargetNodes
			e.recordCallMetric(route, err)
			return contracts.RPCResponse{}, err
		}
		resp, err := e.CallNode(ctx, targetNodeID, route, payload, metadata)
		if err != nil {
			if isDiscoveryFail(err) {
				discoveryResolver.MarkFailure(targetNodeID)
			}
			return resp, err
		}
		discoveryResolver.MarkSuccess(targetNodeID)
		return resp, nil
	}
	if resolver == nil {
		err := ErrNoTargetNodes
		e.recordCallMetric(route, err)
		return contracts.RPCResponse{}, err
	}
	targetNodeID, ok, err := resolver(ctx, service, key)
	if err != nil {
		e.recordCallMetric(route, err)
		return contracts.RPCResponse{}, err
	}
	if !ok || strings.TrimSpace(targetNodeID) == "" {
		err := ErrNoTargetNodes
		e.recordCallMetric(route, err)
		return contracts.RPCResponse{}, err
	}
	return e.CallNode(ctx, targetNodeID, route, payload, metadata)
}

// CallServiceReliable 会重试临时 timeout/pending 失败，但底层仍通过配置的 Bus 投递。
// 在当前 memory、Redis Pub/Sub 和 NATS core 适配器下，这不是持久化 RPC：
// 请求不会落盘，响应也没有消费者确认。
func (e *Endpoint) CallServiceReliable(ctx context.Context, service, key, route string, payload []byte, metadata map[string]string, options RetryOptions) (contracts.RPCResponse, error) {
	if e == nil {
		return contracts.RPCResponse{}, ErrInvalidEndpoint
	}
	options = normRetryOpts(options)
	var lastResp contracts.RPCResponse
	var lastErr error
	for attempt := 1; attempt <= options.Attempts; attempt++ {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return lastResp, err
		}
		attemptMetadata := retryMetadata(metadata, options, attempt)
		resp, err := e.CallService(ctx, service, key, route, payload, attemptMetadata)
		lastResp, lastErr = resp, err
		if err == nil || !shouldRetryCall(err, options) || attempt == options.Attempts {
			return resp, err
		}
		if err := sleepRetryBackoff(ctx, options, attempt); err != nil {
			return lastResp, err
		}
	}
	return lastResp, lastErr
}

// CallNodeReliable 会重试临时 timeout/pending 失败；只有底层 Bus 适配器具备持久化能力时，
// 它才代表持久投递保证。
func (e *Endpoint) CallNodeReliable(ctx context.Context, targetNodeID, route string, payload []byte, metadata map[string]string, options RetryOptions) (contracts.RPCResponse, error) {
	if e == nil {
		return contracts.RPCResponse{}, ErrInvalidEndpoint
	}
	options = normRetryOpts(options)
	var lastResp contracts.RPCResponse
	var lastErr error
	for attempt := 1; attempt <= options.Attempts; attempt++ {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return lastResp, err
		}
		attemptMetadata := retryMetadata(metadata, options, attempt)
		resp, err := e.CallNode(ctx, targetNodeID, route, payload, attemptMetadata)
		lastResp, lastErr = resp, err
		if err == nil || !shouldRetryCall(err, options) || attempt == options.Attempts {
			return resp, err
		}
		if err := sleepRetryBackoff(ctx, options, attempt); err != nil {
			return lastResp, err
		}
	}
	return lastResp, lastErr
}

func (e *Endpoint) handleReqEnvelope(ctx context.Context, env bus.Envelope) error {
	req, ok := rpcReqPayload(env.Payload)
	if !ok {
		return nil
	}
	return e.handleRequest(ctx, req)
}

func (e *Endpoint) handleRequest(ctx context.Context, req contracts.RPCRequest) error {
	if req.TargetNodeID != "" && req.TargetNodeID != e.nodeID {
		return nil
	}
	route := strings.TrimSpace(req.Route)
	var handlerErr error
	defer func() {
		e.recordHandlerMetric(route, handlerErr)
	}()
	if strings.TrimSpace(req.RequestID) == "" {
		handlerErr = ErrInvalidRequestID
		return handlerErr
	}
	if strings.TrimSpace(req.SourceNodeID) == "" {
		handlerErr = ErrInvalidNodeID
		return handlerErr
	}

	ctx = tracing.ExtractCarrier(ctx, req.Metadata)
	ctx, finish := e.startTrace(ctx, "rpc.handle", map[string]string{
		"source_node_id": req.SourceNodeID,
		"route":          req.Route,
	})
	defer func() { finish(handlerErr) }()

	e.mu.RLock()
	handler := e.handlers[route]
	e.mu.RUnlock()

	var resp contracts.RPCResponse
	var err error
	// 请求和响应都做 payload 上限校验，防止 RPC 被当成大对象传输通道。
	if payloadErr := e.checkPayload(req.Payload); payloadErr != nil {
		err = payloadErr
	} else if handler == nil {
		err = fmt.Errorf("no rpc handler registered for route %q", req.Route)
	} else {
		resp, err = handler(ctx, req)
	}
	handlerErr = err
	if err != nil && resp.Error == "" {
		resp.Error = err.Error()
	}
	if payloadErr := e.checkPayload(resp.Payload); payloadErr != nil {
		handlerErr = payloadErr
		resp = contracts.RPCResponse{Error: payloadErr.Error()}
	}
	resp.RequestID = req.RequestID
	resp.SourceNodeID = e.nodeID
	resp.TargetNodeID = req.SourceNodeID
	if resp.Metadata == nil {
		resp.Metadata = cloneMetadata(req.Metadata)
	} else {
		resp.Metadata = cloneMetadata(resp.Metadata)
	}
	resp.Metadata = tracing.InjectCarrier(ctx, resp.Metadata)
	resp.SentAt = time.Now().UTC()

	topic := contracts.RPCResponseTopic(req.SourceNodeID)
	if topic == "" {
		return ErrInvalidNodeID
	}
	if publishErr := e.transport.PublishResponse(ctx, req.SourceNodeID, resp); publishErr != nil {
		handlerErr = publishErr
		return publishErr
	}
	return nil
}

func (e *Endpoint) handleResponse(_ context.Context, resp contracts.RPCResponse) error {
	if resp.TargetNodeID != "" && resp.TargetNodeID != e.nodeID {
		return nil
	}
	if payloadErr := e.checkPayload(resp.Payload); payloadErr != nil {
		resp.Payload = nil
		if resp.Error == "" {
			resp.Error = payloadErr.Error()
		}
	}
	e.mu.Lock()
	ch := e.pending[resp.RequestID]
	delete(e.pending, resp.RequestID)
	e.mu.Unlock()
	// 找不到 pending 通常说明调用方已经超时或节点正在停机；响应直接丢弃。
	if ch == nil {
		return nil
	}
	select {
	case ch <- resp:
	default:
	}
	return nil
}

func (e *Endpoint) removePending(requestID string) {
	e.mu.Lock()
	e.ensureReadyLocked()
	delete(e.pending, requestID)
	e.mu.Unlock()
}

func (e *Endpoint) ensureReadyLocked() {
	if e.handlers == nil {
		e.handlers = make(map[string]Handler)
	}
	if e.pending == nil {
		e.pending = make(map[string]chan contracts.RPCResponse)
	}
}

func (e *Endpoint) withCallTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	e.mu.RLock()
	timeout := e.options.CallTimeout
	e.mu.RUnlock()
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (e *Endpoint) checkPayload(payload []byte) error {
	e.mu.RLock()
	limit := e.options.MaxPayloadBytes
	e.mu.RUnlock()
	if limit > 0 && len(payload) > limit {
		return ErrPayloadTooLarge
	}
	return nil
}

func (e *Endpoint) nextRequestID() string {
	seq := atomic.AddUint64(&e.seq, 1)
	nodeID := strings.TrimSpace(e.nodeID)
	if nodeID == "" {
		nodeID = "unknown"
	}
	return fmt.Sprintf("%s-%d-%d", nodeID, time.Now().UTC().UnixNano(), seq)
}

func (e *Endpoint) startTrace(ctx context.Context, name string, attrs map[string]string) (context.Context, func(error)) {
	e.mu.RLock()
	tracer := e.tracer
	e.mu.RUnlock()
	if tracer == nil {
		return ctx, func(error) {}
	}
	return tracer.Start(ctx, name, attrs)
}

// RegistryResolver 把服务注册表包装成 endpoint 可用的节点解析函数。
func RegistryResolver(reg registry.Registry, rejectMaintaining bool) Resolver {
	resolver := discovery.NewResolver(reg, time.Second, 5*time.Second, rejectMaintaining)
	return func(ctx context.Context, service, key string) (string, bool, error) {
		if reg == nil {
			return "", false, ErrNoTargetNodes
		}
		service = strings.TrimSpace(service)
		if service == "" {
			return "", false, ErrInvalidService
		}
		node, ok, err := resolver.Resolve(ctx, service, key)
		if err != nil {
			return "", false, err
		}
		if !ok {
			return "", false, nil
		}
		return node.NodeID, true, nil
	}
}

func isDiscoveryFail(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, ErrTooManyPending) ||
		errors.Is(err, ErrPayloadTooLarge) ||
		errors.Is(err, ErrNotStarted) {
		return false
	}
	var remoteErr RemoteError
	return !errors.As(err, &remoteErr)
}

func normRetryOpts(options RetryOptions) RetryOptions {
	if options.Attempts <= 0 {
		options.Attempts = 3
	}
	if options.Backoff < 0 {
		options.Backoff = 0
	}
	if options.MaxBackoff < 0 {
		options.MaxBackoff = 0
	}
	options.IdempotencyKey = strings.TrimSpace(options.IdempotencyKey)
	return options
}

func retryMetadata(metadata map[string]string, options RetryOptions, attempt int) map[string]string {
	out := cloneMetadata(metadata)
	if out == nil {
		out = make(map[string]string, 4)
	}
	out["rpc_attempt"] = fmt.Sprint(attempt)
	out["rpc_max_attempts"] = fmt.Sprint(options.Attempts)
	if options.IdempotencyKey != "" {
		out["rpc_idempotency_key"] = options.IdempotencyKey
	}
	return out
}

func shouldRetryCall(err error, options RetryOptions) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, ErrInvalidNodeID) ||
		errors.Is(err, ErrInvalidTarget) ||
		errors.Is(err, ErrInvalidService) ||
		errors.Is(err, ErrInvalidRoute) ||
		errors.Is(err, ErrInvalidRequestID) ||
		errors.Is(err, ErrNoTargetNodes) ||
		errors.Is(err, ErrTooManyPending) ||
		errors.Is(err, ErrPayloadTooLarge) ||
		errors.Is(err, ErrNotStarted) ||
		errors.Is(err, discovery.ErrNoRegistry) ||
		errors.Is(err, discovery.ErrNoRoute) ||
		errors.Is(err, discovery.ErrRouteRequiresKey) {
		return false
	}
	var remoteErr RemoteError
	if errors.As(err, &remoteErr) {
		return options.RetryRemoteErrors
	}
	return true
}

func sleepRetryBackoff(ctx context.Context, options RetryOptions, attempt int) error {
	backoff := retryBackoff(options, attempt)
	if backoff <= 0 {
		return nil
	}
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryBackoff(options RetryOptions, attempt int) time.Duration {
	backoff := options.Backoff
	if backoff <= 0 {
		return 0
	}
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if options.MaxBackoff > 0 && backoff > options.MaxBackoff {
			return options.MaxBackoff
		}
	}
	if options.MaxBackoff > 0 && backoff > options.MaxBackoff {
		return options.MaxBackoff
	}
	return backoff
}

func normalizeOptions(options Options) Options {
	if options.CallTimeout < 0 {
		options.CallTimeout = 0
	}
	if options.MaxPending < 0 {
		options.MaxPending = 0
	}
	if options.MaxPayloadBytes < 0 {
		options.MaxPayloadBytes = 0
	}
	return options
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}

func rpcReqPayload(payload any) (contracts.RPCRequest, bool) {
	switch typed := payload.(type) {
	case contracts.RPCRequest:
		return typed, true
	case *contracts.RPCRequest:
		if typed == nil {
			return contracts.RPCRequest{}, false
		}
		return *typed, true
	default:
		return contracts.RPCRequest{}, false
	}
}

func rpcRespPayload(payload any) (contracts.RPCResponse, bool) {
	switch typed := payload.(type) {
	case contracts.RPCResponse:
		return typed, true
	case *contracts.RPCResponse:
		if typed == nil {
			return contracts.RPCResponse{}, false
		}
		return *typed, true
	default:
		return contracts.RPCResponse{}, false
	}
}
