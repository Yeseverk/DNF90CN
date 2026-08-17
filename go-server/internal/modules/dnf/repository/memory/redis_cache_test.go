package memory

import (
	"context"
	"encoding/json"
	"longheng.io/server/internal/modules/dnf/repository"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gomodule/redigo/redis"
	"longheng.io/server/internal/platform/db"
)

func TestRedisCachedCharacterRepositoryCachesListAndName(t *testing.T) {
	ctx := context.Background()
	backing := newMemoryCharStore()
	executor := newFakeRedisExecutor()
	repo := repository.NewRedisCachedCharacterRepository(backing, executor, repository.RedisCacheOptions{KeyPrefix: "test:dnf"})
	record := repository.CharacterRecord{
		CharacterID: "1",
		AccountID:   "dnf:1",
		Slot:        2,
		Name:        "hero",
		Job:         "15",
		Level:       48,
		Stats:       map[string]int64{"grow_type": 1},
	}
	if err := repo.Save(ctx, record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cached := repo.(*repository.RedisCachedCharStore)
	if got, err := redis.String(executor.Do(ctx, "GET", cached.NameKey("hero"))); err != nil || got != "1" {
		t.Fatalf("name cache = %q err=%v, want 1", got, err)
	}
	if err := backing.Save(ctx, repository.CharacterRecord{CharacterID: "1", AccountID: "dnf:1", Slot: 2, Name: "mutated"}); err != nil {
		t.Fatalf("mutate backing: %v", err)
	}
	loaded, ok, err := repo.Load(ctx, "1")
	if err != nil || !ok {
		t.Fatalf("Load() ok=%v err=%v", ok, err)
	}
	if loaded.Name != "mutated" {
		t.Fatalf("Load() did not use MySQL-authoritative record: %+v", loaded)
	}
	list, err := repo.ListByAccount(ctx, "dnf:1", 8)
	if err != nil {
		t.Fatalf("ListByAccount() error = %v", err)
	}
	if len(list) != 1 || list[0].CharacterID != "1" || list[0].Name != "mutated" {
		t.Fatalf("validated list = %+v, want MySQL-authoritative mutated record", list)
	}
}

func TestRedisCachedCharacterRepositoryRepairsIncompleteAccountList(t *testing.T) {
	ctx := context.Background()
	backing := newMemoryCharStore()
	executor := newFakeRedisExecutor()
	repo := repository.NewRedisCachedCharacterRepository(backing, executor, repository.RedisCacheOptions{KeyPrefix: "test:dnf"})
	first := repository.CharacterRecord{
		CharacterID: "1",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "first",
		Job:         "15",
		Level:       1,
	}
	if err := repo.Save(ctx, first); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	if err := backing.Save(ctx, repository.CharacterRecord{
		CharacterID: "2",
		AccountID:   "dnf:1",
		Slot:        1,
		Name:        "second",
		Job:         "11",
		Level:       1,
	}); err != nil {
		t.Fatalf("seed backing second: %v", err)
	}

	list, err := repo.ListByAccount(ctx, "dnf:1", 8)
	if err != nil {
		t.Fatalf("ListByAccount() error = %v", err)
	}
	if len(list) != 2 || list[0].CharacterID != "1" || list[1].CharacterID != "2" {
		t.Fatalf("repaired list = %+v, want both MySQL-backed records", list)
	}

	cached := repo.(*repository.RedisCachedCharStore)
	if ids, err := redis.Strings(executor.Do(ctx, "ZRANGE", cached.AccountKey("dnf:1"), 0, 7)); err != nil || strings.Join(ids, ",") != "1,2" {
		t.Fatalf("repaired redis index ids=%v err=%v, want 1,2", ids, err)
	}
}

func TestRedisCachedCharacterRepositoryRepairsStaleExtraAccountList(t *testing.T) {
	ctx := context.Background()
	backing := newMemoryCharStore()
	executor := newFakeRedisExecutor()
	repo := repository.NewRedisCachedCharacterRepository(backing, executor, repository.RedisCacheOptions{KeyPrefix: "test:dnf"})
	first := repository.CharacterRecord{
		CharacterID: "1",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "first",
		Job:         "15",
		Level:       1,
	}
	if err := repo.Save(ctx, first); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	cached := repo.(*repository.RedisCachedCharStore)
	if err := cached.CacheRecord(ctx, repository.CharacterRecord{
		CharacterID: "2",
		AccountID:   "dnf:1",
		Slot:        1,
		Name:        "stale",
		Job:         "11",
		Level:       1,
	}); err != nil {
		t.Fatalf("seed stale redis record: %v", err)
	}

	list, err := repo.ListByAccount(ctx, "dnf:1", 8)
	if err != nil {
		t.Fatalf("ListByAccount() error = %v", err)
	}
	if len(list) != 1 || list[0].CharacterID != "1" {
		t.Fatalf("repaired list = %+v, want only MySQL-backed record 1", list)
	}
	if ids, err := redis.Strings(executor.Do(ctx, "ZRANGE", cached.AccountKey("dnf:1"), 0, 7)); err != nil || strings.Join(ids, ",") != "1" {
		t.Fatalf("repaired redis index ids=%v err=%v, want 1", ids, err)
	}
}

func TestRedisCachedCharacterSaveFieldsBackfillsCache(t *testing.T) {
	ctx := context.Background()
	backing := newMemoryCharStore()
	executor := newFakeRedisExecutor()
	repo := repository.NewRedisCachedCharacterRepository(backing, executor, repository.RedisCacheOptions{KeyPrefix: "test:dnf"})
	record := repository.CharacterRecord{
		CharacterID: "7",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "field",
		Stats:       map[string]int64{"str": 10},
	}
	if err := repo.Save(ctx, record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	record.Stats["str"] = 99
	if err := repository.SaveCharacterFields(ctx, repo, record, repository.CharacterFieldStats); err != nil {
		t.Fatalf("SaveCharacterFields() error = %v", err)
	}
	loaded, ok, err := repo.Load(ctx, "7")
	if err != nil || !ok {
		t.Fatalf("Load() ok=%v err=%v", ok, err)
	}
	if loaded.Stats["str"] != 99 {
		t.Fatalf("cached stats = %+v, want str=99", loaded.Stats)
	}
}

func TestRedisCachedCharacterNextNumericIDSeedsFromMySQL(t *testing.T) {
	ctx := context.Background()
	backing := newMemoryCharStore()
	if err := backing.Save(ctx, repository.CharacterRecord{CharacterID: "41", AccountID: "dnf:1", Name: "old"}); err != nil {
		t.Fatalf("seed backing: %v", err)
	}
	repo := repository.NewRedisCachedCharacterRepository(backing, newFakeRedisExecutor(), repository.RedisCacheOptions{KeyPrefix: "test:dnf"})
	first, err := repo.NextNumericID(ctx)
	if err != nil {
		t.Fatalf("NextNumericID() error = %v", err)
	}
	second, err := repo.NextNumericID(ctx)
	if err != nil {
		t.Fatalf("NextNumericID() second error = %v", err)
	}
	if first != 42 || second != 43 {
		t.Fatalf("ids = %d,%d want 42,43", first, second)
	}
}

type fakeRedisExecutor struct {
	values map[string]any
	zsets  map[string]map[string]float64
}

func newFakeRedisExecutor() *fakeRedisExecutor {
	return &fakeRedisExecutor{
		values: make(map[string]any),
		zsets:  make(map[string]map[string]float64),
	}
}

func (e *fakeRedisExecutor) Do(_ context.Context, command string, args ...any) (any, error) {
	switch strings.ToUpper(command) {
	case "PING":
		return "PONG", nil
	case "GET":
		value, ok := e.values[argString(args, 0)]
		if !ok {
			return nil, redis.ErrNil
		}
		return value, nil
	case "SET":
		e.values[argString(args, 0)] = cloneRedisValue(args[1])
		return "OK", nil
	case "SETEX":
		e.values[argString(args, 0)] = cloneRedisValue(args[2])
		return "OK", nil
	case "DEL":
		for _, arg := range args {
			key := toString(arg)
			delete(e.values, key)
			delete(e.zsets, key)
		}
		return len(args), nil
	case "ZADD":
		key := argString(args, 0)
		score, _ := strconv.ParseFloat(toString(args[1]), 64)
		member := toString(args[2])
		if e.zsets[key] == nil {
			e.zsets[key] = make(map[string]float64)
		}
		e.zsets[key][member] = score
		return 1, nil
	case "ZRANGE":
		key := argString(args, 0)
		start, _ := strconv.Atoi(toString(args[1]))
		stop, _ := strconv.Atoi(toString(args[2]))
		return e.zrange(key, start, stop), nil
	case "EXPIRE":
		return 1, nil
	case "EVAL":
		key := argString(args, 2)
		seed, _ := strconv.Atoi(toString(args[3]))
		current, _ := strconv.Atoi(toString(e.values[key]))
		if current < seed {
			current = seed
		}
		current++
		e.values[key] = strconv.Itoa(current)
		return int64(current), nil
	default:
		return nil, redis.Error("unsupported command " + command)
	}
}

func (e *fakeRedisExecutor) DoBatch(ctx context.Context, commands []db.RedisCommand) error {
	for _, command := range commands {
		if _, err := e.Do(ctx, command.Name, command.Args...); err != nil {
			return err
		}
	}
	return nil
}

func (e *fakeRedisExecutor) zrange(key string, start, stop int) []any {
	type entry struct {
		member string
		score  float64
	}
	entries := make([]entry, 0, len(e.zsets[key]))
	for member, score := range e.zsets[key] {
		entries = append(entries, entry{member: member, score: score})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].score == entries[j].score {
			return entries[i].member < entries[j].member
		}
		return entries[i].score < entries[j].score
	})
	if start < 0 {
		start = 0
	}
	if stop >= len(entries) {
		stop = len(entries) - 1
	}
	if start > stop || start >= len(entries) {
		return nil
	}
	out := make([]any, 0, stop-start+1)
	for _, item := range entries[start : stop+1] {
		out = append(out, []byte(item.member))
	}
	return out
}

func argString(args []any, idx int) string {
	if idx >= len(args) {
		return ""
	}
	return toString(args[idx])
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case nil:
		return ""
	default:
		data, _ := json.Marshal(typed)
		return string(data)
	}
}

func cloneRedisValue(value any) any {
	if data, ok := value.([]byte); ok {
		return append([]byte(nil), data...)
	}
	return value
}
