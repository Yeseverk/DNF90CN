package replay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrReplayKeyRequired 通过 errors.Is 暴露空 nonce key。
	ErrReplayKeyRequired = errors.New("replay: key is required")
	// ErrReplayTTLRequired 通过 errors.Is 暴露非法 replay 保留窗口。
	ErrReplayTTLRequired = errors.New("replay: ttl must be positive")
	// ErrReplayStoreUnavailable 通过 errors.Is 暴露 nil 或不可用 store。
	ErrReplayStoreUnavailable = errors.New("replay: store is unavailable")
	// ErrReplayStoreFull 表示不能通过驱逐未过期 nonce 来接收新 nonce。
	ErrReplayStoreFull = errors.New("replay: store is full")
)

// Store 记录短生命周期 nonce 是否已经出现过；实现必须保证 check+write 原子化。
type Store interface {
	// Seen 在 key 首次出现时返回 (true, nil)，重复出现时返回 (false, nil)。
	// ttl 是最大保留窗口，应略大于调用方的 MaxSkew。
	Seen(ctx context.Context, key string, ttl time.Duration) (firstSeen bool, err error)
}

func normalizeInput(ctx context.Context, key string, ttl time.Duration) (context.Context, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, "", ErrReplayKeyRequired
	}
	if ttl <= 0 {
		return nil, "", ErrReplayTTLRequired
	}
	return ctx, key, nil
}

func wrapRedisError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrReplayRedisOperationFailed, err)
}
