package playerloop

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/actor"
)

// ActorKindPlayer 是玩家 actor 在 actor.Manager 中使用的类型标识。
const ActorKindPlayer = "player"

// ActorBackedManager 使用通用 actor 管理器承载玩家事件串行处理。
type ActorBackedManager struct {
	name    string
	handler Handler
	actors  *actor.Manager
	logger  *slog.Logger

	mu      sync.RWMutex
	started bool
}

// NewActorBackedManager 创建基于 actor mailbox 的玩家循环管理器。
func NewActorBackedManager(name string, queue int, handler Handler, logger *slog.Logger, options Options) *ActorBackedManager {
	if queue <= 0 {
		queue = 64
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "player-loop"
	}
	return &ActorBackedManager{
		name:    name,
		handler: handler,
		actors: actor.NewManager(name+"-actors", logger, actor.Options{
			MailboxSize:    queue,
			HandlerTimeout: options.HandlerTimeout,
		}),
		logger: logger,
	}
}

// Name 返回 actor 版玩家循环管理器名称。
func (m *ActorBackedManager) Name() string {
	if m == nil {
		return ""
	}
	return m.name
}

// Start 标记 actor 版玩家循环管理器可接收事件。
func (m *ActorBackedManager) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || m.actors == nil {
		return ErrStopped
	}
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	return nil
}

// Stop 停止 actor 版玩家循环管理器并关闭底层 actor。
func (m *ActorBackedManager) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil || m.actors == nil {
		return nil
	}
	m.mu.Lock()
	m.started = false
	m.mu.Unlock()
	return m.actors.Stop(ctx)
}

// Submit 将事件提交到账号对应的玩家 actor。
func (m *ActorBackedManager) Submit(ctx context.Context, accountID string, payload any) error {
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
	if m == nil || m.actors == nil || !m.isStarted() {
		return ErrStopped
	}
	act, err := m.actorFor(accountID)
	if err != nil {
		return err
	}
	event := Event{AccountID: accountID, Payload: payload, ReceivedAt: time.Now().UTC()}
	err = act.Tell(ctx, actor.Message{Route: "player.event", UserID: accountID, Payload: event})
	if errors.Is(err, actor.ErrMailboxFull) {
		return ErrQueueFull
	}
	if errors.Is(err, actor.ErrActorStopped) || errors.Is(err, actor.ErrActorDraining) {
		return ErrStopped
	}
	return err
}

// Snapshot 返回所有玩家 actor 的队列快照。
func (m *ActorBackedManager) Snapshot() []Snapshot {
	if m == nil || m.actors == nil {
		return nil
	}
	actorSnapshot := m.actors.Snapshot()
	out := make([]Snapshot, 0, len(actorSnapshot.Actors))
	for _, item := range actorSnapshot.Actors {
		if item.Kind != ActorKindPlayer {
			continue
		}
		out = append(out, Snapshot{AccountID: item.ID, Queued: item.MailboxLen})
	}
	return out
}

// ActorSnapshot 返回底层 actor 管理器的完整快照。
func (m *ActorBackedManager) ActorSnapshot() actor.Snapshot {
	if m == nil || m.actors == nil {
		return actor.Snapshot{}
	}
	return m.actors.Snapshot()
}

// DrainAccount 请求指定账号 actor 进入排空状态。
func (m *ActorBackedManager) DrainAccount(ctx context.Context, accountID string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return ErrMissingAccount
	}
	if m == nil || m.actors == nil {
		return nil
	}
	act, ok := m.actors.Get(ActorKindPlayer, accountID)
	if !ok {
		return nil
	}
	return act.Drain(ctx)
}

func (m *ActorBackedManager) actorFor(accountID string) (*actor.Actor, error) {
	if act, ok := m.actors.Get(ActorKindPlayer, accountID); ok {
		return act, nil
	}
	act, err := m.actors.Spawn(ActorKindPlayer, accountID, m.handleActorMessage)
	if errors.Is(err, actor.ErrActorExists) {
		if existing, ok := m.actors.Get(ActorKindPlayer, accountID); ok {
			return existing, nil
		}
	}
	return act, err
}

func (m *ActorBackedManager) handleActorMessage(ctx context.Context, _ *actor.Actor, msg actor.Message) error {
	if m == nil || m.handler == nil {
		return nil
	}
	event, ok := msg.Payload.(Event)
	if !ok {
		event = Event{AccountID: msg.UserID, Payload: msg.Payload, ReceivedAt: msg.SentAt}
	}
	return m.handler(ctx, event)
}

func (m *ActorBackedManager) isStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}
