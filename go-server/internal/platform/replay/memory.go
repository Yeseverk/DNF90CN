package replay

import (
	"container/heap"
	"container/list"
	"context"
	"sync"
	"time"
)

const defaultMemMaxEntries = 1024

type memoryEntry struct {
	key        string
	expiresAt  time.Time
	generation uint64
}

// MemoryStore 是单进程 LRU + TTL nonce 表。它只适合测试、单节点后台或降级路径；
// 多 gateway / logic 实例必须换成共享 Redis，否则不同进程会各自接受同一 nonce。
type MemoryStore struct {
	mu         sync.Mutex
	maxEntries int
	items      map[string]*list.Element
	order      *list.List
	expiry     expiryHeap
	generation uint64
	now        func() time.Time
}

// NewMemoryStore 创建进程内 nonce 表。maxEntries<=0 时使用保守默认值，避免调用方
// 因配置漏填得到一个永远无法拒重的空表。
func NewMemoryStore(maxEntries int) *MemoryStore {
	if maxEntries <= 0 {
		maxEntries = defaultMemMaxEntries
	}
	return &MemoryStore{
		maxEntries: maxEntries,
		items:      make(map[string]*list.Element, maxEntries),
		order:      list.New(),
		expiry:     expiryHeap{},
		now:        time.Now,
	}
}

// Seen 记录 nonce 并返回它是否首次出现；重复 nonce 会返回 false。
func (s *MemoryStore) Seen(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	_, key, err := normalizeInput(ctx, key, ttl)
	if err != nil {
		return false, err
	}
	if s == nil {
		return false, ErrReplayStoreUnavailable
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	now := s.now().UTC()
	s.pruneExpiredLocked(now)
	if existing := s.items[key]; existing != nil {
		entry := existing.Value.(memoryEntry)
		if entry.expiresAt.After(now) {
			s.order.MoveToFront(existing)
			return false, nil
		}
		s.removeLocked(existing)
	}
	if len(s.items) >= s.maxEntries {
		return false, ErrReplayStoreFull
	}
	s.generation++
	entry := memoryEntry{key: key, expiresAt: now.Add(ttl), generation: s.generation}
	s.items[key] = s.order.PushFront(entry)
	heap.Push(&s.expiry, expiryItem{key: key, expiresAt: entry.expiresAt, generation: entry.generation})
	s.compactExpiryLocked()
	return true, nil
}

func (s *MemoryStore) ensureLocked() {
	if s.maxEntries <= 0 {
		s.maxEntries = defaultMemMaxEntries
	}
	if s.items == nil {
		s.items = make(map[string]*list.Element, s.maxEntries)
	}
	if s.order == nil {
		s.order = list.New()
	}
	if s.expiry == nil {
		s.expiry = expiryHeap{}
	}
	if s.now == nil {
		s.now = time.Now
	}
}

func (s *MemoryStore) pruneExpiredLocked(now time.Time) {
	for s.expiry.Len() > 0 {
		next := s.expiry[0]
		if next.expiresAt.After(now) {
			return
		}
		heap.Pop(&s.expiry)
		element := s.items[next.key]
		if element == nil {
			continue
		}
		entry := element.Value.(memoryEntry)
		if entry.generation != next.generation || !entry.expiresAt.Equal(next.expiresAt) {
			continue
		}
		s.removeLocked(element)
	}
}

func (s *MemoryStore) compactExpiryLocked() {
	if s.expiry.Len() <= len(s.items)*2+defaultMemMaxEntries {
		return
	}
	// expiry 是惰性堆：更新会留下旧 generation，直到 prune 时才跳过。
	// 从 LRU 链表重建可以防止长进程积累无界的过期记录尾巴。
	expiry := make(expiryHeap, 0, len(s.items))
	for element := s.order.Front(); element != nil; element = element.Next() {
		entry := element.Value.(memoryEntry)
		expiry = append(expiry, expiryItem(entry))
	}
	heap.Init(&expiry)
	s.expiry = expiry
}

func (s *MemoryStore) removeLocked(element *list.Element) {
	if element == nil {
		return
	}
	entry := element.Value.(memoryEntry)
	delete(s.items, entry.key)
	s.order.Remove(element)
}

type expiryItem struct {
	key        string
	expiresAt  time.Time
	generation uint64
}

type expiryHeap []expiryItem

// Len 返回过期堆里的候选记录数量。
func (h expiryHeap) Len() int { return len(h) }

// Less 按过期时间和 generation 排序，保证最早失效的记录先被裁剪。
func (h expiryHeap) Less(i, j int) bool {
	if !h[i].expiresAt.Equal(h[j].expiresAt) {
		return h[i].expiresAt.Before(h[j].expiresAt)
	}
	return h[i].generation < h[j].generation
}

// Swap 交换两个堆节点。
func (h expiryHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

// Push 把新的过期候选记录加入堆。
func (h *expiryHeap) Push(x any) {
	*h = append(*h, x.(expiryItem))
}

// Pop 移除并返回堆尾节点，供 container/heap 维护最小堆。
func (h *expiryHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
