package ratelimit

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

// ErrRedisExecutorRequired 在 RedisLimiter 没有配置 executor 时返回。
var ErrRedisExecutorRequired = errors.New("ratelimit redis executor is required")

// FailPolicy 描述 Redis 不可用时的行为。
type FailPolicy string

const (
	// FailOpen 在 Redis 出错或超时时放行请求（适合非关键 HTTP 路径）。
	FailOpen FailPolicy = "open"
	// FailClosed 在 Redis 出错或超时时拒绝请求（适合管理接口、危险操作）。
	FailClosed FailPolicy = "closed"
)

// 默认的 token bucket Lua 脚本：所有限流计算在 Redis 内部一次完成，避免
// "GET → 计算 → SET" 类竞争。返回 {allowed, retry_after_ms}。
//
// KEYS[1]   bucket key（每个 rule + identity 一个）
// ARGV[1]   capacity（桶容量，浮点）
// ARGV[2]   refill_per_second（每秒补充令牌数，浮点）
// ARGV[3]   now_ms（当前时间，毫秒）
// ARGV[4]   ttl_ms（key 过期时间，毫秒，应该 >= bucket 满后多保留一段）
// #nosec G101 -- Lua 脚本里的 token 表示限流令牌，不是硬编码凭证。
const redisBucketScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_per_second = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local ttl_ms = tonumber(ARGV[4])
if capacity <= 0 then return {0, ttl_ms} end
if refill_per_second <= 0 then return {0, ttl_ms} end
local data = redis.call("HMGET", key, "tokens", "updated_at_ms")
local tokens = tonumber(data[1])
local last_ms = tonumber(data[2])
if tokens == nil then
  tokens = capacity
  last_ms = now_ms
end
local elapsed_ms = now_ms - last_ms
if elapsed_ms < 0 then elapsed_ms = 0 end
tokens = tokens + (elapsed_ms / 1000.0) * refill_per_second
if tokens > capacity then tokens = capacity end
local allowed = 0
local retry_after_ms = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  local missing = 1 - tokens
  retry_after_ms = math.ceil(missing / refill_per_second * 1000.0)
  if retry_after_ms < 1 then retry_after_ms = 1 end
end
redis.call("HMSET", key, "tokens", tostring(tokens), "updated_at_ms", tostring(now_ms))
redis.call("PEXPIRE", key, ttl_ms)
return {allowed, retry_after_ms}
`

// RedisConfig 用来构造 RedisLimiter。Rules / Window / MaxRequests 语义与单机
// Limiter 完全一致，便于同一套配置在 single-node 演示和 production 之间切换。
type RedisConfig struct {
	Enabled     bool
	Executor    db.RedisExecutor
	KeyPrefix   string
	Window      time.Duration
	MaxRequests int
	Rules       []Rule
	FailPolicy  FailPolicy
	Now         func() time.Time
}

// RedisLimiter 用 Redis Lua token bucket 提供跨节点一致的限流。
// 多个 gateway / logic / adminops 实例共用一组 Redis 即可看到同一个桶。
//
// 与 Limiter 的关键差异：
//   - 失败需要返回 error，HTTP 包装路径会根据 FailPolicy 决定放行或拒绝。
//   - 不再维护进程内 buckets / cleanup；TTL 交给 Redis。
//   - 不支持 trusted-proxy header 识别身份，调用方传 identity；HTTP 包装
//     仍可复用 *Limiter 的 identity 提取逻辑，但 RedisLimiter 自身保持
//     "传 key + path 进来" 的纯函数语义。
type RedisLimiter struct {
	enabled     bool
	executor    db.RedisExecutor
	keyPrefix   string
	defaultRule Rule
	rules       []Rule
	failPolicy  FailPolicy
	now         func() time.Time
}

// NewRedis 构造 RedisLimiter；当 executor 为 nil 时返回错误，避免静默退化为单机。
func NewRedis(cfg RedisConfig) (*RedisLimiter, error) {
	if cfg.Executor == nil {
		return nil, ErrRedisExecutorRequired
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Second
	}
	if cfg.MaxRequests <= 0 {
		cfg.MaxRequests = 60
	}
	prefix := strings.Trim(strings.TrimSpace(cfg.KeyPrefix), ":")
	if prefix == "" {
		prefix = "longheng:ratelimit"
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	policy := normalizeFailPolicy(cfg.FailPolicy)
	rules := normalizeRules(cfg.Rules, cfg.Window, cfg.MaxRequests)
	return &RedisLimiter{
		enabled:   cfg.Enabled,
		executor:  cfg.Executor,
		keyPrefix: prefix,
		defaultRule: Rule{
			Path:        "*",
			Window:      cfg.Window,
			WindowSec:   int64(cfg.Window / time.Second),
			MaxRequests: cfg.MaxRequests,
			Prefix:      true,
		},
		rules:      rules,
		failPolicy: policy,
		now:        now,
	}, nil
}

// Enabled 让 RedisLimiter 与 *Limiter 在调用上保持兼容。
func (r *RedisLimiter) Enabled() bool {
	if r == nil {
		return false
	}
	return r.enabled
}

// FailPolicy 暴露当前的失败策略，便于上层在调用 AllowKeyCtx 出错时做决策。
func (r *RedisLimiter) FailPolicy() FailPolicy {
	if r == nil {
		return FailOpen
	}
	return normalizeFailPolicy(r.failPolicy)
}

// AllowKeyCtx 是核心入口。返回 allowed / retry-after / error。
// error != nil 时调用方应配合 FailPolicy 决定放行还是拒绝。
func (r *RedisLimiter) AllowKeyCtx(ctx context.Context, key string, path string) (bool, time.Duration, error) {
	if r == nil {
		return true, 0, nil
	}
	if !r.enabled {
		return true, 0, nil
	}
	if r.executor == nil {
		return false, 0, ErrRedisExecutorRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}
	rule := r.matchRule(path)
	window := rule.Window
	if window <= 0 {
		window = time.Second
	}
	capacity := float64(rule.MaxRequests)
	if capacity <= 0 {
		capacity = 1
	}
	refillPerSecond := capacity / window.Seconds()
	if refillPerSecond <= 0 {
		refillPerSecond = capacity
	}
	nowFunc := r.now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	now := nowFunc()
	bucketKey := r.bucketKey(key, rule)
	// TTL 给 bucket 至少留两个 window 的余量，避免活跃用户被频繁重建。
	ttlMS := int64(window/time.Millisecond)*2 + 1000

	raw, err := r.executor.Do(ctx, "EVAL", redisBucketScript,
		1, bucketKey,
		strconv.FormatFloat(capacity, 'f', -1, 64),
		strconv.FormatFloat(refillPerSecond, 'f', -1, 64),
		strconv.FormatInt(now.UnixNano()/int64(time.Millisecond), 10),
		strconv.FormatInt(ttlMS, 10),
	)
	if err != nil {
		return false, 0, fmt.Errorf("ratelimit redis EVAL failed: %w", err)
	}
	allowed, retryAfter, err := parseBucketResp(raw)
	if err != nil {
		return false, 0, err
	}
	return allowed, retryAfter, nil
}

// Allow 提供与 *Limiter 一致的便捷形式，使用 Background context。
// 注意：失败语义由 FailPolicy 决定 —— FailOpen 放行、FailClosed 拒绝。
// 需要保留 error 的调用方应直接使用 AllowKeyCtx。
func (r *RedisLimiter) Allow(identity, path string) (bool, time.Duration) {
	allowed, retry, err := r.AllowKeyCtx(context.Background(), identity, path)
	if err != nil {
		return r.FailPolicy() == FailOpen, retry
	}
	return allowed, retry
}

// AllowKey 与 *Limiter.AllowKey 同名，便于在 wiring 层用 Allower 接口互换实现。
func (r *RedisLimiter) AllowKey(key, path string) (bool, time.Duration) {
	return r.Allow(key, path)
}

func (r *RedisLimiter) matchRule(path string) Rule {
	path = strings.TrimSpace(path)
	best := Rule{}
	for _, rule := range r.rules {
		if rule.Prefix {
			if strings.HasPrefix(path, rule.Path) && len(rule.Path) > len(best.Path) {
				best = rule
			}
			continue
		}
		if path == rule.Path && len(rule.Path) > len(best.Path) {
			best = rule
		}
	}
	if best.Path != "" {
		return best
	}
	return r.fallbackRule()
}

func (r *RedisLimiter) bucketKey(identity string, rule Rule) string {
	rulePath := rule.Path
	if rulePath == "" {
		rulePath = "*"
	}
	prefix := strings.Trim(strings.TrimSpace(r.keyPrefix), ":")
	if prefix == "" {
		prefix = "longheng:ratelimit"
	}
	return prefix + ":" + encodeRedisPart(identity) + ":" + encodeRedisPart(rulePath)
}

func (r *RedisLimiter) fallbackRule() Rule {
	rule := r.defaultRule
	if rule.Path == "" {
		rule.Path = "*"
		rule.Prefix = true
	}
	if rule.Window <= 0 {
		rule.Window = time.Second
	}
	if rule.MaxRequests <= 0 {
		rule.MaxRequests = 60
	}
	rule.WindowSec = int64(rule.Window / time.Second)
	return rule
}

func encodeRedisPart(value string) string {
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func normalizeFailPolicy(v FailPolicy) FailPolicy {
	switch FailPolicy(strings.ToLower(strings.TrimSpace(string(v)))) {
	case FailOpen, "":
		return FailOpen
	case FailClosed:
		return FailClosed
	default:
		return FailOpen
	}
}

// parseBucketResp 解析 EVAL 的返回值。RedisExecutor.Do 返回 any，
// 实际类型取决于驱动：redigo 返回 []interface{} + 元素为 int64；其他驱动
// 可能返回 []any 或 nested 类型。这里兼容常见情况，遇到未知形态时给出
// 明确错误，避免静默放行。
func parseBucketResp(raw any) (bool, time.Duration, error) {
	switch v := raw.(type) {
	case []any:
		return parseBucketArray(v)
	default:
		return false, 0, fmt.Errorf("ratelimit redis returned unexpected type %T", raw)
	}
}

func parseBucketArray(values []any) (bool, time.Duration, error) {
	if len(values) < 2 {
		return false, 0, fmt.Errorf("ratelimit redis returned %d fields, want 2", len(values))
	}
	allowed, err := toInt64(values[0])
	if err != nil {
		return false, 0, fmt.Errorf("ratelimit redis allowed field: %w", err)
	}
	retryMS, err := toInt64(values[1])
	if err != nil {
		return false, 0, fmt.Errorf("ratelimit redis retry_after field: %w", err)
	}
	if retryMS < 0 {
		retryMS = 0
	}
	return allowed == 1, time.Duration(retryMS) * time.Millisecond, nil
}

func toInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case uint64:
		if n > uint64(1<<63-1) {
			return 0, fmt.Errorf("uint64 value overflows int64: %d", n)
		}
		return int64(n), nil
	case []byte:
		return strconv.ParseInt(string(n), 10, 64)
	case string:
		return strconv.ParseInt(n, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported numeric type %T", v)
	}
}
