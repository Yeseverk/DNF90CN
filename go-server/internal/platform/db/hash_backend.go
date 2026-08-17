package db

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrHashBackendRequired 表示 hash 后端缺失或没有完成初始化。
var ErrHashBackendRequired = errors.New("hash backend is required")

// HashBackend 是按记录主键和字段名读写二进制字段的后端接口。
type HashBackend interface {
	LoadHash(context.Context, string, []string) (map[string][]byte, bool, error)
	SaveHash(context.Context, string, map[string][]byte, time.Duration) error
}

// HashSaveBatch 表示一次 hash 字段批量保存项。
type HashSaveBatch struct {
	Key    string
	Fields map[string][]byte
	TTL    time.Duration
}

// HashBatchBackend 支持批量保存 hash 字段。
type HashBatchBackend interface {
	SaveHashBatch(context.Context, []HashSaveBatch) error
}

// HashLoadRequest 描述一次 hash 批量读取请求。
type HashLoadRequest struct {
	Key    string
	Fields []string
}

// HashLoadResult 描述一次 hash 批量读取结果。
type HashLoadResult struct {
	Fields map[string][]byte
	Found  bool
}

// HashBatchLoaderBackend 支持批量读取 hash 字段。
type HashBatchLoaderBackend interface {
	LoadHashBatch(context.Context, []HashLoadRequest) (map[string]HashLoadResult, error)
}

// MemoryHashBackend 是线程安全的内存 hash 后端。
type MemoryHashBackend struct {
	mu      sync.RWMutex
	records map[string]map[string][]byte
	ttl     map[string]time.Duration
}

// NewMemoryHashBackend 创建内存 hash 后端。
func NewMemoryHashBackend() *MemoryHashBackend {
	return &MemoryHashBackend{
		records: make(map[string]map[string][]byte),
		ttl:     make(map[string]time.Duration),
	}
}

// LoadHash 从内存读取指定 hash 字段。
func (b *MemoryHashBackend) LoadHash(ctx context.Context, key string, fields []string) (map[string][]byte, bool, error) {
	if b == nil {
		return nil, false, ErrHashBackendRequired
	}
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, ErrRecordKeyRequired
	}
	fields = normHashFields(fields)

	b.mu.RLock()
	record, ok := b.records[key]
	if !ok {
		b.mu.RUnlock()
		return nil, false, nil
	}
	out := make(map[string][]byte, len(record))
	if len(fields) == 0 {
		for name, data := range record {
			out[name] = append([]byte(nil), data...)
		}
	} else {
		for _, name := range fields {
			if data, exists := record[name]; exists {
				out[name] = append([]byte(nil), data...)
			}
		}
	}
	b.mu.RUnlock()
	return out, true, nil
}

// LoadHashBatch 批量读取 hash 字段。
func (b *MemoryHashBackend) LoadHashBatch(ctx context.Context, requests []HashLoadRequest) (map[string]HashLoadResult, error) {
	if b == nil {
		return nil, ErrHashBackendRequired
	}
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make(map[string]HashLoadResult, len(requests))
	for _, request := range requests {
		fields, found, err := b.LoadHash(ctx, request.Key, request.Fields)
		if err != nil {
			return nil, err
		}
		out[strings.TrimSpace(request.Key)] = HashLoadResult{Fields: fields, Found: found}
	}
	return out, nil
}

// SaveHash 保存指定 hash 字段。
func (b *MemoryHashBackend) SaveHash(ctx context.Context, key string, fields map[string][]byte, ttl time.Duration) error {
	if b == nil {
		return ErrHashBackendRequired
	}
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrRecordKeyRequired
	}
	fields, err := cloneHashFields(fields)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return nil
	}

	b.mu.Lock()
	b.ensureReadyLocked()
	record := b.records[key]
	if record == nil {
		record = make(map[string][]byte, len(fields))
		b.records[key] = record
	}
	for name, data := range fields {
		record[name] = append([]byte(nil), data...)
	}
	if ttl > 0 {
		b.ttl[key] = ttl
	}
	b.mu.Unlock()
	return nil
}

// SaveHashBatch 批量保存 hash 字段。
func (b *MemoryHashBackend) SaveHashBatch(ctx context.Context, batches []HashSaveBatch) error {
	if b == nil {
		return ErrHashBackendRequired
	}
	ctx = contextOrBackground(ctx)
	merged, err := mergeHashSaveBatches(batches)
	if err != nil {
		return err
	}
	for _, batch := range merged {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := b.SaveHash(ctx, batch.Key, batch.Fields, batch.TTL); err != nil {
			return err
		}
	}
	return nil
}

func mergeHashSaveBatches(batches []HashSaveBatch) ([]HashSaveBatch, error) {
	if len(batches) == 0 {
		return nil, nil
	}
	merged := make(map[string]HashSaveBatch, len(batches))
	for _, batch := range batches {
		if len(batch.Fields) == 0 {
			continue
		}
		key := strings.TrimSpace(batch.Key)
		if key == "" {
			return nil, ErrRecordKeyRequired
		}
		fields, err := cloneHashFields(batch.Fields)
		if err != nil {
			return nil, err
		}
		if len(fields) == 0 {
			continue
		}
		current := merged[key]
		if current.Fields == nil {
			current = HashSaveBatch{
				Key:    key,
				Fields: make(map[string][]byte, len(fields)),
			}
		}
		for name, data := range fields {
			current.Fields[name] = append([]byte(nil), data...)
		}
		current.TTL = batch.TTL
		merged[key] = current
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]HashSaveBatch, 0, len(keys))
	for _, key := range keys {
		batch := merged[key]
		batch.Fields = cloneBytesMap(batch.Fields)
		out = append(out, batch)
	}
	return out, nil
}

// Check 检查上下文是否已取消。
func (b *MemoryHashBackend) Check(ctx context.Context) error {
	if b == nil {
		return ErrHashBackendRequired
	}
	ctx = contextOrBackground(ctx)
	return ctx.Err()
}

// Snapshot 返回全部 hash 记录副本。
func (b *MemoryHashBackend) Snapshot() map[string]map[string][]byte {
	if b == nil {
		return map[string]map[string][]byte{}
	}
	b.mu.RLock()
	out := make(map[string]map[string][]byte, len(b.records))
	for key, fields := range b.records {
		out[key] = cloneBytesMap(fields)
	}
	b.mu.RUnlock()
	return out
}

func (b *MemoryHashBackend) ensureReadyLocked() {
	if b.records == nil {
		b.records = make(map[string]map[string][]byte)
	}
	if b.ttl == nil {
		b.ttl = make(map[string]time.Duration)
	}
}

func cloneHashFields(fields map[string][]byte) (map[string][]byte, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	out := make(map[string][]byte, len(fields))
	names := make([]string, 0, len(fields))
	for rawName := range fields {
		names = append(names, rawName)
	}
	sort.Strings(names)
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, ErrRecordKeyRequired
		}
		if _, exists := out[name]; exists {
			continue
		}
		data := fields[rawName]
		if data == nil {
			return nil, errors.New("hash field value is nil")
		}
		out[name] = append([]byte(nil), data...)
	}
	return out, nil
}

func cloneBytesMap(in map[string][]byte) map[string][]byte {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(in))
	for key, data := range in {
		out[key] = append([]byte(nil), data...)
	}
	return out
}

func normHashFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			seen[field] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for field := range seen {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}
