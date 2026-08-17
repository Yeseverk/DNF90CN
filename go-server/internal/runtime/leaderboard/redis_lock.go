package leaderboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	lockkit "longheng.io/server/internal/platform/lock"
)

var redisLockRandRead = rand.Read

func (m *RedisManager) withLeaderboardLock(ctx context.Context, leaderboardID string, fn func() error) error {
	if m == nil || m.executor == nil {
		return ErrNilRedisExecutor
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return ErrLeaderboardNotFound
	}
	token, err := redisLockToken()
	if err != nil {
		return err
	}
	if err := m.acquireLbLock(ctx, leaderboardID, token); err != nil {
		return err
	}
	defer func() {
		releaseCtx, cancel := detachedTimeoutCtx(ctx, time.Second)
		defer cancel()
		_ = m.releaseLbLock(releaseCtx, leaderboardID, token)
	}()
	return fn()
}

func redisLockToken() (string, error) {
	var raw [16]byte
	if _, err := redisLockRandRead(raw[:]); err != nil {
		return "", fmt.Errorf("%w: %w", ErrRedisLockToken, err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func (m *RedisManager) acquireLbLock(ctx context.Context, leaderboardID string, token string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil || m.executor == nil {
		return ErrNilRedisExecutor
	}
	for {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		ok, err := lockkit.AcquireRedis(ctx, m.executor, m.lockKey(leaderboardID), token, m.lockTTLValue())
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *RedisManager) releaseLbLock(ctx context.Context, leaderboardID string, token string) error {
	if m == nil || m.executor == nil {
		return ErrNilRedisExecutor
	}
	err := lockkit.ReleaseRedis(ctx, m.executor, m.lockKey(leaderboardID), token)
	if errors.Is(err, lockkit.ErrTokenMismatch) {
		return nil
	}
	return err
}

func detachedTimeoutCtx(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		return context.WithTimeout(context.Background(), timeout)
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}
