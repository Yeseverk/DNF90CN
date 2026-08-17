package cache

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
	"longheng.io/server/internal/platform/db"
)

var ErrNilRedisExecutor = errors.New("cache redis executor is required")

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
}

type RedisStore struct {
	name       string
	keyPrefix  string
	defaultTTL time.Duration
	executor   db.RedisExecutor
	closer     interface{ Close() error }
	metricsMu  sync.RWMutex
	metrics    *Metrics
}

const redisTakeScript = `local v = redis.call("GET", KEYS[1]); if v then redis.call("DEL", KEYS[1]); end; return v`

func NewRedis(options RedisOptions) *RedisStore {
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
	return &RedisStore{
		name:       options.Name,
		keyPrefix:  options.KeyPrefix,
		defaultTTL: options.DefaultTTL,
		executor:   executor,
		closer:     closer,
	}
}

func (s *RedisStore) SetMetrics(m *Metrics) {
	if s == nil {
		return
	}
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metrics = m
}

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, false, err
	}
	key = normalizeKey(key)
	if key == "" {
		return nil, false, ErrKeyRequired
	}
	if err := s.ready(); err != nil {
		return nil, false, err
	}
	value, err := redis.Bytes(s.do(ctx, "GET", s.cacheKey(key)))
	if errors.Is(err, redis.ErrNil) {
		s.cacheMetrics().recordMiss()
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	s.cacheMetrics().recordHit()
	return value, true, nil
}

func (s *RedisStore) Take(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, false, err
	}
	key = normalizeKey(key)
	if key == "" {
		return nil, false, ErrKeyRequired
	}
	if err := s.ready(); err != nil {
		return nil, false, err
	}
	value, err := redis.Bytes(s.do(ctx, "EVAL", redisTakeScript, 1, s.cacheKey(key)))
	if errors.Is(err, redis.ErrNil) {
		s.cacheMetrics().recordMiss()
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	s.cacheMetrics().recordHit()
	return value, true, nil
}

func (s *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	key = normalizeKey(key)
	if key == "" {
		return ErrKeyRequired
	}
	if err := s.ready(); err != nil {
		return err
	}
	if ttl == 0 {
		ttl = s.defaultTTL
	}
	if ttl > 0 {
		_, err := s.do(ctx, "PSETEX", s.cacheKey(key), ttlMillis(ttl), cloneBytes(value))
		return err
	}
	_, err := s.do(ctx, "SET", s.cacheKey(key), cloneBytes(value))
	return err
}

func (s *RedisStore) Delete(ctx context.Context, key string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	key = normalizeKey(key)
	if key == "" {
		return ErrKeyRequired
	}
	if err := s.ready(); err != nil {
		return err
	}
	_, err := s.do(ctx, "DEL", s.cacheKey(key))
	return err
}

func (s *RedisStore) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{Kind: "redis"}
	}
	return Snapshot{
		Name:      s.name,
		Kind:      "redis",
		KeyPrefix: s.keyPrefix,
	}
}

func (s *RedisStore) Close() error {
	if s == nil || s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

func (s *RedisStore) ready() error {
	if s == nil || s.executor == nil {
		return ErrNilRedisExecutor
	}
	return nil
}

func (s *RedisStore) do(ctx context.Context, command string, args ...any) (any, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	return s.executor.Do(ctx, command, args...)
}

func (s *RedisStore) cacheMetrics() *Metrics {
	if s == nil {
		return nil
	}
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	return s.metrics
}

func (s *RedisStore) cacheKey(key string) string {
	if s == nil || s.keyPrefix == "" {
		return key
	}
	return s.keyPrefix + ":" + key
}

func normRedisOpts(options RedisOptions) RedisOptions {
	options.Name = strings.TrimSpace(options.Name)
	if options.Name == "" {
		options.Name = "redis-cache"
	}
	options.KeyPrefix = strings.TrimSpace(options.KeyPrefix)
	if options.KeyPrefix == "" {
		options.KeyPrefix = "cache"
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
	return options
}

func ttlMillis(ttl time.Duration) int64 {
	ms := int64(ttl / time.Millisecond)
	if ms <= 0 {
		return 1
	}
	return ms
}
