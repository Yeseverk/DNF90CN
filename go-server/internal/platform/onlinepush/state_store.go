package onlinepush

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"longheng.io/server/internal/platform/filex"
)

// ErrStateStoreRequired 表示在线推送持久化状态后端缺失。
var ErrStateStoreRequired = errors.New("online push state store is required")

// StateStore 定义在线推送 receipt/offline 状态的持久化后端。
type StateStore interface {
	Load(context.Context) (State, bool, error)
	Save(context.Context, State) error
	Close() error
}

// State 是在线推送持久化快照。
type State struct {
	Receipts    []Receipt         `json:"receipts,omitempty"`
	Idempotency map[string]string `json:"idempotency,omitempty"`
	Offline     []OfflineMessage  `json:"offline,omitempty"`
}

// PersistentStore 在内存 store 外包一层持久化保存和失败回滚。
type PersistentStore struct {
	memory *MemoryStore
	store  StateStore
	saveMu sync.Mutex
}

// NewPersistentStore 从后端加载状态并创建持久化 store。
func NewPersistentStore(ctx context.Context, store StateStore) (*PersistentStore, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrStateStoreRequired
	}
	memory := NewMemoryStore()
	if state, ok, err := store.Load(ctx); err != nil {
		return nil, err
	} else if ok {
		memory.importState(state)
	}
	return &PersistentStore{memory: memory, store: store}, nil
}

// ReserveReceipt 预留 receipt 并持久化。
func (s *PersistentStore) ReserveReceipt(ctx context.Context, receipt Receipt) (Receipt, bool, error) {
	if s == nil || s.memory == nil {
		return Receipt{}, false, ErrStateStoreRequired
	}
	s.saveMu.Lock()
	before := s.memory.exportState()
	result, duplicate, err := s.memory.ReserveReceipt(ctx, receipt)
	if err != nil || duplicate {
		s.saveMu.Unlock()
		return result, duplicate, err
	}
	return result, duplicate, s.persistLocked(ctx, before, nil)
}

// UpdateReceipt 更新 receipt 并持久化。
func (s *PersistentStore) UpdateReceipt(ctx context.Context, receipt Receipt) error {
	if s == nil || s.memory == nil {
		return ErrStateStoreRequired
	}
	s.saveMu.Lock()
	before := s.memory.exportState()
	err := s.memory.UpdateReceipt(ctx, receipt)
	return s.persistLocked(ctx, before, err)
}

// SaveOffline 保存离线消息并持久化。
func (s *PersistentStore) SaveOffline(ctx context.Context, message OfflineMessage) error {
	if s == nil || s.memory == nil {
		return ErrStateStoreRequired
	}
	s.saveMu.Lock()
	before := s.memory.exportState()
	err := s.memory.SaveOffline(ctx, message)
	return s.persistLocked(ctx, before, err)
}

// ListOffline 查询离线消息。
func (s *PersistentStore) ListOffline(ctx context.Context, accountID string, limit int) ([]OfflineMessage, error) {
	if s == nil || s.memory == nil {
		return nil, ErrStateStoreRequired
	}
	return s.memory.ListOffline(ctx, accountID, limit)
}

// DeleteOffline 删除离线消息并持久化。
func (s *PersistentStore) DeleteOffline(ctx context.Context, id string) error {
	if s == nil || s.memory == nil {
		return ErrStateStoreRequired
	}
	s.saveMu.Lock()
	before := s.memory.exportState()
	err := s.memory.DeleteOffline(ctx, id)
	return s.persistLocked(ctx, before, err)
}

// Snapshot 返回持久化 store 的内存摘要。
func (s *PersistentStore) Snapshot(ctx context.Context) Snapshot {
	if s == nil || s.memory == nil {
		return Snapshot{}
	}
	return s.memory.Snapshot(ctx)
}

// Close 关闭底层持久化后端。
func (s *PersistentStore) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	return s.store.Close()
}

// saveMu 覆盖内存变更到失败回滚的完整窗口，否则一次落盘失败会覆盖并发已提交状态。
func (s *PersistentStore) persistLocked(ctx context.Context, before State, err error) error {
	if s == nil {
		return ErrStateStoreRequired
	}
	defer s.saveMu.Unlock()
	if err != nil {
		return err
	}
	if s.memory == nil || s.store == nil {
		return ErrStateStoreRequired
	}
	if saveErr := s.store.Save(ctx, s.memory.exportState()); saveErr != nil {
		s.memory.importState(before)
		return saveErr
	}
	return nil
}

type JSONFileStateStore struct {
	Path string
}

// NewJSONFileStateStore 创建 JSON 文件状态后端。
func NewJSONFileStateStore(path string) *JSONFileStateStore {
	return &JSONFileStateStore{Path: path}
}

// Load 从 JSON 文件加载在线推送状态。
func (s *JSONFileStateStore) Load(ctx context.Context) (State, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return State{}, false, err
	}
	if s == nil || s.Path == "" {
		return State{}, false, ErrStateStoreRequired
	}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, false, err
	}
	return state, true, nil
}

// Save 将在线推送状态原子写入 JSON 文件。
func (s *JSONFileStateStore) Save(ctx context.Context, state State) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.Path == "" {
		return ErrStateStoreRequired
	}
	path := filepath.Clean(s.Path)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return filex.AtomicWriteFile(path, data, 0o600)
}

// Close 关闭 JSON 文件状态后端。
func (s *JSONFileStateStore) Close() error {
	return nil
}

func (s *MemoryStore) exportState() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipts := make([]Receipt, 0, len(s.receipts))
	for _, receipt := range s.receipts {
		receipts = append(receipts, cloneReceipt(receipt))
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].ID < receipts[j].ID })
	offline := make([]OfflineMessage, 0, len(s.offline))
	for _, message := range s.offline {
		offline = append(offline, cloneOffline(message))
	}
	sort.Slice(offline, func(i, j int) bool { return offline[i].ID < offline[j].ID })
	idempotency := make(map[string]string, len(s.idempotent))
	for key, value := range s.idempotent {
		idempotency[key] = value
	}
	if len(idempotency) == 0 {
		idempotency = nil
	}
	return State{Receipts: receipts, Idempotency: idempotency, Offline: offline}
}

func (s *MemoryStore) importState(state State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receipts = make(map[string]Receipt, len(state.Receipts))
	s.idempotent = make(map[string]string, len(state.Idempotency))
	s.offline = make(map[string]OfflineMessage, len(state.Offline))
	for _, receipt := range state.Receipts {
		receipt = cloneReceipt(receipt)
		if receipt.ID == "" {
			continue
		}
		s.receipts[receipt.ID] = receipt
		if receipt.IdempotencyKey != "" {
			s.idempotent[receipt.IdempotencyKey] = receipt.ID
		}
	}
	for key, value := range state.Idempotency {
		if key != "" && value != "" {
			s.idempotent[key] = value
		}
	}
	for _, message := range state.Offline {
		message = cloneOffline(message)
		if message.ID == "" {
			continue
		}
		s.offline[message.ID] = message
	}
}
