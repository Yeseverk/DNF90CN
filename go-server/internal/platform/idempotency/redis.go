package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"longheng.io/server/internal/platform/db"
)

const redisBeginScript = `
-- Begin 原子检查 committed/pending/sequence 水位，只写 pending 占位，不提交业务结果。
local committed_key = KEYS[1]
local committed_seq_key = KEYS[2]
local pending_key = KEYS[3]
local pending_seq_key = KEYS[4]
local ttl_ms = tonumber(ARGV[1])
local seq = tonumber(ARGV[2]) or 0
local fingerprint = ARGV[3] or ""
local owner = ARGV[4] or ""
local function metadata(value)
  local first = string.find(value or "", "\n", 1, true)
  if first == nil then return "", "" end
  local second = string.find(value, "\n", first + 1, true)
  if second == nil then return string.sub(value, first + 1), "" end
  return string.sub(value, first + 1, second - 1), string.sub(value, second + 1)
end
local function conflicts(value)
  local stored, _ = metadata(value)
  return stored ~= "" and fingerprint ~= "" and stored ~= fingerprint
end
local function marker(status)
  return status .. "\n" .. fingerprint .. "\n" .. owner
end
local committed = redis.call("GET", committed_key)
if committed then
  if conflicts(committed) then return "conflict" end
  return "duplicate"
end
local pending_value = redis.call("GET", pending_key)
if pending_value then
  if conflicts(pending_value) then return "conflict" end
  return "in_flight"
end
if seq > 0 then
  local highest = tonumber(redis.call("GET", committed_seq_key) or "0")
  if highest >= seq then
    redis.call("PSETEX", committed_key, ttl_ms, marker("replay"))
    return "replay"
  end
  while true do
    local pending = redis.call("ZREVRANGE", pending_seq_key, 0, 0, "WITHSCORES")
    if pending[1] == nil then
      break
    end
    if redis.call("EXISTS", pending[1]) == 1 then
      return "in_flight"
    end
    redis.call("ZREM", pending_seq_key, pending[1])
  end
  redis.call("ZADD", pending_seq_key, seq, pending_key)
  redis.call("PEXPIRE", pending_seq_key, ttl_ms)
end
redis.call("PSETEX", pending_key, ttl_ms, marker("pending"))
return "accepted"
`

const redisCommitScript = `
-- Commit 把 pending 移入 committed，并推进 sequence 水位；之后重复请求只能走 duplicate/replay。
local committed_key = KEYS[1]
local committed_seq_key = KEYS[2]
local pending_key = KEYS[3]
local pending_seq_key = KEYS[4]
local ttl_ms = tonumber(ARGV[1])
local seq = tonumber(ARGV[2]) or 0
local status = ARGV[3]
local fingerprint = ARGV[4] or ""
local owner = ARGV[5] or ""
if status == "" then
  status = "accepted"
end
local function metadata(value)
  local first = string.find(value or "", "\n", 1, true)
  if first == nil then return "", "" end
  local second = string.find(value, "\n", first + 1, true)
  if second == nil then return string.sub(value, first + 1), "" end
  return string.sub(value, first + 1, second - 1), string.sub(value, second + 1)
end
local function conflicts(value)
  local stored, _ = metadata(value)
  return stored ~= "" and fingerprint ~= "" and stored ~= fingerprint
end
local committed = redis.call("GET", committed_key)
if committed then
  if conflicts(committed) then return "conflict" end
  local _, committed_owner = metadata(committed)
  if committed_owner == owner then return "already_committed" end
  return "lost"
end
local pending = redis.call("GET", pending_key)
if pending then
  local _, pending_owner = metadata(pending)
  if pending_owner ~= owner then return "lost" end
  if conflicts(pending) then return "conflict" end
elseif owner ~= "" then
  return "lost"
end
redis.call("PSETEX", committed_key, ttl_ms, status .. "\n" .. fingerprint .. "\n" .. owner)
if seq > 0 then
  local highest = tonumber(redis.call("GET", committed_seq_key) or "0")
  if highest < seq then
    redis.call("SET", committed_seq_key, seq, "PX", ttl_ms)
  else
    redis.call("PEXPIRE", committed_seq_key, ttl_ms)
  end
  redis.call("ZREM", pending_seq_key, pending_key)
end
redis.call("DEL", pending_key)
return status
`

const redisCommitResLua = `
-- CommitResult 在一个 Lua 原子操作里提交防重状态和可重放结果，响应热缓存不参与一致性边界。
local committed_key = KEYS[1]
local committed_seq_key = KEYS[2]
local pending_key = KEYS[3]
local pending_seq_key = KEYS[4]
local result_key = KEYS[5]
local ttl_ms = tonumber(ARGV[1])
local seq = tonumber(ARGV[2]) or 0
local status = ARGV[3]
local result = ARGV[4]
local fingerprint = ARGV[5] or ""
local owner = ARGV[6] or ""
if status == "" then
  status = "accepted"
end
local function metadata(value)
  local first = string.find(value or "", "\n", 1, true)
  if first == nil then return "", "" end
  local second = string.find(value, "\n", first + 1, true)
  if second == nil then return string.sub(value, first + 1), "" end
  return string.sub(value, first + 1, second - 1), string.sub(value, second + 1)
end
local function conflicts(value)
  local stored, _ = metadata(value)
  return stored ~= "" and fingerprint ~= "" and stored ~= fingerprint
end
local committed = redis.call("GET", committed_key)
if committed then
  if conflicts(committed) then return "conflict" end
  local _, committed_owner = metadata(committed)
  if committed_owner == owner and redis.call("GET", result_key) == result then return "already_committed" end
  return "lost"
end
local pending = redis.call("GET", pending_key)
if pending then
  local _, pending_owner = metadata(pending)
  if pending_owner ~= owner then return "lost" end
  if conflicts(pending) then return "conflict" end
elseif owner ~= "" then
  return "lost"
end
redis.call("PSETEX", committed_key, ttl_ms, status .. "\n" .. fingerprint .. "\n" .. owner)
redis.call("PSETEX", result_key, ttl_ms, result)
if seq > 0 then
  local highest = tonumber(redis.call("GET", committed_seq_key) or "0")
  if highest < seq then
    redis.call("SET", committed_seq_key, seq, "PX", ttl_ms)
  else
    redis.call("PEXPIRE", committed_seq_key, ttl_ms)
  end
  redis.call("ZREM", pending_seq_key, pending_key)
end
redis.call("DEL", pending_key)
return status
`

const redisAbortScript = `
-- Abort 只删除 pending key 和 pending sequence 索引，不推进 committed 水位，允许失败请求重试。
local pending_key = KEYS[1]
local pending_seq_key = KEYS[2]
local fingerprint = ARGV[1] or ""
local owner = ARGV[2] or ""
local pending = redis.call("GET", pending_key)
if pending then
  local split = string.find(pending, "\n", 1, true)
  local stored = ""
  local stored_owner = ""
  if split ~= nil then
    local second = string.find(pending, "\n", split + 1, true)
    if second == nil then
      stored = string.sub(pending, split + 1)
    else
      stored = string.sub(pending, split + 1, second - 1)
      stored_owner = string.sub(pending, second + 1)
    end
  end
  if stored_owner ~= owner then return "lost" end
  if stored ~= "" and fingerprint ~= "" and stored ~= fingerprint then
    return "conflict"
  end
elseif owner ~= "" then
  return "lost"
end
redis.call("DEL", pending_key)
redis.call("ZREM", pending_seq_key, pending_key)
return "aborted"
`

const redisCommitLookup = `
local key = KEYS[1]
local seq_key = KEYS[2]
local ttl_ms = tonumber(ARGV[1])
local seq = tonumber(ARGV[2]) or 0
local fingerprint = ARGV[3] or ""
local value = redis.call("GET", key)
if value then
  local split = string.find(value, "\n", 1, true)
  local stored = ""
  if split ~= nil then
    local second = string.find(value, "\n", split + 1, true)
    if second == nil then stored = string.sub(value, split + 1) else stored = string.sub(value, split + 1, second - 1) end
  end
  if stored ~= "" and fingerprint ~= "" and stored ~= fingerprint then
    return "conflict"
  end
  return "duplicate"
end
if seq > 0 then
  local highest = tonumber(redis.call("GET", seq_key) or "0")
  if highest >= seq then
    local source_ttl = tonumber(redis.call("PTTL", seq_key) or "-1")
    local replay_ttl = ttl_ms
    if source_ttl > 0 and source_ttl < replay_ttl then replay_ttl = source_ttl end
    if replay_ttl > 0 then redis.call("PSETEX", key, replay_ttl, "replay\n" .. fingerprint .. "\n") end
    return "replay"
  end
end
return "miss"
`

const redisCommitStore = `
local key = KEYS[1]
local seq_key = KEYS[2]
local ttl_ms = tonumber(ARGV[1])
local seq = tonumber(ARGV[2]) or 0
local status = ARGV[3]
local fingerprint = ARGV[4] or ""
local owner = ARGV[5] or ""
if status == "" then
  status = "accepted"
end
local current = redis.call("GET", key)
if current then
  local split = string.find(current, "\n", 1, true)
  local stored = ""
  if split ~= nil then
    local second = string.find(current, "\n", split + 1, true)
    if second == nil then stored = string.sub(current, split + 1) else stored = string.sub(current, split + 1, second - 1) end
  end
  if stored ~= "" and fingerprint ~= "" and stored ~= fingerprint then
    return "conflict"
  end
end
redis.call("PSETEX", key, ttl_ms, status .. "\n" .. fingerprint .. "\n" .. owner)
if seq > 0 then
  local highest = tonumber(redis.call("GET", seq_key) or "0")
  if highest < seq then
    redis.call("SET", seq_key, seq, "PX", ttl_ms)
  else
    redis.call("PEXPIRE", seq_key, ttl_ms)
  end
end
return status
`

const redisCommitRecover = `
local key = KEYS[1]
local seq_key = KEYS[2]
local seq = tonumber(ARGV[1]) or 0
local fingerprint = ARGV[2] or ""
local owner = ARGV[3] or ""
local value = redis.call("GET", key)
if value then
  local split = string.find(value, "\n", 1, true)
  local stored = ""
  local stored_owner = ""
  if split ~= nil then
    local second = string.find(value, "\n", split + 1, true)
    if second == nil then stored = string.sub(value, split + 1) else
      stored = string.sub(value, split + 1, second - 1)
      stored_owner = string.sub(value, second + 1)
    end
  end
  if stored ~= "" and fingerprint ~= "" and stored ~= fingerprint then
    return "conflict"
  end
  if stored_owner == owner then return "hit" end
  return "lost"
end
if owner == "" and seq > 0 then
  local highest = tonumber(redis.call("GET", seq_key) or "0")
  if highest >= seq then
    return "hit"
  end
end
return "miss"
`

const redisResultLookup = `
return redis.call("GET", KEYS[1])
`

const redisResultRecover = `
if redis.call("EXISTS", KEYS[1]) ~= 1 then
  return "miss"
end
local marker = redis.call("GET", KEYS[1])
local first = string.find(marker or "", "\n", 1, true)
local stored_owner = ""
if first ~= nil then
  local second = string.find(marker, "\n", first + 1, true)
  if second ~= nil then stored_owner = string.sub(marker, second + 1) end
end
if stored_owner ~= ARGV[2] then return "miss" end
local result = redis.call("GET", KEYS[2])
if result == ARGV[1] then
  return "hit"
end
return "miss"
`

type RedisOptions struct {
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
	Now            func() time.Time
	Executor       db.RedisExecutor
}

type redisStore struct {
	keyPrefix string
	ttl       time.Duration
	executor  db.RedisExecutor
	closer    interface{ Close() error }
}

func NewRedis(options RedisOptions) *Guard {
	options = normRedisOpts(options)
	return New(Options{
		TTL:   options.TTL,
		Now:   options.Now,
		Kind:  "redis",
		Store: newRedisOpts(options),
	})
}

func newRedisOpts(options RedisOptions) *redisStore {
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
	return newRedisStore(options.KeyPrefix, options.TTL, executor, closer)
}

func newRedisStore(keyPrefix string, ttl time.Duration, executor db.RedisExecutor, closer interface{ Close() error }) *redisStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	keyPrefix = strings.TrimSpace(keyPrefix)
	if keyPrefix == "" {
		keyPrefix = "idempotency"
	}
	return &redisStore{
		keyPrefix: keyPrefix,
		ttl:       ttl,
		executor:  executor,
		closer:    closer,
	}
}

func normRedisOpts(options RedisOptions) RedisOptions {
	options.Address = strings.TrimSpace(options.Address)
	if options.Address == "" {
		options.Address = "127.0.0.1:6379"
	}
	options.KeyPrefix = strings.TrimSpace(options.KeyPrefix)
	if options.KeyPrefix == "" {
		options.KeyPrefix = "idempotency"
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
	if options.TTL <= 0 {
		options.TTL = 10 * time.Minute
	}
	return options
}

func (s *redisStore) Check(ctx context.Context, item Request) (Decision, error) {
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

func (s *redisStore) Begin(ctx context.Context, item Request) (Decision, error) {
	if s == nil || s.executor == nil {
		return Decision{}, fmt.Errorf("redis idempotency executor is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	key := item.Key
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	// Redis Begin 全部交给 Lua，避免多 logic 节点同时接受同一 key 或同一 sequence。
	statusText, err := redis.String(s.executor.Do(
		ctx,
		"EVAL",
		redisBeginScript,
		4,
		s.requestKey(key),
		s.sequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		s.pendingRequestKey(key),
		s.pendingSequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		int64(s.ttlOrDefault()/time.Millisecond),
		item.Sequence,
		item.Fingerprint,
		item.reservationToken,
	))
	if err != nil {
		return Decision{}, err
	}
	if statusText == "conflict" {
		return Decision{}, ErrRequestConflict
	}
	status := Status(statusText)
	if status != StatusDuplicate && status != StatusInFlight && status != StatusReplay {
		status = StatusAccepted
	}
	decision := Decision{Status: status, Key: key, Sequence: item.Sequence}
	if status == StatusAccepted {
		decision.ownerToken = item.reservationToken
	}
	return decision, nil
}

func (s *redisStore) Commit(ctx context.Context, item Request, decision Decision) error {
	if s == nil || s.executor == nil {
		return fmt.Errorf("redis idempotency executor is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := decision.Key
	if key == "" {
		key = item.Key
	}
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	status := decision.Status
	if status == "" {
		status = StatusAccepted
	}
	// Commit 的 Lua 同步删除 pending 并写 committed；调用方必须在业务 handler 成功后再调用。
	statusText, err := redis.String(s.executor.Do(
		ctx,
		"EVAL",
		redisCommitScript,
		4,
		s.requestKey(key),
		s.sequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		s.pendingRequestKey(key),
		s.pendingSequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		int64(s.ttlOrDefault()/time.Millisecond),
		item.Sequence,
		string(status),
		item.Fingerprint,
		decision.ownerToken,
	))
	if err == nil && statusText == "conflict" {
		return ErrRequestConflict
	}
	if err == nil && statusText == "lost" {
		return ErrReservationLost
	}
	if err != nil && s.redisCommitRecovered(ctx, item, key, decision.ownerToken) {
		return nil
	}
	return err
}

func (s *redisStore) CommitResult(ctx context.Context, item Request, decision Decision, payload []byte) error {
	if s == nil || s.executor == nil {
		return fmt.Errorf("redis idempotency executor is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := decision.Key
	if key == "" {
		key = item.Key
	}
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	status := decision.Status
	if status == "" {
		status = StatusAccepted
	}
	statusText, err := redis.String(s.executor.Do(
		ctx,
		"EVAL",
		redisCommitResLua,
		5,
		s.requestKey(key),
		s.sequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		s.pendingRequestKey(key),
		s.pendingSequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		s.resultKey(key),
		int64(s.ttlOrDefault()/time.Millisecond),
		item.Sequence,
		string(status),
		payload,
		item.Fingerprint,
		decision.ownerToken,
	))
	if err == nil && statusText == "conflict" {
		return ErrRequestConflict
	}
	if err == nil && statusText == "lost" {
		return ErrReservationLost
	}
	if err != nil && s.redisResultRecovered(ctx, key, payload, decision.ownerToken) {
		return nil
	}
	return err
}

func (s *redisStore) LookupResult(ctx context.Context, decision Decision) ([]byte, bool, error) {
	if s == nil || s.executor == nil {
		return nil, false, fmt.Errorf("redis idempotency executor is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	key := strings.TrimSpace(decision.Key)
	if key == "" {
		return nil, false, nil
	}
	payload, err := redis.Bytes(s.executor.Do(ctx, "EVAL", redisResultLookup, 1, s.resultKey(key)))
	if errors.Is(err, redis.ErrNil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return append([]byte(nil), payload...), true, nil
}

func (s *redisStore) redisResultRecovered(parent context.Context, key string, payload []byte, ownerToken string) bool {
	ctx, cancel := idemRecoverCtx(parent)
	defer cancel()
	status, err := redis.String(s.executor.Do(
		ctx,
		"EVAL",
		redisResultRecover,
		2,
		s.requestKey(key),
		s.resultKey(key),
		payload,
		ownerToken,
	))
	return err == nil && status == "hit"
}

func (s *redisStore) redisCommitRecovered(parent context.Context, item Request, key, ownerToken string) bool {
	ctx, cancel := idemRecoverCtx(parent)
	defer cancel()
	status, err := redis.String(s.executor.Do(
		ctx,
		"EVAL",
		redisCommitRecover,
		2,
		s.requestKey(key),
		s.sequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		item.Sequence,
		item.Fingerprint,
		ownerToken,
	))
	return err == nil && status == "hit"
}

func (s *redisStore) Abort(ctx context.Context, item Request, decision Decision) error {
	if s == nil || s.executor == nil {
		return fmt.Errorf("redis idempotency executor is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := decision.Key
	if key == "" {
		key = item.Key
	}
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	// Abort 用于 handler 失败，避免 Redis pending 卡住后续重试。
	statusText, err := redis.String(s.executor.Do(
		ctx,
		"EVAL",
		redisAbortScript,
		2,
		s.pendingRequestKey(key),
		s.pendingSequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		item.Fingerprint,
		decision.ownerToken,
	))
	if err == nil && statusText == "conflict" {
		return ErrRequestConflict
	}
	if err == nil && statusText == "lost" {
		return ErrReservationLost
	}
	return err
}

func (s *redisStore) LookupCommitted(ctx context.Context, item Request) (Decision, bool, error) {
	if s == nil || s.executor == nil {
		return Decision{}, false, fmt.Errorf("redis idempotency executor is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, false, err
	}
	key := item.Key
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	statusText, err := redis.String(s.executor.Do(
		ctx,
		"EVAL",
		redisCommitLookup,
		2,
		s.committedRequestKey(key),
		s.committedSequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		int64(s.ttlOrDefault()/time.Millisecond),
		item.Sequence,
		item.Fingerprint,
	))
	if err != nil {
		return Decision{}, false, err
	}
	switch Status(statusText) {
	case StatusDuplicate:
		return Decision{Status: StatusDuplicate, Key: key, Sequence: item.Sequence}, true, nil
	case StatusReplay:
		return Decision{Status: StatusReplay, Key: key, Sequence: item.Sequence}, true, nil
	default:
		if statusText == "conflict" {
			return Decision{}, false, ErrRequestConflict
		}
		return Decision{}, false, nil
	}
}

func (s *redisStore) StoreCommitted(ctx context.Context, item Request, decision Decision) error {
	return s.storeCommittedTTL(ctx, item, decision, s.ttlOrDefault())
}

func (s *redisStore) storeCommittedTTL(ctx context.Context, item Request, decision Decision, ttl time.Duration) error {
	if s == nil || s.executor == nil {
		return fmt.Errorf("redis idempotency executor is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl < time.Millisecond {
		return nil
	}
	key := decision.Key
	if key == "" {
		key = item.Key
	}
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	status := decision.Status
	if status == "" {
		status = StatusAccepted
	}
	statusText, err := redis.String(s.executor.Do(
		ctx,
		"EVAL",
		redisCommitStore,
		2,
		s.committedRequestKey(key),
		s.committedSequenceKey(sequenceScope(item.Scope, item.Subject, item.Session)),
		int64(ttl/time.Millisecond),
		item.Sequence,
		string(status),
		item.Fingerprint,
		decision.ownerToken,
	))
	if err == nil && statusText == "conflict" {
		return ErrRequestConflict
	}
	return err
}

func (s *redisStore) Snapshot() map[string]any {
	if s == nil {
		return map[string]any{}
	}
	return map[string]any{
		"backend":     "redis",
		"ttl_seconds": int64(s.ttlOrDefault() / time.Second),
		"key_prefix":  s.keyPrefixOrDefault(),
	}
}

func (s *redisStore) Close(context.Context) error {
	if s == nil || s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

func (s *redisStore) requestKey(key string) string {
	return s.keyPrefixOrDefault() + ":key:" + digestKey(key)
}

func (s *redisStore) sequenceKey(scope string) string {
	return s.keyPrefixOrDefault() + ":seq:" + digestKey(scope)
}

func (s *redisStore) committedRequestKey(key string) string {
	return s.keyPrefixOrDefault() + ":committed:key:" + digestKey(key)
}

func (s *redisStore) committedSequenceKey(scope string) string {
	return s.keyPrefixOrDefault() + ":committed:seq:" + digestKey(scope)
}

func (s *redisStore) pendingRequestKey(key string) string {
	return s.keyPrefixOrDefault() + ":pending:key:" + digestKey(key)
}

func (s *redisStore) pendingSequenceKey(scope string) string {
	return s.keyPrefixOrDefault() + ":pending:seq:" + digestKey(scope)
}

func (s *redisStore) resultKey(key string) string {
	return s.keyPrefixOrDefault() + ":result:key:" + digestKey(key)
}

func (s *redisStore) ttlOrDefault() time.Duration {
	if s == nil || s.ttl <= 0 {
		return 10 * time.Minute
	}
	return s.ttl
}

func (s *redisStore) keyPrefixOrDefault() string {
	if s == nil {
		return "idempotency"
	}
	prefix := strings.TrimSpace(s.keyPrefix)
	if prefix == "" {
		return "idempotency"
	}
	return prefix
}

func digestKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
