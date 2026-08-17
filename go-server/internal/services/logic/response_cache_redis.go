package logic

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/pkg/contracts"
)

type redisRespCache struct {
	keyPrefix string
	ttl       time.Duration
	pool      *redis.Pool
}

// closeRedisConnErr 在响应缓存操作成功时保留连接关闭错误。
func closeRedisConnErr(conn redis.Conn, err *error) {
	if conn == nil || err == nil {
		return
	}
	if closeErr := conn.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

func newRedisRespCache(cfg config.IdempotencySection, ttl time.Duration) *redisRespCache {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	address := strings.TrimSpace(cfg.RedisAddress)
	if address == "" {
		address = "127.0.0.1:6379"
	}
	poolSize := cfg.RedisPoolSize
	if poolSize <= 0 {
		poolSize = 8
	}
	timeout := time.Duration(cfg.RedisTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	pool := &redis.Pool{
		MaxIdle:     poolSize,
		MaxActive:   poolSize * 2,
		IdleTimeout: time.Minute,
		Wait:        true,
		Dial: func() (redis.Conn, error) {
			dialOptions := []redis.DialOption{
				redis.DialConnectTimeout(timeout),
				redis.DialReadTimeout(timeout),
				redis.DialWriteTimeout(timeout),
				redis.DialDatabase(cfg.RedisDB),
			}
			if cfg.RedisPassword != "" {
				dialOptions = append(dialOptions, redis.DialPassword(cfg.RedisPassword))
			}
			return redis.Dial("tcp", address, dialOptions...)
		},
		TestOnBorrow: func(conn redis.Conn, lastUsed time.Time) error {
			if time.Since(lastUsed) < time.Minute {
				return nil
			}
			_, err := conn.Do("PING")
			return err
		},
	}
	return &redisRespCache{
		keyPrefix: respCachePrefix(cfg.KeyPrefix),
		ttl:       ttl,
		pool:      pool,
	}
}

// Store 将响应缓存写入 Redis 并设置毫秒级 TTL。
func (c *redisRespCache) Store(ctx context.Context, key string, response contracts.LogicPlayerResponse) error {
	if c == nil || c.pool == nil {
		return errors.New("logic redis response cache is nil")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	data, err := json.Marshal(clonePlayerResp(response))
	if err != nil {
		return err
	}
	ttlMillis := int64(c.ttl / time.Millisecond)
	if ttlMillis <= 0 {
		ttlMillis = int64((10 * time.Minute) / time.Millisecond)
	}
	_, err = c.do(ctx, "PSETEX", c.responseKey(key), ttlMillis, data)
	return err
}

// Get 从 Redis 读取响应缓存。
func (c *redisRespCache) Get(ctx context.Context, key string) (contracts.LogicPlayerResponse, bool, error) {
	if c == nil || c.pool == nil {
		return contracts.LogicPlayerResponse{}, false, errors.New("logic redis response cache is nil")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return contracts.LogicPlayerResponse{}, false, nil
	}
	value, err := redis.Bytes(c.do(ctx, "GET", c.responseKey(key)))
	if errors.Is(err, redis.ErrNil) {
		return contracts.LogicPlayerResponse{}, false, nil
	}
	if err != nil {
		return contracts.LogicPlayerResponse{}, false, err
	}
	var response contracts.LogicPlayerResponse
	if err := json.Unmarshal(value, &response); err != nil {
		return contracts.LogicPlayerResponse{}, false, err
	}
	return clonePlayerResp(response), true, nil
}

// Delete 从 Redis 删除指定响应缓存。
func (c *redisRespCache) Delete(ctx context.Context, key string) error {
	if c == nil || c.pool == nil {
		return nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	_, err := c.do(ctx, "DEL", c.responseKey(key))
	return err
}

// Snapshot 返回 Redis 响应缓存的配置状态。
func (c *redisRespCache) Snapshot() map[string]any {
	out := map[string]any{
		"backend": "redis",
	}
	if c == nil {
		return out
	}
	out["ttl_seconds"] = int64(c.ttl / time.Second)
	out["key_prefix"] = c.keyPrefix
	return out
}

// Close 关闭 Redis 响应缓存连接池。
func (c *redisRespCache) Close(context.Context) error {
	if c == nil || c.pool == nil {
		return nil
	}
	return c.pool.Close()
}

func (c *redisRespCache) do(ctx context.Context, command string, args ...any) (reply any, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := c.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRedisConnErr(conn, &err)
	return conn.Do(command, args...)
}

func (c *redisRespCache) responseKey(key string) string {
	return c.keyPrefix + ":response:" + digestRespCacheKey(key)
}
