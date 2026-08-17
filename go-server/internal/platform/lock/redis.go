package lock

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
	"longheng.io/server/internal/platform/db"
)

// ErrNilRedisExecutor 表示 Redis 锁缺少执行器，无法访问外部分布式存储。
var ErrNilRedisExecutor = errors.New("lock redis executor is required")

// #nosec G101 -- Lua 脚本里的 token 是锁令牌变量名，不是硬编码凭证。
const redisReleaseToken = `
local token = redis.call("GET", KEYS[1])
if not token then
  return "missing"
end
if token == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return "mismatch"
`

const RedisReleaseIfTokenScript = redisReleaseToken

// RedisOptions 配置 Redis 分布式锁管理器。
type RedisOptions struct {
	Name           string
	Executor       db.RedisExecutor
	KeyPrefix      string
	Address        string
	Password       string
	DB             int
	PoolSize       int
	Timeout        time.Duration
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	DefaultTTL     time.Duration
	TokenGenerator TokenGenerator
}

// RedisManager 基于 Redis SET NX PX 和 token 校验脚本提供分布式租约。
type RedisManager struct {
	mu             sync.RWMutex
	name           string
	keyPrefix      string
	defaultTTL     time.Duration
	executor       db.RedisExecutor
	closer         interface{ Close() error }
	tokenGenerator TokenGenerator
	metrics        *Metrics
}

// NewRedis 创建 Redis 分布式锁管理器；未传 Executor 时会按地址配置创建 redigo 执行器。
func NewRedis(options RedisOptions) *RedisManager {
	options = normRedisOpts(options)
	executor := options.Executor
	var closer interface{ Close() error }
	if executor == nil {
		redigo := db.NewRedigoExecutor(db.RedigoOptions{
			Address:        options.Address,
			Password:       options.Password,
			DB:             options.DB,
			PoolSize:       options.PoolSize,
			Timeout:        options.Timeout,
			ConnectTimeout: options.ConnectTimeout,
			ReadTimeout:    options.ReadTimeout,
			WriteTimeout:   options.WriteTimeout,
		})
		executor = redigo
		closer = redigo
	} else if candidate, ok := executor.(interface{ Close() error }); ok {
		closer = candidate
	}
	return &RedisManager{
		name:           options.Name,
		keyPrefix:      options.KeyPrefix,
		defaultTTL:     options.DefaultTTL,
		executor:       executor,
		closer:         closer,
		tokenGenerator: options.TokenGenerator,
	}
}

// SetMetrics 替换 Redis 锁采集器；Acquire 会在读锁内取快照后再记录。
func (m *RedisManager) SetMetrics(metrics *Metrics) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.metrics = metrics
	m.mu.Unlock()
}

// Acquire 尝试在 Redis 中获取指定 key 的租约。
func (m *RedisManager) Acquire(ctx context.Context, key string, ttl time.Duration) (lease Lease, err error) {
	if m == nil {
		return nil, ErrManagerRequired
	}
	m.mu.RLock()
	metricsRef := m.metrics
	defaultTTL := m.defaultTTL
	executor := m.executor
	tokenGenerator := m.tokenGenerator
	keyPrefix := m.keyPrefix
	m.mu.RUnlock()
	defer func() {
		metricsRef.recordAcquire(classifyAcquire(err))
	}()
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	key = normalizeKey(key)
	if key == "" {
		return nil, ErrKeyRequired
	}
	if executor == nil {
		return nil, ErrNilRedisExecutor
	}
	if ttl <= 0 {
		ttl = defaultTTL
		if ttl <= 0 {
			ttl = 10 * time.Second
		}
	}
	if tokenGenerator == nil {
		tokenGenerator = randomToken
	}
	token, err := tokenGenerator()
	if err != nil {
		return nil, err
	}
	ok, err := AcquireRedis(ctx, executor, redisLockKey(keyPrefix, key), token, ttl)
	if errors.Is(err, redis.ErrNil) {
		return nil, ErrLockHeld
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLockHeld
	}
	return &redisLease{manager: m, key: key, token: token, expiresAt: time.Now().UTC().Add(ttl)}, nil
}

// Snapshot 返回 Redis 锁管理器的配置状态。
func (m *RedisManager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{Kind: "redis", Closed: true}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Snapshot{
		Name:      m.name,
		Kind:      "redis",
		KeyPrefix: m.keyPrefix,
	}
}

// Close 关闭由管理器持有的 Redis 连接池；外部注入的无 Close 执行器会被忽略。
func (m *RedisManager) Close() error {
	if m == nil || m.closer == nil {
		return nil
	}
	m.mu.RLock()
	closer := m.closer
	m.mu.RUnlock()
	if closer == nil {
		return nil
	}
	return closer.Close()
}

func (m *RedisManager) release(ctx context.Context, key, token string) error {
	if m == nil {
		return ErrManagerRequired
	}
	m.mu.RLock()
	executor := m.executor
	keyPrefix := m.keyPrefix
	m.mu.RUnlock()
	if executor == nil {
		return ErrNilRedisExecutor
	}
	return ReleaseRedis(ctx, executor, redisLockKey(keyPrefix, key), token)
}

// AcquireRedis 使用 Redis 原生命令获取一个带 TTL 的分布式锁。
func AcquireRedis(ctx context.Context, executor db.RedisExecutor, key, token string, ttl time.Duration) (bool, error) {
	if executor == nil {
		return false, ErrNilRedisExecutor
	}
	key = normalizeKey(key)
	token = strings.TrimSpace(token)
	if key == "" {
		return false, ErrKeyRequired
	}
	if token == "" {
		return false, ErrTokenMismatch
	}
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	result, err := redis.String(executor.Do(ctx, "SET", key, token, "NX", "PX", ttlMillis(ttl)))
	if errors.Is(err, redis.ErrNil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(result), "OK"), nil
}

// ReleaseRedis 只在 token 匹配时释放 Redis 锁，避免旧租约删除新持有者。
func ReleaseRedis(ctx context.Context, executor db.RedisExecutor, key, token string) error {
	if executor == nil {
		return ErrNilRedisExecutor
	}
	key = normalizeKey(key)
	token = strings.TrimSpace(token)
	if key == "" {
		return ErrKeyRequired
	}
	if token == "" {
		return ErrTokenMismatch
	}
	result, err := executor.Do(ctx, "EVAL", redisReleaseToken, 1, key, token)
	if err != nil {
		return err
	}
	return redisReleaseResult(result)
}

func redisReleaseResult(result any) error {
	switch value := result.(type) {
	case int64:
		if value == 1 {
			return nil
		}
	case []byte:
		if string(value) == "1" || string(value) == "missing" {
			return nil
		}
	case string:
		if value == "1" || value == "missing" {
			return nil
		}
	}
	if text, err := redis.String(result, nil); err == nil && (text == "1" || text == "missing") {
		return nil
	}
	return ErrTokenMismatch
}

type redisLease struct {
	manager   *RedisManager
	key       string
	token     string
	expiresAt time.Time
}

func (l *redisLease) Key() string {
	return l.key
}

func (l *redisLease) Token() string {
	return l.token
}

func (l *redisLease) ExpiresAt() time.Time {
	return l.expiresAt
}

func (l *redisLease) Release(ctx context.Context) error {
	if l == nil || l.manager == nil {
		return nil
	}
	releaseCtx, cancel := releaseContext(ctx)
	defer cancel()
	return l.manager.release(releaseCtx, l.key, l.token)
}

func redisLockKey(prefix, key string) string {
	key = normalizeKey(key)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return key
	}
	return prefix + ":" + key
}

func normRedisOpts(options RedisOptions) RedisOptions {
	options.Name = strings.TrimSpace(options.Name)
	if options.Name == "" {
		options.Name = "redis-lock"
	}
	options.KeyPrefix = strings.TrimSpace(options.KeyPrefix)
	if options.KeyPrefix == "" {
		options.KeyPrefix = "lock"
	}
	options.Address = strings.TrimSpace(options.Address)
	if options.Address == "" {
		options.Address = "127.0.0.1:6379"
	}
	if options.PoolSize <= 0 {
		options.PoolSize = 8
	}
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Second
	}
	if options.ConnectTimeout <= 0 {
		options.ConnectTimeout = options.Timeout
	}
	if options.ReadTimeout <= 0 {
		options.ReadTimeout = options.Timeout
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = options.Timeout
	}
	if options.DefaultTTL <= 0 {
		options.DefaultTTL = 10 * time.Second
	}
	if options.TokenGenerator == nil {
		options.TokenGenerator = randomToken
	}
	return options
}

func ttlMillis(ttl time.Duration) int64 {
	ms := int64(ttl / time.Millisecond)
	if ms <= 0 {
		return 1
	}
	return ms
}
