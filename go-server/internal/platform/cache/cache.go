package cache

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrKeyRequired    = errors.New("cache key is required")
	ErrNilMemoryStore = errors.New("cache memory store is required")
)

type Store interface {
	Get(context.Context, string) ([]byte, bool, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
	Snapshot() Snapshot
	Close() error
}

type TakeStore interface {
	Take(context.Context, string) ([]byte, bool, error)
}

type Snapshot struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Keys       int    `json:"keys,omitempty"`
	MaxEntries int    `json:"max_entries,omitempty"`
	KeyPrefix  string `json:"key_prefix,omitempty"`
	Closed     bool   `json:"closed,omitempty"`
}

type MemoryOptions struct {
	Name       string
	DefaultTTL time.Duration
	MaxEntries int
	Now        func() time.Time
}

type MemoryStore struct {
	mu         sync.RWMutex
	initOnce   sync.Once
	name       string
	defaultTTL time.Duration
	maxEntries int
	now        func() time.Time
	items      map[string]memoryItem
	closed     bool
	metrics    *Metrics
}

type memoryItem struct {
	value      []byte
	expiresAt  time.Time
	lastAccess *atomic.Int64
}

func NewMemory(options MemoryOptions) *MemoryStore {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "memory-cache"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{
		name:       name,
		defaultTTL: options.DefaultTTL,
		maxEntries: options.MaxEntries,
		now:        now,
		items:      make(map[string]memoryItem),
	}
}

func (s *MemoryStore) SetMetrics(m *Metrics) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.metrics = m
	s.mu.Unlock()
}

func (s *MemoryStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, false, err
	}
	key = normalizeKey(key)
	if key == "" {
		return nil, false, ErrKeyRequired
	}
	if err := s.ready(); err != nil {
		return nil, false, err
	}
	now := s.now().UTC()

	s.mu.RLock()
	item, ok := s.items[key]
	closed := s.closed
	metricsRef := s.metrics
	if closed {
		s.mu.RUnlock()
		metricsRef.recordMiss()
		return nil, false, nil
	}
	if !ok {
		s.mu.RUnlock()
		metricsRef.recordMiss()
		return nil, false, nil
	}
	if expired(item, now) {
		s.mu.RUnlock()
		s.deleteExpired(key, now)
		metricsRef.recordEviction()
		metricsRef.recordMiss()
		return nil, false, nil
	}
	value := cloneBytes(item.value)
	lastAccess := item.lastAccess
	s.mu.RUnlock()
	storeLastAccess(lastAccess, now)
	metricsRef.recordHit()
	return value, true, nil
}

func (s *MemoryStore) Take(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, false, err
	}
	key = normalizeKey(key)
	if key == "" {
		return nil, false, ErrKeyRequired
	}
	if err := s.ready(); err != nil {
		return nil, false, err
	}
	now := s.now().UTC()

	s.mu.Lock()
	item, ok := s.items[key]
	closed := s.closed
	metricsRef := s.metrics
	if closed {
		s.mu.Unlock()
		metricsRef.recordMiss()
		return nil, false, nil
	}
	if !ok {
		s.mu.Unlock()
		metricsRef.recordMiss()
		return nil, false, nil
	}
	if expired(item, now) {
		delete(s.items, key)
		s.mu.Unlock()
		metricsRef.recordEviction()
		metricsRef.recordMiss()
		return nil, false, nil
	}
	delete(s.items, key)
	value := cloneBytes(item.value)
	s.mu.Unlock()
	metricsRef.recordHit()
	return value, true, nil
}

func (s *MemoryStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	key = normalizeKey(key)
	if key == "" {
		return ErrKeyRequired
	}
	if err := s.ready(); err != nil {
		return err
	}
	if ttl == 0 {
		ttl = s.defaultTTL
	}
	now := s.now().UTC()
	item := memoryItem{value: cloneBytes(value), lastAccess: newAccessClock(now)}
	if ttl > 0 {
		item.expiresAt = now.Add(ttl)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.items[key] = item
	evicted := s.evictOverflowLocked()
	metricsRef := s.metrics
	s.mu.Unlock()
	for idx := 0; idx < evicted; idx++ {
		metricsRef.recordEviction()
	}
	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	key = normalizeKey(key)
	if key == "" {
		return ErrKeyRequired
	}
	if err := s.ready(); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.items, key)
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Snapshot() Snapshot {
	if err := s.ready(); err != nil {
		return Snapshot{Kind: "memory"}
	}
	now := s.now().UTC()
	s.mu.Lock()
	metricsRef := s.metrics
	evicted := 0
	for key, item := range s.items {
		if expired(item, now) {
			delete(s.items, key)
			evicted++
		}
	}
	snapshot := Snapshot{
		Name:       s.name,
		Kind:       "memory",
		Keys:       len(s.items),
		MaxEntries: s.maxEntries,
		Closed:     s.closed,
	}
	s.mu.Unlock()
	for idx := 0; idx < evicted; idx++ {
		metricsRef.recordEviction()
	}
	return snapshot
}

func (s *MemoryStore) Close() error {
	if s == nil {
		return nil
	}
	if err := s.ready(); err != nil {
		return err
	}
	s.mu.Lock()
	s.closed = true
	s.items = make(map[string]memoryItem)
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) ready() error {
	if s == nil {
		return ErrNilMemoryStore
	}
	s.initOnce.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.name = strings.TrimSpace(s.name)
		if s.name == "" {
			s.name = "memory-cache"
		}
		if s.now == nil {
			s.now = time.Now
		}
		if s.items == nil {
			s.items = make(map[string]memoryItem)
		}
	})
	return nil
}

func normalizeKey(key string) string {
	return strings.TrimSpace(key)
}

func expired(item memoryItem, now time.Time) bool {
	return !item.expiresAt.IsZero() && !item.expiresAt.After(now)
}

func (s *MemoryStore) evictOverflowLocked() int {
	if s.maxEntries <= 0 {
		return 0
	}
	overflow := len(s.items) - s.maxEntries
	if overflow <= 0 {
		return 0
	}
	keys := make([]string, 0, len(s.items))
	for key := range s.items {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := itemAccessTime(s.items[keys[i]])
		right := itemAccessTime(s.items[keys[j]])
		if left.Equal(right) {
			return keys[i] < keys[j]
		}
		return left.Before(right)
	})
	if overflow > len(keys) {
		overflow = len(keys)
	}
	for idx := 0; idx < overflow; idx++ {
		delete(s.items, keys[idx])
	}
	return overflow
}

func newAccessClock(now time.Time) *atomic.Int64 {
	clock := &atomic.Int64{}
	storeLastAccess(clock, now)
	return clock
}

func storeLastAccess(clock *atomic.Int64, now time.Time) {
	if clock != nil {
		clock.Store(now.UnixNano())
	}
}

func itemAccessTime(item memoryItem) time.Time {
	if item.lastAccess != nil {
		if unixNano := item.lastAccess.Load(); unixNano > 0 {
			return time.Unix(0, unixNano).UTC()
		}
	}
	return item.expiresAt
}

func (s *MemoryStore) deleteExpired(key string, now time.Time) {
	s.mu.Lock()
	item, ok := s.items[key]
	if ok && expired(item, now) {
		delete(s.items, key)
	}
	s.mu.Unlock()
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), value...)
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
