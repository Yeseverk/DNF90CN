package db

import (
	"context"
	"strings"
	"sync"
)

// MemoryStore 是线程安全的内存记录存储。
type MemoryStore[T any] struct {
	mu      sync.RWMutex
	records map[string]T
	keyFn   KeyFunc[T]
	cloneFn CloneFunc[T]
}

// NewMemoryStore 创建内存记录存储。
func NewMemoryStore[T any](keyFn KeyFunc[T], cloneFn CloneFunc[T]) *MemoryStore[T] {
	if cloneFn == nil {
		cloneFn = IdentityClone[T]
	}
	return &MemoryStore[T]{
		records: make(map[string]T),
		keyFn:   keyFn,
		cloneFn: cloneFn,
	}
}

// Load 从内存按主键读取记录。
func (s *MemoryStore[T]) Load(_ context.Context, key string) (T, bool, error) {
	s.mu.RLock()
	record, ok := s.records[key]
	s.mu.RUnlock()
	if !ok {
		var zero T
		return zero, false, nil
	}
	return s.cloneFn(record), true, nil
}

// Check 检查上下文是否已取消。
func (s *MemoryStore[T]) Check(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	return ctx.Err()
}

// Save 保存记录副本到内存。
func (s *MemoryStore[T]) Save(_ context.Context, record T) error {
	key, err := RecordKey(s.keyFn, record)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.records[key] = s.cloneFn(record)
	s.mu.Unlock()
	return nil
}

// Delete removes one record. It is primarily used by in-memory transactional
// tests to restore an aggregate that did not exist before a failed commit.
func (s *MemoryStore[T]) Delete(ctx context.Context, key string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrRecordKeyRequired
	}
	s.mu.Lock()
	delete(s.records, key)
	s.mu.Unlock()
	return nil
}

// Snapshot 返回全部记录副本，可按 less 排序。
func (s *MemoryStore[T]) Snapshot(less func(T, T) bool) []T {
	s.mu.RLock()
	out := make([]T, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, s.cloneFn(record))
	}
	s.mu.RUnlock()
	if less != nil {
		sortRecords(out, less)
	}
	return out
}

func sortRecords[T any](records []T, less func(T, T) bool) {
	for i := 1; i < len(records); i++ {
		for j := i; j > 0 && less(records[j], records[j-1]); j-- {
			records[j], records[j-1] = records[j-1], records[j]
		}
	}
}
