package db

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type asyncDeadSinkOpts[T any, F comparable] struct {
	Limit           int
	Store           DeadLetterStore[T, F]
	StoreTimeout    time.Duration
	RecordKey       KeyFunc[T]
	NormalizeKey    func(string) string
	Clone           CloneFunc[T]
	NormalizeFields func([]F) []F
}

type asyncDeadLetterSink[T any, F comparable] struct {
	mu              sync.Mutex
	storeMu         sync.Mutex
	limit           int
	store           DeadLetterStore[T, F]
	storeTimeout    time.Duration
	storeError      string
	deadLetters     map[string]AsyncStoreDeadLetter[T, F]
	deadOrder       []string
	recordKey       KeyFunc[T]
	normalizeKey    func(string) string
	clone           CloneFunc[T]
	normalizeFields func([]F) []F
}

func newAsyncDeadSink[T any, F comparable](options asyncDeadSinkOpts[T, F]) *asyncDeadLetterSink[T, F] {
	if options.Clone == nil {
		options.Clone = IdentityClone[T]
	}
	if options.StoreTimeout <= 0 {
		options.StoreTimeout = defAsyncFlush
	}
	return &asyncDeadLetterSink[T, F]{
		limit:           options.Limit,
		store:           options.Store,
		storeTimeout:    options.StoreTimeout,
		deadLetters:     make(map[string]AsyncStoreDeadLetter[T, F]),
		recordKey:       options.RecordKey,
		normalizeKey:    options.NormalizeKey,
		clone:           options.Clone,
		normalizeFields: options.NormalizeFields,
	}
}

// Load 从持久化死信存储恢复死信记录。
func (s *asyncDeadLetterSink[T, F]) Load() {
	if s.store == nil {
		return
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	ctx, cancel := s.storeContext()
	deadLetters, err := s.store.List(ctx)
	cancel()
	if err != nil {
		s.setStoreError(err)
		return
	}

	s.mu.Lock()
	for _, dead := range deadLetters {
		dead = s.cloneDeadLetter(dead)
		dead.AccountID = s.NormalizeRecordKey(dead.AccountID)
		if dead.AccountID == "" {
			accountID, err := s.RecordKey(dead.Profile)
			if err != nil {
				continue
			}
			dead.AccountID = accountID
		}
		if dead.AccountID == "" {
			continue
		}
		if _, exists := s.deadLetters[dead.AccountID]; !exists {
			s.deadOrder = append(s.deadOrder, dead.AccountID)
		}
		s.deadLetters[dead.AccountID] = dead
	}
	evicted := s.enforceLimitLocked()
	s.mu.Unlock()
	s.deletePersistedMany(evicted)
}

// Get 返回指定账号的死信副本。
func (s *asyncDeadLetterSink[T, F]) Get(accountID string) (AsyncStoreDeadLetter[T, F], bool) {
	accountID = s.NormalizeRecordKey(accountID)
	s.mu.Lock()
	defer s.mu.Unlock()
	dead, ok := s.deadLetters[accountID]
	if !ok {
		return AsyncStoreDeadLetter[T, F]{}, false
	}
	return s.cloneDeadLetter(dead), true
}

// Add 写入一条死信并持久化；持久化失败时回滚内存死信，调用方应继续保留 pending。
func (s *asyncDeadLetterSink[T, F]) Add(save pendingSave[T, F]) error {
	accountID, err := s.RecordKey(save.profile)
	if err != nil {
		return err
	}
	dead := AsyncStoreDeadLetter[T, F]{
		AccountID:     accountID,
		Profile:       s.clone(save.profile),
		Fields:        s.NormalizeFields(save.fields),
		Attempts:      save.attempts,
		Error:         save.lastError,
		FirstFailedAt: save.firstFailedAt,
		LastFailedAt:  save.lastFailedAt,
	}

	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	s.mu.Lock()
	previous, existed := s.deadLetters[accountID]
	if _, exists := s.deadLetters[accountID]; !exists {
		s.deadOrder = append(s.deadOrder, accountID)
	}
	s.deadLetters[accountID] = dead
	s.mu.Unlock()

	if err := s.persist(dead); err != nil {
		s.rollbackAdd(accountID, previous, existed)
		return err
	}
	s.mu.Lock()
	evicted := s.enforceLimitLocked()
	s.mu.Unlock()
	s.deletePersistedMany(evicted)
	return nil
}

// Take 取出并删除指定账号死信。
func (s *asyncDeadLetterSink[T, F]) Take(accountID string) (AsyncStoreDeadLetter[T, F], bool) {
	accountID = s.NormalizeRecordKey(accountID)
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	s.mu.Lock()

	dead, ok := s.deadLetters[accountID]
	if !ok {
		s.mu.Unlock()
		return AsyncStoreDeadLetter[T, F]{}, false
	}
	delete(s.deadLetters, accountID)
	s.removeOrderLocked(accountID)
	out := s.cloneDeadLetter(dead)
	s.mu.Unlock()
	s.deletePersisted(accountID)
	return out, true
}

func (s *asyncDeadLetterSink[T, F]) restoreMemory(dead AsyncStoreDeadLetter[T, F]) {
	dead = s.cloneDeadLetter(dead)
	dead.AccountID = s.NormalizeRecordKey(dead.AccountID)
	if dead.AccountID == "" {
		accountID, err := s.RecordKey(dead.Profile)
		if err != nil {
			return
		}
		dead.AccountID = accountID
	}
	if dead.AccountID == "" {
		return
	}
	s.mu.Lock()
	if _, exists := s.deadLetters[dead.AccountID]; !exists {
		s.deadOrder = append(s.deadOrder, dead.AccountID)
	}
	s.deadLetters[dead.AccountID] = dead
	s.mu.Unlock()
}

// List 返回按失败时间排序的死信快照。
func (s *asyncDeadLetterSink[T, F]) List() []AsyncStoreDeadLetter[T, F] {
	s.mu.Lock()
	out := make([]AsyncStoreDeadLetter[T, F], 0, len(s.deadLetters))
	for _, dead := range s.deadLetters {
		out = append(out, s.cloneDeadLetter(dead))
	}
	s.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].LastFailedAt.Equal(out[j].LastFailedAt) {
			return out[i].AccountID < out[j].AccountID
		}
		return out[i].LastFailedAt.Before(out[j].LastFailedAt)
	})
	return out
}

// Count 返回当前死信数量。
func (s *asyncDeadLetterSink[T, F]) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.deadLetters)
}

// Limit 返回死信内存上限。
func (s *asyncDeadLetterSink[T, F]) Limit() int {
	return s.limit
}

// StoreName 返回持久化死信存储类型名。
func (s *asyncDeadLetterSink[T, F]) StoreName() string {
	if s.store == nil {
		return ""
	}
	return fmt.Sprintf("%T", s.store)
}

// LastError 返回最近一次死信持久化错误。
func (s *asyncDeadLetterSink[T, F]) LastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storeError
}

// CheckStore 检查死信存储可用性。
func (s *asyncDeadLetterSink[T, F]) CheckStore(ctx context.Context) error {
	return Check(ctx, s.store)
}

// RecordKey 计算并归一化死信记录主键。
func (s *asyncDeadLetterSink[T, F]) RecordKey(record T) (string, error) {
	key, err := RecordKey(s.recordKey, record)
	if err != nil {
		return "", err
	}
	return s.NormalizeRecordKey(key), nil
}

// NormalizeRecordKey 归一化死信主键。
func (s *asyncDeadLetterSink[T, F]) NormalizeRecordKey(key string) string {
	if s.normalizeKey == nil {
		return key
	}
	return s.normalizeKey(key)
}

// NormalizeFields 归一化死信字段列表。
func (s *asyncDeadLetterSink[T, F]) NormalizeFields(fields []F) []F {
	if s.normalizeFields == nil {
		return append([]F(nil), fields...)
	}
	return s.normalizeFields(fields)
}

func (s *asyncDeadLetterSink[T, F]) cloneDeadLetter(dead AsyncStoreDeadLetter[T, F]) AsyncStoreDeadLetter[T, F] {
	dead.Profile = s.clone(dead.Profile)
	dead.Fields = s.NormalizeFields(dead.Fields)
	return dead
}

func (s *asyncDeadLetterSink[T, F]) enforceLimitLocked() []string {
	var evicted []string
	for s.limit > 0 && len(s.deadLetters) > s.limit && len(s.deadOrder) > 0 {
		evict := s.deadOrder[0]
		s.deadOrder = s.deadOrder[1:]
		delete(s.deadLetters, evict)
		evicted = append(evicted, evict)
	}
	return evicted
}

func (s *asyncDeadLetterSink[T, F]) removeOrderLocked(accountID string) {
	for i, id := range s.deadOrder {
		if id == accountID {
			s.deadOrder = append(s.deadOrder[:i], s.deadOrder[i+1:]...)
			return
		}
	}
}

func (s *asyncDeadLetterSink[T, F]) persist(dead AsyncStoreDeadLetter[T, F]) error {
	if s.store == nil {
		return nil
	}
	ctx, cancel := s.storeContext()
	err := s.store.Save(ctx, s.cloneDeadLetter(dead))
	cancel()
	if err != nil {
		s.setStoreError(err)
		return err
	}
	s.setStoreError(nil)
	return nil
}

func (s *asyncDeadLetterSink[T, F]) deletePersisted(accountID string) {
	if s.store == nil {
		return
	}
	ctx, cancel := s.storeContext()
	err := s.store.Delete(ctx, accountID)
	cancel()
	if err != nil {
		s.setStoreError(err)
		return
	}
	s.setStoreError(nil)
}

func (s *asyncDeadLetterSink[T, F]) deletePersistedMany(accountIDs []string) {
	for _, accountID := range accountIDs {
		s.deletePersisted(accountID)
	}
}

func (s *asyncDeadLetterSink[T, F]) storeContext() (context.Context, context.CancelFunc) {
	timeout := defAsyncFlush
	if s != nil && s.storeTimeout > 0 {
		timeout = s.storeTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (s *asyncDeadLetterSink[T, F]) rollbackAdd(accountID string, previous AsyncStoreDeadLetter[T, F], existed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existed {
		s.deadLetters[accountID] = previous
		return
	}
	delete(s.deadLetters, accountID)
	s.removeOrderLocked(accountID)
}

func (s *asyncDeadLetterSink[T, F]) setStoreError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.storeError = ""
		return
	}
	s.storeError = err.Error()
}
