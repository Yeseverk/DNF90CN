package player

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"longheng.io/server/internal/platform/readmodel"
)

const (
	redisCleanMissing    = readmodel.RedisCleanupMissingScript
	redisCleanCorrupt    = readmodel.RedisCleanupCorruptScript
	redisRoleIndexDel    = readmodel.RedisDeleteSecondaryIndexIfPrimaryScript
	redisSummaryKeyspace = redisPlayerSumNS
	redisSummaryRole     = redisPlayerSumIndex
)

var errBadPlayerSummary = readmodel.ErrCorruptRecord

// RedisSummaryStoreOptions 是 Redis 玩家摘要读模型的连接、命名空间和 TTL 配置。
type RedisSummaryStoreOptions struct {
	Address        string
	Password       string
	DB             int
	KeyPrefix      string
	PoolSize       int
	Timeout        time.Duration
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	TTL            time.Duration
}

// RedisSummaryStore 基于 Redis 保存玩家摘要读模型和角色索引。
type RedisSummaryStore struct {
	keyPrefix string
	ttl       time.Duration
	executor  redisExecutor
	inner     *readmodel.RedisStore[PlayerSummary, PlayerSummaryQuery]
}

// NewRedisSummaryStore 创建 Redis 玩家摘要读模型存储。
func NewRedisSummaryStore(options RedisSummaryStoreOptions) *RedisSummaryStore {
	options = normSummaryOpts(options)
	pool := &redis.Pool{
		MaxIdle:     options.PoolSize,
		MaxActive:   options.PoolSize * 2,
		IdleTimeout: time.Minute,
		Wait:        true,
		Dial: func() (redis.Conn, error) {
			dialOptions := []redis.DialOption{
				redis.DialConnectTimeout(options.ConnectTimeout),
				redis.DialReadTimeout(options.ReadTimeout),
				redis.DialWriteTimeout(options.WriteTimeout),
				redis.DialDatabase(options.DB),
			}
			if options.Password != "" {
				dialOptions = append(dialOptions, redis.DialPassword(options.Password))
			}
			return redis.Dial("tcp", options.Address, dialOptions...)
		},
		TestOnBorrow: func(conn redis.Conn, lastUsed time.Time) error {
			if time.Since(lastUsed) < time.Minute {
				return nil
			}
			_, err := conn.Do("PING")
			return err
		},
	}
	return newRedisSummaryStore(&redigoExecutor{pool: pool}, options)
}

func newRedisSummaryStore(executor redisExecutor, options RedisSummaryStoreOptions) *RedisSummaryStore {
	options = normSummaryOpts(options)
	store := &RedisSummaryStore{
		keyPrefix: options.KeyPrefix,
		ttl:       options.TTL,
		executor:  executor,
	}
	store.inner = store.newInner()
	return store
}

func normSummaryOpts(options RedisSummaryStoreOptions) RedisSummaryStoreOptions {
	options.Address = strings.TrimSpace(options.Address)
	if options.Address == "" {
		options.Address = "127.0.0.1:6379"
	}
	options.KeyPrefix = strings.TrimSpace(options.KeyPrefix)
	if options.KeyPrefix == "" {
		options.KeyPrefix = redisSummaryKeyspace
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

// SavePlayerSummary 保存玩家摘要到 Redis 读模型。
func (s *RedisSummaryStore) SavePlayerSummary(ctx context.Context, summary PlayerSummary) error {
	return s.readModel().Save(ctx, summary)
}

// GetPlayerSummary 按账号 ID 从 Redis 读模型读取玩家摘要。
func (s *RedisSummaryStore) GetPlayerSummary(ctx context.Context, accountID string) (PlayerSummary, bool, error) {
	return s.readModel().Get(ctx, accountID)
}

// GetPlayerSummaryByRoleID 按角色 ID 从 Redis 读模型读取玩家摘要。
func (s *RedisSummaryStore) GetPlayerSummaryByRoleID(ctx context.Context, roleID string) (PlayerSummary, bool, error) {
	return s.readModel().GetBySecondaryID(ctx, roleID)
}

// ListPlayerSummariesByAccountIDs 按账号 ID 批量读取 Redis 玩家摘要。
func (s *RedisSummaryStore) ListPlayerSummariesByAccountIDs(ctx context.Context, accountIDs []string) ([]PlayerSummary, error) {
	return s.readModel().ListByPrimaryIDs(ctx, accountIDs)
}

// ListByRoleIDs 按角色 ID 批量读取 Redis 玩家摘要。
func (s *RedisSummaryStore) ListByRoleIDs(ctx context.Context, roleIDs []string) ([]PlayerSummary, error) {
	return s.readModel().ListBySecondaryIDs(ctx, roleIDs)
}

// SearchPlayerSummaries 按查询条件搜索 Redis 玩家摘要。
func (s *RedisSummaryStore) SearchPlayerSummaries(ctx context.Context, query PlayerSummaryQuery) ([]PlayerSummary, error) {
	return s.readModel().Search(ctx, query)
}

// Check 检查 Redis 玩家摘要读模型存储。
func (s *RedisSummaryStore) Check(ctx context.Context) error {
	return s.readModel().Check(ctx)
}

// Close 关闭 Redis 玩家摘要读模型连接。
func (s *RedisSummaryStore) Close(ctx context.Context) error {
	return s.readModel().Close(ctx)
}

func (s *RedisSummaryStore) summaryKey(accountID string) string {
	return s.readModel().SummaryKey(accountID)
}

func (s *RedisSummaryStore) roleKey(roleID string) string {
	return s.readModel().SecondaryKey(roleID)
}

func (s *RedisSummaryStore) accountsKey() string {
	return s.readModel().PrimaryIDsKey()
}

func (s *RedisSummaryStore) readModel() *readmodel.RedisStore[PlayerSummary, PlayerSummaryQuery] {
	if s.inner == nil {
		s.inner = s.newInner()
	}
	s.inner.SetExecutor(s.executor)
	return s.inner
}

func (s *RedisSummaryStore) newInner() *readmodel.RedisStore[PlayerSummary, PlayerSummaryQuery] {
	inner, err := readmodel.NewRedisStore[PlayerSummary, PlayerSummaryQuery](readmodel.RedisStoreOptions[PlayerSummary, PlayerSummaryQuery]{
		Model:              summaryReadOpts(),
		Executor:           s.executor,
		KeyPrefix:          s.keyPrefix,
		HashField:          redisPlayerSumField,
		PrimarySetName:     redisPlayerSumSet,
		SecondaryIndexName: redisSummaryRole,
		TTL:                s.ttl,
		Encode: func(summary PlayerSummary) ([]byte, error) {
			return json.Marshal(summary)
		},
		Decode: decodePlayerSummary,
	})
	if err != nil {
		panic(err)
	}
	return inner
}

func decodePlayerSummary(accountID string, data []byte) (PlayerSummary, error) {
	var summary PlayerSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return PlayerSummary{}, fmt.Errorf("%w %s: %w", errBadPlayerSummary, accountID, err)
	}
	accountID = strings.TrimSpace(accountID)
	if accountID != "" {
		summary.AccountID = accountID
	} else {
		summary.AccountID = strings.TrimSpace(summary.AccountID)
	}
	summary = normPlayerSummaryID(summary)
	return clonePlayerSummary(summary), nil
}
