package player

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrMySQLShardConfig 表示 MySQL 玩家 Profile 分片配置不合法。
var ErrMySQLShardConfig = errors.New("mysql profile shard config is invalid")

const mysqlShardSlots = 1024

// MySQLShardOptions 描述单个 MySQL 玩家 Profile 分片的连接和槽位配置。
type MySQLShardOptions struct {
	ID              string
	DSN             string
	TableName       string
	TablePrefix     string
	HashSlots       string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	EnsureSchema    bool
}

// ShardedMySQLStoreOptions 是分片 MySQL 玩家 Profile 存储的全局默认值和分片清单。
type ShardedMySQLStoreOptions struct {
	DefaultDSN             string
	DefaultMaxOpenConns    int
	DefaultMaxIdleConns    int
	DefaultConnMaxLifetime time.Duration
	EnsureSchema           bool
	Shards                 []MySQLShardOptions
}

// ShardedMySQLStore 按账号 ID 路由到多个 MySQL 玩家 Profile 存储。
type ShardedMySQLStore struct {
	shards     map[string]*MySQLStore
	order      []string
	slotOwners []string
}

// NewShardedMySQLStore 创建分片 MySQL 玩家 Profile 存储。
func NewShardedMySQLStore(options ShardedMySQLStoreOptions) (*ShardedMySQLStore, error) {
	if len(options.Shards) == 0 {
		return nil, fmt.Errorf("%w: at least one mysql shard is required", ErrMySQLShardConfig)
	}
	out := &ShardedMySQLStore{
		shards: make(map[string]*MySQLStore, len(options.Shards)),
	}
	normalizedShards := make([]MySQLShardOptions, 0, len(options.Shards))
	for idx, shard := range options.Shards {
		normalized, err := normMySQLShardOpts(options, shard, idx)
		if err != nil {
			return nil, err
		}
		if _, exists := out.shards[normalized.ID]; exists {
			return nil, fmt.Errorf("%w: duplicated shard id %q", ErrMySQLShardConfig, normalized.ID)
		}
		normalizedShards = append(normalizedShards, normalized)
		out.shards[normalized.ID] = NewMySQLStore(MySQLStoreOptions{
			DSN:             normalized.DSN,
			TableName:       normalized.TableName,
			MaxOpenConns:    normalized.MaxOpenConns,
			MaxIdleConns:    normalized.MaxIdleConns,
			ConnMaxLifetime: normalized.ConnMaxLifetime,
			EnsureSchema:    normalized.EnsureSchema,
		})
		out.order = append(out.order, normalized.ID)
	}
	sort.Strings(out.order)
	slotOwners, err := buildShardOwners(normalizedShards)
	if err != nil {
		_ = out.Close(context.Background())
		return nil, err
	}
	out.slotOwners = slotOwners
	return out, nil
}

func normMySQLShardOpts(defaults ShardedMySQLStoreOptions, shard MySQLShardOptions, idx int) (MySQLShardOptions, error) {
	shard.ID = strings.TrimSpace(shard.ID)
	if shard.ID == "" {
		shard.ID = fmt.Sprintf("shard_%d", idx)
	}
	if !isSafeProfileShardID(shard.ID) {
		return MySQLShardOptions{}, fmt.Errorf("%w: shard id %q can only contain letters, digits, dot, dash, or underscore", ErrMySQLShardConfig, shard.ID)
	}
	shard.DSN = strings.TrimSpace(shard.DSN)
	if shard.DSN == "" {
		shard.DSN = strings.TrimSpace(defaults.DefaultDSN)
	}
	if shard.DSN == "" {
		return MySQLShardOptions{}, fmt.Errorf("%w: shard %q dsn is required", ErrMySQLShardConfig, shard.ID)
	}
	shard.TableName = strings.TrimSpace(shard.TableName)
	shard.TablePrefix = strings.TrimSpace(shard.TablePrefix)
	shard.HashSlots = strings.TrimSpace(shard.HashSlots)
	if shard.TableName == "" && shard.TablePrefix != "" {
		shard.TableName = shard.TablePrefix + "_" + mysqlProfileTable
	}
	if shard.TableName == "" {
		shard.TableName = mysqlProfileTable
	}
	if !isMySQLProfileID(shard.TableName) {
		return MySQLShardOptions{}, fmt.Errorf("%w: shard %q table name %q must be a sql identifier", ErrMySQLShardConfig, shard.ID, shard.TableName)
	}
	if shard.MaxOpenConns <= 0 {
		shard.MaxOpenConns = defaults.DefaultMaxOpenConns
	}
	if shard.MaxIdleConns <= 0 {
		shard.MaxIdleConns = defaults.DefaultMaxIdleConns
	}
	if shard.ConnMaxLifetime <= 0 {
		shard.ConnMaxLifetime = defaults.DefaultConnMaxLifetime
	}
	shard.EnsureSchema = shard.EnsureSchema || defaults.EnsureSchema
	return shard, nil
}

func buildShardOwners(shards []MySQLShardOptions) ([]string, error) {
	useSlots := false
	for _, shard := range shards {
		if strings.TrimSpace(shard.HashSlots) != "" {
			useSlots = true
			break
		}
	}
	if !useSlots {
		return nil, nil
	}

	owners := make([]string, mysqlShardSlots)
	for _, shard := range shards {
		if strings.TrimSpace(shard.HashSlots) == "" {
			return nil, fmt.Errorf("%w: shard %q hash_slots is required when any shard uses slot routing", ErrMySQLShardConfig, shard.ID)
		}
		ranges, err := parseShardSlots(shard.HashSlots)
		if err != nil {
			return nil, fmt.Errorf("%w: shard %q %w", ErrMySQLShardConfig, shard.ID, err)
		}
		for _, item := range ranges {
			for slot := item.start; slot <= item.end; slot++ {
				if owners[slot] != "" {
					return nil, fmt.Errorf("%w: hash slot %d is assigned to both %q and %q", ErrMySQLShardConfig, slot, owners[slot], shard.ID)
				}
				owners[slot] = shard.ID
			}
		}
	}
	for slot, owner := range owners {
		if owner == "" {
			return nil, fmt.Errorf("%w: hash slot %d is not assigned to any shard", ErrMySQLShardConfig, slot)
		}
	}
	return owners, nil
}

type mysqlShardSlotRange struct {
	start int
	end   int
}

func parseShardSlots(value string) ([]mysqlShardSlotRange, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("hash_slots is required")
	}
	parts := strings.Split(value, ",")
	out := make([]mysqlShardSlotRange, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("hash_slots contains empty range")
		}
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("hash slot range %q is invalid", part)
		}
		start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		if err != nil {
			return nil, fmt.Errorf("hash slot range %q has invalid start", part)
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("hash slot range %q has invalid end", part)
			}
		}
		if start < 0 || end < start || end >= mysqlShardSlots {
			return nil, fmt.Errorf("hash slot range %q must be within 0-%d", part, mysqlShardSlots-1)
		}
		out = append(out, mysqlShardSlotRange{start: start, end: end})
	}
	return out, nil
}

func isSafeProfileShardID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if r == '.' || r == '-' || r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return !strings.HasPrefix(value, ".") && !strings.HasSuffix(value, ".") && !strings.Contains(value, "..")
}

func isMySQLProfileID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for idx, r := range value {
		if idx == 0 {
			if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				continue
			}
			return false
		}
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// Load 按账号 ID 路由到对应 MySQL 分片读取玩家 Profile。
func (s *ShardedMySQLStore) Load(ctx context.Context, accountID string) (Profile, bool, error) {
	store, _, err := s.storeForAccount(accountID)
	if err != nil {
		return Profile{}, false, err
	}
	return store.Load(ctx, accountID)
}

// Save 按账号 ID 路由到对应 MySQL 分片保存完整玩家 Profile。
func (s *ShardedMySQLStore) Save(ctx context.Context, profile Profile) error {
	return s.SaveFields(ctx, profile, AllProfileFields()...)
}

// SaveFields 按账号 ID 路由到对应 MySQL 分片保存指定玩家字段。
func (s *ShardedMySQLStore) SaveFields(ctx context.Context, profile Profile, fields ...ProfileField) error {
	profile, err := normMySQLIdentity(profile)
	if err != nil {
		return err
	}
	store, _, err := s.storeForAccount(profile.AccountID)
	if err != nil {
		return err
	}
	return store.SaveFields(ctx, profile, fields...)
}

// LoadByRoleID 遍历 MySQL 分片按角色 ID 查找玩家 Profile。
func (s *ShardedMySQLStore) LoadByRoleID(ctx context.Context, roleID string) (Profile, bool, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return Profile{}, false, err
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return Profile{}, false, fmt.Errorf("role id is required")
	}
	for _, shardID := range s.order {
		profile, ok, err := s.shards[shardID].LoadByRoleID(ctx, roleID)
		if err != nil {
			return Profile{}, false, err
		}
		if ok {
			return profile, true, nil
		}
	}
	return Profile{}, false, nil
}

// ListProfiles 从所有 MySQL 分片按账号游标合并扫描玩家 Profile。
func (s *ShardedMySQLStore) ListProfiles(ctx context.Context, afterAccountID string, limit int) ([]Profile, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}
	afterAccountID = strings.TrimSpace(afterAccountID)
	out := make([]Profile, 0, limit)
	for _, shardID := range s.order {
		items, err := s.shards[shardID].ListProfiles(ctx, afterAccountID, limit)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AccountID < out[j].AccountID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Check 检查全部 MySQL 玩家 Profile 分片。
func (s *ShardedMySQLStore) Check(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, shardID := range s.order {
		if err := s.shards[shardID].Check(ctx); err != nil {
			return fmt.Errorf("check mysql profile shard %s: %w", shardID, err)
		}
	}
	return nil
}

// Close 关闭全部 MySQL 玩家 Profile 分片连接。
func (s *ShardedMySQLStore) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	var errs []error
	for _, shardID := range s.order {
		if err := s.shards[shardID].Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("close mysql profile shard %s: %w", shardID, err))
		}
	}
	return errors.Join(errs...)
}

// ResolveShardID 按账号 ID 解析目标 MySQL 分片 ID。
func (s *ShardedMySQLStore) ResolveShardID(accountID string) (string, error) {
	if s == nil || len(s.order) == 0 {
		return "", fmt.Errorf("%w: no mysql profile shard is configured", ErrMySQLShardConfig)
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return "", fmt.Errorf("account id is required")
	}
	if len(s.order) == 1 {
		return s.order[0], nil
	}
	checksum := crc32.ChecksumIEEE([]byte(accountID))
	if len(s.slotOwners) == mysqlShardSlots {
		slot := int(checksum % mysqlShardSlots)
		return s.slotOwners[slot], nil
	}
	if len(s.order) > math.MaxUint32 {
		return "", fmt.Errorf("%w: mysql profile shard count exceeds uint32 range", ErrMySQLShardConfig)
	}
	shardIndex := int(checksum % uint32(len(s.order))) //nolint:gosec // G115：len 已先校验不超过 uint32，取模结果可安全转回 slice 下标。
	return s.order[shardIndex], nil
}

// TableNames 返回每个 MySQL 分片对应的 Profile 表名。
func (s *ShardedMySQLStore) TableNames() map[string]string {
	out := make(map[string]string, len(s.shards))
	for _, shardID := range s.order {
		out[shardID] = s.shards[shardID].TableName()
	}
	return out
}

func (s *ShardedMySQLStore) storeForAccount(accountID string) (*MySQLStore, string, error) {
	shardID, err := s.ResolveShardID(accountID)
	if err != nil {
		return nil, "", err
	}
	store := s.shards[shardID]
	if store == nil {
		return nil, "", fmt.Errorf("%w: shard %q store is missing", ErrMySQLShardConfig, shardID)
	}
	return store, shardID, nil
}
