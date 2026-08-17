package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// JSONFileStoreOptions 配置 JSON 文件记录存储。
type JSONFileStoreOptions[T any] struct {
	Directory        string
	DefaultDirectory string
	Key              KeyFunc[T]
	Clone            CloneFunc[T]
	WithLoadedKey    func(T, string) T
}

// JSONFileStore 用单记录 JSON 文件实现 Store。
type JSONFileStore[T any] struct {
	mu            sync.RWMutex
	dir           string
	keyFn         KeyFunc[T]
	cloneFn       CloneFunc[T]
	withLoadedKey func(T, string) T
}

// NewJSONFileStore 创建 JSON 文件记录存储。
func NewJSONFileStore[T any](options JSONFileStoreOptions[T]) *JSONFileStore[T] {
	dir := strings.TrimSpace(options.Directory)
	if dir == "" {
		dir = strings.TrimSpace(options.DefaultDirectory)
	}
	if dir == "" {
		dir = "data/records"
	}
	cloneFn := options.Clone
	if cloneFn == nil {
		cloneFn = IdentityClone[T]
	}
	return &JSONFileStore[T]{
		dir:           dir,
		keyFn:         options.Key,
		cloneFn:       cloneFn,
		withLoadedKey: options.WithLoadedKey,
	}
}

// Load 从 JSON 文件读取记录。
func (s *JSONFileStore[T]) Load(ctx context.Context, key string) (T, bool, error) {
	ctx = contextOrBackground(ctx)
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	path, err := s.recordPath(key)
	if err != nil {
		return zero, false, err
	}
	s.mu.RLock()
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	s.mu.RUnlock()
	if errors.Is(err, os.ErrNotExist) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	var record T
	if err := json.Unmarshal(data, &record); err != nil {
		return zero, false, fmt.Errorf("decode record %s: %w", key, err)
	}
	if s.withLoadedKey != nil {
		record = s.withLoadedKey(record, key)
	}
	return s.cloneFn(record), true, nil
}

// Check 检查记录目录是否可写。
func (s *JSONFileStore[T]) Check(ctx context.Context) error {
	return checkWritableDir(ctx, s.dir)
}

// Save 原子写入记录 JSON 文件。
func (s *JSONFileStore[T]) Save(ctx context.Context, record T) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := RecordKey(s.keyFn, record)
	if err != nil {
		return err
	}
	path, err := s.recordPath(key)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return WriteFileAtomically(ctx, path, data, 0o600)
}

func (s *JSONFileStore[T]) recordPath(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ErrRecordKeyRequired
	}
	name := base64.RawURLEncoding.EncodeToString([]byte(key)) + ".json"
	return filepath.Join(s.dir, name), nil
}

func checkWritableDir(ctx context.Context, dir string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".preflight-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Remove(tmpName); err != nil {
		return err
	}
	return ctx.Err()
}
