package datatable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var (
	ErrDataSourceRequired  = errors.New("data table source is required")
	ErrIndexKeyRequired    = errors.New("data table index key function is required")
	ErrDuplicateIndexKey   = errors.New("data table index key is duplicated")
	ErrMismatchedTableView = errors.New("data table view name does not match row set")
	ErrPreparedViewInvalid = errors.New("prepared data table view is invalid")
)

type RowSource interface {
	Decode(name string, out any) error
	Get(name string) (Table, bool)
	Manifest() Manifest
}

type KeyFunc[T any, K comparable] func(T) (K, bool)

type ViewBinding interface {
	TableName() string
	Prepare(context.Context, RowSource) (PreparedView, error)
}

type PreparedView struct {
	Table    string `json:"table"`
	Rows     int    `json:"rows"`
	apply    func()
	commit   func() error
	rollback func()
}

func NewPreparedView(table string, rows int, apply func()) (PreparedView, error) {
	return NewViewWithRollback(table, rows, apply, nil)
}

func NewViewWithRollback(table string, rows int, apply func(), rollback func()) (PreparedView, error) {
	var commit func() error
	if apply != nil {
		commit = func() error { apply(); return nil }
	}
	view := PreparedView{
		Table:    strings.TrimSpace(table),
		Rows:     rows,
		apply:    apply,
		commit:   commit,
		rollback: rollback,
	}
	if err := view.Validate(); err != nil {
		return PreparedView{}, err
	}
	return view, nil
}

func (p PreparedView) Validate() error {
	if strings.TrimSpace(p.Table) == "" || p.commit == nil {
		return ErrPreparedViewInvalid
	}
	return nil
}

func (p PreparedView) Commit() error {
	if err := p.Validate(); err != nil {
		return err
	}
	return p.commit()
}

func (p PreparedView) Rollback() {
	if p.rollback != nil {
		p.rollback()
	}
}

type RowSet[T any] struct {
	Table    Table    `json:"table"`
	Manifest Manifest `json:"manifest"`
	Rows     []T      `json:"rows,omitempty"`
}

func (s RowSet[T]) Clone() RowSet[T] {
	s.Rows = cloneRows(s.Rows)
	return s
}

type IndexedRowSet[T any, K comparable] struct {
	RowSet[T]
	Index map[K]T `json:"-"`
}

func (s IndexedRowSet[T, K]) Clone() IndexedRowSet[T, K] {
	out := IndexedRowSet[T, K]{
		RowSet: s.RowSet.Clone(),
		Index:  make(map[K]T, len(s.Index)),
	}
	for key, row := range s.Index {
		out.Index[key] = cloneValue(row)
	}
	return out
}

func LoadRows[T any](source RowSource, name string) (RowSet[T], error) {
	name = strings.TrimSpace(name)
	if source == nil {
		return RowSet[T]{}, ErrDataSourceRequired
	}
	if name == "" {
		return RowSet[T]{}, ErrTableRequired
	}
	table, ok := source.Get(name)
	if !ok {
		return RowSet[T]{}, fmt.Errorf("%w: %s", ErrTableNotFound, name)
	}
	var rows []T
	if err := source.Decode(name, &rows); err != nil {
		var wrapped struct {
			Items []T `json:"items"`
		}
		if wrappedErr := source.Decode(name, &wrapped); wrappedErr != nil || wrapped.Items == nil {
			return RowSet[T]{}, err
		}
		rows = wrapped.Items
	}
	return RowSet[T]{
		Table:    table,
		Manifest: source.Manifest(),
		Rows:     append([]T(nil), rows...),
	}, nil
}

func UniqueIndex[T any, K comparable](rows []T, keyFn KeyFunc[T, K]) (map[K]T, error) {
	if keyFn == nil {
		return nil, ErrIndexKeyRequired
	}
	index := make(map[K]T, len(rows))
	for _, row := range rows {
		key, ok := keyFn(row)
		if !ok {
			continue
		}
		if _, exists := index[key]; exists {
			return nil, fmt.Errorf("%w: %v", ErrDuplicateIndexKey, key)
		}
		index[key] = row
	}
	return index, nil
}

func MultiIndex[T any, K comparable](rows []T, keyFn KeyFunc[T, K]) (map[K][]T, error) {
	if keyFn == nil {
		return nil, ErrIndexKeyRequired
	}
	index := make(map[K][]T)
	for _, row := range rows {
		key, ok := keyFn(row)
		if !ok {
			continue
		}
		index[key] = append(index[key], row)
	}
	return cloneMultiIndex(index), nil
}

type TableView[T any] struct {
	name string
	mu   sync.RWMutex
	set  RowSet[T]
}

func NewTableView[T any](name string) *TableView[T] {
	return &TableView[T]{name: strings.TrimSpace(name)}
}

func (v *TableView[T]) Name() string {
	if v == nil {
		return ""
	}
	return v.name
}

func (v *TableView[T]) TableName() string {
	return v.Name()
}

func (v *TableView[T]) Prepare(ctx context.Context, source RowSource) (PreparedView, error) {
	if err := contextErr(ctx); err != nil {
		return PreparedView{}, err
	}
	if v == nil {
		return PreparedView{}, ErrTableRequired
	}
	next, err := LoadRows[T](source, v.name)
	if err != nil {
		return PreparedView{}, err
	}
	v.mu.RLock()
	previous := v.set.Clone()
	v.mu.RUnlock()
	return NewViewWithRollback(v.name, len(next.Rows), func() {
		v.mu.Lock()
		v.set = next.Clone()
		v.mu.Unlock()
	}, func() {
		v.mu.Lock()
		v.set = previous.Clone()
		v.mu.Unlock()
	})
}

func (v *TableView[T]) Reload(ctx context.Context, source RowSource) (RowSet[T], error) {
	if err := contextErr(ctx); err != nil {
		return RowSet[T]{}, err
	}
	if v == nil {
		return RowSet[T]{}, ErrTableRequired
	}
	next, err := LoadRows[T](source, v.name)
	if err != nil {
		return RowSet[T]{}, err
	}
	v.mu.Lock()
	v.set = next.Clone()
	v.mu.Unlock()
	return next.Clone(), nil
}

func (v *TableView[T]) Replace(set RowSet[T]) error {
	if v == nil {
		return ErrTableRequired
	}
	if strings.TrimSpace(v.name) != strings.TrimSpace(set.Table.Name) {
		return fmt.Errorf("%w: view=%s rows=%s", ErrMismatchedTableView, v.name, set.Table.Name)
	}
	v.mu.Lock()
	v.set = set.Clone()
	v.mu.Unlock()
	return nil
}

func (v *TableView[T]) Snapshot() RowSet[T] {
	if v == nil {
		return RowSet[T]{}
	}
	v.mu.RLock()
	set := v.set.Clone()
	v.mu.RUnlock()
	return set
}

func (v *TableView[T]) Rows() []T {
	return v.Snapshot().Rows
}

func (v *TableView[T]) Clear() {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.set = RowSet[T]{Table: Table{Name: v.name}}
	v.mu.Unlock()
}

type UniqueIndexView[T any, K comparable] struct {
	name  string
	keyFn KeyFunc[T, K]

	mu  sync.RWMutex
	set IndexedRowSet[T, K]
}

func NewUniqueIndexView[T any, K comparable](name string, keyFn KeyFunc[T, K]) *UniqueIndexView[T, K] {
	return &UniqueIndexView[T, K]{
		name:  strings.TrimSpace(name),
		keyFn: keyFn,
	}
}

func (v *UniqueIndexView[T, K]) Name() string {
	if v == nil {
		return ""
	}
	return v.name
}

func (v *UniqueIndexView[T, K]) TableName() string {
	return v.Name()
}

func (v *UniqueIndexView[T, K]) Prepare(ctx context.Context, source RowSource) (PreparedView, error) {
	if err := contextErr(ctx); err != nil {
		return PreparedView{}, err
	}
	if v == nil {
		return PreparedView{}, ErrTableRequired
	}
	next, err := v.build(source)
	if err != nil {
		return PreparedView{}, err
	}
	v.mu.RLock()
	previous := v.set.Clone()
	v.mu.RUnlock()
	return NewViewWithRollback(v.name, len(next.Rows), func() {
		v.mu.Lock()
		v.set = next.Clone()
		v.mu.Unlock()
	}, func() {
		v.mu.Lock()
		v.set = previous.Clone()
		v.mu.Unlock()
	})
}

func (v *UniqueIndexView[T, K]) Reload(ctx context.Context, source RowSource) (IndexedRowSet[T, K], error) {
	if err := contextErr(ctx); err != nil {
		return IndexedRowSet[T, K]{}, err
	}
	if v == nil {
		return IndexedRowSet[T, K]{}, ErrTableRequired
	}
	next, err := v.build(source)
	if err != nil {
		return IndexedRowSet[T, K]{}, err
	}
	v.mu.Lock()
	v.set = next.Clone()
	v.mu.Unlock()
	return next.Clone(), nil
}

func (v *UniqueIndexView[T, K]) Snapshot() IndexedRowSet[T, K] {
	if v == nil {
		return IndexedRowSet[T, K]{}
	}
	v.mu.RLock()
	set := v.set.Clone()
	v.mu.RUnlock()
	return set
}

func (v *UniqueIndexView[T, K]) Rows() []T {
	return v.Snapshot().Rows
}

func (v *UniqueIndexView[T, K]) Clear() {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.set = IndexedRowSet[T, K]{
		RowSet: RowSet[T]{Table: Table{Name: v.name}},
		Index:  make(map[K]T),
	}
	v.mu.Unlock()
}

func (v *UniqueIndexView[T, K]) Get(key K) (T, bool) {
	var zero T
	if v == nil {
		return zero, false
	}
	v.mu.RLock()
	row, ok := v.set.Index[key]
	v.mu.RUnlock()
	if !ok {
		return zero, false
	}
	return cloneValue(row), true
}

func (v *UniqueIndexView[T, K]) build(source RowSource) (IndexedRowSet[T, K], error) {
	rows, err := LoadRows[T](source, v.name)
	if err != nil {
		return IndexedRowSet[T, K]{}, err
	}
	index, err := UniqueIndex(rows.Rows, v.keyFn)
	if err != nil {
		return IndexedRowSet[T, K]{}, err
	}
	return IndexedRowSet[T, K]{
		RowSet: rows.Clone(),
		Index:  index,
	}, nil
}

func cloneMultiIndex[T any, K comparable](in map[K][]T) map[K][]T {
	out := make(map[K][]T, len(in))
	for key, rows := range in {
		out[key] = cloneRows(rows)
	}
	return out
}

func cloneRows[T any](rows []T) []T {
	if len(rows) == 0 {
		return nil
	}
	out := make([]T, len(rows))
	for i, row := range rows {
		out[i] = cloneValue(row)
	}
	return out
}

func cloneValue[T any](value T) T {
	cloned := cloneReflect(reflect.ValueOf(value))
	if !cloned.IsValid() {
		return value
	}
	out, ok := cloned.Interface().(T)
	if !ok {
		return value
	}
	return out
}

func cloneReflect(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		elem := cloneReflect(v.Elem())
		if elem.IsValid() && elem.Type().AssignableTo(v.Type().Elem()) {
			out.Elem().Set(elem)
		} else {
			out.Elem().Set(v.Elem())
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			setClonedValue(out.Index(i), v.Index(i))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			value := cloneReflect(iter.Value())
			if value.IsValid() && value.Type().AssignableTo(v.Type().Elem()) {
				out.SetMapIndex(iter.Key(), value)
			} else {
				out.SetMapIndex(iter.Key(), iter.Value())
			}
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			if !out.Field(i).CanSet() {
				return v
			}
			setClonedValue(out.Field(i), v.Field(i))
		}
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		elem := cloneReflect(v.Elem())
		if elem.IsValid() && elem.Type().AssignableTo(v.Type()) {
			return elem
		}
		out := reflect.New(v.Type()).Elem()
		if elem.IsValid() && elem.Type().AssignableTo(out.Type()) {
			out.Set(elem)
			return out
		}
		return v
	default:
		return v
	}
}

func setClonedValue(dst, src reflect.Value) {
	value := cloneReflect(src)
	if value.IsValid() && value.Type().AssignableTo(dst.Type()) {
		dst.Set(value)
		return
	}
	dst.Set(src)
}
