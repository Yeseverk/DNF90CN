package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// AsyncStoreDeadLetter 描述异步存储刷盘失败后的死信记录。
type AsyncStoreDeadLetter[T any, F comparable] struct {
	AccountID     string    `json:"account_id"`
	Profile       T         `json:"profile"`
	Fields        []F       `json:"fields,omitempty"`
	Attempts      int       `json:"attempts"`
	Error         string    `json:"error"`
	FirstFailedAt time.Time `json:"first_failed_at"`
	LastFailedAt  time.Time `json:"last_failed_at"`
}

// DeadLetterStore 是死信持久化接口。
type DeadLetterStore[T any, F comparable] interface {
	Save(context.Context, AsyncStoreDeadLetter[T, F]) error
	Delete(context.Context, string) error
	List(context.Context) ([]AsyncStoreDeadLetter[T, F], error)
}

// FileDeadLetterStoreOptions 配置文件死信存储目录。
type FileDeadLetterStoreOptions struct {
	Directory        string
	DefaultDirectory string
}

// FileDeadLetterStore 用本地 JSON 文件保存死信。
type FileDeadLetterStore[T any, F comparable] struct {
	mu  sync.RWMutex
	dir string
}

// NewFileDeadLetterStore 创建文件死信存储。
func NewFileDeadLetterStore[T any, F comparable](options FileDeadLetterStoreOptions) *FileDeadLetterStore[T, F] {
	dir := strings.TrimSpace(options.Directory)
	if dir == "" {
		dir = strings.TrimSpace(options.DefaultDirectory)
	}
	if dir == "" {
		dir = "data/dead_letters"
	}
	return &FileDeadLetterStore[T, F]{dir: dir}
}

// Save 原子写入一条死信文件。
func (s *FileDeadLetterStore[T, F]) Save(ctx context.Context, dead AsyncStoreDeadLetter[T, F]) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.deadLetterPath(dead.AccountID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(dead, "", "  ")
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

// Check 检查死信目录是否可写。
func (s *FileDeadLetterStore[T, F]) Check(ctx context.Context) error {
	return checkWritableDir(ctx, s.dir)
}

// Delete 删除指定账号死信文件。
func (s *FileDeadLetterStore[T, F]) Delete(ctx context.Context, accountID string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.deadLetterPath(accountID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// List 读取并按失败时间排序全部死信。
func (s *FileDeadLetterStore[T, F]) List(ctx context.Context) ([]AsyncStoreDeadLetter[T, F], error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]AsyncStoreDeadLetter[T, F], 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var dead AsyncStoreDeadLetter[T, F]
		if err := json.Unmarshal(data, &dead); err != nil {
			return nil, fmt.Errorf("decode dead letter %s: %w", entry.Name(), err)
		}
		out = append(out, dead)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastFailedAt.Equal(out[j].LastFailedAt) {
			return out[i].AccountID < out[j].AccountID
		}
		return out[i].LastFailedAt.Before(out[j].LastFailedAt)
	})
	return out, nil
}

func (s *FileDeadLetterStore[T, F]) deadLetterPath(accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return "", ErrRecordKeyRequired
	}
	name := base64.RawURLEncoding.EncodeToString([]byte(accountID)) + ".json"
	return filepath.Join(s.dir, name), nil
}
