package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrRecordKeyRequired 表示记录缺少可用主键。
	ErrRecordKeyRequired = errors.New("record key is required")
	// ErrAsyncStoreClosed 表示异步存储已关闭。
	ErrAsyncStoreClosed = errors.New("async store is closed")
	// ErrAllFieldsRequired 表示调用方需要提供完整字段集合。
	ErrAllFieldsRequired = errors.New("all fields function is required")
)

// KeyFunc 从记录中提取稳定主键。
type KeyFunc[T any] func(T) string

// CloneFunc 拷贝记录，避免缓存和调用方共享可变对象。
type CloneFunc[T any] func(T) T

// Store 是最小记录存取接口。
type Store[T any] interface {
	Load(context.Context, string) (T, bool, error)
	Save(context.Context, T) error
}

// Checker 是存储预检接口。
type Checker interface {
	Check(context.Context) error
}

// FieldStore 支持按字段局部保存记录。
type FieldStore[T any, F comparable] interface {
	Store[T]
	SaveFields(context.Context, T, ...F) error
}

// FieldSave 表示一次局部字段保存请求。
type FieldSave[T any, F comparable] struct {
	Record T
	Fields []F
}

// BatchFieldStore 支持批量局部字段保存。
type BatchFieldStore[T any, F comparable] interface {
	SaveFieldBatch(context.Context, []FieldSave[T, F]) error
}

// SaveFieldsFunc 是可注入的局部保存函数。
type SaveFieldsFunc[T any, F comparable] func(context.Context, Store[T], T, ...F) error

// SaveFieldBatchFunc 是可注入的批量局部保存函数。
type SaveFieldBatchFunc[T any, F comparable] func(context.Context, Store[T], []FieldSave[T, F]) error

// Expirer 是支持设置记录过期时间的存储接口。
type Expirer interface {
	Expire(context.Context, string, time.Duration) error
}

// ExpireFunc 是可注入的过期设置函数。
type ExpireFunc[T any] func(context.Context, Store[T], string, time.Duration) error

// IdentityClone 原样返回记录，适用于不可变值或调用方自行拷贝的场景。
func IdentityClone[T any](record T) T {
	return record
}

// RecordKey 提取并校验记录主键。
func RecordKey[T any](keyFn KeyFunc[T], record T) (string, error) {
	if keyFn == nil {
		return "", fmt.Errorf("%w: key function is nil", ErrRecordKeyRequired)
	}
	key := strings.TrimSpace(keyFn(record))
	if key == "" {
		return "", ErrRecordKeyRequired
	}
	return key, nil
}

// SaveFields 按字段保存；底层不支持字段保存时退化为整条保存。
func SaveFields[T any, F comparable](ctx context.Context, store Store[T], record T, normalize func([]F) []F, fields ...F) error {
	if store == nil {
		return errors.New("store is nil")
	}
	ctx = contextOrBackground(ctx)
	if normalize != nil {
		fields = normalize(fields)
	}
	if len(fields) == 0 {
		return nil
	}
	if fieldStore, ok := store.(FieldStore[T, F]); ok {
		return fieldStore.SaveFields(ctx, record, fields...)
	}
	return store.Save(ctx, record)
}

// SaveFieldBatch 批量保存字段；底层不支持批量时逐条退化。
func SaveFieldBatch[T any, F comparable](ctx context.Context, store Store[T], normalize func([]F) []F, saves []FieldSave[T, F]) error {
	if store == nil {
		return errors.New("store is nil")
	}
	ctx = contextOrBackground(ctx)
	batch := normalizeFieldSaves(normalize, saves)
	if len(batch) == 0 {
		return nil
	}
	if batchStore, ok := store.(BatchFieldStore[T, F]); ok {
		return batchStore.SaveFieldBatch(ctx, batch)
	}
	for _, save := range batch {
		if err := SaveFields(ctx, store, save.Record, normalize, save.Fields...); err != nil {
			return err
		}
	}
	return nil
}

// Expire 在存储支持过期能力时设置记录 TTL。
func Expire(ctx context.Context, store any, key string, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	expirer, ok := store.(Expirer)
	if !ok {
		return nil
	}
	return expirer.Expire(ctx, key, ttl)
}

func normalizeFieldSaves[T any, F comparable](normalize func([]F) []F, saves []FieldSave[T, F]) []FieldSave[T, F] {
	if len(saves) == 0 {
		return nil
	}
	out := make([]FieldSave[T, F], 0, len(saves))
	for _, save := range saves {
		fields := append([]F(nil), save.Fields...)
		if normalize != nil {
			fields = normalize(fields)
		}
		if len(fields) == 0 {
			continue
		}
		out = append(out, FieldSave[T, F]{
			Record: save.Record,
			Fields: fields,
		})
	}
	return out
}

// CloseOrFlush 优先关闭存储，否则刷新待写队列。
func CloseOrFlush(ctx context.Context, store any) error {
	if closer, ok := store.(interface {
		Close(context.Context) error
	}); ok {
		return closer.Close(ctx)
	}
	if flusher, ok := store.(interface {
		Flush(context.Context) error
	}); ok {
		return flusher.Flush(ctx)
	}
	return nil
}

// Check 对实现 Checker 的对象执行预检。
func Check(ctx context.Context, target any) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if checker, ok := target.(Checker); ok {
		return checker.Check(ctx)
	}
	return nil
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
