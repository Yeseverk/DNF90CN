package config

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrConfigProviderRequired = errors.New("config provider is required")
	ErrConfigKeyRequired      = errors.New("config key is required")
	ErrConfigSnapshotNotFound = errors.New("config snapshot not found")
)

type Snapshot struct {
	Key       string            `json:"key"`
	Data      []byte            `json:"data,omitempty"`
	Version   string            `json:"version,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

type Provider interface {
	LoadSnapshot(context.Context, string) (Snapshot, bool, error)
}

type WatchProvider interface {
	Provider
	WatchSnapshots(context.Context, string) (<-chan Snapshot, <-chan error)
}

type ProviderFunc func(context.Context, string) (Snapshot, bool, error)

func (f ProviderFunc) LoadSnapshot(ctx context.Context, key string) (Snapshot, bool, error) {
	if f == nil {
		return Snapshot{}, false, ErrConfigProviderRequired
	}
	return f(ctx, key)
}

func LoadRequiredSnapshot(ctx context.Context, provider Provider, key string) (Snapshot, error) {
	if provider == nil {
		return Snapshot{}, ErrConfigProviderRequired
	}
	if strings.TrimSpace(key) == "" {
		return Snapshot{}, ErrConfigKeyRequired
	}
	snapshot, ok, err := provider.LoadSnapshot(ctx, key)
	if err != nil {
		return Snapshot{}, err
	}
	if !ok {
		return Snapshot{}, ErrConfigSnapshotNotFound
	}
	return snapshot, nil
}
