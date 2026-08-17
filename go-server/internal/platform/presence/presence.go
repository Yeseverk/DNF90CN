package presence

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalidUserID 表示用户 ID 为空或非法。
	ErrInvalidUserID = errors.New("presence user id is required")
	// ErrInvalidSessionID 表示 session ID 为空或非法。
	ErrInvalidSessionID = errors.New("presence session id is required")
	// ErrInvalidStatus 表示在线状态为空。
	ErrInvalidStatus = errors.New("presence status is required")
	// ErrNilSender 表示广播发送函数为空。
	ErrNilSender = errors.New("presence sender is required")
	// ErrManagerRequired 表示内存版 presence manager 为空。
	ErrManagerRequired = errors.New("presence manager is required")
)

const (
	// StatusOnline 是默认在线状态。
	StatusOnline = "online"

	defPresenceShards = 64
)

// Runtime 定义 presence 模块对网关和社交服务暴露的在线状态能力。
type Runtime interface {
	Track(context.Context, Presence) (Presence, error)
	Untrack(context.Context, string, string) (Presence, bool, error)
	Touch(context.Context, string, string, time.Time) (Presence, bool, error)
	SetStatus(context.Context, string, string, string, map[string]string) (Presence, bool, error)
	Presence(context.Context, string) ([]Presence, error)
	Session(context.Context, string) (Presence, bool, error)
	Subscribe(context.Context, string, string) error
	Unsubscribe(context.Context, string, string) error
	Subscribers(context.Context, string) ([]string, error)
	Subscriptions(context.Context, string) ([]string, error)
	Broadcast(context.Context, string, SendFunc) error
	Snapshot() Snapshot
}

// Presence 描述一个用户在某个 session 上的在线状态。
type Presence struct {
	UserID    string            `json:"user_id"`
	SessionID string            `json:"session_id"`
	NodeID    string            `json:"node_id,omitempty"`
	Status    string            `json:"status"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Subscription 表示一个订阅者关注某个目标用户在线状态。
type Subscription struct {
	SubscriberID string `json:"subscriber_id"`
	TargetID     string `json:"target_id"`
}

// Snapshot 是 presence manager 的一致性快照。
type Snapshot struct {
	Name              string         `json:"name"`
	PresenceCount     int            `json:"presence_count"`
	UserCount         int            `json:"user_count"`
	SubscriptionCount int            `json:"subscription_count"`
	Presences         []Presence     `json:"presences,omitempty"`
	Subscriptions     []Subscription `json:"subscriptions,omitempty"`
}

// SendFunc 是 Broadcast fanout 时调用的发送函数。
type SendFunc func(context.Context, string, []Presence) error

// Options 控制内存版 presence manager 的名称、时间源和分片数。
type Options struct {
	Name       string
	Now        func() time.Time
	ShardCount int
}

// Manager 维护本机 session、用户在线状态和订阅关系索引。
type Manager struct {
	initOnce   sync.Once
	shardCount int

	name string
	now  func() time.Time

	// opMu 保护 session->presence 与 user->sessions 双索引的跨分片更新；心跳和状态刷新只锁单个 session 分片。
	opMu sync.Mutex

	presenceShards []presenceShard
	userShards     []userSessionShard

	subMu         sync.RWMutex
	subscriptions map[string]map[string]struct{}
	subscribers   map[string]map[string]struct{}
}

type presenceShard struct {
	mu        sync.RWMutex
	presences map[string]Presence
}

type userSessionShard struct {
	mu           sync.RWMutex
	userSessions map[string]map[string]struct{}
}

// New 创建内存版 presence manager。
func New(options Options) *Manager {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "presence"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	shardCount := options.ShardCount
	if shardCount <= 0 {
		shardCount = defPresenceShards
	}
	m := &Manager{
		name:       name,
		now:        now,
		shardCount: shardCount,
	}
	m.ensureReady()
	return m
}

// Track 注册或替换某个 session 的在线状态。
func (m *Manager) Track(ctx context.Context, item Presence) (Presence, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, err
	}
	if m == nil {
		return Presence{}, ErrManagerRequired
	}
	m.ensureReady()
	item, err := normalizePresence(item, m.now().UTC())
	if err != nil {
		return Presence{}, err
	}

	m.opMu.Lock()
	defer m.opMu.Unlock()

	sessionShard := m.presenceShard(item.SessionID)
	sessionShard.mu.Lock()
	old, hadOld := sessionShard.presences[item.SessionID]
	sessionShard.presences[item.SessionID] = item
	sessionShard.mu.Unlock()

	if hadOld && old.UserID != item.UserID {
		m.removeUserSession(old.UserID, old.SessionID)
	}
	m.addUserSession(item.UserID, item.SessionID)
	return clonePresence(item), nil
}

// Untrack 移除指定 session 的在线状态；userID 非空时必须匹配当前绑定。
func (m *Manager) Untrack(ctx context.Context, userID, sessionID string) (Presence, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, false, err
	}
	if m == nil {
		return Presence{}, false, ErrManagerRequired
	}
	m.ensureReady()
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Presence{}, false, ErrInvalidSessionID
	}

	m.opMu.Lock()
	defer m.opMu.Unlock()

	sessionShard := m.presenceShard(sessionID)
	sessionShard.mu.Lock()
	item, ok := sessionShard.presences[sessionID]
	if !ok {
		sessionShard.mu.Unlock()
		return Presence{}, false, nil
	}
	// 如果调用方带 userID，必须匹配当前绑定，防止旧 gateway 的断开事件误删新绑定。
	if userID != "" && item.UserID != userID {
		sessionShard.mu.Unlock()
		return Presence{}, false, nil
	}
	delete(sessionShard.presences, sessionID)
	sessionShard.mu.Unlock()

	m.removeUserSession(item.UserID, sessionID)
	return clonePresence(item), true, nil
}

// Touch 刷新指定 session 的在线更新时间。
func (m *Manager) Touch(ctx context.Context, userID, sessionID string, at time.Time) (Presence, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, false, err
	}
	if m == nil {
		return Presence{}, false, ErrManagerRequired
	}
	m.ensureReady()
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Presence{}, false, ErrInvalidSessionID
	}
	if at.IsZero() {
		at = m.now()
	}

	sessionShard := m.presenceShard(sessionID)
	sessionShard.mu.Lock()
	defer sessionShard.mu.Unlock()
	item, ok := sessionShard.presences[sessionID]
	if !ok {
		return Presence{}, false, nil
	}
	// Touch 只允许当前 user/session 刷新 LastSeen，旧连接的心跳不能延长新 session。
	if userID != "" && item.UserID != userID {
		return Presence{}, false, nil
	}
	item.UpdatedAt = at.UTC()
	sessionShard.presences[sessionID] = item
	return clonePresence(item), true, nil
}

// SetStatus 更新指定 session 的在线状态和元数据。
func (m *Manager) SetStatus(ctx context.Context, userID, sessionID, status string, metadata map[string]string) (Presence, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, false, err
	}
	if m == nil {
		return Presence{}, false, ErrManagerRequired
	}
	m.ensureReady()
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	status = strings.TrimSpace(status)
	if sessionID == "" {
		return Presence{}, false, ErrInvalidSessionID
	}
	if status == "" {
		return Presence{}, false, ErrInvalidStatus
	}

	sessionShard := m.presenceShard(sessionID)
	sessionShard.mu.Lock()
	defer sessionShard.mu.Unlock()
	item, ok := sessionShard.presences[sessionID]
	if !ok {
		return Presence{}, false, nil
	}
	// SetStatus 与 Touch 使用同一绑定校验；metadata=nil 表示清空而不是保留旧值。
	if userID != "" && item.UserID != userID {
		return Presence{}, false, nil
	}
	item.Status = status
	item.Metadata = normalizeMetadata(metadata)
	item.UpdatedAt = m.now().UTC()
	sessionShard.presences[sessionID] = item
	return clonePresence(item), true, nil
}

// Presence 查询某个用户当前所有在线 session。
func (m *Manager) Presence(ctx context.Context, userID string) ([]Presence, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerRequired
	}
	m.ensureReady()
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, ErrInvalidUserID
	}

	out := m.presencesByUser(userID)
	sortPresences(out)
	return out, nil
}

// Session 查询指定 session 的在线状态。
func (m *Manager) Session(ctx context.Context, sessionID string) (Presence, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, false, err
	}
	if m == nil {
		return Presence{}, false, ErrManagerRequired
	}
	m.ensureReady()
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Presence{}, false, ErrInvalidSessionID
	}

	sessionShard := m.presenceShard(sessionID)
	sessionShard.mu.RLock()
	item, ok := sessionShard.presences[sessionID]
	sessionShard.mu.RUnlock()
	if !ok {
		return Presence{}, false, nil
	}
	return clonePresence(item), true, nil
}

// Subscribe 订阅目标用户在线状态变化。
func (m *Manager) Subscribe(ctx context.Context, subscriberID, targetID string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if m == nil {
		return ErrManagerRequired
	}
	m.ensureReady()
	subscriberID, targetID, err := normSubIDs(subscriberID, targetID)
	if err != nil {
		return err
	}

	m.subMu.Lock()
	defer m.subMu.Unlock()
	if m.subscriptions[subscriberID] == nil {
		m.subscriptions[subscriberID] = make(map[string]struct{})
	}
	if m.subscribers[targetID] == nil {
		m.subscribers[targetID] = make(map[string]struct{})
	}
	m.subscriptions[subscriberID][targetID] = struct{}{}
	m.subscribers[targetID][subscriberID] = struct{}{}
	return nil
}

// Unsubscribe 取消订阅目标用户在线状态变化。
func (m *Manager) Unsubscribe(ctx context.Context, subscriberID, targetID string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if m == nil {
		return ErrManagerRequired
	}
	m.ensureReady()
	subscriberID, targetID, err := normSubIDs(subscriberID, targetID)
	if err != nil {
		return err
	}

	m.subMu.Lock()
	defer m.subMu.Unlock()
	if targets := m.subscriptions[subscriberID]; targets != nil {
		delete(targets, targetID)
		if len(targets) == 0 {
			delete(m.subscriptions, subscriberID)
		}
	}
	if watchers := m.subscribers[targetID]; watchers != nil {
		delete(watchers, subscriberID)
		if len(watchers) == 0 {
			delete(m.subscribers, targetID)
		}
	}
	return nil
}

// Subscribers 返回订阅目标用户的所有订阅者。
func (m *Manager) Subscribers(ctx context.Context, targetID string) ([]string, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerRequired
	}
	m.ensureReady()
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return nil, ErrInvalidUserID
	}
	m.subMu.RLock()
	out := sortedSet(m.subscribers[targetID])
	m.subMu.RUnlock()
	return out, nil
}

// Subscriptions 返回订阅者关注的所有目标用户。
func (m *Manager) Subscriptions(ctx context.Context, subscriberID string) ([]string, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerRequired
	}
	m.ensureReady()
	subscriberID = strings.TrimSpace(subscriberID)
	if subscriberID == "" {
		return nil, ErrInvalidUserID
	}
	m.subMu.RLock()
	out := sortedSet(m.subscriptions[subscriberID])
	m.subMu.RUnlock()
	return out, nil
}

// Broadcast 将目标用户当前在线状态广播给所有订阅者。
func (m *Manager) Broadcast(ctx context.Context, targetID string, send SendFunc) error {
	if send == nil {
		return ErrNilSender
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if m == nil {
		return ErrManagerRequired
	}
	m.ensureReady()
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return ErrInvalidUserID
	}

	// Broadcast 对当前在线 session 快照逐个发送，发送失败即返回；它不是可靠离线推送。
	m.subMu.RLock()
	subscribers := sortedSet(m.subscribers[targetID])
	m.subMu.RUnlock()
	presences := m.presencesByUser(targetID)
	sortPresences(presences)

	for _, subscriberID := range subscribers {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		if err := send(ctx, subscriberID, clonePresences(presences)); err != nil {
			return err
		}
	}
	return nil
}

// Snapshot 返回 presence manager 当前快照。
func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.ensureReady()
	presences := make([]Presence, 0)
	for i := range m.presenceShards {
		shard := &m.presenceShards[i]
		shard.mu.RLock()
		for _, item := range shard.presences {
			presences = append(presences, clonePresence(item))
		}
		shard.mu.RUnlock()
	}

	m.subMu.RLock()
	subscriptions := make([]Subscription, 0)
	for subscriberID, targets := range m.subscriptions {
		for targetID := range targets {
			subscriptions = append(subscriptions, Subscription{
				SubscriberID: subscriberID,
				TargetID:     targetID,
			})
		}
	}
	m.subMu.RUnlock()

	sortPresences(presences)
	sortSubscriptions(subscriptions)
	return Snapshot{
		Name:              m.name,
		PresenceCount:     len(presences),
		UserCount:         countPresenceUsers(presences),
		SubscriptionCount: len(subscriptions),
		Presences:         presences,
		Subscriptions:     subscriptions,
	}
}

func (m *Manager) ensureReady() {
	m.initOnce.Do(func() {
		m.name = strings.TrimSpace(m.name)
		if m.name == "" {
			m.name = "presence"
		}
		if m.now == nil {
			m.now = time.Now
		}
		shardCount := m.shardCount
		if shardCount <= 0 {
			switch {
			case len(m.presenceShards) > 0:
				shardCount = len(m.presenceShards)
			case len(m.userShards) > 0:
				shardCount = len(m.userShards)
			default:
				shardCount = defPresenceShards
			}
		}
		if len(m.presenceShards) == 0 {
			m.presenceShards = make([]presenceShard, shardCount)
		}
		if len(m.userShards) == 0 {
			m.userShards = make([]userSessionShard, shardCount)
		}
		m.shardCount = len(m.presenceShards)
		for i := range m.presenceShards {
			if m.presenceShards[i].presences == nil {
				m.presenceShards[i].presences = make(map[string]Presence)
			}
		}
		for i := range m.userShards {
			if m.userShards[i].userSessions == nil {
				m.userShards[i].userSessions = make(map[string]map[string]struct{})
			}
		}
		if m.subscriptions == nil {
			m.subscriptions = make(map[string]map[string]struct{})
		}
		if m.subscribers == nil {
			m.subscribers = make(map[string]map[string]struct{})
		}
	})
}

func (m *Manager) addUserSession(userID, sessionID string) {
	shard := m.userShard(userID)
	shard.mu.Lock()
	if shard.userSessions[userID] == nil {
		shard.userSessions[userID] = make(map[string]struct{})
	}
	shard.userSessions[userID][sessionID] = struct{}{}
	shard.mu.Unlock()
}

func (m *Manager) removeUserSession(userID, sessionID string) {
	shard := m.userShard(userID)
	shard.mu.Lock()
	if sessions := shard.userSessions[userID]; sessions != nil {
		delete(sessions, sessionID)
		if len(sessions) == 0 {
			delete(shard.userSessions, userID)
		}
	}
	shard.mu.Unlock()
}

func (m *Manager) presencesByUser(userID string) []Presence {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	userShard := m.userShard(userID)
	userShard.mu.RLock()
	sessions := sortedSet(userShard.userSessions[userID])
	userShard.mu.RUnlock()

	presences := make([]Presence, 0, len(sessions))
	for _, sessionID := range sessions {
		sessionShard := m.presenceShard(sessionID)
		sessionShard.mu.RLock()
		item, ok := sessionShard.presences[sessionID]
		sessionShard.mu.RUnlock()
		if ok && item.UserID == userID {
			presences = append(presences, clonePresence(item))
		}
	}
	return presences
}

func countPresenceUsers(presences []Presence) int {
	if len(presences) == 0 {
		return 0
	}
	users := make(map[string]struct{}, len(presences))
	for _, item := range presences {
		users[item.UserID] = struct{}{}
	}
	return len(users)
}

func (m *Manager) presenceShard(sessionID string) *presenceShard {
	return &m.presenceShards[shardIndex(sessionID, len(m.presenceShards))]
}

func (m *Manager) userShard(userID string) *userSessionShard {
	return &m.userShards[shardIndex(userID, len(m.userShards))]
}

func shardIndex(key string, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	return int(hash % uint32(shardCount)) //nolint:gosec // G115：取模结果严格小于 shardCount，可安全作为 shard 下标。
}

func normalizePresence(item Presence, now time.Time) (Presence, error) {
	item.UserID = strings.TrimSpace(item.UserID)
	item.SessionID = strings.TrimSpace(item.SessionID)
	item.NodeID = strings.TrimSpace(item.NodeID)
	item.Status = strings.TrimSpace(item.Status)
	if item.UserID == "" {
		return Presence{}, ErrInvalidUserID
	}
	if item.SessionID == "" {
		return Presence{}, ErrInvalidSessionID
	}
	if item.Status == "" {
		item.Status = StatusOnline
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	} else {
		item.UpdatedAt = item.UpdatedAt.UTC()
	}
	item.Metadata = normalizeMetadata(item.Metadata)
	return item, nil
}

func normSubIDs(subscriberID, targetID string) (string, string, error) {
	subscriberID = strings.TrimSpace(subscriberID)
	targetID = strings.TrimSpace(targetID)
	if subscriberID == "" || targetID == "" {
		return "", "", ErrInvalidUserID
	}
	return subscriberID, targetID, nil
}

func normalizeMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func clonePresence(item Presence) Presence {
	item.Metadata = cloneMetadata(item.Metadata)
	return item
}

func clonePresences(items []Presence) []Presence {
	if len(items) == 0 {
		return nil
	}
	out := make([]Presence, len(items))
	for i := range items {
		out[i] = clonePresence(items[i])
	}
	return out
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

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortPresences(items []Presence) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].UserID != items[j].UserID {
			return items[i].UserID < items[j].UserID
		}
		return items[i].SessionID < items[j].SessionID
	})
}

func sortSubscriptions(items []Subscription) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].SubscriberID != items[j].SubscriberID {
			return items[i].SubscriberID < items[j].SubscriberID
		}
		return items[i].TargetID < items[j].TargetID
	})
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
