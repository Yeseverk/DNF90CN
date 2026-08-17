package db

import (
	"context"
	"errors"
	"sync"
)

// DirtyRecordOptions 配置脏字段记录的初始值和字段集合。
type DirtyRecordOptions[T any, F comparable] struct {
	Record       T
	Fields       FieldRegistry[F]
	Clone        CloneFunc[T]
	Dirty        []F
	MarkAllDirty bool
}

// DirtySaveResult 描述一次脏字段保存结果。
type DirtySaveResult[F comparable] struct {
	Saved     bool
	Fields    []F
	Remaining []F
}

// DirtyRecord 在内存中维护记录快照和脏字段集合。
type DirtyRecord[T any, F comparable] struct {
	mu       sync.RWMutex
	record   T
	fields   FieldRegistry[F]
	clone    CloneFunc[T]
	dirty    map[F]uint64
	revision uint64
}

// NewDirtyRecord 创建脏字段记录。
func NewDirtyRecord[T any, F comparable](options DirtyRecordOptions[T, F]) *DirtyRecord[T, F] {
	clone := options.Clone
	if clone == nil {
		clone = IdentityClone[T]
	}
	r := &DirtyRecord[T, F]{
		record: clone(options.Record),
		fields: options.Fields,
		clone:  clone,
		dirty:  make(map[F]uint64),
	}
	if options.MarkAllDirty {
		r.markAllDirtyLocked()
	} else {
		r.markDirtyLocked(options.Dirty...)
	}
	return r
}

// Record 返回当前记录副本。
func (r *DirtyRecord[T, F]) Record() T {
	r.mu.RLock()
	record := r.clone(r.record)
	r.mu.RUnlock()
	return record
}

// Snapshot 返回当前记录副本和脏字段列表。
func (r *DirtyRecord[T, F]) Snapshot() (T, []F) {
	r.mu.RLock()
	record := r.clone(r.record)
	fields := r.dirtyFieldsLocked()
	r.mu.RUnlock()
	return record, fields
}

// DirtyFields 返回当前脏字段列表。
func (r *DirtyRecord[T, F]) DirtyFields() []F {
	r.mu.RLock()
	fields := r.dirtyFieldsLocked()
	r.mu.RUnlock()
	return fields
}

// MarkDirty 标记指定字段为脏。
func (r *DirtyRecord[T, F]) MarkDirty(fields ...F) {
	r.mu.Lock()
	r.markDirtyLocked(fields...)
	r.mu.Unlock()
}

// MarkAllDirty 标记全部字段为脏。
func (r *DirtyRecord[T, F]) MarkAllDirty() {
	r.mu.Lock()
	r.markAllDirtyLocked()
	r.mu.Unlock()
}

// ClearDirty 清除指定字段的脏标记。
func (r *DirtyRecord[T, F]) ClearDirty(fields ...F) {
	r.mu.Lock()
	for _, field := range r.fields.Normalize(fields) {
		delete(r.dirty, field)
	}
	r.mu.Unlock()
}

// ClearAllDirty 清除全部脏标记。
func (r *DirtyRecord[T, F]) ClearAllDirty() {
	r.mu.Lock()
	clear(r.dirty)
	r.mu.Unlock()
}

// Replace 替换记录并标记指定字段。
func (r *DirtyRecord[T, F]) Replace(record T, fields ...F) {
	r.mu.Lock()
	r.record = r.clone(record)
	r.markDirtyLocked(fields...)
	r.mu.Unlock()
}

// Mutate 在锁内修改记录，并返回修改前后副本。
func (r *DirtyRecord[T, F]) Mutate(mutate func(*T) ([]F, error)) (T, T, error) {
	var zero T
	if mutate == nil {
		return zero, zero, errors.New("dirty record mutation is required")
	}
	r.mu.Lock()
	before := r.clone(r.record)
	next := r.clone(r.record)
	fields, err := mutate(&next)
	if err != nil {
		r.mu.Unlock()
		return before, before, err
	}
	r.record = r.clone(next)
	r.markDirtyLocked(fields...)
	after := r.clone(r.record)
	r.mu.Unlock()
	return before, after, nil
}

// SaveDirty 保存当前脏字段，成功后只清除本次已保存字段。
func (r *DirtyRecord[T, F]) SaveDirty(ctx context.Context, store Store[T]) (DirtySaveResult[F], error) {
	if store == nil {
		return DirtySaveResult[F]{}, errors.New("store is nil")
	}
	ctx = contextOrBackground(ctx)
	record, fields, revisions := r.saveSnapshot()
	if len(fields) == 0 {
		return DirtySaveResult[F]{Saved: false}, ctx.Err()
	}
	if err := SaveFields(ctx, store, record, r.fields.Normalize, fields...); err != nil {
		return DirtySaveResult[F]{Saved: false, Fields: append([]F(nil), fields...), Remaining: r.DirtyFields()}, err
	}

	r.mu.Lock()
	for _, field := range fields {
		if current, ok := r.dirty[field]; ok && current == revisions[field] {
			delete(r.dirty, field)
		}
	}
	remaining := r.dirtyFieldsLocked()
	r.mu.Unlock()
	return DirtySaveResult[F]{
		Saved:     true,
		Fields:    append([]F(nil), fields...),
		Remaining: remaining,
	}, nil
}

func (r *DirtyRecord[T, F]) saveSnapshot() (T, []F, map[F]uint64) {
	r.mu.RLock()
	record := r.clone(r.record)
	fields := r.dirtyFieldsLocked()
	revisions := make(map[F]uint64, len(fields))
	for _, field := range fields {
		revisions[field] = r.dirty[field]
	}
	r.mu.RUnlock()
	return record, fields, revisions
}

func (r *DirtyRecord[T, F]) markDirtyLocked(fields ...F) {
	for _, field := range r.fields.Normalize(fields) {
		r.revision++
		r.dirty[field] = r.revision
	}
}

func (r *DirtyRecord[T, F]) markAllDirtyLocked() {
	r.markDirtyLocked(r.fields.All()...)
}

func (r *DirtyRecord[T, F]) dirtyFieldsLocked() []F {
	if len(r.dirty) == 0 {
		return nil
	}
	out := make([]F, 0, len(r.dirty))
	for _, field := range r.fields.All() {
		if _, ok := r.dirty[field]; ok {
			out = append(out, field)
		}
	}
	return out
}
