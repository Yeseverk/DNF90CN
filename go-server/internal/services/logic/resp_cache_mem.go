package logic

import (
	"context"
	"strings"
	"sync"
	"time"

	"longheng.io/server/pkg/contracts"
)

type memRespCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]logicRespCacheEntry
}

type logicRespCacheEntry struct {
	response  contracts.LogicPlayerResponse
	expiresAt time.Time
}

func newMemRespCache(ttl time.Duration) *memRespCache {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &memRespCache{
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]logicRespCacheEntry),
	}
}

// Store 将响应写入内存缓存并按 TTL 记录过期时间。
func (c *memRespCache) Store(_ context.Context, key string, response contracts.LogicPlayerResponse) error {
	key = strings.TrimSpace(key)
	if c == nil || key == "" {
		return nil
	}
	now := c.nowUTC()
	ttl := c.ttl
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	c.mu.Lock()
	c.pruneLocked(now)
	if c.entries == nil {
		c.entries = make(map[string]logicRespCacheEntry)
	}
	c.entries[key] = logicRespCacheEntry{
		response:  clonePlayerResp(response),
		expiresAt: now.Add(ttl),
	}
	c.mu.Unlock()
	return nil
}

// Get 从内存缓存读取未过期的响应。
func (c *memRespCache) Get(_ context.Context, key string) (contracts.LogicPlayerResponse, bool, error) {
	key = strings.TrimSpace(key)
	if c == nil || key == "" {
		return contracts.LogicPlayerResponse{}, false, nil
	}
	now := c.nowUTC()
	c.mu.Lock()
	c.pruneLocked(now)
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if !ok {
		return contracts.LogicPlayerResponse{}, false, nil
	}
	return clonePlayerResp(entry.response), true, nil
}

// Delete 从内存缓存删除指定响应。
func (c *memRespCache) Delete(_ context.Context, key string) error {
	key = strings.TrimSpace(key)
	if c == nil || key == "" {
		return nil
	}
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
	return nil
}

// Snapshot 返回内存响应缓存的条目数和 TTL 信息。
func (c *memRespCache) Snapshot() map[string]any {
	out := map[string]any{
		"backend": "memory",
	}
	if c == nil {
		return out
	}
	c.mu.Lock()
	now := c.nowUTC()
	c.pruneLocked(now)
	out["ttl_seconds"] = int64(c.ttl / time.Second)
	out["entries"] = len(c.entries)
	c.mu.Unlock()
	return out
}

// Close 关闭内存响应缓存。
func (c *memRespCache) Close(context.Context) error {
	return nil
}

func (c *memRespCache) nowUTC() time.Time {
	if c == nil || c.now == nil {
		return time.Now().UTC()
	}
	return c.now().UTC()
}

func (c *memRespCache) pruneLocked(now time.Time) {
	if c.entries == nil {
		return
	}
	for key, entry := range c.entries {
		if !entry.expiresAt.After(now) {
			delete(c.entries, key)
		}
	}
}
