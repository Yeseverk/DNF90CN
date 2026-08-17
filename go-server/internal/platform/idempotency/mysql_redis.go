package idempotency

import (
	"context"
	"errors"
	"sync"
	"time"
)

type MySQLRedisOptions struct {
	Redis RedisOptions
	MySQL MySQLOptions
	TTL   time.Duration
	Now   func() time.Time
}

type mySQLRedisStore struct {
	redis *redisStore
	mysql Store
	ttl   time.Duration
	now   func() time.Time
	stats mySQLRedisStats
}

type mySQLRedisStats struct {
	mu               sync.Mutex
	redisDuplicate   int64
	redisReplay      int64
	redisError       int64
	redisBackfill    int64
	redisCommitError int64
	mysqlFallback    int64
}

func NewMySQLRedis(options MySQLRedisOptions) *Guard {
	options = normMySQLRedisOpts(options)
	redisOptions := options.Redis
	redisOptions.TTL = options.TTL
	redisOptions.Now = options.Now
	redisStore := newRedisOpts(redisOptions)

	mysqlOptions := options.MySQL
	mysqlOptions.TTL = options.TTL
	mysqlOptions.Now = options.Now
	mysqlOptions.EnsureSchema = true
	mysqlStore := newMySQLStore(mysqlOptions)

	return New(Options{
		TTL:  options.TTL,
		Now:  options.Now,
		Kind: "mysql_redis",
		Store: &mySQLRedisStore{
			redis: redisStore,
			mysql: mysqlStore,
			ttl:   options.TTL,
			now:   options.Now,
		},
	})
}

func normMySQLRedisOpts(options MySQLRedisOptions) MySQLRedisOptions {
	if options.TTL <= 0 {
		options.TTL = 10 * time.Minute
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return options
}

func (s *mySQLRedisStore) Check(ctx context.Context, item Request) (Decision, error) {
	decision, err := s.Begin(ctx, item)
	if err != nil {
		return Decision{}, err
	}
	if decision.Status == StatusAccepted {
		if err := s.Commit(ctx, item, decision); err != nil {
			return Decision{}, err
		}
	}
	return decision, nil
}

func (s *mySQLRedisStore) Begin(ctx context.Context, item Request) (Decision, error) {
	if s == nil || s.mysql == nil {
		return Decision{}, ErrInvalidRequest
	}
	// Redis 只查询已经提交完成的热结果；pending/accepted 状态以 MySQL 为准。
	// 这样可以避免“Redis 先接受、MySQL 失败”导致重试被缓存误丢。
	if s.redis != nil {
		redisDecision, hit, err := s.redis.LookupCommitted(ctx, item)
		if errors.Is(err, ErrRequestConflict) {
			return Decision{}, err
		} else if err != nil {
			s.stats.recordRedisError()
		} else if hit {
			s.stats.recordRedisDecision(redisDecision.Status)
			return redisDecision, nil
		}
	}
	s.stats.recordMySQLFallback()
	store, ok := s.mysql.(transactionalStore)
	if !ok {
		return Decision{}, ErrInvalidRequest
	}
	decision, err := store.Begin(ctx, item)
	if err != nil {
		return Decision{}, err
	}
	// MySQL 判定为 duplicate/replay 后再回填 Redis，用缓存加速后续重试。
	if s.redis != nil && (decision.Status == StatusDuplicate || decision.Status == StatusReplay) {
		cacheItem := item
		cacheItem.Fingerprint = decision.fingerprint
		if err := s.backfillRedis(ctx, cacheItem, decision, decision.expiresAt); err != nil {
			s.stats.recordRedisErr()
		} else {
			s.stats.recordRedisBackfill()
		}
	}
	return decision, nil
}

func (s *mySQLRedisStore) Commit(ctx context.Context, item Request, decision Decision) error {
	if s == nil || s.mysql == nil {
		return ErrInvalidRequest
	}
	store, ok := s.mysql.(transactionalStore)
	if !ok {
		return ErrInvalidRequest
	}
	var expiresAt time.Time
	if mysqlStore, ok := s.mysql.(*mySQLStore); ok {
		var err error
		expiresAt, err = mysqlStore.commitWithExpiry(ctx, item, decision, nil, false)
		if err != nil {
			return err
		}
	} else {
		if err := store.Commit(ctx, item, decision); err != nil {
			return err
		}
		expiresAt = s.nowOrDefault().Add(s.ttlOrDefault())
	}
	// 提交顺序必须是 MySQL 先成功、Redis 后回填；Redis 回填失败只影响加速，不影响权威状态。
	if s.redis != nil {
		if err := s.backfillRedis(ctx, item, decision, expiresAt); err != nil {
			s.stats.recordRedisErr()
		} else {
			s.stats.recordRedisBackfill()
		}
	}
	return nil
}

func (s *mySQLRedisStore) CommitResult(ctx context.Context, item Request, decision Decision, payload []byte) error {
	if s == nil || s.mysql == nil {
		return ErrInvalidRequest
	}
	store, ok := s.mysql.(resultStore)
	if !ok {
		return ErrResultStoreRequired
	}
	// MySQL 在一个事务里提交 marker 与结果；Redis 仍只做 committed 热索引，失败不影响权威结果。
	var expiresAt time.Time
	if mysqlStore, ok := s.mysql.(*mySQLStore); ok {
		var err error
		expiresAt, err = mysqlStore.commitWithExpiry(ctx, item, decision, payload, true)
		if err != nil {
			return err
		}
	} else {
		if err := store.CommitResult(ctx, item, decision, payload); err != nil {
			return err
		}
		expiresAt = s.nowOrDefault().Add(s.ttlOrDefault())
	}
	if s.redis != nil {
		if err := s.backfillRedis(ctx, item, decision, expiresAt); err != nil {
			s.stats.recordRedisErr()
		} else {
			s.stats.recordRedisBackfill()
		}
	}
	return nil
}

func (s *mySQLRedisStore) backfillRedis(ctx context.Context, item Request, decision Decision, expiresAt time.Time) error {
	if s == nil || s.redis == nil {
		return nil
	}
	ttl := s.ttlOrDefault()
	if !expiresAt.IsZero() {
		ttl = expiresAt.Sub(s.nowOrDefault())
	}
	return s.redis.storeCommittedTTL(ctx, item, decision, ttl)
}

func (s *mySQLRedisStore) ttlOrDefault() time.Duration {
	if s == nil || s.ttl <= 0 {
		return 10 * time.Minute
	}
	return s.ttl
}

func (s *mySQLRedisStore) nowOrDefault() time.Time {
	if s != nil && s.now == nil {
		if mysqlStore, ok := s.mysql.(*mySQLStore); ok {
			return mysqlStore.nowOrDefault()
		}
	}
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func (s *mySQLRedisStore) LookupResult(ctx context.Context, decision Decision) ([]byte, bool, error) {
	if s == nil || s.mysql == nil {
		return nil, false, ErrInvalidRequest
	}
	store, ok := s.mysql.(resultStore)
	if !ok {
		return nil, false, ErrResultStoreRequired
	}
	return store.LookupResult(ctx, decision)
}

func (s *mySQLRedisStore) Abort(ctx context.Context, item Request, decision Decision) error {
	if s == nil || s.mysql == nil {
		return ErrInvalidRequest
	}
	store, ok := s.mysql.(transactionalStore)
	if !ok {
		return ErrInvalidRequest
	}
	return store.Abort(ctx, item, decision)
}

func (s *mySQLRedisStore) Snapshot() map[string]any {
	out := map[string]any{
		"backend": "mysql_redis",
	}
	if s == nil {
		return out
	}
	if s.redis != nil {
		out["redis"] = s.redis.Snapshot()
	}
	if s.mysql != nil {
		out["mysql"] = s.mysql.Snapshot()
	}
	out["hybrid_metrics"] = s.stats.snapshot()
	return out
}

func (s *mySQLRedisStore) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	var first error
	if s.redis != nil {
		first = s.redis.Close(ctx)
	}
	if s.mysql != nil {
		if closer, ok := s.mysql.(interface {
			Close(context.Context) error
		}); ok {
			if err := closer.Close(ctx); first == nil {
				first = err
			}
		}
	}
	return first
}

func (s *mySQLRedisStats) recordRedisDecision(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch status {
	case StatusDuplicate:
		s.redisDuplicate++
	case StatusReplay:
		s.redisReplay++
	}
}

func (s *mySQLRedisStats) recordRedisError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redisError++
}

func (s *mySQLRedisStats) recordRedisBackfill() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redisBackfill++
}

func (s *mySQLRedisStats) recordRedisErr() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redisCommitError++
}

func (s *mySQLRedisStats) recordMySQLFallback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mysqlFallback++
}

func (s *mySQLRedisStats) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"redis_duplicate":    s.redisDuplicate,
		"redis_replay":       s.redisReplay,
		"redis_error":        s.redisError,
		"redis_backfill":     s.redisBackfill,
		"redis_commit_error": s.redisCommitError,
		"mysql_fallback":     s.mysqlFallback,
	}
}
