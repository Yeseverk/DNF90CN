package profile

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/db"
)

const rtRecordLockShards = 64

type SaveFieldsFunc[T any, F comparable] func(context.Context, db.Store[T], T, ...F) error

type PrepareFunc[T any, F comparable] func(T, string, time.Time, bool) (T, []F, error)

type SavePrepareFunc[T any, F comparable] func(T, time.Time) (T, []F, error)

type TouchFunc[T any, F comparable] func(T, time.Time) (T, []F, error)

type SameFunc[T any] func(current, snapshot T) bool

var ErrCloneRequired = errors.New("profile runtime clone function is required")

type Options[T any, F comparable] struct {
	Store         db.Store[T]
	Fields        db.FieldRegistry[F]
	Clone         func(T) T
	NewRecord     func(string, time.Time) T
	PrepareLoaded PrepareFunc[T, F]
	PrepareSaved  SavePrepareFunc[T, F]
	Touch         TouchFunc[T, F]
	SaveFields    SaveFieldsFunc[T, F]
	SameActive    SameFunc[T]
	NormalizeKey  func(string) string
}

type Runtime[T any, F comparable] struct {
	store         db.Store[T]
	fields        db.FieldRegistry[F]
	clone         func(T) T
	newRecord     func(string, time.Time) T
	prepareLoaded PrepareFunc[T, F]
	prepareSaved  SavePrepareFunc[T, F]
	touch         TouchFunc[T, F]
	saveFields    SaveFieldsFunc[T, F]
	sameActive    SameFunc[T]
	normalizeKey  func(string) string

	mu     sync.RWMutex
	active map[string]T
	dirty  map[string]map[F]uint64
	rev    uint64

	recordLocks [rtRecordLockShards]sync.Mutex
}

func NewRuntime[T any, F comparable](options Options[T, F]) (*Runtime[T, F], error) {
	if options.Store == nil {
		return nil, fmt.Errorf("profile runtime store is required")
	}
	if options.NewRecord == nil {
		return nil, fmt.Errorf("profile runtime new record function is required")
	}
	if options.SaveFields == nil {
		return nil, fmt.Errorf("profile runtime save fields function is required")
	}
	if options.Clone == nil && hasRefFields[T]() {
		return nil, fmt.Errorf("%w: %s", ErrCloneRequired, recordTypeName[T]())
	}
	if options.Clone == nil {
		options.Clone = db.IdentityClone[T]
	}
	return &Runtime[T, F]{
		store:         options.Store,
		fields:        options.Fields,
		clone:         options.Clone,
		newRecord:     options.NewRecord,
		prepareLoaded: options.PrepareLoaded,
		prepareSaved:  options.PrepareSaved,
		touch:         options.Touch,
		saveFields:    options.SaveFields,
		sameActive:    options.SameActive,
		normalizeKey:  options.NormalizeKey,
		active:        make(map[string]T),
		dirty:         make(map[string]map[F]uint64),
	}, nil
}

func hasRefFields[T any]() bool {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return false
	}
	return typeHasRefFields(t)
}

func typeHasRefFields(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		return true
	}
	switch t.Kind() {
	case reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return true
	case reflect.Array:
		return typeHasRefFields(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if directFieldHasRef(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

func directFieldHasRef(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		return true
	}
	switch t.Kind() {
	case reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return true
	case reflect.Array:
		return directFieldHasRef(t.Elem())
	default:
		return false
	}
}

func recordTypeName[T any]() string {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return "<nil>"
	}
	return t.String()
}

func (r *Runtime[T, F]) LoadOrCreate(ctx context.Context, key string) (T, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var zero T
	key = r.normalizeRecordKey(key)
	if key == "" {
		return zero, false, fmt.Errorf("profile key is required")
	}

	unlock := r.lockRecord(key)
	defer unlock()
	return r.loadOrCreateLocked(ctx, key)
}

func (r *Runtime[T, F]) loadOrCreateLocked(ctx context.Context, key string) (T, bool, error) {
	var zero T
	r.mu.RLock()
	if record, ok := r.active[key]; ok {
		r.mu.RUnlock()
		return r.clone(record), false, nil
	}
	r.mu.RUnlock()

	now := time.Now().UTC()
	record, existed, err := r.store.Load(ctx, key)
	if err != nil {
		return zero, false, err
	}
	dirty := r.fields.NewSet()
	if !existed {
		record = r.newRecord(key, now)
		dirty.AddAll()
	} else {
		record = r.clone(record)
	}
	if r.prepareLoaded != nil {
		record, err = r.applyPrepareLoaded(record, key, now, existed, dirty)
		if err != nil {
			return zero, false, err
		}
	}

	r.mu.Lock()
	if existing, ok := r.active[key]; ok {
		r.mu.Unlock()
		return r.clone(existing), false, nil
	}
	record = r.clone(record)
	r.active[key] = record
	r.markDirtyLocked(key, dirty.List()...)
	r.mu.Unlock()
	return r.clone(record), true, nil
}

// SaveAndUnload 保存当前在线快照，并在记录未被并发修改时卸载。
// 保存链路只操作克隆快照，避免 PrepareSaved 或底层存储污染 active 状态。
func (r *Runtime[T, F]) SaveAndUnload(ctx context.Context, key string) (T, T, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var zero T
	key = r.normalizeRecordKey(key)
	if key == "" {
		return zero, zero, false, fmt.Errorf("profile key is required")
	}

	now := time.Now().UTC()
	r.mu.Lock()
	record, ok := r.active[key]
	if !ok {
		r.mu.Unlock()
		return zero, zero, false, nil
	}
	recordSnapshot := r.clone(record)
	saveRecord := r.clone(recordSnapshot)
	dirtyFields, dirtyRevs := r.dirtySnapshotLocked(key)
	if r.prepareSaved != nil {
		var fields []F
		var err error
		saveRecord, fields, err = r.prepareSaved(saveRecord, now)
		if err != nil {
			r.mu.Unlock()
			return zero, zero, false, err
		}
		dirtyFields = r.fields.Merge(dirtyFields, fields)
	}
	r.mu.Unlock()

	if err := r.saveFields(ctx, r.store, saveRecord, dirtyFields...); err != nil {
		return zero, zero, false, err
	}

	summaryRecord := saveRecord
	r.mu.Lock()
	if current, exists := r.active[key]; exists {
		r.clearDirtyLocked(key, dirtyRevs)
		if r.same(current, recordSnapshot) && len(r.dirty[key]) == 0 {
			delete(r.active, key)
			delete(r.dirty, key)
		} else {
			summaryRecord = current
		}
	}
	r.mu.Unlock()
	return r.clone(saveRecord), r.clone(summaryRecord), true, nil
}

func (r *Runtime[T, F]) Upsert(key string, mutate func(T, bool, time.Time) (T, []F, error)) (T, error) {
	var zero T
	key = r.normalizeRecordKey(key)
	if key == "" {
		return zero, fmt.Errorf("profile key is required")
	}
	if mutate == nil {
		return zero, fmt.Errorf("profile upsert mutation is required")
	}
	now := time.Now().UTC()

	unlock := r.lockRecord(key)
	defer unlock()

	r.mu.RLock()
	record, existed := r.active[key]
	if !existed {
		record = r.newRecord(key, now)
	} else {
		record = r.clone(record)
	}
	r.mu.RUnlock()

	record, fields, err := mutate(record, existed, now)
	if err != nil {
		return zero, err
	}
	record = r.clone(record)
	r.mu.Lock()
	r.active[key] = record
	if !existed {
		all := r.fields.All()
		fields = append(all, fields...)
	}
	r.markDirtyLocked(key, fields...)
	r.mu.Unlock()
	return r.clone(record), nil
}

func (r *Runtime[T, F]) MutateLoaded(ctx context.Context, key string, mutate func(*T, time.Time) ([]F, error)) (T, T, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var zero T
	key = r.normalizeRecordKey(key)
	if key == "" {
		return zero, zero, fmt.Errorf("profile key is required")
	}
	if mutate == nil {
		return zero, zero, fmt.Errorf("profile mutation is required")
	}
	const attempts = 2
	for attempt := 0; attempt < attempts; attempt++ {
		unlock := r.lockRecord(key)
		if _, _, err := r.loadOrCreateLocked(ctx, key); err != nil {
			unlock()
			return zero, zero, err
		}
		now := time.Now().UTC()
		r.mu.RLock()
		record, ok := r.active[key]
		if !ok {
			r.mu.RUnlock()
			unlock()
			if err := ctx.Err(); err != nil {
				return zero, zero, err
			}
			continue
		}
		before := r.clone(record)
		record = r.clone(record)
		r.mu.RUnlock()
		fields, err := mutate(&record, now)
		if err != nil {
			unlock()
			return zero, zero, err
		}
		if r.touch != nil {
			var touchFields []F
			record, touchFields, err = r.touch(record, now)
			if err != nil {
				unlock()
				return zero, zero, err
			}
			fields = append(fields, touchFields...)
		}
		record = r.clone(record)
		r.mu.Lock()
		r.active[key] = record
		r.markDirtyLocked(key, fields...)
		r.mu.Unlock()
		unlock()
		return before, r.clone(record), nil
	}
	return zero, zero, fmt.Errorf("profile %s was unloaded during mutation", key)
}

func (r *Runtime[T, F]) MutateActive(key string, mutate func(*T, time.Time) ([]F, error)) (T, T, bool, error) {
	var zero T
	key = r.normalizeRecordKey(key)
	if key == "" {
		return zero, zero, false, fmt.Errorf("profile key is required")
	}
	if mutate == nil {
		return zero, zero, false, fmt.Errorf("profile mutation is required")
	}
	now := time.Now().UTC()
	unlock := r.lockRecord(key)
	defer unlock()

	r.mu.RLock()
	record, ok := r.active[key]
	if !ok {
		r.mu.RUnlock()
		return zero, zero, false, nil
	}
	before := r.clone(record)
	record = r.clone(record)
	r.mu.RUnlock()
	fields, err := mutate(&record, now)
	if err != nil {
		return zero, zero, true, err
	}
	if r.touch != nil && len(fields) > 0 {
		var touchFields []F
		record, touchFields, err = r.touch(record, now)
		if err != nil {
			return zero, zero, true, err
		}
		fields = append(fields, touchFields...)
	}
	record = r.clone(record)
	r.mu.Lock()
	r.active[key] = record
	r.markDirtyLocked(key, fields...)
	r.mu.Unlock()
	return before, r.clone(record), true, nil
}

func (r *Runtime[T, F]) LoadDetached(ctx context.Context, key string) (T, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	key = r.normalizeRecordKey(key)
	if key == "" {
		var zero T
		return zero, false, fmt.Errorf("profile key is required")
	}
	record, ok, err := r.store.Load(ctx, key)
	if err != nil || !ok {
		return record, ok, err
	}
	return r.clone(record), true, nil
}

func (r *Runtime[T, F]) NewDetached(key string, now time.Time) T {
	key = r.normalizeRecordKey(key)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return r.newRecord(key, now)
}

func (r *Runtime[T, F]) AttachIfAbsent(key string, record T, fields ...F) (T, bool) {
	key = r.normalizeRecordKey(key)
	if key == "" {
		return r.clone(record), false
	}
	unlock := r.lockRecord(key)
	defer unlock()

	r.mu.Lock()
	if current, ok := r.active[key]; ok {
		r.mu.Unlock()
		return r.clone(current), false
	}
	record = r.clone(record)
	r.active[key] = record
	r.markDirtyLocked(key, fields...)
	r.mu.Unlock()
	return r.clone(record), true
}

func (r *Runtime[T, F]) Get(key string) (T, bool) {
	key = r.normalizeRecordKey(key)
	r.mu.RLock()
	record, ok := r.active[key]
	r.mu.RUnlock()
	return r.clone(record), ok
}

func (r *Runtime[T, F]) Snapshot() []T {
	r.mu.RLock()
	keys := make([]string, 0, len(r.active))
	for key := range r.active {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	records := make([]T, 0, len(keys))
	for _, key := range keys {
		records = append(records, r.active[key])
	}
	r.mu.RUnlock()

	out := make([]T, 0, len(records))
	for _, record := range records {
		out = append(out, r.clone(record))
	}
	return out
}

func (r *Runtime[T, F]) ActiveKeys() []string {
	r.mu.RLock()
	keys := make([]string, 0, len(r.active))
	for key := range r.active {
		keys = append(keys, key)
	}
	r.mu.RUnlock()
	sort.Strings(keys)
	return keys
}

func (r *Runtime[T, F]) ActiveByPredicate(match func(T) bool) (T, bool) {
	var zero T
	if match == nil {
		return zero, false
	}
	r.mu.RLock()
	records := make([]T, 0, len(r.active))
	for _, record := range r.active {
		records = append(records, record)
	}
	r.mu.RUnlock()

	for _, record := range records {
		cloned := r.clone(record)
		if match(cloned) {
			return cloned, true
		}
	}
	return zero, false
}

func (r *Runtime[T, F]) applyPrepareLoaded(record T, key string, now time.Time, existed bool, dirty db.FieldSet[F]) (T, error) {
	record, fields, err := r.prepareLoaded(record, key, now, existed)
	if err != nil {
		return record, err
	}
	dirty.Add(fields...)
	return record, nil
}

func (r *Runtime[T, F]) markDirtyLocked(key string, fields ...F) {
	fields = r.fields.Normalize(fields)
	if len(fields) == 0 {
		return
	}
	revisions := r.dirty[key]
	if revisions == nil {
		revisions = make(map[F]uint64, len(fields))
	}
	for _, field := range fields {
		r.rev++
		revisions[field] = r.rev
	}
	r.dirty[key] = revisions
}

func (r *Runtime[T, F]) dirtySnapshotLocked(key string) ([]F, map[F]uint64) {
	revisions := r.dirty[key]
	fields := r.dirtyFieldsLocked(revisions)
	if len(fields) == 0 {
		return nil, nil
	}
	snapshot := make(map[F]uint64, len(fields))
	for _, field := range fields {
		snapshot[field] = revisions[field]
	}
	return fields, snapshot
}

func (r *Runtime[T, F]) clearDirtyLocked(key string, saved map[F]uint64) {
	if len(saved) == 0 {
		return
	}
	revisions := r.dirty[key]
	for field, savedRev := range saved {
		if currentRev, ok := revisions[field]; ok && currentRev == savedRev {
			delete(revisions, field)
		}
	}
	if len(revisions) == 0 {
		delete(r.dirty, key)
		return
	}
	r.dirty[key] = revisions
}

func (r *Runtime[T, F]) dirtyFieldsLocked(revisions map[F]uint64) []F {
	if len(revisions) == 0 {
		return nil
	}
	fields := make([]F, 0, len(revisions))
	for _, field := range r.fields.All() {
		if _, ok := revisions[field]; ok {
			fields = append(fields, field)
		}
	}
	return fields
}

func (r *Runtime[T, F]) normalizeRecordKey(key string) string {
	if r.normalizeKey != nil {
		return r.normalizeKey(key)
	}
	return strings.TrimSpace(key)
}

func (r *Runtime[T, F]) lockRecord(key string) func() {
	shard := rtRecordShard(key)
	r.recordLocks[shard].Lock()
	return func() {
		r.recordLocks[shard].Unlock()
	}
}

func rtRecordShard(key string) int {
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	// 分片数固定为 64，索引一定落在 int 安全范围内。
	return int(hash & (rtRecordLockShards - 1))
}

func (r *Runtime[T, F]) same(current, snapshot T) bool {
	if r.sameActive == nil {
		return false
	}
	return r.sameActive(current, snapshot)
}
