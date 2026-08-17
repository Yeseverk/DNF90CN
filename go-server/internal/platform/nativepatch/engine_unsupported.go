//go:build !linux || !amd64 || !cgo

package nativepatch

import (
	"context"
	"time"
)

type NativeEngine struct{}

func NewNativeEngine() *NativeEngine {
	return &NativeEngine{}
}

func (e *NativeEngine) Supported() bool {
	return false
}

func (e *NativeEngine) Apply(ctx context.Context, pkg Package) (Result, error) {
	if err := contextErr(ctx); err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	return Result{Version: pkg.Plan.Version, Target: pkg.Plan.Target, StartedAt: now, EndedAt: now}, ErrUnsupported
}

func (e *NativeEngine) Restore(ctx context.Context) (Result, error) {
	if err := contextErr(ctx); err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	return Result{StartedAt: now, EndedAt: now, Restored: false}, ErrUnsupported
}

func (e *NativeEngine) Snapshot() Snapshot {
	return Snapshot{Supported: false}
}
