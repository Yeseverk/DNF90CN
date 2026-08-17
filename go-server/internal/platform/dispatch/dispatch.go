package dispatch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/hotreload"
	"longheng.io/server/internal/platform/pipeline"
	"longheng.io/server/internal/platform/tracing"
	"longheng.io/server/pkg/protocol"
)

const defaultReloadWait = 2 * time.Second

type Request struct {
	GatewayNodeID string
	SessionID     string
	AccountID     string
	PacketID      int32
	MsgID         uint32
	Compressed    bool
	Body          []byte
	Metadata      map[string]string
	ReceivedAt    time.Time
}

type Response struct {
	PacketID   int32
	MsgID      uint32
	Compressed bool
	Body       []byte
	Metadata   map[string]string
	Note       string
}

type HandlerFunc func(context.Context, Request) (Response, error)

type RequestHook func(context.Context, Request)

type HandlerBeforeHook func(context.Context, HandlerMeta, Request) (context.Context, Request, error)

type HandlerAfterHook func(context.Context, HandlerMeta, Request, Response, error) (Response, error)

type SlowHook func(context.Context, HandlerMeta, Request, time.Duration)

type MuxOptions struct {
	HandlerTimeout       time.Duration
	SlowThreshold        time.Duration
	HotReloadWaitTimeout time.Duration
	PlayerStatsLimit     int
	SlowHook             SlowHook
}

type HandlerMeta struct {
	MsgID            uint32 `json:"msg_id"`
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	AuthRequired     bool   `json:"auth_required,omitempty"`
	PlayerScoped     bool   `json:"player_scoped,omitempty"`
	Internal         bool   `json:"internal,omitempty"`
	Stateful         bool   `json:"stateful,omitempty"`
	Authorized       bool   `json:"authorized,omitempty"`
	Idempotent       bool   `json:"idempotent,omitempty"`
	RouteGroup       string `json:"route_group,omitempty"`
	ProfileOperation string `json:"profile_operation,omitempty"`
	CriticalEvent    bool   `json:"critical_event,omitempty"`
	Durability       string `json:"durability,omitempty"`
	DurabilityOwner  string `json:"durability_owner,omitempty"`
}

type registeredHandler struct {
	meta    HandlerMeta
	handler HandlerFunc
}

type PlayerStats struct {
	AccountID string    `json:"account_id"`
	Requests  int64     `json:"requests"`
	LastMsgID uint32    `json:"last_msg_id,omitempty"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
}

type playerStatsState struct {
	requests  int64
	lastMsgID uint32
	lastSeen  time.Time
}

type Mux struct {
	mu               sync.RWMutex
	handlers         map[uint32]registeredHandler
	defaultHandler   HandlerFunc
	hooks            []RequestHook
	beforeHooks      map[uint32][]HandlerBeforeHook
	afterHooks       map[uint32][]HandlerAfterHook
	pipeline         *pipeline.Pipeline[Request, Response]
	timeout          time.Duration
	slowThreshold    time.Duration
	hotReloadWait    time.Duration
	slowHook         SlowHook
	hotReloadStates  map[uint32]*hotReloadState
	playerStats      map[string]*playerStatsState
	playerStatsOrder []string
	playerStatsHead  int
	playerStatsLimit int
	slowCount        atomic.Uint64
}

type hotReloadState struct {
	barrier      *hotreload.DrainBarrier
	activationMu sync.Mutex
}

func New() *Mux {
	return NewWithOptions(MuxOptions{})
}

func NewWithOptions(options MuxOptions) *Mux {
	if options.HandlerTimeout < 0 {
		options.HandlerTimeout = 0
	}
	if options.SlowThreshold < 0 {
		options.SlowThreshold = 0
	}
	if options.HotReloadWaitTimeout == 0 {
		options.HotReloadWaitTimeout = defaultReloadWait
	}
	if options.HotReloadWaitTimeout < 0 {
		options.HotReloadWaitTimeout = 0
	}
	if options.PlayerStatsLimit == 0 {
		options.PlayerStatsLimit = 4096
	}
	return &Mux{
		handlers:         make(map[uint32]registeredHandler),
		beforeHooks:      make(map[uint32][]HandlerBeforeHook),
		afterHooks:       make(map[uint32][]HandlerAfterHook),
		pipeline:         pipeline.New[Request, Response](),
		timeout:          options.HandlerTimeout,
		slowThreshold:    options.SlowThreshold,
		hotReloadWait:    options.HotReloadWaitTimeout,
		slowHook:         options.SlowHook,
		hotReloadStates:  make(map[uint32]*hotReloadState),
		playerStats:      make(map[string]*playerStatsState),
		playerStatsOrder: make([]string, 0, minPositive(options.PlayerStatsLimit, 64)),
		playerStatsLimit: options.PlayerStatsLimit,
	}
}

func (m *Mux) SetHandlerTimeout(timeout time.Duration) {
	if m == nil {
		return
	}
	if timeout < 0 {
		timeout = 0
	}
	m.mu.Lock()
	m.timeout = timeout
	m.mu.Unlock()
}

func (m *Mux) SetSlowHook(threshold time.Duration, hook SlowHook) {
	if m == nil {
		return
	}
	if threshold < 0 {
		threshold = 0
	}
	m.mu.Lock()
	m.slowThreshold = threshold
	m.slowHook = hook
	m.mu.Unlock()
}

func (m *Mux) SetHotReloadWaitTimeout(timeout time.Duration) {
	if m == nil {
		return
	}
	if timeout < 0 {
		timeout = 0
	}
	m.mu.Lock()
	m.hotReloadWait = timeout
	m.mu.Unlock()
}

func (m *Mux) Handle(msgID uint32, handler HandlerFunc) error {
	return m.HandleMeta(HandlerMeta{MsgID: msgID, Name: fmt.Sprintf("msg_%d", msgID)}, handler)
}

func (m *Mux) MustHandle(msgID uint32, handler HandlerFunc) {
	if err := m.Handle(msgID, handler); err != nil {
		panic(err)
	}
}

func (m *Mux) HandleMeta(meta HandlerMeta, handler HandlerFunc) error {
	return m.register(meta, handler)
}

func (m *Mux) MustHandleMeta(meta HandlerMeta, handler HandlerFunc) {
	if err := m.HandleMetaChecked(meta, handler); err != nil {
		panic(err)
	}
}

func (m *Mux) HandleMetaChecked(meta HandlerMeta, handler HandlerFunc) error {
	return m.register(meta, handler)
}

func (m *Mux) register(meta HandlerMeta, handler HandlerFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if meta.MsgID == 0 {
		return nil
	}
	if handler == nil {
		delete(m.handlers, meta.MsgID)
		return nil
	}
	meta = NormalizeHandlerMeta(meta)
	if err := ValidateHandlerMeta(meta); err != nil {
		return err
	}
	m.handlers[meta.MsgID] = registeredHandler{meta: meta, handler: handler}
	return nil
}

func NormalizeHandlerMeta(meta HandlerMeta) HandlerMeta {
	if meta.Name == "" {
		meta.Name = fmt.Sprintf("msg_%d", meta.MsgID)
	}
	meta.RouteGroup = strings.TrimSpace(meta.RouteGroup)
	meta.Durability = strings.ToLower(strings.TrimSpace(meta.Durability))
	meta.DurabilityOwner = strings.TrimSpace(meta.DurabilityOwner)
	if meta.AuthRequired {
		meta.Authorized = true
	}
	if meta.PlayerScoped {
		meta.Stateful = true
		if meta.RouteGroup == "" {
			meta.RouteGroup = "player"
		}
	}
	return meta
}

func ValidateHandlerMeta(meta HandlerMeta) error {
	meta = NormalizeHandlerMeta(meta)
	if !meta.CriticalEvent {
		return nil
	}
	name := meta.Name
	if name == "" {
		name = fmt.Sprintf("msg_%d", meta.MsgID)
	}
	if !meta.Idempotent {
		return fmt.Errorf("critical handler %s must be idempotent", name)
	}
	if !AllowedCriticalDurability(meta.Durability) {
		return fmt.Errorf("critical handler %s durability %q must be eventlog/outbox/db_transaction/transactional_outbox", name, meta.Durability)
	}
	if meta.DurabilityOwner == "" {
		return fmt.Errorf("critical handler %s durability owner is required", name)
	}
	return nil
}

func AllowedCriticalDurability(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "eventlog", "outbox", "db_transaction", "transactional_outbox":
		return true
	default:
		return false
	}
}

func (m *Mux) HandleDefault(handler HandlerFunc) {
	m.mu.Lock()
	m.defaultHandler = handler
	m.mu.Unlock()
}

func (m *Mux) UseBefore(before pipeline.Before[Request]) {
	if before == nil {
		return
	}
	m.mu.Lock()
	m.pipeline.UseBefore(before)
	m.mu.Unlock()
}

func (m *Mux) UseAfter(after pipeline.After[Response]) {
	if after == nil {
		return
	}
	m.mu.Lock()
	m.pipeline.UseAfter(after)
	m.mu.Unlock()
}

func (m *Mux) AddHook(hook RequestHook) {
	if hook == nil {
		return
	}
	m.mu.Lock()
	m.hooks = append(m.hooks, hook)
	m.pipeline.UseBefore(func(ctx context.Context, req Request) (context.Context, Request, error) {
		hook(ctx, req)
		return ctx, req, nil
	})
	m.mu.Unlock()
}

func (m *Mux) UseHandlerBefore(msgID uint32, hook HandlerBeforeHook) error {
	if m == nil || hook == nil || msgID == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.handlers[msgID]; !ok {
		return fmt.Errorf("handler %d is not registered", msgID)
	}
	m.beforeHooks[msgID] = append(m.beforeHooks[msgID], hook)
	return nil
}

func (m *Mux) UseHandlerAfter(msgID uint32, hook HandlerAfterHook) error {
	if m == nil || hook == nil || msgID == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.handlers[msgID]; !ok {
		return fmt.Errorf("handler %d is not registered", msgID)
	}
	m.afterHooks[msgID] = append(m.afterHooks[msgID], hook)
	return nil
}

func (m *Mux) Snapshot() []HandlerMeta {
	m.mu.RLock()
	out := make([]HandlerMeta, 0, len(m.handlers))
	for _, registered := range m.handlers {
		out = append(out, registered.meta)
	}
	m.mu.RUnlock()

	sortHandlerMeta(out)
	return out
}

func (m *Mux) Meta(msgID uint32) (HandlerMeta, bool) {
	m.mu.RLock()
	registered, ok := m.handlers[msgID]
	m.mu.RUnlock()
	if !ok {
		return HandlerMeta{}, false
	}
	return registered.meta, true
}

func (m *Mux) PlayerStats(accountID string) (PlayerStats, bool) {
	if m == nil {
		return PlayerStats{}, false
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return PlayerStats{}, false
	}
	m.mu.RLock()
	state, ok := m.playerStats[accountID]
	if !ok {
		m.mu.RUnlock()
		return PlayerStats{}, false
	}
	out := PlayerStats{
		AccountID: accountID,
		Requests:  state.requests,
		LastMsgID: state.lastMsgID,
		LastSeen:  state.lastSeen,
	}
	m.mu.RUnlock()
	return out, true
}

func (m *Mux) PlayerStatsSnapshot(limit int) []PlayerStats {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	out := make([]PlayerStats, 0, len(m.playerStats))
	for accountID, state := range m.playerStats {
		out = append(out, PlayerStats{
			AccountID: accountID,
			Requests:  state.requests,
			LastMsgID: state.lastMsgID,
			LastSeen:  state.lastSeen,
		})
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].AccountID < out[j].AccountID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (m *Mux) Dispatch(ctx context.Context, req Request) (resp Response, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	leave, err := m.enterHotReloadDrain(ctx, req.MsgID)
	if err != nil {
		return Response{}, err
	}
	defer leave()
	defer func() {
		if rec := recover(); rec != nil {
			resp = Response{}
			panicErr := tracing.PanicError(ctx, "dispatch.handler", rec, map[string]string{
				"msg_id":  fmt.Sprintf("%d", req.MsgID),
				"account": req.AccountID,
			})
			err = protocol.Errorf(protocol.CodeInternal, "%v", panicErr)
		}
	}()

	m.mu.RLock()
	registered := m.handlers[req.MsgID]
	handler := registered.handler
	if handler == nil {
		handler = m.defaultHandler
	}
	requestPipeline := m.pipeline.Clone()
	beforeHooks := append([]HandlerBeforeHook(nil), m.beforeHooks[req.MsgID]...)
	afterHooks := append([]HandlerAfterHook(nil), m.afterHooks[req.MsgID]...)
	timeout := m.timeout
	slowThreshold := m.slowThreshold
	slowHook := m.slowHook
	m.mu.RUnlock()

	if handler == nil {
		return Response{}, protocol.Errorf(protocol.CodeNotImplemented, "no handler registered for msg_id %d", req.MsgID)
	}
	if registered.handler != nil {
		if err := EnforceRoutePolicy(registered.meta, req); err != nil {
			return Response{}, err
		}
	}
	m.recordPlayerRequest(req)
	execCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	started := time.Now()
	execute := func(ctx context.Context, req Request) (Response, error) {
		var err error
		for _, hook := range beforeHooks {
			ctx, req, err = hook(ctx, registered.meta, req)
			if err != nil {
				return Response{}, err
			}
		}
		resp, err := handler(ctx, req)
		for _, hook := range afterHooks {
			resp, err = hook(ctx, registered.meta, req, resp, err)
		}
		return resp, err
	}
	if timeout > 0 {
		resp, err = execHardDeadline(execCtx, requestPipeline, req, execute)
	} else {
		resp, err = requestPipeline.Execute(execCtx, req, execute)
	}
	duration := time.Since(started)
	if slowThreshold > 0 && duration >= slowThreshold {
		m.slowCount.Add(1)
		if slowHook != nil {
			callSlowHook(slowHook, execCtx, registered.meta, req, duration)
		}
	}
	if err != nil {
		return Response{}, err
	}
	if resp.MsgID == 0 {
		resp.MsgID = req.MsgID
	}
	if resp.PacketID == 0 {
		resp.PacketID = req.PacketID
	}
	return resp, nil
}

func (m *Mux) enterHotReloadDrain(ctx context.Context, msgID uint32) (func(), error) {
	if m == nil || msgID == 0 {
		return func() {}, nil
	}
	m.mu.RLock()
	waitTimeout := m.hotReloadWait
	state := m.hotReloadStates[msgID]
	m.mu.RUnlock()
	if state == nil || state.barrier == nil {
		return func() {}, nil
	}
	waitCtx := ctx
	cancel := func() {}
	if waitTimeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, waitTimeout)
	}
	leave, err := state.barrier.EnterWhenReady(waitCtx)
	cancel()
	if err == nil {
		return leave, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, protocol.NewError(protocol.CodeUnavailable, "handler hot reload wait timeout")
	}
	return nil, err
}

func (m *Mux) hotReloadBarrier(msgID uint32, create bool) *hotreload.DrainBarrier {
	state := m.hotReloadState(msgID, create)
	if state == nil {
		return nil
	}
	return state.barrier
}

func (m *Mux) hotReloadState(msgID uint32, create bool) *hotReloadState {
	if m == nil || msgID == 0 {
		return nil
	}
	m.mu.RLock()
	state := m.hotReloadStates[msgID]
	m.mu.RUnlock()
	if state != nil || !create {
		return state
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state = m.hotReloadStates[msgID]
	if state == nil {
		state = &hotReloadState{barrier: hotreload.NewDrainBarrier()}
		m.hotReloadStates[msgID] = state
	}
	return state
}

func (m *Mux) recordPlayerRequest(req Request) {
	accountID := strings.TrimSpace(req.AccountID)
	if accountID == "" {
		return
	}
	m.mu.Lock()
	if m.playerStatsLimit < 0 {
		m.mu.Unlock()
		return
	}
	if m.playerStats == nil {
		m.playerStats = make(map[string]*playerStatsState)
	}
	state := m.playerStats[accountID]
	if state == nil {
		if m.playerStatsLimit > 0 && len(m.playerStats) >= m.playerStatsLimit {
			m.evictStatsLocked()
		}
		state = &playerStatsState{}
		m.playerStats[accountID] = state
		m.playerStatsOrder = append(m.playerStatsOrder, accountID)
	}
	state.requests++
	state.lastMsgID = req.MsgID
	state.lastSeen = time.Now().UTC()
	m.mu.Unlock()
}

func (m *Mux) evictStatsLocked() {
	for m.playerStatsHead < len(m.playerStatsOrder) {
		accountID := m.playerStatsOrder[m.playerStatsHead]
		m.playerStatsHead++
		if _, ok := m.playerStats[accountID]; ok {
			delete(m.playerStats, accountID)
			break
		}
	}
	if m.playerStatsHead > 1024 && m.playerStatsHead*2 >= len(m.playerStatsOrder) {
		m.playerStatsOrder = append([]string(nil), m.playerStatsOrder[m.playerStatsHead:]...)
		m.playerStatsHead = 0
	}
}

type dispatchResult struct {
	resp Response
	err  error
}

func execHardDeadline(ctx context.Context, requestPipeline *pipeline.Pipeline[Request, Response], req Request, handler pipeline.Handler[Request, Response]) (Response, error) {
	done := make(chan dispatchResult, 1)
	go func() {
		var result dispatchResult
		defer func() {
			if rec := recover(); rec != nil {
				panicErr := tracing.PanicError(ctx, "dispatch.handler", rec, map[string]string{
					"msg_id":  fmt.Sprintf("%d", req.MsgID),
					"account": req.AccountID,
				})
				result.err = protocol.Errorf(protocol.CodeInternal, "%v", panicErr)
			}
			done <- result
		}()
		result.resp, result.err = requestPipeline.Execute(ctx, req, handler)
	}()
	select {
	case result := <-done:
		return result.resp, result.err
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}
}

func callSlowHook(hook SlowHook, ctx context.Context, meta HandlerMeta, req Request, duration time.Duration) {
	defer func() {
		_ = recover()
	}()
	hook(ctx, meta, req, duration)
}

func EnforceRoutePolicy(meta HandlerMeta, req Request) error {
	if meta.Internal && strings.TrimSpace(req.GatewayNodeID) != "" {
		return protocol.NewError(protocol.CodeUnauthorized, "internal handler is not callable from gateway")
	}
	if (meta.Authorized || meta.AuthRequired) && strings.TrimSpace(req.AccountID) == "" {
		return protocol.NewError(protocol.CodeUnauthorized, "account authorization is required")
	}
	if (meta.Stateful || meta.PlayerScoped) && strings.TrimSpace(req.AccountID) == "" {
		return protocol.NewError(protocol.CodeBadRequest, "account_id is required")
	}
	return nil
}

func sortHandlerMeta(handlers []HandlerMeta) {
	sort.Slice(handlers, func(i, j int) bool {
		return handlers[i].MsgID < handlers[j].MsgID
	})
}

func minPositive(left, right int) int {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}
