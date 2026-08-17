package presence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/db"
)

// ErrNilRedisExecutor 表示 Redis presence manager 没有可用执行器。
var ErrNilRedisExecutor = errors.New("presence redis executor is required")

// redisPresenceTrack 原子维护 session、user 和反向索引。
const redisPresenceTrack = `
local presence_key = KEYS[1]
local session_user_key = KEYS[2]
local sessions_index_key = KEYS[3]
local users_index_key = KEYS[4]
local user_sessions_key = KEYS[5]
local user_sessions_prefix = ARGV[1]
local data = ARGV[2]
local user_id = ARGV[3]
local session_id = ARGV[4]
local ttl = tonumber(ARGV[5]) or 0
local old_user_id = redis.call("GET", session_user_key)
if old_user_id and old_user_id ~= user_id then
  local old_user_sessions_key = user_sessions_prefix .. old_user_id
  redis.call("SREM", old_user_sessions_key, session_id)
  if redis.call("SCARD", old_user_sessions_key) == 0 then
    redis.call("SREM", users_index_key, old_user_id)
  end
end
if ttl > 0 then
  redis.call("SET", presence_key, data, "EX", ttl)
  redis.call("SET", session_user_key, user_id, "EX", ttl)
else
  redis.call("SET", presence_key, data)
  redis.call("SET", session_user_key, user_id)
end
redis.call("SADD", sessions_index_key, session_id)
redis.call("SADD", users_index_key, user_id)
redis.call("SADD", user_sessions_key, session_id)
return "OK"
`

// redisPresenceRemove 会校验 expected_user_id，防止旧 gateway 把新绑定的 session 删除。
const redisPresenceRemove = `
local presence_key = KEYS[1]
local session_user_key = KEYS[2]
local sessions_index_key = KEYS[3]
local users_index_key = KEYS[4]
local user_sessions_prefix = ARGV[1]
local session_id = ARGV[2]
local expected_user_id = ARGV[3]
local user_id = redis.call("GET", session_user_key)
if user_id and user_id ~= "" and expected_user_id ~= "" and user_id ~= expected_user_id then
  return "mismatch"
end
if not user_id or user_id == "" then
  user_id = expected_user_id
end
redis.call("DEL", presence_key)
redis.call("DEL", session_user_key)
redis.call("SREM", sessions_index_key, session_id)
if user_id and user_id ~= "" then
  local user_sessions_key = user_sessions_prefix .. user_id
  redis.call("SREM", user_sessions_key, session_id)
  if redis.call("SCARD", user_sessions_key) == 0 then
    redis.call("SREM", users_index_key, user_id)
  end
end
return "removed"
`

// redisPresenceUpdate 在 Redis 内部读取、校验、修改 JSON 并刷新 TTL。
// 这样 Touch/SetStatus 不会发生跨 gateway 读旧值再覆盖新绑定的竞态。
const redisPresenceUpdate = `
local presence_key = KEYS[1]
local session_user_key = KEYS[2]
local expected_user_id = ARGV[1]
local status = ARGV[2]
local metadata_json = ARGV[3]
local metadata_set = ARGV[4]
local updated_at = ARGV[5]
local ttl = tonumber(ARGV[6]) or 0
local user_id = redis.call("GET", session_user_key)
if not user_id or user_id == "" then
  return "missing"
end
if expected_user_id ~= "" and user_id ~= expected_user_id then
  return "mismatch"
end
local raw = redis.call("GET", presence_key)
if not raw or raw == "" then
  return "missing"
end
local ok, item = pcall(cjson.decode, raw)
if not ok then
  return "decode_error"
end
if item["user_id"] and item["user_id"] ~= user_id then
  return "mismatch"
end
if status ~= "" then
  item["status"] = status
end
if metadata_set == "1" then
  if metadata_json == "" then
    item["metadata"] = nil
  else
    local meta_ok, metadata = pcall(cjson.decode, metadata_json)
    if not meta_ok then
      return "decode_error"
    end
    item["metadata"] = metadata
  end
end
item["updated_at"] = updated_at
local data = cjson.encode(item)
if ttl > 0 then
  redis.call("SET", presence_key, data, "EX", ttl)
  redis.call("SET", session_user_key, user_id, "EX", ttl)
else
  redis.call("SET", presence_key, data)
  redis.call("SET", session_user_key, user_id)
end
return "updated"
`

// redisSubScript 原子维护订阅和被订阅双索引。
const redisSubScript = `
local subscriptions_index_key = KEYS[1]
local subscribers_index_key = KEYS[2]
local subscriptions_key = KEYS[3]
local subscribers_key = KEYS[4]
local subscriber_id = ARGV[1]
local target_id = ARGV[2]
redis.call("SADD", subscriptions_index_key, subscriber_id)
redis.call("SADD", subscribers_index_key, target_id)
redis.call("SADD", subscriptions_key, target_id)
redis.call("SADD", subscribers_key, subscriber_id)
return "OK"
`

// redisUnsubScript 原子删除订阅和被订阅双索引。
const redisUnsubScript = `
local subscriptions_index_key = KEYS[1]
local subscribers_index_key = KEYS[2]
local subscriptions_key = KEYS[3]
local subscribers_key = KEYS[4]
local subscriber_id = ARGV[1]
local target_id = ARGV[2]
redis.call("SREM", subscriptions_key, target_id)
redis.call("SREM", subscribers_key, subscriber_id)
if redis.call("SCARD", subscriptions_key) == 0 then
  redis.call("SREM", subscriptions_index_key, subscriber_id)
end
if redis.call("SCARD", subscribers_key) == 0 then
  redis.call("SREM", subscribers_index_key, target_id)
end
return "OK"
`

// RedisOptions 控制 Redis 版 presence manager 的名称、执行器、key 前缀和 TTL。
type RedisOptions struct {
	Name      string
	Executor  db.RedisExecutor
	KeyPrefix string
	Now       func() time.Time
	TTL       time.Duration
}

// RedisManager 使用 Redis 共享在线状态和订阅关系，适合多 gateway 协同。
type RedisManager struct {
	initOnce sync.Once

	name     string
	executor db.RedisExecutor
	closer   interface{ Close() error }
	prefix   string
	now      func() time.Time
	ttl      time.Duration

	mu sync.Mutex
}

// NewRedis 创建 Redis 版 presence manager。
func NewRedis(options RedisOptions) *RedisManager {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "redis-presence"
	}
	prefix := strings.Trim(strings.TrimSpace(options.KeyPrefix), ":")
	if prefix == "" {
		prefix = "longheng:presence"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	manager := &RedisManager{
		name:     name,
		executor: options.Executor,
		closer:   redisCloser(options.Executor),
		prefix:   prefix,
		now:      now,
		ttl:      options.TTL,
	}
	manager.ensureReady()
	return manager
}

func redisCloser(executor db.RedisExecutor) interface{ Close() error } {
	closer, _ := executor.(interface{ Close() error })
	return closer
}

func (m *RedisManager) ensureReady() {
	m.initOnce.Do(func() {
		m.name = strings.TrimSpace(m.name)
		if m.name == "" {
			m.name = "redis-presence"
		}
		m.prefix = strings.Trim(strings.TrimSpace(m.prefix), ":")
		if m.prefix == "" {
			m.prefix = "longheng:presence"
		}
		if m.now == nil {
			m.now = time.Now
		}
		if m.closer == nil {
			m.closer = redisCloser(m.executor)
		}
	})
}

func requireRedisManager(m *RedisManager) error {
	if m == nil {
		return ErrNilRedisExecutor
	}
	m.ensureReady()
	return nil
}

// Close 关闭底层 Redis 连接；没有连接所有权时为空操作。
func (m *RedisManager) Close() error {
	if m == nil {
		return nil
	}
	m.ensureReady()
	if m.closer == nil {
		return nil
	}
	return m.closer.Close()
}

// Track 注册或替换某个 session 的 Redis 在线状态。
func (m *RedisManager) Track(ctx context.Context, item Presence) (Presence, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, err
	}
	if err := requireRedisManager(m); err != nil {
		return Presence{}, err
	}
	item, err := normalizePresence(item, m.now().UTC())
	if err != nil {
		return Presence{}, err
	}

	data, err := json.Marshal(item)
	if err != nil {
		return Presence{}, err
	}
	// Track 使用 Lua 同时写 session、user 索引和反向索引，避免多 gateway 下索引半更新。
	if _, err := m.do(ctx, "EVAL", redisPresenceTrack, 5,
		m.presenceKey(item.SessionID),
		m.sessionUserKey(item.SessionID),
		m.sessionsIndexKey(),
		m.usersIndexKey(),
		m.userSessionsKey(item.UserID),
		m.userSessionsPrefix(),
		data,
		item.UserID,
		item.SessionID,
		redisTTLSeconds(m.ttl),
	); err != nil {
		return Presence{}, err
	}
	return clonePresence(item), nil
}

// Untrack 移除指定 session 的 Redis 在线状态；userID 非空时必须匹配当前绑定。
func (m *RedisManager) Untrack(ctx context.Context, userID, sessionID string) (Presence, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, false, err
	}
	if err := requireRedisManager(m); err != nil {
		return Presence{}, false, err
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Presence{}, false, ErrInvalidSessionID
	}

	// 先读当前 session 再按 user 校验删除，避免断线事件误删已重绑的新 session。
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok, err := m.session(ctx, sessionID)
	if err != nil || !ok {
		return Presence{}, false, err
	}
	if userID != "" && item.UserID != userID {
		return Presence{}, false, nil
	}
	removed, err := m.removePresence(ctx, item)
	if err != nil {
		return Presence{}, false, err
	}
	if !removed {
		return Presence{}, false, nil
	}
	return clonePresence(item), true, nil
}

// Touch 刷新指定 session 的 Redis 在线更新时间。
func (m *RedisManager) Touch(ctx context.Context, userID, sessionID string, at time.Time) (Presence, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, false, err
	}
	if err := requireRedisManager(m); err != nil {
		return Presence{}, false, err
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Presence{}, false, ErrInvalidSessionID
	}
	if at.IsZero() {
		at = m.now()
	}

	// Touch 不在客户端侧改 JSON，而是交给 Lua 校验 user 后刷新 updated_at 和 TTL。
	updated, err := m.updatePresence(ctx, userID, sessionID, "", nil, false, at.UTC())
	if err != nil || !updated {
		return Presence{}, false, err
	}
	item, ok, err := m.session(ctx, sessionID)
	if err != nil || !ok {
		return Presence{}, false, err
	}
	return clonePresence(item), true, nil
}

// SetStatus 更新指定 session 的 Redis 在线状态和元数据。
func (m *RedisManager) SetStatus(ctx context.Context, userID, sessionID, status string, metadata map[string]string) (Presence, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, false, err
	}
	if err := requireRedisManager(m); err != nil {
		return Presence{}, false, err
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	status = strings.TrimSpace(status)
	if sessionID == "" {
		return Presence{}, false, ErrInvalidSessionID
	}
	if status == "" {
		return Presence{}, false, ErrInvalidStatus
	}

	// metadata 传 nil 表示显式清空；调用方要保留旧 metadata 时不要调用 SetStatus。
	updated, err := m.updatePresence(ctx, userID, sessionID, status, normalizeMetadata(metadata), true, m.now().UTC())
	if err != nil || !updated {
		return Presence{}, false, err
	}
	item, ok, err := m.session(ctx, sessionID)
	if err != nil || !ok {
		return Presence{}, false, err
	}
	return clonePresence(item), true, nil
}

// Presence 查询某个用户在 Redis 中的所有在线 session。
func (m *RedisManager) Presence(ctx context.Context, userID string) ([]Presence, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := requireRedisManager(m); err != nil {
		return nil, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, ErrInvalidUserID
	}
	value, err := m.do(ctx, "SMEMBERS", m.userSessionsKey(userID))
	if err != nil {
		return nil, err
	}
	sessionIDs, err := redisStrings(value)
	if err != nil {
		return nil, err
	}
	sort.Strings(sessionIDs)
	out := make([]Presence, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		item, ok, err := m.session(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if !ok {
			// 读到过期 session 时顺手清理用户反向索引，降低长期高 churn 下的脏索引。
			_, _ = m.do(ctx, "SREM", m.userSessionsKey(userID), sessionID)
			continue
		}
		out = append(out, clonePresence(item))
	}
	_ = m.cleanupUserIndex(ctx, userID)
	sortPresences(out)
	return out, nil
}

// Session 查询 Redis 中指定 session 的在线状态。
func (m *RedisManager) Session(ctx context.Context, sessionID string) (Presence, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Presence{}, false, err
	}
	if err := requireRedisManager(m); err != nil {
		return Presence{}, false, err
	}
	return m.session(ctx, sessionID)
}

// Subscribe 在 Redis 中订阅目标用户在线状态变化。
func (m *RedisManager) Subscribe(ctx context.Context, subscriberID, targetID string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := requireRedisManager(m); err != nil {
		return err
	}
	// Subscribe 通过 Lua 同时维护 subscriber->targets 和 target->subscribers 两个索引，避免并发订阅留下半边索引。
	subscriberID, targetID, err := normSubIDs(subscriberID, targetID)
	if err != nil {
		return err
	}
	_, err = m.do(ctx, "EVAL", redisSubScript, 4,
		m.subIndexKey(),
		m.subscribersIndexKey(),
		m.subscriptionsKey(subscriberID),
		m.subscribersKey(targetID),
		subscriberID,
		targetID,
	)
	return err
}

// Unsubscribe 在 Redis 中取消订阅目标用户在线状态变化。
func (m *RedisManager) Unsubscribe(ctx context.Context, subscriberID, targetID string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := requireRedisManager(m); err != nil {
		return err
	}
	// Unsubscribe 也用 Lua 做双索引删除；当集合为空时同步清 index，降低 stale cleanup 压力。
	subscriberID, targetID, err := normSubIDs(subscriberID, targetID)
	if err != nil {
		return err
	}
	_, err = m.do(ctx, "EVAL", redisUnsubScript, 4,
		m.subIndexKey(),
		m.subscribersIndexKey(),
		m.subscriptionsKey(subscriberID),
		m.subscribersKey(targetID),
		subscriberID,
		targetID,
	)
	return err
}

// Subscribers 返回 Redis 中订阅目标用户的所有订阅者。
func (m *RedisManager) Subscribers(ctx context.Context, targetID string) ([]string, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := requireRedisManager(m); err != nil {
		return nil, err
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return nil, ErrInvalidUserID
	}
	value, err := m.do(ctx, "SMEMBERS", m.subscribersKey(targetID))
	if err != nil {
		return nil, err
	}
	out, err := redisStrings(value)
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// Subscriptions 返回 Redis 中订阅者关注的所有目标用户。
func (m *RedisManager) Subscriptions(ctx context.Context, subscriberID string) ([]string, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := requireRedisManager(m); err != nil {
		return nil, err
	}
	subscriberID = strings.TrimSpace(subscriberID)
	if subscriberID == "" {
		return nil, ErrInvalidUserID
	}
	value, err := m.do(ctx, "SMEMBERS", m.subscriptionsKey(subscriberID))
	if err != nil {
		return nil, err
	}
	out, err := redisStrings(value)
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// Broadcast 将 Redis 中目标用户当前在线状态广播给所有订阅者。
func (m *RedisManager) Broadcast(ctx context.Context, targetID string, send SendFunc) error {
	if send == nil {
		return ErrNilSender
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := requireRedisManager(m); err != nil {
		return err
	}
	subscribers, err := m.Subscribers(ctx, targetID)
	if err != nil {
		return err
	}
	presences, err := m.Presence(ctx, targetID)
	if err != nil {
		return err
	}
	// Broadcast 只负责 fanout 当前在线状态，不提供 ack 或离线补偿。
	// 需要可靠通知的业务应把事件先落 EventLog/Outbox。
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

// Snapshot 返回 Redis presence manager 当前快照；Redis 不可用时返回带名称的空快照。
func (m *RedisManager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.ensureReady()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snapshot, err := m.snapshot(ctx)
	if err != nil {
		return Snapshot{Name: m.name}
	}
	return snapshot
}

func (m *RedisManager) session(ctx context.Context, sessionID string) (Presence, bool, error) {
	if err := requireRedisManager(m); err != nil {
		return Presence{}, false, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Presence{}, false, ErrInvalidSessionID
	}
	value, err := m.do(ctx, "GET", m.presenceKey(sessionID))
	if err != nil {
		return Presence{}, false, err
	}
	data, ok, err := redisBytes(value)
	if err != nil || !ok {
		return Presence{}, ok, err
	}
	var item Presence
	if err := json.Unmarshal(data, &item); err != nil {
		return Presence{}, false, fmt.Errorf("decode presence: %w", err)
	}
	item.Metadata = normalizeMetadata(item.Metadata)
	return item, true, nil
}

func (m *RedisManager) updatePresence(ctx context.Context, userID, sessionID, status string, metadata map[string]string, updateMetadata bool, updatedAt time.Time) (bool, error) {
	if err := requireRedisManager(m); err != nil {
		return false, err
	}
	metadataJSON := ""
	if metadata != nil {
		data, err := json.Marshal(metadata)
		if err != nil {
			return false, err
		}
		metadataJSON = string(data)
	}
	statusText, err := redisString(m.do(ctx, "EVAL", redisPresenceUpdate, 2,
		m.presenceKey(sessionID),
		m.sessionUserKey(sessionID),
		userID,
		status,
		metadataJSON,
		boolRedisArg(updateMetadata),
		updatedAt.Format(time.RFC3339Nano),
		redisTTLSeconds(m.ttl),
	))
	if err != nil {
		return false, err
	}
	switch statusText {
	case "updated":
		return true, nil
	case "missing", "mismatch":
		return false, nil
	case "decode_error":
		return false, fmt.Errorf("decode presence")
	default:
		return false, fmt.Errorf("unexpected redis presence update status %q", statusText)
	}
}

func boolRedisArg(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func (m *RedisManager) removePresence(ctx context.Context, item Presence) (bool, error) {
	if err := requireRedisManager(m); err != nil {
		return false, err
	}
	status, err := redisString(m.do(ctx, "EVAL", redisPresenceRemove, 4,
		m.presenceKey(item.SessionID),
		m.sessionUserKey(item.SessionID),
		m.sessionsIndexKey(),
		m.usersIndexKey(),
		m.userSessionsPrefix(),
		item.SessionID,
		item.UserID,
	))
	if err != nil {
		return false, err
	}
	return status == "removed", nil
}

func (m *RedisManager) snapshot(ctx context.Context) (Snapshot, error) {
	if err := requireRedisManager(m); err != nil {
		return Snapshot{}, err
	}
	sessionIDs, err := m.redisSet(ctx, m.sessionsIndexKey())
	if err != nil {
		return Snapshot{}, err
	}
	presences := make([]Presence, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		item, ok, err := m.session(ctx, sessionID)
		if err != nil {
			return Snapshot{}, err
		}
		if !ok {
			// Snapshot 是管理面读路径，可以顺带清理过期 session 索引。
			_, _ = m.do(ctx, "SREM", m.sessionsIndexKey(), sessionID)
			continue
		}
		presences = append(presences, item)
	}
	sortPresences(presences)

	userIDs, err := m.redisSet(ctx, m.usersIndexKey())
	if err != nil {
		return Snapshot{}, err
	}
	userCount := 0
	for _, userID := range userIDs {
		presences, err := m.Presence(ctx, userID)
		if err != nil {
			return Snapshot{}, err
		}
		if len(presences) > 0 {
			userCount++
		}
	}

	subscriberIDs, err := m.redisSet(ctx, m.subIndexKey())
	if err != nil {
		return Snapshot{}, err
	}
	subscriptions := make([]Subscription, 0)
	for _, subscriberID := range subscriberIDs {
		targets, err := m.Subscriptions(ctx, subscriberID)
		if err != nil {
			return Snapshot{}, err
		}
		if len(targets) == 0 {
			// 订阅者没有任何目标时清掉全局索引，避免长期压测后列表膨胀。
			_, _ = m.do(ctx, "SREM", m.subIndexKey(), subscriberID)
			continue
		}
		for _, targetID := range targets {
			subscriptions = append(subscriptions, Subscription{SubscriberID: subscriberID, TargetID: targetID})
		}
	}
	sortSubscriptions(subscriptions)

	return Snapshot{
		Name:              m.name,
		PresenceCount:     len(presences),
		UserCount:         userCount,
		SubscriptionCount: len(subscriptions),
		Presences:         presences,
		Subscriptions:     subscriptions,
	}, nil
}

func (m *RedisManager) cleanupUserIndex(ctx context.Context, userID string) error {
	if err := requireRedisManager(m); err != nil {
		return err
	}
	count, err := m.redisInt(ctx, "SCARD", m.userSessionsKey(userID))
	if err != nil {
		return err
	}
	if count == 0 {
		_, err = m.do(ctx, "SREM", m.usersIndexKey(), userID)
	}
	return err
}

func (m *RedisManager) redisSet(ctx context.Context, key string) ([]string, error) {
	if err := requireRedisManager(m); err != nil {
		return nil, err
	}
	value, err := m.do(ctx, "SMEMBERS", key)
	if err != nil {
		return nil, err
	}
	out, err := redisStrings(value)
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func (m *RedisManager) redisInt(ctx context.Context, command string, args ...any) (int64, error) {
	if err := requireRedisManager(m); err != nil {
		return 0, err
	}
	value, err := m.do(ctx, command, args...)
	if err != nil {
		return 0, err
	}
	return redisInt64(value)
}

func (m *RedisManager) do(ctx context.Context, command string, args ...any) (any, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := requireRedisManager(m); err != nil {
		return nil, err
	}
	if m.executor == nil {
		return nil, ErrNilRedisExecutor
	}
	return m.executor.Do(ctx, command, args...)
}

func (m *RedisManager) presenceKey(sessionID string) string {
	return m.prefix + ":session:" + sessionID
}

func (m *RedisManager) sessionUserKey(sessionID string) string {
	return m.prefix + ":session_user:" + sessionID
}

func (m *RedisManager) userSessionsKey(userID string) string {
	return m.prefix + ":user_sessions:" + userID
}

func (m *RedisManager) userSessionsPrefix() string {
	return m.prefix + ":user_sessions:"
}

func (m *RedisManager) sessionsIndexKey() string {
	return m.prefix + ":sessions"
}

func (m *RedisManager) usersIndexKey() string {
	return m.prefix + ":users"
}

func (m *RedisManager) subscriptionsKey(subscriberID string) string {
	return m.prefix + ":subscriptions:" + subscriberID
}

func (m *RedisManager) subscribersKey(targetID string) string {
	return m.prefix + ":subscribers:" + targetID
}

func (m *RedisManager) subIndexKey() string {
	return m.prefix + ":subscriptions"
}

func (m *RedisManager) subscribersIndexKey() string {
	return m.prefix + ":subscribers"
}

func redisTTLSeconds(ttl time.Duration) int64 {
	if ttl <= 0 {
		return 0
	}
	seconds := int64(ttl / time.Second)
	if seconds <= 0 {
		return 1
	}
	return seconds
}

func redisBytes(value any) ([]byte, bool, error) {
	switch v := value.(type) {
	case nil:
		return nil, false, nil
	case []byte:
		return append([]byte(nil), v...), true, nil
	case string:
		return []byte(v), true, nil
	default:
		return nil, false, fmt.Errorf("unexpected redis bulk type %T", value)
	}
}

func redisString(value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	switch v := value.(type) {
	case nil:
		return "", nil
	case []byte:
		return string(v), nil
	case string:
		return v, nil
	default:
		return "", fmt.Errorf("unexpected redis string type %T", value)
	}
}

func redisStrings(value any) ([]string, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return append([]string(nil), v...), nil
	case [][]byte:
		out := make([]string, len(v))
		for i := range v {
			out[i] = string(v[i])
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			switch x := item.(type) {
			case []byte:
				out = append(out, string(x))
			case string:
				out = append(out, x)
			default:
				return nil, fmt.Errorf("unexpected redis string item %T", item)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unexpected redis string slice type %T", value)
	}
}

func redisInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case uint64:
		const maxInt64 = uint64(1<<63 - 1)
		if v > maxInt64 {
			return 0, fmt.Errorf("redis integer overflows int64: %d", v)
		}
		return int64(v), nil
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("unexpected redis integer type %T", value)
	}
}
