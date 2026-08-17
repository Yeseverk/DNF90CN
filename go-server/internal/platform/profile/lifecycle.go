package profile

import (
	"context"
	"fmt"
	"time"

	"longheng.io/server/internal/platform/db"
)

type SaveActiveHook[T any] func(context.Context, string, T, T) error

type LifecycleOptions[T any, F comparable] struct {
	Store   db.Store[T]
	Runtime *Runtime[T, F]
}

type Lifecycle[T any, F comparable] struct {
	store   db.Store[T]
	runtime *Runtime[T, F]
}

func NewLifecycle[T any, F comparable](options LifecycleOptions[T, F]) (*Lifecycle[T, F], error) {
	if options.Store == nil {
		return nil, fmt.Errorf("profile lifecycle store is required")
	}
	if options.Runtime == nil {
		return nil, fmt.Errorf("profile lifecycle runtime is required")
	}
	return &Lifecycle[T, F]{
		store:   options.Store,
		runtime: options.Runtime,
	}, nil
}

func (l *Lifecycle[T, F]) Preflight(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return db.Check(ctx, l.store)
}

func (l *Lifecycle[T, F]) CloseOrFlush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return db.CloseOrFlush(ctx, l.store)
}

func (l *Lifecycle[T, F]) LoadOrCreate(ctx context.Context, key string) (T, bool, error) {
	return l.runtime.LoadOrCreate(ctx, key)
}

func (l *Lifecycle[T, F]) SaveAndUnload(ctx context.Context, key string) (T, T, bool, error) {
	return l.runtime.SaveAndUnload(ctx, key)
}

func (l *Lifecycle[T, F]) SaveActiveAndUnload(ctx context.Context, hook SaveActiveHook[T]) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var firstErr error
	for _, key := range l.runtime.ActiveKeys() {
		saved, summary, ok, err := l.runtime.SaveAndUnload(ctx, key)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if ok && hook != nil {
			if err := hook(ctx, key, saved, summary); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (l *Lifecycle[T, F]) Upsert(key string, mutate func(T, bool, time.Time) (T, []F, error)) (T, error) {
	return l.runtime.Upsert(key, mutate)
}

func (l *Lifecycle[T, F]) MutateLoaded(ctx context.Context, key string, mutate func(*T, time.Time) ([]F, error)) (T, T, error) {
	return l.runtime.MutateLoaded(ctx, key, mutate)
}

func (l *Lifecycle[T, F]) MutateActive(key string, mutate func(*T, time.Time) ([]F, error)) (T, T, bool, error) {
	return l.runtime.MutateActive(key, mutate)
}

func (l *Lifecycle[T, F]) LoadDetached(ctx context.Context, key string) (T, bool, error) {
	return l.runtime.LoadDetached(ctx, key)
}

func (l *Lifecycle[T, F]) NewDetached(key string, now time.Time) T {
	return l.runtime.NewDetached(key, now)
}

func (l *Lifecycle[T, F]) AttachIfAbsent(key string, record T, fields ...F) (T, bool) {
	return l.runtime.AttachIfAbsent(key, record, fields...)
}

func (l *Lifecycle[T, F]) Get(key string) (T, bool) {
	return l.runtime.Get(key)
}

func (l *Lifecycle[T, F]) Snapshot() []T {
	return l.runtime.Snapshot()
}

func (l *Lifecycle[T, F]) ActiveKeys() []string {
	return l.runtime.ActiveKeys()
}

func (l *Lifecycle[T, F]) ActiveByPredicate(match func(T) bool) (T, bool) {
	return l.runtime.ActiveByPredicate(match)
}
