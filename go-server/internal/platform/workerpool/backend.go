package workerpool

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

const BackendSelf = "self"

type Backend interface {
	Name() string
	Start(context.Context) error
	Stop(context.Context) error
	Submit(func()) error
	SubmitWait(context.Context, func()) error
	SubmitContext(context.Context, func(context.Context) error) (<-chan Result, error)
	Snapshot() Snapshot
}

type FactoryOptions struct {
	Backend string
	Name    string
	Size    int
	Queue   int
	Logger  *slog.Logger
}

func NewBackend(options FactoryOptions) (Backend, error) {
	backend := strings.ToLower(strings.TrimSpace(options.Backend))
	if backend == "" {
		backend = BackendSelf
	}
	switch backend {
	case BackendSelf:
		return New(options.Name, options.Size, options.Queue, options.Logger), nil
	default:
		return nil, fmt.Errorf("%w: unsupported worker backend %q", ErrInvalidTask, options.Backend)
	}
}

func MustNewBackend(options FactoryOptions) Backend {
	backend, err := NewBackend(options)
	if err != nil {
		panic(err)
	}
	return backend
}

var _ Backend = (*Pool)(nil)
