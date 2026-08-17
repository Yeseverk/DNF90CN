package logic

import (
	"context"

	"longheng.io/server/pkg/contracts"
)

type hybridRespCache struct {
	redis logicResponseCache
	mysql logicResponseCache
}

// Store 先写权威 MySQL 响应缓存，再尝试写 Redis 热缓存。
func (c *hybridRespCache) Store(ctx context.Context, key string, response contracts.LogicPlayerResponse) error {
	var durableErr error
	// MySQL 是权威响应缓存，必须先写；Redis 只是热缓存，失败不能覆盖 MySQL 写入结果。
	if c != nil && c.mysql != nil {
		durableErr = c.mysql.Store(ctx, key, response)
	}
	var redisErr error
	if c != nil && c.redis != nil {
		redisErr = c.redis.Store(ctx, key, response)
	}
	if durableErr != nil {
		return durableErr
	}
	if c == nil || c.mysql == nil {
		return redisErr
	}
	return nil
}

// Get 先读 Redis 热缓存，未命中时读取 MySQL 并回填 Redis。
func (c *hybridRespCache) Get(ctx context.Context, key string) (contracts.LogicPlayerResponse, bool, error) {
	var redisErr error
	// 读取先走 Redis，未命中再读 MySQL 并回填。Redis 错误只在没有 MySQL 兜底时才向上暴露。
	if c != nil && c.redis != nil {
		response, ok, err := c.redis.Get(ctx, key)
		if ok {
			return response, true, nil
		}
		if err != nil {
			redisErr = err
		}
	}
	if c != nil && c.mysql != nil {
		response, ok, err := c.mysql.Get(ctx, key)
		if err != nil || !ok {
			if err != nil {
				return contracts.LogicPlayerResponse{}, false, err
			}
			return contracts.LogicPlayerResponse{}, false, redisErr
		}
		if c.redis != nil {
			_ = c.redis.Store(ctx, key, response)
		}
		return response, true, nil
	}
	return contracts.LogicPlayerResponse{}, false, redisErr
}

// Delete 同时删除 Redis 和 MySQL 中的响应缓存。
func (c *hybridRespCache) Delete(ctx context.Context, key string) error {
	if c != nil && c.redis != nil {
		if err := c.redis.Delete(ctx, key); err != nil {
			return err
		}
	}
	if c != nil && c.mysql != nil {
		return c.mysql.Delete(ctx, key)
	}
	return nil
}

// Snapshot 返回混合响应缓存的后端状态快照。
func (c *hybridRespCache) Snapshot() map[string]any {
	out := map[string]any{
		"backend": "mysql_redis",
	}
	if c == nil {
		return out
	}
	if c.redis != nil {
		out["redis"] = c.redis.Snapshot()
	}
	if c.mysql != nil {
		out["mysql"] = c.mysql.Snapshot()
	}
	return out
}

// Close 关闭混合响应缓存持有的后端连接。
func (c *hybridRespCache) Close(ctx context.Context) error {
	var first error
	if c != nil && c.redis != nil {
		first = c.redis.Close(ctx)
	}
	if c != nil && c.mysql != nil {
		if err := c.mysql.Close(ctx); first == nil {
			first = err
		}
	}
	return first
}
