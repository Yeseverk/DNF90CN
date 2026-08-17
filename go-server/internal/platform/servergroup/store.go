package servergroup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"longheng.io/server/internal/platform/filex"
)

// ErrStoreEmpty 表示服务器分组存储为空或尚未初始化。
var ErrStoreEmpty = errors.New("server group store is empty")

// Store 定义服务器分组计划的持久化接口。
type Store interface {
	Load(context.Context) (Plan, error)
	Save(context.Context, Plan) error
}

// FileStore 用本地 JSON 文件保存服务器分组计划。
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore 创建文件型服务器分组计划存储。
func NewFileStore(path string) (*FileStore, error) {
	path = normalizeID(path)
	if path == "" {
		return nil, fmt.Errorf("%w: file store path is required", ErrInvalidPlan)
	}
	return &FileStore{path: path}, nil
}

// Path 返回文件型服务器分组存储路径。
func (s *FileStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load 从文件读取并校验服务器分组计划。
func (s *FileStore) Load(ctx context.Context) (Plan, error) {
	if err := ctxErr(ctx); err != nil {
		return Plan{}, err
	}
	if s == nil {
		return Plan{}, ErrStoreEmpty
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Plan{}, ErrStoreEmpty
	}
	if err != nil {
		return Plan{}, err
	}
	if len(data) == 0 {
		return Plan{}, ErrStoreEmpty
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return Plan{}, fmt.Errorf("decode server group plan %s: %w", s.path, err)
	}
	normalized, _, err := normalizePlan(plan)
	if err != nil {
		return Plan{}, err
	}
	return normalized, nil
}

// Save 原子写入服务器分组计划文件。
func (s *FileStore) Save(ctx context.Context, plan Plan) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreEmpty
	}
	normalized, _, err := normalizePlan(plan)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeFileReplace(s.path, data)
}

// LoadManager 从存储加载管理器，存储为空时使用 fallback 初始化。
func LoadManager(ctx context.Context, store Store, fallback Plan) (*Manager, error) {
	if store != nil {
		plan, err := store.Load(ctx)
		if err == nil {
			return New(plan)
		}
		if !errors.Is(err, ErrStoreEmpty) {
			return nil, err
		}
	}
	manager, err := New(fallback)
	if err != nil {
		return nil, err
	}
	if store != nil {
		if err := store.Save(ctx, manager.Snapshot()); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

// SaveManager 将管理器当前计划保存到存储。
func SaveManager(ctx context.Context, store Store, manager *Manager) error {
	if manager == nil {
		return ErrNotFound
	}
	if store == nil {
		return ErrStoreEmpty
	}
	return store.Save(ctx, manager.Snapshot())
}

func writeFileReplace(path string, data []byte) error {
	if err := filex.AtomicWriteFile(path, data, 0o600); err != nil {
		return err
	}
	_ = os.Chtimes(path, time.Now(), time.Now())
	return nil
}
