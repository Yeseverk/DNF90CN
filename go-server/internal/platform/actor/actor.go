package actor

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
)

var (
	ErrInvalidActor  = errors.New("actor kind and id are required")
	ErrActorExists   = errors.New("actor already exists")
	ErrActorMissing  = errors.New("actor not found")
	ErrActorStopped  = errors.New("actor is stopped")
	ErrActorDraining = errors.New("actor is draining")
	ErrMailboxFull   = errors.New("actor mailbox is full")
	ErrNoHandler     = errors.New("actor route handler not found")
)

type Handler func(context.Context, *Actor, Message) error

type InvokeFunc func(context.Context, *Actor) error

type SlowHook func(context.Context, *Actor, Message, time.Duration)

type Message struct {
	Route    string            `json:"route,omitempty"`
	Event    string            `json:"event,omitempty"`
	UserID   string            `json:"user_id,omitempty"`
	Payload  any               `json:"payload,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	SentAt   time.Time         `json:"sent_at"`
}

type Options struct {
	MailboxSize    int
	HandlerTimeout time.Duration
	SlowThreshold  time.Duration
	SlowHook       SlowHook
}

type Manager struct {
	name    string
	logger  *slog.Logger
	options Options

	mu       sync.RWMutex
	actors   map[string]*Actor
	bindings map[string]map[string]string
}

type Actor struct {
	manager *Manager
	kind    string
	id      string
	pid     string
	logger  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	box    chan command
	drain  chan struct{}

	mu             sync.RWMutex
	defaultHandler Handler
	handlers       map[string]Handler
	bindings       map[string]struct{}

	startedAt        time.Time
	stopped          atomic.Bool
	stopping         atomic.Bool
	drainRequested   atomic.Bool
	drainOnce        sync.Once
	processed        atomic.Int64
	failures         atomic.Int64
	slowCommands     atomic.Int64
	lastCommandNanos atomic.Int64
}

type Snapshot struct {
	Name       string          `json:"name"`
	Actors     []ActorSnapshot `json:"actors"`
	Bindings   []Binding       `json:"bindings"`
	Total      int             `json:"total"`
	TotalBound int             `json:"total_bound"`
}

type ActorSnapshot struct {
	Kind          string    `json:"kind"`
	ID            string    `json:"id"`
	PID           string    `json:"pid"`
	MailboxLen    int       `json:"mailbox_len"`
	MailboxCap    int       `json:"mailbox_cap"`
	Processed     int64     `json:"processed"`
	Failures      int64     `json:"failures"`
	SlowCommands  int64     `json:"slow_commands,omitempty"`
	LastCommandMS int64     `json:"last_command_ms,omitempty"`
	Draining      bool      `json:"draining,omitempty"`
	Stopped       bool      `json:"stopped"`
	StartedAt     time.Time `json:"started_at"`
	BoundUsers    []string  `json:"bound_users,omitempty"`
	Routes        []string  `json:"routes,omitempty"`
}

type Binding struct {
	UserID string `json:"user_id"`
	Kind   string `json:"kind"`
	PID    string `json:"pid"`
}

type command struct {
	ctx context.Context
	msg Message
	fn  InvokeFunc
	ack chan error
}

func NewManager(name string, logger *slog.Logger, opts ...Options) *Manager {
	options := Options{MailboxSize: 1024}
	if len(opts) > 0 {
		options = opts[0]
	}
	if options.MailboxSize <= 0 {
		options.MailboxSize = 1024
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "actor-manager"
	}
	return &Manager{
		name:     name,
		logger:   logger,
		options:  options,
		actors:   make(map[string]*Actor),
		bindings: make(map[string]map[string]string),
	}
}

func (m *Manager) Name() string {
	if m == nil || m.name == "" {
		return "actor-manager"
	}
	return m.name
}

func (m *Manager) Start(context.Context) error {
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.RLock()
	actors := make([]*Actor, 0, len(m.actors))
	for _, actor := range m.actors {
		actors = append(actors, actor)
	}
	m.mu.RUnlock()

	// Stop 会先广播取消再等待，避免单个卡住的 actor 让后续 actor 继续运行。
	for _, actor := range actors {
		actor.requestStop()
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	for _, actor := range actors {
		wg.Add(1)
		go func(actor *Actor) {
			defer wg.Done()
			<-actor.done
			m.remove(actor)
		}(actor)
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) Spawn(kind, id string, handler Handler) (*Actor, error) {
	if m == nil {
		return nil, ErrInvalidActor
	}
	kind, id, pid, err := normActorIdentity(kind, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	actor := &Actor{
		manager:        m,
		kind:           kind,
		id:             id,
		pid:            pid,
		logger:         m.logger,
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
		box:            make(chan command, m.options.MailboxSize),
		drain:          make(chan struct{}),
		defaultHandler: handler,
		handlers:       make(map[string]Handler),
		bindings:       make(map[string]struct{}),
		startedAt:      time.Now().UTC(),
	}

	m.mu.Lock()
	if _, ok := m.actors[pid]; ok {
		m.mu.Unlock()
		cancel()
		return nil, ErrActorExists
	}
	m.actors[pid] = actor
	m.mu.Unlock()

	// 每个 actor 的 handler 都在自己的 goroutine 中运行，保证每个 PID 的状态串行化。
	go actor.run()
	return actor, nil
}

func (m *Manager) Get(kind, id string) (*Actor, bool) {
	if m == nil {
		return nil, false
	}
	_, _, pid, err := normActorIdentity(kind, id)
	if err != nil {
		return nil, false
	}
	return m.GetPID(pid)
}

func (m *Manager) GetPID(pid string) (*Actor, bool) {
	if m == nil {
		return nil, false
	}
	pid = strings.TrimSpace(pid)
	m.mu.RLock()
	actor, ok := m.actors[pid]
	m.mu.RUnlock()
	return actor, ok
}

func (m *Manager) Tell(ctx context.Context, kind, id string, msg Message) error {
	actor, ok := m.Get(kind, id)
	if !ok {
		return ErrActorMissing
	}
	return actor.Tell(ctx, msg)
}

func (m *Manager) Call(ctx context.Context, kind, id string, msg Message) error {
	actor, ok := m.Get(kind, id)
	if !ok {
		return ErrActorMissing
	}
	return actor.Call(ctx, msg)
}

func (m *Manager) Invoke(ctx context.Context, kind, id string, fn InvokeFunc) error {
	actor, ok := m.Get(kind, id)
	if !ok {
		return ErrActorMissing
	}
	return actor.Invoke(ctx, fn)
}

func (m *Manager) Kill(ctx context.Context, kind, id string) error {
	actor, ok := m.Get(kind, id)
	if !ok {
		return ErrActorMissing
	}
	return actor.Stop(ctx)
}

func (m *Manager) BindUser(userID, kind, id string) error {
	if m == nil {
		return ErrActorMissing
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ErrInvalidActor
	}
	actor, ok := m.Get(kind, id)
	if !ok {
		return ErrActorMissing
	}
	m.mu.Lock()
	if _, ok := m.bindings[userID]; !ok {
		m.bindings[userID] = make(map[string]string)
	}
	// 一个用户可以绑定多种 actor kind，但同一种 kind 只能解析到一个 actor。
	m.bindings[userID][actor.kind] = actor.pid
	m.mu.Unlock()

	actor.mu.Lock()
	actor.bindings[userID] = struct{}{}
	actor.mu.Unlock()
	return nil
}
func (m *Manager) UnbindUser(userID, kind string) {
	if m == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	kind = strings.TrimSpace(kind)
	if userID == "" || kind == "" {
		return
	}
	m.mu.Lock()
	pid := ""
	if bindings := m.bindings[userID]; bindings != nil {
		pid = bindings[kind]
		delete(bindings, kind)
		if len(bindings) == 0 {
			delete(m.bindings, userID)
		}
	}
	m.mu.Unlock()
	if pid == "" {
		return
	}
	if actor, ok := m.GetPID(pid); ok {
		actor.mu.Lock()
		delete(actor.bindings, userID)
		actor.mu.Unlock()
	}
}

func (m *Manager) LocateUser(userID, kind string) (*Actor, bool) {
	if m == nil {
		return nil, false
	}
	userID = strings.TrimSpace(userID)
	kind = strings.TrimSpace(kind)
	if userID == "" || kind == "" {
		return nil, false
	}
	m.mu.RLock()
	pid := ""
	if bindings := m.bindings[userID]; bindings != nil {
		pid = bindings[kind]
	}
	m.mu.RUnlock()
	if pid == "" {
		return nil, false
	}
	return m.GetPID(pid)
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.RLock()
	actors := make([]ActorSnapshot, 0, len(m.actors))
	for _, actor := range m.actors {
		actors = append(actors, actor.Snapshot())
	}
	bindings := make([]Binding, 0)
	for userID, byKind := range m.bindings {
		for kind, pid := range byKind {
			bindings = append(bindings, Binding{UserID: userID, Kind: kind, PID: pid})
		}
	}
	snapshot := Snapshot{
		Name:       m.Name(),
		Actors:     actors,
		Bindings:   bindings,
		Total:      len(actors),
		TotalBound: len(bindings),
	}
	m.mu.RUnlock()

	sort.Slice(snapshot.Actors, func(i, j int) bool {
		return snapshot.Actors[i].PID < snapshot.Actors[j].PID
	})
	sort.Slice(snapshot.Bindings, func(i, j int) bool {
		if snapshot.Bindings[i].UserID != snapshot.Bindings[j].UserID {
			return snapshot.Bindings[i].UserID < snapshot.Bindings[j].UserID
		}
		return snapshot.Bindings[i].Kind < snapshot.Bindings[j].Kind
	})
	return snapshot
}

func (a *Actor) Kind() string {
	if a == nil {
		return ""
	}
	return a.kind
}

func (a *Actor) ID() string {
	if a == nil {
		return ""
	}
	return a.id
}

func (a *Actor) PID() string {
	if a == nil {
		return ""
	}
	return a.pid
}

func (a *Actor) Handle(route string, handler Handler) error {
	if a == nil {
		return ErrActorMissing
	}
	route = strings.TrimSpace(route)
	if route == "" {
		return ErrNoHandler
	}
	a.mu.Lock()
	if handler == nil {
		delete(a.handlers, route)
	} else {
		a.handlers[route] = handler
	}
	a.mu.Unlock()
	return nil
}

func (a *Actor) SetDefault(handler Handler) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.defaultHandler = handler
	a.mu.Unlock()
}

func (a *Actor) Tell(ctx context.Context, msg Message) error {
	return a.enqueue(command{ctx: ctx, msg: normalizeMessage(msg)})
}

func (a *Actor) Call(ctx context.Context, msg Message) error {
	ack := make(chan error, 1)
	if err := a.enqueue(command{ctx: ctx, msg: normalizeMessage(msg), ack: ack}); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Call 会等待当前消息执行完成；不要在同一个 actor 的 handler 内同步调用自己。
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Actor) Invoke(ctx context.Context, fn InvokeFunc) error {
	return a.InvokeMessage(ctx, Message{}, fn)
}

func (a *Actor) InvokeMessage(ctx context.Context, msg Message, fn InvokeFunc) error {
	if fn == nil {
		return nil
	}
	ack := make(chan error, 1)
	if err := a.enqueue(command{ctx: ctx, msg: normalizeMessage(msg), fn: fn, ack: ack}); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Actor) AfterFunc(d time.Duration, fn InvokeFunc) *time.Timer {
	if a == nil || fn == nil || a.stopped.Load() {
		return nil
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return time.AfterFunc(d, func() {
		if err := a.Invoke(ctx, fn); err != nil && a.logger != nil && !errors.Is(err, ErrActorStopped) && !errors.Is(err, context.Canceled) {
			a.logger.Warn("actor delayed invoke failed", "pid", a.pid, "error", err)
		}
	})
}

func (a *Actor) Stop(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.requestStop()
	select {
	case <-a.done:
	case <-ctx.Done():
		return ctx.Err()
	}

	if a.manager != nil {
		a.manager.remove(a)
	}
	return nil
}

func (a *Actor) requestStop() {
	if a != nil && a.stopping.CompareAndSwap(false, true) {
		a.cancel()
	}
}

func (a *Actor) Drain(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if a.drainRequested.CompareAndSwap(false, true) {
		a.drainOnce.Do(func() { close(a.drain) })
	}
	// Drain 会拒绝新消息，同时允许 mailbox 中已有消息处理完成。
	select {
	case <-a.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if a.manager != nil {
		a.manager.remove(a)
	}
	return nil
}

func (a *Actor) Snapshot() ActorSnapshot {
	if a == nil {
		return ActorSnapshot{}
	}
	a.mu.RLock()
	boundUsers := make([]string, 0, len(a.bindings))
	for userID := range a.bindings {
		boundUsers = append(boundUsers, userID)
	}
	routes := make([]string, 0, len(a.handlers))
	for route := range a.handlers {
		routes = append(routes, route)
	}
	a.mu.RUnlock()
	sort.Strings(boundUsers)
	sort.Strings(routes)
	return ActorSnapshot{
		Kind:          a.kind,
		ID:            a.id,
		PID:           a.pid,
		MailboxLen:    len(a.box),
		MailboxCap:    cap(a.box),
		Processed:     a.processed.Load(),
		Failures:      a.failures.Load(),
		SlowCommands:  a.slowCommands.Load(),
		LastCommandMS: a.lastCommandNanos.Load() / int64(time.Millisecond),
		Draining:      a.drainRequested.Load(),
		Stopped:       a.stopped.Load(),
		StartedAt:     a.startedAt,
		BoundUsers:    boundUsers,
		Routes:        routes,
	}
}

func (a *Actor) enqueue(cmd command) error {
	if a == nil {
		return ErrActorMissing
	}
	if a.stopped.Load() || a.stopping.Load() {
		return ErrActorStopped
	}
	if a.drainRequested.Load() {
		return ErrActorDraining
	}
	if cmd.ctx == nil {
		cmd.ctx = context.Background()
	}
	if err := cmd.ctx.Err(); err != nil {
		return err
	}
	// 非阻塞 mailbox 写入让背压显式暴露给调用方。
	select {
	case a.box <- cmd:
		return nil
	default:
		return ErrMailboxFull
	}
}

func (a *Actor) run() {
	defer func() {
		a.stopped.Store(true)
		a.rejectPending(ErrActorStopped)
		close(a.done)
	}()
	for {
		if a.ctx.Err() != nil {
			return
		}
		if a.drainRequested.Load() {
			// Drain 模式只处理 mailbox 中已经存在的命令。
			select {
			case cmd := <-a.box:
				if a.ctx.Err() != nil {
					return
				}
				a.handle(cmd)
				continue
			default:
				return
			}
		}
		select {
		case <-a.ctx.Done():
			return
		case <-a.drain:
			continue
		case cmd := <-a.box:
			if a.ctx.Err() != nil {
				return
			}
			a.handle(cmd)
		}
	}
}

func (a *Actor) rejectPending(err error) {
	for {
		select {
		case cmd := <-a.box:
			if cmd.ack != nil {
				cmd.ack <- err
			}
		default:
			return
		}
	}
}

func (a *Actor) handle(cmd command) {
	cmdCtx, cancel := a.commandContext(cmd.ctx)
	defer cancel()
	cmd.ctx = cmdCtx
	started := time.Now()
	err := a.execute(cmd)
	duration := time.Since(started)
	a.lastCommandNanos.Store(duration.Nanoseconds())
	if a.manager != nil && a.manager.options.SlowThreshold > 0 && duration >= a.manager.options.SlowThreshold {
		// 慢 hook 在 actor 状态锁外执行，避免诊断逻辑阻塞路由。
		a.slowCommands.Add(1)
		callSlowHook(a.manager.options.SlowHook, cmdCtx, a, cmd.msg, duration)
	}
	if err != nil {
		a.failures.Add(1)
		if a.logger != nil && !errors.Is(err, context.Canceled) {
			a.logger.Warn("actor command failed", "pid", a.pid, "route", cmd.msg.Route, "error", err)
		}
	}
	a.processed.Add(1)
	if cmd.ack != nil {
		cmd.ack <- err
	}
}
func callSlowHook(hook SlowHook, ctx context.Context, actor *Actor, msg Message, duration time.Duration) {
	if hook == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	hook(ctx, actor, msg, duration)
}

func (a *Actor) commandContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	stopActorCancel := func() bool { return true }
	if a != nil && a.ctx != nil {
		// Actor 停止时必须取消正在执行的 handler，即使调用方 ctx 仍然存活。
		stopActorCancel = context.AfterFunc(a.ctx, cancel)
	}
	timeoutCancel := context.CancelFunc(func() {})
	if a != nil && a.manager != nil && a.manager.options.HandlerTimeout > 0 {
		ctx, timeoutCancel = context.WithTimeout(ctx, a.manager.options.HandlerTimeout)
	}
	return ctx, func() {
		timeoutCancel()
		stopActorCancel()
		cancel()
	}
}
func (a *Actor) execute(cmd command) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			// panic 会转换成命令错误返回，保证 actor goroutine 继续存活。
			err = fmt.Errorf("actor panic: %v", rec)
		}
	}()
	if cmd.fn != nil {
		return cmd.fn(cmd.ctx, a)
	}
	handler := a.handlerFor(cmd.msg.Route)
	if handler == nil {
		return ErrNoHandler
	}
	return handler(cmd.ctx, a, cmd.msg)
}

func (a *Actor) handlerFor(route string) Handler {
	a.mu.RLock()
	defer a.mu.RUnlock()
	route = strings.TrimSpace(route)
	if route != "" {
		if handler := a.handlers[route]; handler != nil {
			return handler
		}
	}
	return a.defaultHandler
}

func (m *Manager) remove(actor *Actor) {
	if m == nil || actor == nil {
		return
	}
	m.mu.Lock()
	delete(m.actors, actor.pid)
	for userID, byKind := range m.bindings {
		if byKind[actor.kind] == actor.pid {
			delete(byKind, actor.kind)
			if len(byKind) == 0 {
				delete(m.bindings, userID)
			}
		}
	}
	m.mu.Unlock()
}

func normActorIdentity(kind, id string) (string, string, string, error) {
	kind = strings.TrimSpace(kind)
	id = strings.TrimSpace(id)
	if kind == "" || id == "" || !validActorIDPart(kind) || !validActorIDPart(id) {
		return "", "", "", ErrInvalidActor
	}
	return kind, id, kind + "/" + id, nil
}

func validActorIDPart(value string) bool {
	return !strings.ContainsAny(value, "/\x00")
}

func normalizeMessage(msg Message) Message {
	msg.Route = strings.TrimSpace(msg.Route)
	msg.Event = strings.TrimSpace(msg.Event)
	msg.UserID = strings.TrimSpace(msg.UserID)
	if msg.SentAt.IsZero() {
		msg.SentAt = time.Now().UTC()
	}
	if len(msg.Metadata) > 0 {
		meta := make(map[string]string, len(msg.Metadata))
		for key, value := range msg.Metadata {
			key = strings.TrimSpace(key)
			if key != "" {
				meta[key] = value
			}
		}
		msg.Metadata = meta
	}
	return msg
}
