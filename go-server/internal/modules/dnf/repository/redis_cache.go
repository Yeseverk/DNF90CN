// 本文件负责 DNF 仓储的 Redis 可重建缓存包装。
// MySQL 仍是权威存储；Redis 只保存角色读模型、名字索引和数字 ID 分配游标。
package repository

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"longheng.io/server/internal/platform/db"
)

const defaultRedisCachePrefix = "dnf:repository"

const redisNextIDScript = `
local key = KEYS[1]
local seed = tonumber(ARGV[1])
local current = tonumber(redis.call("GET", key) or "0")
if current < seed then
	redis.call("SET", key, seed)
end
return redis.call("INCR", key)
`

// RedisCacheOptions 描述 DNF 仓储 Redis 缓存参数。
type RedisCacheOptions struct {
	KeyPrefix string
	TTL       time.Duration
}

type RedisCachedCharStore struct {
	backing  CharacterRepository
	executor db.RedisExecutor
	options  RedisCacheOptions
}

// NewCachedGroup 使用 Redis 包装 DNF 仓储聚合。
// 包装层只替换角色仓储的读模型缓存和 ID 游标；账号、背包、技能等权威写入仍走原 MySQL 仓储。
func NewCachedGroup(group Group, executor db.RedisExecutor, options RedisCacheOptions) Group {
	if executor == nil || group.Character == nil {
		return group
	}
	group.Character = NewRedisCachedCharacterRepository(group.Character, executor, options)
	return group
}

// NewRedisCachedCharacterRepository 创建角色仓储的 Redis 缓存装饰器。
// Save/SaveFields 会先写 MySQL，再 best-effort 写 Redis；Load/List/Find 允许 Redis 失败后回退 MySQL。
func NewRedisCachedCharacterRepository(backing CharacterRepository, executor db.RedisExecutor, options RedisCacheOptions) CharacterRepository {
	return &RedisCachedCharStore{
		backing:  backing,
		executor: executor,
		options:  normalizeRedisCacheOptions(options),
	}
}

// Check 校验底层 MySQL 仓储并探测 Redis 连接。
// 启动期显式启用 Redis 时应尽早发现连接错误；运行期读写仍允许回退 MySQL。
func (s *RedisCachedCharStore) Check(ctx context.Context) error {
	if s == nil || s.backing == nil {
		return ErrRepoMissing
	}
	if s.executor == nil {
		return db.ErrRedisExecutorClosed
	}
	if err := db.Check(ctx, s.backing); err != nil {
		return err
	}
	_, err := s.executor.Do(ctx, "PING")
	return err
}

// Load 优先从 Redis 读取角色缓存，未命中或缓存损坏时回退 MySQL 并回填。
func (s *RedisCachedCharStore) Load(ctx context.Context, characterID string) (CharacterRecord, bool, error) {
	record, ok, err := s.backing.Load(ctx, characterID)
	if err != nil || !ok {
		return CharacterRecord{}, ok, err
	}
	_ = s.CacheRecord(ctx, record)
	return CloneCharacter(record), true, nil
}

// Save 先写权威 MySQL，再刷新 Redis 角色缓存和可重建索引。
func (s *RedisCachedCharStore) Save(ctx context.Context, record CharacterRecord) error {
	if err := s.backing.Save(ctx, record); err != nil {
		return err
	}
	_ = s.CacheRecord(ctx, record)
	return nil
}

// CreateCharacter 透传底层 insert-only 新建语义，成功后再刷新 Redis 读模型。
func (s *RedisCachedCharStore) CreateCharacter(ctx context.Context, record CharacterRecord) error {
	var err error
	if creator, ok := s.backing.(CharacterCreator); ok {
		err = creator.CreateCharacter(ctx, record)
	} else {
		err = ErrCharacterCreateMissing
	}
	if err != nil {
		return err
	}
	_ = s.CacheRecord(ctx, record)
	return nil
}

// SaveFields 先执行底层字段化保存，再按 MySQL 最新记录回填 Redis。
// Redis 回填失败不会改变本次 MySQL 写入结果。
func (s *RedisCachedCharStore) SaveFields(ctx context.Context, record CharacterRecord, fields ...CharacterField) error {
	var err error
	if fieldStore, ok := s.backing.(interface {
		SaveFields(context.Context, CharacterRecord, ...CharacterField) error
	}); ok {
		err = fieldStore.SaveFields(ctx, record, fields...)
	} else {
		err = s.backing.Save(ctx, record)
	}
	if err != nil {
		return err
	}
	if loaded, ok, loadErr := s.backing.Load(ctx, record.CharacterID); loadErr == nil && ok {
		_ = s.CacheRecord(ctx, loaded)
		return nil
	}
	_ = s.CacheRecord(ctx, record)
	return nil
}

func (s *RedisCachedCharStore) AdvanceStoryDigest(ctx context.Context, characterID string, level, migrationVersion uint32) error {
	advancer, ok := s.backing.(CharacterStoryDigestAdvancer)
	if !ok {
		return ErrCharacterStoryDigestAdvanceUnavailable
	}
	if err := advancer.AdvanceStoryDigest(ctx, characterID, level, migrationVersion); err != nil {
		return err
	}
	if loaded, found, err := s.backing.Load(ctx, characterID); err == nil && found {
		_ = s.CacheRecord(ctx, loaded)
	}
	return nil
}

// ListByAccount 优先读取 Redis 账号角色索引，并用 MySQL 权威列表校验命中结果。
// Redis 只是可重建读模型；历史修复、手工删库或旧缓存都不能让选角页继续看到脏角色。
func (s *RedisCachedCharStore) ListByAccount(ctx context.Context, accountID string, limit int) ([]CharacterRecord, error) {
	records, err := s.backing.ListByAccount(ctx, accountID, limit)
	if err != nil {
		return nil, err
	}
	_ = s.cacheAccountList(ctx, accountID, records)
	return cloneCharacters(records), nil
}

// FindIDByName 以 MySQL 名字索引为准，Redis 只做成功后的可重建回填。
func (s *RedisCachedCharStore) FindIDByName(ctx context.Context, name string) (string, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, nil
	}
	id, ok, err := s.backing.FindIDByName(ctx, name)
	if err != nil || !ok {
		return "", ok, err
	}
	_ = s.setValue(ctx, s.NameKey(name), id)
	return id, true, nil
}

// NextNumericID 用 Redis 原子游标分配数字角色 ID，并用 MySQL 最大值作为游标种子。
// Redis 不可用时回退 MySQL 分配；该兜底只保证单进程重建链路可用。
func (s *RedisCachedCharStore) NextNumericID(ctx context.Context) (int, error) {
	nextFromDB, err := s.backing.NextNumericID(ctx)
	if err != nil {
		return 0, err
	}
	seed := nextFromDB - 1
	if seed < 0 {
		seed = 0
	}
	next, err := redis.Int(s.executor.Do(ctx, "EVAL", redisNextIDScript, 1, s.nextIDKey(), seed))
	if err != nil {
		return nextFromDB, nil
	}
	if next < nextFromDB {
		return nextFromDB, nil
	}
	return next, nil
}

func (s *RedisCachedCharStore) CacheRecord(ctx context.Context, record CharacterRecord) error {
	record = CloneCharacter(record)
	if strings.TrimSpace(record.CharacterID) == "" {
		return db.ErrRecordKeyRequired
	}
	commands, err := s.recordCommands(record)
	if err != nil {
		return err
	}
	return db.DoRedisBatch(ctx, s.executor, commands)
}

func (s *RedisCachedCharStore) cacheAccountList(ctx context.Context, accountID string, records []CharacterRecord) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return db.ErrRecordKeyRequired
	}
	commands := []db.RedisCommand{{Name: "DEL", Args: []any{s.AccountKey(accountID)}}}
	for _, record := range records {
		if strings.TrimSpace(record.CharacterID) == "" {
			continue
		}
		commands = append(commands, db.RedisCommand{
			Name: "ZADD",
			Args: []any{s.AccountKey(accountID), record.Slot, record.CharacterID},
		})
		recordCommands, err := s.recordCommands(record)
		if err != nil {
			return err
		}
		commands = append(commands, recordCommands...)
	}
	commands = s.appendSet(commands, s.accountReadyKey(accountID), "1")
	if s.options.TTL > 0 {
		commands = append(commands, db.RedisCommand{Name: "EXPIRE", Args: []any{s.AccountKey(accountID), ttlSeconds(s.options.TTL)}})
	}
	return db.DoRedisBatch(ctx, s.executor, commands)
}

func (s *RedisCachedCharStore) recordCommands(record CharacterRecord) ([]db.RedisCommand, error) {
	commands := make([]db.RedisCommand, 0, 6)
	if strings.TrimSpace(record.AccountID) != "" {
		commands = append(commands, db.RedisCommand{
			Name: "ZADD",
			Args: []any{s.AccountKey(record.AccountID), record.Slot, record.CharacterID},
		})
		if s.options.TTL > 0 {
			commands = append(commands, db.RedisCommand{Name: "EXPIRE", Args: []any{s.AccountKey(record.AccountID), ttlSeconds(s.options.TTL)}})
		}
		commands = s.appendSet(commands, s.accountReadyKey(record.AccountID), "1")
	}
	if strings.TrimSpace(record.Name) != "" {
		commands = s.appendSet(commands, s.NameKey(record.Name), record.CharacterID)
	}
	return commands, nil
}

func (s *RedisCachedCharStore) setValue(ctx context.Context, key string, value any) error {
	commands := s.appendSet(nil, key, value)
	return db.DoRedisBatch(ctx, s.executor, commands)
}

func (s *RedisCachedCharStore) appendSet(commands []db.RedisCommand, key string, value any) []db.RedisCommand {
	if s.options.TTL > 0 {
		return append(commands, db.RedisCommand{
			Name: "SETEX",
			Args: []any{key, ttlSeconds(s.options.TTL), value},
		})
	}
	return append(commands, db.RedisCommand{
		Name: "SET",
		Args: []any{key, value},
	})
}

func (s *RedisCachedCharStore) AccountKey(accountID string) string {
	return s.options.KeyPrefix + ":account:" + digestRedisKey(accountID) + ":characters"
}

func (s *RedisCachedCharStore) accountReadyKey(accountID string) string {
	return s.AccountKey(accountID) + ":ready"
}

func (s *RedisCachedCharStore) NameKey(name string) string {
	return s.options.KeyPrefix + ":character_name:" + digestRedisKey(name)
}

func (s *RedisCachedCharStore) nextIDKey() string {
	return s.options.KeyPrefix + ":character:next_id"
}

func normalizeRedisCacheOptions(options RedisCacheOptions) RedisCacheOptions {
	options.KeyPrefix = strings.TrimSpace(options.KeyPrefix)
	if options.KeyPrefix == "" {
		options.KeyPrefix = defaultRedisCachePrefix
	}
	if options.TTL < 0 {
		options.TTL = 0
	}
	return options
}

func digestRedisKey(value string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func ttlSeconds(ttl time.Duration) int {
	seconds := int(ttl / time.Second)
	if seconds <= 0 {
		return 1
	}
	return seconds
}

func cloneCharacters(records []CharacterRecord) []CharacterRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]CharacterRecord, len(records))
	for idx, record := range records {
		out[idx] = CloneCharacter(record)
	}
	return out
}
