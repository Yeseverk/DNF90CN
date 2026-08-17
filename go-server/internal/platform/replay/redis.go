package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

var (
	// ErrReplayRedisExecutorRequired 保留旧 API 兼容；新代码统一用 ErrReplayStoreUnavailable 判断装配缺失。
	ErrReplayRedisExecutorRequired = ErrReplayStoreUnavailable
	// ErrReplayRedisOperationFailed 通过 errors.Is 暴露 Redis 写入失败，上层应拒绝请求并让调用方重试。
	ErrReplayRedisOperationFailed = errors.New("replay redis operation failed")
	// ErrReplayRedisUnexpectedReply 通过 errors.Is 暴露驱动返回值异常，避免未知结果被当成放行。
	ErrReplayRedisUnexpectedReply = errors.New("replay redis returned unexpected reply")
)

// RedisOptions 是 Redis 重放保护 store 的装配参数。
type RedisOptions struct {
	Executor  db.RedisExecutor
	KeyPrefix string
}

// RedisStore 使用 Redis SET NX EX 实现跨进程 nonce 去重。
type RedisStore struct {
	executor  db.RedisExecutor
	keyPrefix string
}

const redisSeenScript = `
local ok = redis.call("SET", KEYS[1], "1", "NX", "EX", ARGV[1])
if ok then
	return 1
end
return 0
`

// NewRedisStore 创建 Redis 重放保护 store；KeyPrefix 为空时使用框架默认前缀。
func NewRedisStore(opts RedisOptions) *RedisStore {
	prefix := strings.Trim(strings.TrimSpace(opts.KeyPrefix), ":")
	if prefix == "" {
		prefix = "longheng:replay"
	}
	return &RedisStore{executor: opts.Executor, keyPrefix: prefix}
}

// Seen 记录 nonce 并返回它是否首次出现；Redis 不可用时不会放行请求。
func (s *RedisStore) Seen(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ctx, key, err := normalizeInput(ctx, key, ttl)
	if err != nil {
		return false, err
	}
	if s == nil || s.executor == nil {
		return false, ErrReplayStoreUnavailable
	}
	reply, err := s.executor.Do(ctx, "EVAL", redisSeenScript, 1, s.redisKey(key), ttlSeconds(ttl))
	if err != nil {
		return false, wrapRedisError(err)
	}
	return parseSeenReply(reply)
}

func (s *RedisStore) redisKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return s.keyPrefix + ":" + hex.EncodeToString(sum[:])
}

func ttlSeconds(ttl time.Duration) int64 {
	seconds := int64(math.Ceil(ttl.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

func parseSeenReply(reply any) (bool, error) {
	switch v := reply.(type) {
	case int:
		if v == 1 {
			return true, nil
		}
		if v == 0 {
			return false, nil
		}
	case int64:
		if v == 1 {
			return true, nil
		}
		if v == 0 {
			return false, nil
		}
	case string:
		switch strings.TrimSpace(v) {
		case "1":
			return true, nil
		case "0":
			return false, nil
		}
	case []byte:
		switch strings.TrimSpace(string(v)) {
		case "1":
			return true, nil
		case "0":
			return false, nil
		}
	}
	return false, fmt.Errorf("%w: %T", ErrReplayRedisUnexpectedReply, reply)
}
