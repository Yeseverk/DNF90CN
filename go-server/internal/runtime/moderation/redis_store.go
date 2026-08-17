package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

var ErrNilRedisStoreExecutor = errors.New("moderation redis executor is required")

const defRedisSanctionPref = "longheng:moderation"

const sanctionUpsertLua = `
local old_subject_key = KEYS[4]
redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])
redis.call("SADD", KEYS[2], ARGV[1])
redis.call("SADD", KEYS[3], ARGV[1])
if old_subject_key ~= "" and old_subject_key ~= KEYS[3] then
  redis.call("SREM", old_subject_key, ARGV[1])
end
return 1
`

const sanctionRmScript = `
local current = redis.call("HGET", KEYS[1], ARGV[1])
if not current then
  return 0
end
if current ~= ARGV[2] then
  return 2
end
redis.call("HDEL", KEYS[1], ARGV[1])
redis.call("SREM", KEYS[2], ARGV[1])
redis.call("SREM", KEYS[3], ARGV[1])
return 1
`

const sanctionRmRetries = 3

type RedisSanctionStoreOptions struct {
	Executor  db.RedisExecutor
	KeyPrefix string
	Now       func() time.Time
}

type RedisSanctionStore struct {
	executor db.RedisExecutor
	prefix   string
	now      func() time.Time
}

func NewRedisSanctionStore(options RedisSanctionStoreOptions) *RedisSanctionStore {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &RedisSanctionStore{
		executor: options.Executor,
		prefix:   normSanctionPref(options.KeyPrefix),
		now:      now,
	}
}

func (s *RedisSanctionStore) Upsert(ctx context.Context, sanction Sanction) (Sanction, error) {
	if err := ctxErr(ctx); err != nil {
		return Sanction{}, err
	}
	executor, prefix, now, err := s.ready()
	if err != nil {
		return Sanction{}, err
	}
	item, err := normalizeSanction(sanction, now().UTC())
	if err != nil {
		return Sanction{}, err
	}
	data, err := json.Marshal(item)
	if err != nil {
		return Sanction{}, err
	}
	itemsKey := sanctionItemsKey(prefix)
	oldSubjectKey := sanctionSubjectKey(prefix, item.Subject)
	if existing, ok, err := redisSanctionByID(ctx, executor, itemsKey, item.ID); err != nil {
		return Sanction{}, err
	} else if ok {
		oldSubjectKey = sanctionSubjectKey(prefix, existing.Subject)
	}
	if _, err := redisDo(ctx, executor, "EVAL",
		sanctionUpsertLua,
		4,
		itemsKey,
		sanctionIndexKey(prefix),
		sanctionSubjectKey(prefix, item.Subject),
		oldSubjectKey,
		item.ID,
		data,
	); err != nil {
		return Sanction{}, err
	}
	return item, nil
}

func (s *RedisSanctionStore) Remove(ctx context.Context, id string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	id = normalizeToken(id)
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidSanction)
	}
	executor, prefix, _, err := s.ready()
	if err != nil {
		return err
	}
	itemsKey := sanctionItemsKey(prefix)
	for attempt := 0; attempt < sanctionRmRetries; attempt++ {
		item, raw, ok, err := redisSanctionRaw(ctx, executor, itemsKey, id)
		if err != nil {
			return err
		}
		if !ok {
			return ErrSanctionMissing
		}
		status, err := redisInt64Value(redisDo(ctx, executor, "EVAL",
			sanctionRmScript,
			3,
			itemsKey,
			sanctionIndexKey(prefix),
			sanctionSubjectKey(prefix, item.Subject),
			id,
			raw,
		))
		if err != nil {
			return err
		}
		switch status {
		case 0:
			return ErrSanctionMissing
		case 1:
			return nil
		case 2:
			continue
		default:
			return fmt.Errorf("unexpected moderation redis remove status %d", status)
		}
	}
	return fmt.Errorf("%w: concurrent remove conflict", ErrInvalidSanction)
}

func (s *RedisSanctionStore) Active(ctx context.Context, query SanctionQuery) ([]Sanction, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	executor, prefix, now, err := s.ready()
	if err != nil {
		return nil, err
	}
	query.Subject = normalizeToken(query.Subject)
	query.Scope = normalizeToken(query.Scope)
	if query.Subject == "" {
		return nil, fmt.Errorf("%w: subject is required", ErrInvalidSanction)
	}
	if query.Now.IsZero() {
		query.Now = now().UTC()
	} else {
		query.Now = query.Now.UTC()
	}
	ids, err := redisStrings(ctx, executor, "SMEMBERS", sanctionSubjectKey(prefix, query.Subject))
	if err != nil {
		return nil, err
	}
	kindSet := sanctionKindSet(query.Kinds)
	out := make([]Sanction, 0, len(ids))
	for _, id := range ids {
		item, ok, err := redisSanctionByID(ctx, executor, sanctionItemsKey(prefix), id)
		if err != nil {
			return nil, err
		}
		if !ok || item.Subject != query.Subject {
			_, _ = redisDo(ctx, executor, "SREM", sanctionSubjectKey(prefix, query.Subject), id)
			continue
		}
		if !sanctionScopeApplies(item.Scope, query.Scope) || !item.ActiveAt(query.Now) {
			continue
		}
		if len(kindSet) > 0 {
			if _, ok := kindSet[item.Kind]; !ok {
				continue
			}
		}
		out = append(out, cloneSanction(item))
	}
	sortSanctions(out)
	return out, nil
}

func (s *RedisSanctionStore) Snapshot(ctx context.Context) (SanctionSnapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return SanctionSnapshot{}, err
	}
	executor, prefix, _, err := s.ready()
	if err != nil {
		return SanctionSnapshot{}, err
	}
	ids, err := redisStrings(ctx, executor, "SMEMBERS", sanctionIndexKey(prefix))
	if err != nil {
		return SanctionSnapshot{}, err
	}
	items := make([]Sanction, 0, len(ids))
	for _, id := range ids {
		item, ok, err := redisSanctionByID(ctx, executor, sanctionItemsKey(prefix), id)
		if err != nil {
			return SanctionSnapshot{}, err
		}
		if !ok {
			_, _ = redisDo(ctx, executor, "SREM", sanctionIndexKey(prefix), id)
			continue
		}
		items = append(items, cloneSanction(item))
	}
	sortSanctions(items)
	return SanctionSnapshot{Items: items}, nil
}

func redisSanctionByID(ctx context.Context, executor db.RedisExecutor, itemsKey, id string) (Sanction, bool, error) {
	item, _, ok, err := redisSanctionRaw(ctx, executor, itemsKey, id)
	return item, ok, err
}

func redisSanctionRaw(ctx context.Context, executor db.RedisExecutor, itemsKey, id string) (Sanction, []byte, bool, error) {
	value, err := redisDo(ctx, executor, "HGET", itemsKey, normalizeToken(id))
	if err != nil {
		return Sanction{}, nil, false, err
	}
	data, ok, err := redisBulkBytes(value)
	if err != nil || !ok {
		return Sanction{}, nil, ok, err
	}
	var item Sanction
	if err := json.Unmarshal(data, &item); err != nil {
		return Sanction{}, nil, false, fmt.Errorf("decode moderation redis sanction: %w", err)
	}
	return cloneSanction(item), data, true, nil
}

func redisStrings(ctx context.Context, executor db.RedisExecutor, command string, args ...any) ([]string, error) {
	value, err := redisDo(ctx, executor, command, args...)
	if err != nil {
		return nil, err
	}
	return redisStringSlice(value)
}

func redisDo(ctx context.Context, executor db.RedisExecutor, command string, args ...any) (any, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if executor == nil {
		return nil, ErrNilRedisStoreExecutor
	}
	return executor.Do(ctx, command, args...)
}

func (s *RedisSanctionStore) ready() (db.RedisExecutor, string, func() time.Time, error) {
	if s == nil || s.executor == nil {
		return nil, "", nil, ErrNilRedisStoreExecutor
	}
	now := s.now
	if now == nil {
		now = time.Now
	}
	return s.executor, normSanctionPref(s.prefix), now, nil
}

func normSanctionPref(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		return defRedisSanctionPref
	}
	return prefix
}

func sanctionItemsKey(prefix string) string {
	return prefix + ":sanctions"
}

func sanctionIndexKey(prefix string) string {
	return prefix + ":sanction_index"
}

func sanctionSubjectKey(prefix, subject string) string {
	return prefix + ":subject:" + normalizeToken(subject)
}

func redisStringSlice(value any) ([]string, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return append([]string(nil), v...), nil
	case [][]byte:
		out := make([]string, len(v))
		for i := range v {
			out[i] = string(v[i])
		}
		sort.Strings(out)
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			switch x := item.(type) {
			case string:
				out = append(out, x)
			case []byte:
				out = append(out, string(x))
			default:
				return nil, fmt.Errorf("unexpected redis string item %T", item)
			}
		}
		sort.Strings(out)
		return out, nil
	default:
		return nil, fmt.Errorf("unexpected redis string slice type %T", value)
	}
}

func redisInt64Value(value any, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case uint64:
		const maxInt64 = uint64(1<<63 - 1)
		if v > maxInt64 {
			return 0, fmt.Errorf("redis integer overflows int64: %d", v)
		}
		return int64(v), nil
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("unexpected redis integer type %T", value)
	}
}

func redisBulkBytes(value any) ([]byte, bool, error) {
	switch v := value.(type) {
	case nil:
		return nil, false, nil
	case []byte:
		return append([]byte(nil), v...), true, nil
	case string:
		return []byte(v), true, nil
	default:
		return nil, false, fmt.Errorf("unexpected redis bulk type %T", value)
	}
}
