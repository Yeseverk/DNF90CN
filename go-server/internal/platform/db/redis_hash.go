package db

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// RedisExecutor 是最小 Redis 命令执行接口。
type RedisExecutor interface {
	Do(context.Context, string, ...any) (any, error)
}

// RedisBatchExecutor 支持批量发送 Redis 命令。
type RedisBatchExecutor interface {
	DoBatch(context.Context, []RedisCommand) error
}

// RedisCommand 描述一条 Redis 命令。
type RedisCommand struct {
	Name string
	Args []any
}

// RedisHashBatch 描述一次 Redis hash 字段保存项。
type RedisHashBatch struct {
	Key    string
	Fields map[string][]byte
	TTL    time.Duration
}

// SaveRedisHashFields 保存单个 Redis hash 的字段。
func SaveRedisHashFields(ctx context.Context, executor RedisExecutor, key string, fields map[string][]byte, ttl time.Duration) error {
	return SaveRedisHashFieldBatches(ctx, executor, []RedisHashBatch{{
		Key:    key,
		Fields: fields,
		TTL:    ttl,
	}})
}

// SaveRedisHashFieldBatches 批量保存 Redis hash 字段。
func SaveRedisHashFieldBatches(ctx context.Context, executor RedisExecutor, batches []RedisHashBatch) error {
	if executor == nil {
		return errors.New("redis executor is nil")
	}
	commands, err := redisHashCmds(batches)
	if err != nil {
		return err
	}
	if len(commands) == 0 {
		return nil
	}
	return DoRedisBatch(ctx, executor, commands)
}

// DoRedisBatch 执行一组 Redis 命令，优先使用批量执行器。
func DoRedisBatch(ctx context.Context, executor RedisExecutor, commands []RedisCommand) error {
	if executor == nil {
		return errors.New("redis executor is nil")
	}
	if len(commands) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if batcher, ok := executor.(RedisBatchExecutor); ok {
		return batcher.DoBatch(ctx, cloneRedisCommands(commands))
	}
	for _, cmd := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := executor.Do(ctx, cmd.Name, cmd.Args...); err != nil {
			return err
		}
	}
	return nil
}

func redisHashCmds(batches []RedisHashBatch) ([]RedisCommand, error) {
	if len(batches) == 0 {
		return nil, nil
	}
	merged, err := mergeRedisBatches(batches)
	if err != nil {
		return nil, err
	}
	commands := make([]RedisCommand, 0, len(merged)*2)
	for _, batch := range merged {
		key := strings.TrimSpace(batch.Key)
		values := batch.Fields
		names := make([]string, 0, len(values))
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		hsetArgs := make([]any, 0, 1+len(names)*2)
		hsetArgs = append(hsetArgs, key)
		for _, name := range names {
			hsetArgs = append(hsetArgs, name, append([]byte(nil), values[name]...))
		}
		commands = append(commands, RedisCommand{Name: "HSET", Args: hsetArgs})
		ttlSeconds := int(batch.TTL.Seconds())
		if len(names) > 0 && ttlSeconds > 0 {
			commands = append(commands, RedisCommand{
				Name: "EXPIRE",
				Args: []any{key, ttlSeconds},
			})
		}
	}
	return commands, nil
}

func mergeRedisBatches(batches []RedisHashBatch) ([]RedisHashBatch, error) {
	converted := make([]HashSaveBatch, 0, len(batches))
	for _, batch := range batches {
		converted = append(converted, HashSaveBatch(batch))
	}
	merged, err := mergeHashSaveBatches(converted)
	if err != nil {
		return nil, err
	}
	out := make([]RedisHashBatch, 0, len(merged))
	for _, batch := range merged {
		out = append(out, RedisHashBatch(batch))
	}
	return out, nil
}

func cloneRedisCommands(commands []RedisCommand) []RedisCommand {
	if len(commands) == 0 {
		return nil
	}
	out := make([]RedisCommand, len(commands))
	for i, command := range commands {
		args := make([]any, len(command.Args))
		for j, arg := range command.Args {
			args[j] = cloneRedisCommandArg(arg)
		}
		out[i] = RedisCommand{
			Name: command.Name,
			Args: args,
		}
	}
	return out
}

func cloneRedisCommandArg(arg any) any {
	if data, ok := arg.([]byte); ok {
		return append([]byte(nil), data...)
	}
	return arg
}
