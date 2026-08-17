package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	appkit "longheng.io/server/internal/platform/app"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/idempotency"
	"longheng.io/server/internal/reference/player"
)

func newPlayerStore(cfg config.ProfileStoreSection, env *appkit.Env) player.Store {
	store, _ := newPlayerStores(cfg, env)
	return store
}

func newIdempotencyGuard(cfg config.IdempotencySection) *idempotency.Guard {
	ttl := time.Duration(cfg.TTLSeconds) * time.Second
	switch strings.ToLower(strings.TrimSpace(cfg.Kind)) {
	case "redis":
		return idempotency.NewRedis(idempotency.RedisOptions{
			Address:        cfg.RedisAddress,
			Password:       cfg.RedisPassword,
			DB:             cfg.RedisDB,
			KeyPrefix:      cfg.KeyPrefix,
			PoolSize:       cfg.RedisPoolSize,
			Timeout:        time.Duration(cfg.RedisTimeoutSeconds) * time.Second,
			ConnectTimeout: time.Duration(cfg.RedisConnectTimeoutSecs) * time.Second,
			ReadTimeout:    time.Duration(cfg.RedisReadTimeoutSecs) * time.Second,
			WriteTimeout:   time.Duration(cfg.RedisWriteTimeoutSecs) * time.Second,
			TTL:            ttl,
		})
	case "mysql":
		return idempotency.NewMySQL(idempotency.MySQLOptions{
			DSN:             cfg.MySQLDSN,
			MaxOpenConns:    cfg.MySQLMaxOpenConns,
			MaxIdleConns:    cfg.MySQLMaxIdleConns,
			ConnMaxLifetime: time.Duration(cfg.MySQLConnMaxLifetimeSec) * time.Second,
			KeyPrefix:       cfg.KeyPrefix,
			TTL:             ttl,
			EnsureSchema:    true,
		})
	case "mysql_redis":
		return idempotency.NewMySQLRedis(idempotency.MySQLRedisOptions{
			TTL: ttl,
			Redis: idempotency.RedisOptions{
				Address:        cfg.RedisAddress,
				Password:       cfg.RedisPassword,
				DB:             cfg.RedisDB,
				KeyPrefix:      cfg.KeyPrefix,
				PoolSize:       cfg.RedisPoolSize,
				Timeout:        time.Duration(cfg.RedisTimeoutSeconds) * time.Second,
				ConnectTimeout: time.Duration(cfg.RedisConnectTimeoutSecs) * time.Second,
				ReadTimeout:    time.Duration(cfg.RedisReadTimeoutSecs) * time.Second,
				WriteTimeout:   time.Duration(cfg.RedisWriteTimeoutSecs) * time.Second,
			},
			MySQL: idempotency.MySQLOptions{
				DSN:             cfg.MySQLDSN,
				MaxOpenConns:    cfg.MySQLMaxOpenConns,
				MaxIdleConns:    cfg.MySQLMaxIdleConns,
				ConnMaxLifetime: time.Duration(cfg.MySQLConnMaxLifetimeSec) * time.Second,
				KeyPrefix:       cfg.KeyPrefix,
				EnsureSchema:    true,
			},
		})
	default:
		return idempotency.New(idempotency.Options{TTL: ttl})
	}
}

func newPlayerStores(cfg config.ProfileStoreSection, env *appkit.Env) (player.Store, player.SummaryStore) {
	return newProfileStore(cfg, env), newSummaryStore(cfg, env)
}

func newProfileStore(cfg config.ProfileStoreSection, env *appkit.Env) player.Store {
	var store player.Store
	switch strings.ToLower(strings.TrimSpace(cfg.StoreKind)) {
	case "", "memory":
		store = player.NewMemoryStore()
	case "file", "json":
		store = player.NewFileStore(cfg.StoreDirectory)
	case "redis", "pika", "profiledb", "profile_db":
		store = player.NewRedisStore(player.RedisStoreOptions{
			Address:        cfg.StoreAddress,
			Password:       cfg.StorePassword,
			DB:             cfg.StoreDB,
			KeyPrefix:      cfg.StoreKeyPrefix,
			PoolSize:       cfg.StorePoolSize,
			Timeout:        time.Duration(cfg.StoreTimeoutSeconds) * time.Second,
			ConnectTimeout: time.Duration(cfg.StoreConnectTimeout) * time.Second,
			ReadTimeout:    time.Duration(cfg.StoreReadTimeout) * time.Second,
			WriteTimeout:   time.Duration(cfg.StoreWriteTimeout) * time.Second,
			TTL:            time.Duration(cfg.StoreTTLSeconds) * time.Second,
		})
	case "mysql", "mysql_redis":
		if cfg.MySQLShardingEnabled || len(cfg.MySQLShards) > 0 {
			sharded, err := newShardedStore(cfg)
			if err != nil {
				if env != nil && env.Logger != nil {
					env.Logger.Error("invalid sharded mysql player store config", "error", err)
				}
				return failingPlayerStore{err: fmt.Errorf("invalid sharded mysql player store config: %w", err)}
			} else {
				store = sharded
			}
		} else {
			store = player.NewMySQLStore(player.MySQLStoreOptions{
				DSN:             cfg.MySQLDSN,
				MaxOpenConns:    cfg.MySQLMaxOpenConns,
				MaxIdleConns:    cfg.MySQLMaxIdleConns,
				ConnMaxLifetime: time.Duration(cfg.MySQLConnMaxLifetime) * time.Second,
				EnsureSchema:    true,
			})
		}
	default:
		if env != nil && env.Logger != nil {
			env.Logger.Warn("unsupported player store kind, falling back to memory", "kind", cfg.StoreKind)
		}
		store = player.NewMemoryStore()
	}

	switch strings.ToLower(strings.TrimSpace(cfg.SaveMode)) {
	case "", "sync":
		return store
	case "async", "writebehind", "write_behind":
		asyncStore := player.NewAsyncStore(store, player.AsyncStoreOptions{
			FlushInterval:   time.Duration(cfg.AsyncFlushIntervalSeconds) * time.Second,
			MaxPending:      cfg.AsyncMaxPending,
			RetryBackoff:    time.Duration(cfg.AsyncRetryBackoffSeconds) * time.Second,
			MaxRetries:      cfg.AsyncMaxRetries,
			AutoExpireTTL:   time.Duration(cfg.AsyncAutoExpireSeconds) * time.Second,
			DeadLetterLimit: cfg.AsyncDeadLetterLimit,
			DeadLetterStore: player.NewFileDeadLetterStore(cfg.AsyncDeadLetterDirectory),
		})
		if roleStore, ok := store.(player.RoleProfileStore); ok {
			return player.NewRoleAwareAsyncStore(asyncStore, roleStore)
		}
		return asyncStore
	default:
		if env != nil && env.Logger != nil {
			env.Logger.Warn("unsupported player save mode, using sync store", "mode", cfg.SaveMode)
		}
		return store
	}
}

type failingPlayerStore struct {
	err error
}

// Load 返回配置错误，阻止损坏的玩家存储继续启动。
func (s failingPlayerStore) Load(context.Context, string) (player.Profile, bool, error) {
	return player.Profile{}, false, s.err
}

// Save 返回配置错误，避免玩家资料写入未知后端。
func (s failingPlayerStore) Save(context.Context, player.Profile) error {
	return s.err
}

// Check 返回配置错误，供启动前检查暴露存储配置问题。
func (s failingPlayerStore) Check(context.Context) error {
	return s.err
}

func newShardedStore(cfg config.ProfileStoreSection) (*player.ShardedMySQLStore, error) {
	shards := make([]player.MySQLShardOptions, 0, len(cfg.MySQLShards))
	for _, shard := range cfg.MySQLShards {
		maxOpen := shard.MaxOpenConns
		if maxOpen == 0 {
			maxOpen = cfg.MySQLMaxOpenConns
		}
		maxIdle := shard.MaxIdleConns
		if maxIdle == 0 {
			maxIdle = cfg.MySQLMaxIdleConns
		}
		lifetime := shard.ConnMaxLifetime
		if lifetime == 0 {
			lifetime = cfg.MySQLConnMaxLifetime
		}
		shards = append(shards, player.MySQLShardOptions{
			ID:              shard.ID,
			DSN:             shard.DSN,
			TableName:       shard.TableName,
			TablePrefix:     shard.TablePrefix,
			HashSlots:       shard.HashSlots,
			MaxOpenConns:    maxOpen,
			MaxIdleConns:    maxIdle,
			ConnMaxLifetime: time.Duration(lifetime) * time.Second,
			EnsureSchema:    true,
		})
	}
	return player.NewShardedMySQLStore(player.ShardedMySQLStoreOptions{
		DefaultDSN:             cfg.MySQLDSN,
		DefaultMaxOpenConns:    cfg.MySQLMaxOpenConns,
		DefaultMaxIdleConns:    cfg.MySQLMaxIdleConns,
		DefaultConnMaxLifetime: time.Duration(cfg.MySQLConnMaxLifetime) * time.Second,
		EnsureSchema:           true,
		Shards:                 shards,
	})
}

func newSummaryStore(cfg config.ProfileStoreSection, env *appkit.Env) player.SummaryStore {
	kind := strings.ToLower(strings.TrimSpace(cfg.SummaryStoreKind))
	if kind == "" && strings.ToLower(strings.TrimSpace(cfg.StoreKind)) == "mysql_redis" {
		kind = "redis"
	}
	switch kind {
	case "", "memory":
		return player.NewMemorySummaryStore()
	case "redis", "pika":
		return player.NewRedisSummaryStore(player.RedisSummaryStoreOptions{
			Address:        cfg.SummaryStoreAddress,
			Password:       cfg.SummaryStorePassword,
			DB:             cfg.SummaryStoreDB,
			KeyPrefix:      cfg.SummaryStoreKeyPrefix,
			PoolSize:       cfg.SummaryStorePoolSize,
			Timeout:        time.Duration(cfg.SummaryStoreTimeout) * time.Second,
			ConnectTimeout: time.Duration(cfg.SummaryStoreConnectTimeout) * time.Second,
			ReadTimeout:    time.Duration(cfg.SummaryStoreReadTimeout) * time.Second,
			WriteTimeout:   time.Duration(cfg.SummaryStoreWriteTimeout) * time.Second,
			TTL:            time.Duration(cfg.SummaryStoreTTL) * time.Second,
		})
	default:
		if env != nil && env.Logger != nil {
			env.Logger.Warn("unsupported player summary store kind, falling back to memory", "kind", cfg.SummaryStoreKind)
		}
		return player.NewMemorySummaryStore()
	}
}
