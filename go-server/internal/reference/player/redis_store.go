package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"longheng.io/server/internal/platform/db"
)

const (
	redisProfileLegacy = "profile"
)

// closeRedigoErr 在 Redis 操作成功时保留连接关闭错误，避免连接池异常被静默吞掉。
func closeRedigoErr(conn redis.Conn, err *error) {
	if conn == nil || err == nil {
		return
	}
	if closeErr := conn.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

// RedisStoreOptions 是 Redis 玩家 Profile 存储的连接、命名空间和 TTL 配置。
type RedisStoreOptions struct {
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

// RedisStore 基于 Redis Hash 保存玩家 Profile 的字段化数据。
type RedisStore struct {
	keyPrefix string
	ttl       time.Duration
	executor  redisExecutor
}

type redisExecutor interface {
	db.RedisExecutor
	Close() error
}

// NewRedisStore 创建 Redis 玩家 Profile 存储。
func NewRedisStore(options RedisStoreOptions) *RedisStore {
	options = normRedisStoreOpts(options)
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
	return newRedisStoreExec(&redigoExecutor{pool: pool}, options)
}

func newRedisStoreExec(executor redisExecutor, options RedisStoreOptions) *RedisStore {
	options = normRedisStoreOpts(options)
	return &RedisStore{
		keyPrefix: options.KeyPrefix,
		ttl:       options.TTL,
		executor:  executor,
	}
}

func normRedisStoreOpts(options RedisStoreOptions) RedisStoreOptions {
	options.Address = strings.TrimSpace(options.Address)
	if options.Address == "" {
		options.Address = "127.0.0.1:6379"
	}
	options.KeyPrefix = strings.TrimSpace(options.KeyPrefix)
	if options.KeyPrefix == "" {
		options.KeyPrefix = "profile"
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

// Load 按账号 ID 从 Redis 读取玩家 Profile。
func (s *RedisStore) Load(ctx context.Context, accountID string) (Profile, bool, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return Profile{}, false, err
	}
	accountID = strings.TrimSpace(accountID)
	key, err := s.profileKey(accountID)
	if err != nil {
		return Profile{}, false, err
	}
	baseField, ok := profileDBHashField(ProfileFieldBase)
	if !ok {
		return Profile{}, false, fmt.Errorf("profile base field is not configured")
	}
	fields := redisModuleFields()
	modules, exists, err := s.hmgetBytes(ctx, key, fields...)
	if err != nil {
		return Profile{}, false, err
	}
	if !exists[baseField] {
		return s.loadLegacyProfile(ctx, key, accountID)
	}

	profile, err := profileFromRedis(accountID, modules)
	if err != nil {
		return Profile{}, false, err
	}
	profile = normAccountID(profile, accountID)
	return cloneProfile(profile), true, nil
}

// Check 通过 PING 检查 Redis 玩家 Profile 存储连接。
func (s *RedisStore) Check(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	reply, err := s.executor.Do(ctx, "PING")
	pong, err := redis.String(reply, err)
	if err != nil {
		return err
	}
	if strings.ToUpper(pong) != "PONG" {
		return fmt.Errorf("unexpected redis ping response %q", pong)
	}
	return nil
}

func (s *RedisStore) hgetBytes(ctx context.Context, key, field string) ([]byte, bool, error) {
	reply, err := s.executor.Do(ctx, "HGET", key, field)
	data, err := redis.Bytes(reply, err)
	if errors.Is(err, redis.ErrNil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (s *RedisStore) hmgetBytes(ctx context.Context, key string, fields ...string) (map[string][]byte, map[string]bool, error) {
	args := make([]any, 0, len(fields)+1)
	args = append(args, key)
	for _, field := range fields {
		args = append(args, field)
	}
	reply, err := s.executor.Do(ctx, "HMGET", args...)
	values, err := redis.Values(reply, err)
	if err != nil {
		return nil, nil, err
	}
	if len(values) != len(fields) {
		return nil, nil, fmt.Errorf("redis hmget %s returned %d values for %d fields", key, len(values), len(fields))
	}
	out := make(map[string][]byte, len(fields))
	exists := make(map[string]bool, len(fields))
	for idx, field := range fields {
		data, err := redis.Bytes(values[idx], nil)
		if errors.Is(err, redis.ErrNil) {
			out[field] = nil
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("redis hmget %s field %s: %w", key, field, err)
		}
		out[field] = data
		exists[field] = true
	}
	return out, exists, nil
}

func (s *RedisStore) loadLegacyProfile(ctx context.Context, key, accountID string) (Profile, bool, error) {
	data, ok, err := s.hgetBytes(ctx, key, redisProfileLegacy)
	if err != nil || !ok {
		return Profile{}, false, err
	}
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, false, fmt.Errorf("decode redis legacy profile %s: %w", accountID, err)
	}
	if profile.AccountID == "" {
		profile.AccountID = accountID
	}
	profile = normAccountID(profile, accountID)
	return cloneProfile(profile), true, nil
}

// Save 保存完整玩家 Profile 到 Redis。
func (s *RedisStore) Save(ctx context.Context, profile Profile) error {
	return s.SaveFields(ctx, profile, AllProfileFields()...)
}

// SaveFields 只保存玩家 Profile 的指定 Redis Hash 字段。
func (s *RedisStore) SaveFields(ctx context.Context, profile Profile, fields ...ProfileField) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	profile = normProfileID(profile)
	key, err := s.profileKey(profile.AccountID)
	if err != nil {
		return err
	}
	fields = normProfileFields(fields)
	if len(fields) == 0 {
		return nil
	}
	values, err := encodeProfileFields(profile, fields)
	if err != nil {
		return err
	}
	return db.SaveRedisHashFields(ctx, s.executor, key, values, s.ttl)
}

// SaveFieldBatch 批量保存多个玩家 Profile 的字段变更。
func (s *RedisStore) SaveFieldBatch(ctx context.Context, saves []db.FieldSave[Profile, ProfileField]) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	batches := make([]db.RedisHashBatch, 0, len(saves))
	for _, save := range saves {
		save.Record = normProfileID(save.Record)
		key, err := s.profileKey(save.Record.AccountID)
		if err != nil {
			return err
		}
		fields := normProfileFields(save.Fields)
		if len(fields) == 0 {
			continue
		}
		values, err := encodeProfileFields(save.Record, fields)
		if err != nil {
			return err
		}
		batches = append(batches, db.RedisHashBatch{
			Key:    key,
			Fields: values,
			TTL:    s.ttl,
		})
	}
	return db.SaveRedisHashFieldBatches(ctx, s.executor, batches)
}

// Expire 为指定玩家 Profile 设置 Redis 过期时间。
func (s *RedisStore) Expire(ctx context.Context, accountID string, ttl time.Duration) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		return nil
	}
	key, err := s.profileKey(accountID)
	if err != nil {
		return err
	}
	_, err = s.executor.Do(ctx, "EXPIRE", key, int(ttl.Seconds()))
	return err
}

// Close 关闭 Redis 玩家 Profile 存储连接池。
func (s *RedisStore) Close(context.Context) error {
	if s == nil || s.executor == nil {
		return nil
	}
	return s.executor.Close()
}

func (s *RedisStore) profileKey(accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return "", fmt.Errorf("account id is required")
	}
	return s.keyPrefix + ":" + accountID, nil
}

type redigoExecutor struct {
	pool *redis.Pool
}

// Do 在 redigo 连接池上执行单条 Redis 命令。
func (e *redigoExecutor) Do(ctx context.Context, command string, args ...any) (reply any, err error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := e.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRedigoErr(conn, &err)
	return conn.Do(command, args...)
}

// DoBatch 在同一条 redigo 连接上批量发送并接收 Redis 命令。
func (e *redigoExecutor) DoBatch(ctx context.Context, commands []db.RedisCommand) (err error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(commands) == 0 {
		return nil
	}
	conn, err := e.pool.GetContext(ctx)
	if err != nil {
		return err
	}
	defer closeRedigoErr(conn, &err)
	return db.RunRedigoBatch(ctx, conn, commands)
}

// Close 关闭 redigo 连接池。
func (e *redigoExecutor) Close() error {
	return e.pool.Close()
}
