package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RedisHashBackend 用 Redis hash 实现 HashBackend。
type RedisHashBackend struct {
	Executor RedisExecutor
}

// NewRedisHashBackend 创建 Redis hash 后端。
func NewRedisHashBackend(executor RedisExecutor) RedisHashBackend {
	return RedisHashBackend{Executor: executor}
}

// LoadHash 从 Redis hash 读取字段。
func (b RedisHashBackend) LoadHash(ctx context.Context, key string, fields []string) (map[string][]byte, bool, error) {
	if b.Executor == nil {
		return nil, false, errors.New("redis executor is nil")
	}
	ctx = contextOrBackground(ctx)
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, ErrRecordKeyRequired
	}
	fields = normHashFields(fields)
	if len(fields) == 0 {
		reply, err := b.Executor.Do(ctx, "HGETALL", key)
		if err != nil {
			return nil, false, err
		}
		return redisHGetAllFields(reply)
	}
	args := make([]any, 0, 1+len(fields))
	args = append(args, key)
	for _, field := range fields {
		args = append(args, field)
	}
	reply, err := b.Executor.Do(ctx, "HMGET", args...)
	if err != nil {
		return nil, false, err
	}
	values, err := redisArray(reply)
	if err != nil {
		return nil, false, err
	}
	if len(values) != len(fields) {
		return nil, false, fmt.Errorf("redis HMGET returned %d values for %d fields", len(values), len(fields))
	}
	out := make(map[string][]byte, len(fields))
	for i, value := range values {
		data, ok, err := redisBulkBytes(value)
		if err != nil {
			return nil, false, err
		}
		if ok {
			out[fields[i]] = data
		}
	}
	return out, len(out) > 0, nil
}

// SaveHash 保存 Redis hash 字段。
func (b RedisHashBackend) SaveHash(ctx context.Context, key string, fields map[string][]byte, ttl time.Duration) error {
	return SaveRedisHashFields(ctx, b.Executor, key, fields, ttl)
}

// SaveHashBatch 批量保存 Redis hash 字段。
func (b RedisHashBackend) SaveHashBatch(ctx context.Context, batches []HashSaveBatch) error {
	merged, err := mergeHashSaveBatches(batches)
	if err != nil {
		return err
	}
	redisBatches := make([]RedisHashBatch, 0, len(merged))
	for _, batch := range merged {
		redisBatches = append(redisBatches, RedisHashBatch(batch))
	}
	return SaveRedisHashFieldBatches(ctx, b.Executor, redisBatches)
}

// Check 通过 PING 检查 Redis 连接。
func (b RedisHashBackend) Check(ctx context.Context) error {
	if b.Executor == nil {
		return errors.New("redis executor is nil")
	}
	ctx = contextOrBackground(ctx)
	_, err := b.Executor.Do(ctx, "PING")
	return err
}

func redisHGetAllFields(reply any) (map[string][]byte, bool, error) {
	values, err := redisArray(reply)
	if err != nil {
		return nil, false, err
	}
	if len(values)%2 != 0 {
		return nil, false, fmt.Errorf("redis HGETALL returned odd value count %d", len(values))
	}
	out := make(map[string][]byte, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		name, ok, err := redisBulkString(values[i])
		if err != nil {
			return nil, false, err
		}
		if !ok {
			continue
		}
		data, ok, err := redisBulkBytes(values[i+1])
		if err != nil {
			return nil, false, err
		}
		if ok {
			out[name] = data
		}
	}
	return out, len(out) > 0, nil
}

func redisArray(reply any) ([]any, error) {
	switch value := reply.(type) {
	case nil:
		return nil, nil
	case []any:
		return value, nil
	case [][]byte:
		out := make([]any, len(value))
		for i, item := range value {
			if item == nil {
				out[i] = nil
			} else {
				out[i] = append([]byte(nil), item...)
			}
		}
		return out, nil
	case []string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unexpected redis array reply %T", reply)
	}
}

func redisBulkString(value any) (string, bool, error) {
	data, ok, err := redisBulkBytes(value)
	if err != nil || !ok {
		return "", ok, err
	}
	return string(data), true, nil
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
		return nil, false, fmt.Errorf("unexpected redis bulk reply %T", value)
	}
}
