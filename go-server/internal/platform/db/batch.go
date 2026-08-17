package db

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// BatchLoader 支持按主键批量加载记录。
type BatchLoader[T any] interface {
	LoadBatch(context.Context, []string) (map[string]T, error)
}

// LoadBatch 批量加载记录；底层不支持批量时逐条加载。
func LoadBatch[T any](ctx context.Context, store Store[T], keys []string, normalizeKey func(string) string) (map[string]T, error) {
	if store == nil {
		return nil, errors.New("store is nil")
	}
	ctx = contextOrBackground(ctx)
	keys = normalizeBatchKeys(keys, normalizeKey)
	if len(keys) == 0 {
		return map[string]T{}, ctx.Err()
	}
	if loader, ok := store.(BatchLoader[T]); ok {
		return loader.LoadBatch(ctx, keys)
	}
	out := make(map[string]T, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, ok, err := store.Load(ctx, key)
		if err != nil {
			return nil, err
		}
		if ok {
			out[key] = record
		}
	}
	return out, nil
}

func normalizeBatchKeys(keys []string, normalizeKey func(string) string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = normalizeLookupKey(key, normalizeKey)
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func normalizeLookupKey(key string, normalizeKey func(string) string) string {
	if normalizeKey != nil {
		key = normalizeKey(key)
	}
	return strings.TrimSpace(key)
}
