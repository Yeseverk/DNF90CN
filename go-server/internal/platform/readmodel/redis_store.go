package readmodel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"longheng.io/server/internal/platform/db"
)

const (
	redisScanCount = 256

	RedisCleanupMissingScript = `
if redis.call("HGET", KEYS[2], ARGV[1]) == false then
  return redis.call("SREM", KEYS[1], ARGV[2])
end
return 0`

	RedisCleanupCorruptScript = `
local current = redis.call("HGET", KEYS[2], ARGV[1])
if current == ARGV[3] then
  redis.call("DEL", KEYS[2])
  return redis.call("SREM", KEYS[1], ARGV[2])
end
return 0`

	RedisDeleteSecondaryIndexIfPrimaryScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`
)

var ErrCorruptRecord = errors.New("corrupt read model record")

type RedisStoreOptions[T any, Q any] struct {
	Model              ModelOptions[T, Q]
	Executor           db.RedisExecutor
	KeyPrefix          string
	HashField          string
	PrimarySetName     string
	SecondaryIndexName string
	TTL                time.Duration
	// ScanCount 控制 Search 无索引兜底扫描时每次 SSCAN 建议返回的主键数。
	ScanCount int
	Encode    func(T) ([]byte, error)
	Decode    func(string, []byte) (T, error)
}

type RedisStore[T any, Q any] struct {
	options   ModelOptions[T, Q]
	executor  db.RedisExecutor
	keyspace  redisKeyspace
	ttl       time.Duration
	scanCount int
	encode    func(T) ([]byte, error)
	decode    func(string, []byte) (T, error)
}

type redisKeyspace struct {
	prefix             string
	hashField          string
	primarySetName     string
	secondaryIndexName string
}

type cleanupCandidate struct {
	primaryID string
	data      []byte
}

func NewRedisStore[T any, Q any](options RedisStoreOptions[T, Q]) (*RedisStore[T, Q], error) {
	if err := options.Model.validate(); err != nil {
		return nil, err
	}
	if options.Executor == nil {
		return nil, errors.New("redis executor is nil")
	}
	keyspace := redisKeyspace{
		prefix:             strings.TrimSpace(options.KeyPrefix),
		hashField:          strings.TrimSpace(options.HashField),
		primarySetName:     strings.TrimSpace(options.PrimarySetName),
		secondaryIndexName: strings.TrimSpace(options.SecondaryIndexName),
	}
	if keyspace.prefix == "" {
		keyspace.prefix = "readmodel"
	}
	if keyspace.hashField == "" {
		keyspace.hashField = "summary"
	}
	if keyspace.primarySetName == "" {
		keyspace.primarySetName = "ids"
	}
	if keyspace.secondaryIndexName == "" {
		keyspace.secondaryIndexName = "secondary"
	}
	scanCount := options.ScanCount
	if scanCount <= 0 {
		scanCount = redisScanCount
	}
	encode := options.Encode
	if encode == nil {
		encode = func(record T) ([]byte, error) {
			return json.Marshal(record)
		}
	}
	decode := options.Decode
	if decode == nil {
		decode = func(_ string, data []byte) (T, error) {
			var record T
			if err := json.Unmarshal(data, &record); err != nil {
				return record, fmt.Errorf("%w: %w", ErrCorruptRecord, err)
			}
			return record, nil
		}
	}
	return &RedisStore[T, Q]{
		options:   options.Model,
		executor:  options.Executor,
		keyspace:  keyspace,
		ttl:       options.TTL,
		scanCount: scanCount,
		encode:    encode,
		decode:    decode,
	}, nil
}

func (s *RedisStore[T, Q]) SetExecutor(executor db.RedisExecutor) {
	s.executor = executor
}

func (s *RedisStore[T, Q]) Save(ctx context.Context, record T) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	record = s.options.normalize(record)
	primaryID := s.options.primaryID(record)
	if primaryID == "" {
		return nil
	}
	old, err := s.recordForSave(ctx, primaryID)
	if err != nil {
		return err
	}
	data, err := s.encode(record)
	if err != nil {
		return err
	}
	secondaryID := s.options.secondaryID(record)
	oldSecondaryID := s.options.secondaryID(old)

	commands := []db.RedisCommand{
		{Name: "HSET", Args: []any{s.SummaryKey(primaryID), s.keyspace.hashField, data}},
		{Name: "SADD", Args: []any{s.PrimaryIDsKey(), primaryID}},
	}
	if secondaryID != "" {
		commands = append(commands, db.RedisCommand{Name: "SET", Args: []any{s.SecondaryKey(secondaryID), primaryID}})
	}
	if oldSecondaryID != "" && oldSecondaryID != secondaryID {
		commands = append(commands, db.RedisCommand{Name: "EVAL", Args: []any{RedisDeleteSecondaryIndexIfPrimaryScript, 1, s.SecondaryKey(oldSecondaryID), primaryID}})
	}
	if ttlSeconds := int(s.ttl.Seconds()); ttlSeconds > 0 {
		commands = append(commands, db.RedisCommand{Name: "EXPIRE", Args: []any{s.SummaryKey(primaryID), ttlSeconds}})
		commands = append(commands, db.RedisCommand{Name: "EXPIRE", Args: []any{s.PrimaryIDsKey(), ttlSeconds}})
		if secondaryID != "" {
			commands = append(commands, db.RedisCommand{Name: "EXPIRE", Args: []any{s.SecondaryKey(secondaryID), ttlSeconds}})
		}
	}
	return db.DoRedisBatch(ctx, s.executor, commands)
}

func (s *RedisStore[T, Q]) Get(ctx context.Context, primaryID string) (T, bool, error) {
	ctx = contextOrBackground(ctx)
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	primaryID = strings.TrimSpace(primaryID)
	if primaryID == "" {
		return zero, false, nil
	}
	data, ok, err := s.recordData(ctx, primaryID)
	if err != nil || !ok {
		return zero, ok, err
	}
	record, err := s.decodeRecord(primaryID, data)
	if err != nil {
		return zero, false, err
	}
	return record, true, nil
}

func (s *RedisStore[T, Q]) GetBySecondaryID(ctx context.Context, secondaryID string) (T, bool, error) {
	ctx = contextOrBackground(ctx)
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	secondaryID = strings.TrimSpace(secondaryID)
	if secondaryID == "" {
		return zero, false, nil
	}
	reply, err := s.executor.Do(ctx, "GET", s.SecondaryKey(secondaryID))
	primaryID, err := redis.String(reply, err)
	if errors.Is(err, redis.ErrNil) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	record, ok, err := s.Get(ctx, primaryID)
	if err != nil {
		return zero, false, err
	}
	if !ok || s.options.secondaryID(record) != secondaryID {
		s.deleteStaleSecondary(ctx, secondaryID, primaryID)
		return zero, false, nil
	}
	return record, true, nil
}

func (s *RedisStore[T, Q]) ListByPrimaryIDs(ctx context.Context, primaryIDs []string) ([]T, error) {
	ctx = contextOrBackground(ctx)
	return s.listByPrimaryIDs(ctx, primaryIDs, false)
}

func (s *RedisStore[T, Q]) listByPrimaryIDs(ctx context.Context, primaryIDs []string, cleanupMissing bool) ([]T, error) {
	ctx = contextOrBackground(ctx)
	primaryIDs = normalizeIDs(primaryIDs)
	out := make([]T, 0, len(primaryIDs))
	cleanup := make([]cleanupCandidate, 0)
	for _, primaryID := range primaryIDs {
		data, ok, err := s.recordData(ctx, primaryID)
		if err != nil {
			return nil, err
		}
		if !ok {
			if cleanupMissing {
				cleanup = append(cleanup, cleanupCandidate{primaryID: primaryID})
			}
			continue
		}
		record, err := s.decodeRecord(primaryID, data)
		if err != nil {
			if cleanupMissing && errors.Is(err, ErrCorruptRecord) {
				cleanup = append(cleanup, cleanupCandidate{primaryID: primaryID, data: append([]byte(nil), data...)})
				continue
			}
			return nil, err
		}
		out = append(out, record)
	}
	if len(cleanup) > 0 {
		s.removePrimaryIDsBest(ctx, cleanup)
	}
	s.options.sort(out)
	return out, nil
}

func (s *RedisStore[T, Q]) ListBySecondaryIDs(ctx context.Context, secondaryIDs []string) ([]T, error) {
	ctx = contextOrBackground(ctx)
	secondaryIDs = normalizeIDs(secondaryIDs)
	out := make([]T, 0, len(secondaryIDs))
	for _, secondaryID := range secondaryIDs {
		record, ok, err := s.GetBySecondaryID(ctx, secondaryID)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, record)
		}
	}
	s.options.sort(out)
	return out, nil
}

func (s *RedisStore[T, Q]) Search(ctx context.Context, query Q) ([]T, error) {
	ctx = contextOrBackground(ctx)
	if ids := s.options.queryPrimaryIDs(query); len(ids) > 0 {
		records, err := s.ListByPrimaryIDs(ctx, ids)
		return s.options.filter(records, query), err
	}
	if ids := s.options.querySecondaryIDs(query); len(ids) > 0 {
		records, err := s.ListBySecondaryIDs(ctx, ids)
		return s.options.filter(records, query), err
	}
	return s.scanSearch(ctx, query)
}

func (s *RedisStore[T, Q]) scanSearch(ctx context.Context, query Q) ([]T, error) {
	limit := s.options.queryLimit(query)
	stopEarly := limit > 0 && s.options.Less == nil
	out := make([]T, 0)
	cursor := "0"
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		reply, err := s.executor.Do(ctx, "SSCAN", s.PrimaryIDsKey(), cursor, "COUNT", s.scanCount)
		next, primaryIDs, err := parseSSCAN(reply, err)
		if errors.Is(err, redis.ErrNil) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		records, err := s.listByPrimaryIDs(ctx, primaryIDs, true)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			candidate := s.options.clone(record)
			if !s.options.match(candidate, query) {
				continue
			}
			out = append(out, candidate)
			if stopEarly && len(out) >= limit {
				return s.finishSearch(out, query), nil
			}
		}
		if next == "0" {
			break
		}
		cursor = next
	}
	return s.finishSearch(out, query), nil
}

func (s *RedisStore[T, Q]) finishSearch(records []T, query Q) []T {
	s.options.sort(records)
	if limit := s.options.queryLimit(query); limit > 0 && len(records) > limit {
		return records[:limit]
	}
	return records
}

func parseSSCAN(reply any, err error) (string, []string, error) {
	values, err := redis.Values(reply, err)
	if errors.Is(err, redis.ErrNil) {
		return "", nil, err
	}
	if err != nil {
		return "", nil, err
	}
	if len(values) != 2 {
		return "", nil, fmt.Errorf("unexpected redis SSCAN response with %d parts", len(values))
	}
	cursor, err := redis.String(values[0], nil)
	if err != nil {
		return "", nil, err
	}
	primaryIDs, err := redis.Strings(values[1], nil)
	if err != nil {
		return "", nil, err
	}
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		cursor = "0"
	}
	return cursor, normalizeIDs(primaryIDs), nil
}

func (s *RedisStore[T, Q]) Check(ctx context.Context) error {
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

func (s *RedisStore[T, Q]) Close(context.Context) error {
	if s == nil || s.executor == nil {
		return nil
	}
	if closer, ok := s.executor.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func (s *RedisStore[T, Q]) SummaryKey(primaryID string) string {
	return s.keyspace.prefix + ":summary:" + encodeRedisKeyPart(strings.TrimSpace(primaryID))
}

func (s *RedisStore[T, Q]) SecondaryKey(secondaryID string) string {
	return s.keyspace.prefix + ":secondary:" + encodeRedisKeyPart(s.keyspace.secondaryIndexName) + ":" + encodeRedisKeyPart(strings.TrimSpace(secondaryID))
}

func (s *RedisStore[T, Q]) PrimaryIDsKey() string {
	return s.keyspace.prefix + ":" + s.keyspace.primarySetName
}

func encodeRedisKeyPart(value string) string {
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func (s *RedisStore[T, Q]) recordForSave(ctx context.Context, primaryID string) (T, error) {
	var zero T
	data, ok, err := s.recordData(ctx, primaryID)
	if err != nil || !ok {
		return zero, err
	}
	record, err := s.decodeRecord(primaryID, data)
	if err != nil {
		return zero, err
	}
	return record, nil
}

func (s *RedisStore[T, Q]) recordData(ctx context.Context, primaryID string) ([]byte, bool, error) {
	reply, err := s.executor.Do(ctx, "HGET", s.SummaryKey(primaryID), s.keyspace.hashField)
	data, err := redis.Bytes(reply, err)
	if errors.Is(err, redis.ErrNil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (s *RedisStore[T, Q]) decodeRecord(primaryID string, data []byte) (T, error) {
	var zero T
	record, err := s.decode(primaryID, data)
	if err != nil {
		return zero, err
	}
	record = s.options.normalize(record)
	return s.options.clone(record), nil
}

func (s *RedisStore[T, Q]) deleteStaleSecondary(ctx context.Context, secondaryID, primaryID string) {
	secondaryID = strings.TrimSpace(secondaryID)
	primaryID = strings.TrimSpace(primaryID)
	if secondaryID == "" || primaryID == "" {
		return
	}
	_, _ = s.executor.Do(ctx, "EVAL", RedisDeleteSecondaryIndexIfPrimaryScript, 1, s.SecondaryKey(secondaryID), primaryID)
}

func (s *RedisStore[T, Q]) removePrimaryIDsBest(ctx context.Context, candidates []cleanupCandidate) {
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		primaryID := strings.TrimSpace(candidate.primaryID)
		if primaryID == "" {
			continue
		}
		if _, ok := seen[primaryID]; ok {
			continue
		}
		seen[primaryID] = struct{}{}
		if len(candidate.data) == 0 {
			_, _ = s.executor.Do(ctx, "EVAL", RedisCleanupMissingScript, 2, s.PrimaryIDsKey(), s.SummaryKey(primaryID), s.keyspace.hashField, primaryID)
			continue
		}
		_, _ = s.executor.Do(ctx, "EVAL", RedisCleanupCorruptScript, 2, s.PrimaryIDsKey(), s.SummaryKey(primaryID), s.keyspace.hashField, primaryID, candidate.data)
	}
}
