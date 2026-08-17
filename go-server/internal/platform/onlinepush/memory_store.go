package onlinepush

import (
	"context"
	"sort"
	"strings"
	"sync"
)

type MemoryStore struct {
	initOnce   sync.Once
	mu         sync.RWMutex
	receipts   map[string]Receipt
	idempotent map[string]string
	offline    map[string]OfflineMessage
}

// NewMemoryStore 创建内存版在线推送状态存储。
func NewMemoryStore() *MemoryStore {
	store := &MemoryStore{}
	store.ensureReady()
	return store
}

func (s *MemoryStore) ensureReady() {
	s.initOnce.Do(func() {
		if s.receipts == nil {
			s.receipts = make(map[string]Receipt)
		}
		if s.idempotent == nil {
			s.idempotent = make(map[string]string)
		}
		if s.offline == nil {
			s.offline = make(map[string]OfflineMessage)
		}
	})
}

// ReserveReceipt 按幂等键预留 receipt；重复请求返回已存在的副本。
func (s *MemoryStore) ReserveReceipt(ctx context.Context, receipt Receipt) (Receipt, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Receipt{}, false, err
	}
	if s == nil {
		return Receipt{}, false, ErrStoreRequired
	}
	s.ensureReady()
	s.mu.Lock()
	defer s.mu.Unlock()
	if receipt.IdempotencyKey != "" {
		if id := s.idempotent[receipt.IdempotencyKey]; id != "" {
			existing := cloneReceipt(s.receipts[id])
			existing.Duplicate = true
			return existing, true, nil
		}
	}
	s.receipts[receipt.ID] = cloneReceipt(receipt)
	if receipt.IdempotencyKey != "" {
		s.idempotent[receipt.IdempotencyKey] = receipt.ID
	}
	return cloneReceipt(receipt), false, nil
}

// UpdateReceipt 更新 receipt 最终状态。
func (s *MemoryStore) UpdateReceipt(ctx context.Context, receipt Receipt) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	s.ensureReady()
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.receipts[receipt.ID]; ok && receipt.CreatedAt.IsZero() {
		receipt.CreatedAt = old.CreatedAt
	}
	s.receipts[receipt.ID] = cloneReceipt(receipt)
	if receipt.IdempotencyKey != "" {
		s.idempotent[receipt.IdempotencyKey] = receipt.ID
	}
	return nil
}

// SaveOffline 保存离线消息。
func (s *MemoryStore) SaveOffline(ctx context.Context, message OfflineMessage) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	s.ensureReady()
	s.mu.Lock()
	s.offline[message.ID] = cloneOffline(message)
	s.mu.Unlock()
	return nil
}

// ListOffline 查询指定账号的离线消息。
func (s *MemoryStore) ListOffline(ctx context.Context, accountID string, limit int) ([]OfflineMessage, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreRequired
	}
	s.ensureReady()
	accountID = strings.TrimSpace(accountID)
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	s.mu.RLock()
	out := make([]OfflineMessage, 0, len(s.offline))
	for _, message := range s.offline {
		if accountID != "" && message.AccountID != accountID {
			continue
		}
		out = append(out, cloneOffline(message))
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// DeleteOffline 删除指定离线消息。
func (s *MemoryStore) DeleteOffline(ctx context.Context, id string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	s.ensureReady()
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.offline[id]; !ok {
		return ErrOfflineNotFound
	}
	delete(s.offline, id)
	return nil
}

// Snapshot 返回当前在线推送状态摘要。
func (s *MemoryStore) Snapshot(ctx context.Context) Snapshot {
	if ctxErr(ctx) != nil || s == nil {
		return Snapshot{}
	}
	s.ensureReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	byStatus := make(map[string]int)
	for _, receipt := range s.receipts {
		byStatus[receipt.Status]++
	}
	return Snapshot{
		Receipts: len(s.receipts),
		Offline:  len(s.offline),
		ByStatus: byStatus,
	}
}
