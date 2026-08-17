package idempotency

import (
	"context"
	"sync"
	"time"
)

type memoryStore struct {
	mu         sync.Mutex
	ttl        time.Duration
	now        func() time.Time
	seen       map[string]memorySeenEntry
	results    map[string]memoryResult
	pending    map[string]pendingMemoryEntry
	highest    map[string]memSeqWatermark
	pendingSeq map[string]map[uint64]int
}

type memoryResult struct {
	payload   []byte
	expiresAt time.Time
}

type memorySeenEntry struct {
	fingerprint string
	expiresAt   time.Time
}

type pendingMemoryEntry struct {
	fingerprint string
	ownerToken  string
	expiresAt   time.Time
	seqScope    string
	sequence    uint64
}

type memSeqWatermark struct {
	sequence  uint64
	expiresAt time.Time
}

func newMemoryStore(ttl time.Duration, now func() time.Time) *memoryStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &memoryStore{
		ttl:        ttl,
		now:        now,
		seen:       make(map[string]memorySeenEntry),
		results:    make(map[string]memoryResult),
		pending:    make(map[string]pendingMemoryEntry),
		highest:    make(map[string]memSeqWatermark),
		pendingSeq: make(map[string]map[uint64]int),
	}
}

func (s *memoryStore) Check(ctx context.Context, item Request) (Decision, error) {
	if s == nil {
		return Decision{}, ErrStoreRequired
	}
	decision, err := s.Begin(ctx, item)
	if err != nil {
		return Decision{}, err
	}
	if decision.Status == StatusAccepted {
		if err := s.Commit(ctx, item, decision); err != nil {
			return Decision{}, err
		}
	}
	return decision, nil
}

func (s *memoryStore) Begin(ctx context.Context, item Request) (Decision, error) {
	if err := ctxErr(ctx); err != nil {
		return Decision{}, err
	}
	if s == nil {
		return Decision{}, ErrStoreRequired
	}
	key := item.Key
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	seqScope := sequenceScope(item.Scope, item.Subject, item.Session)

	// Begin 只写 pending 占位：同 key 重入返回 in_flight，同序列低水位返回 duplicate/replay，handler 成功后才进入 committed。
	s.mu.Lock()
	s.ensureReadyLocked()
	now := item.Now
	if now.IsZero() {
		now = s.now().UTC()
	} else {
		now = now.UTC()
	}
	s.pruneLocked(now)
	if seen, ok := s.seen[key]; ok && seen.expiresAt.After(now) {
		if reqFPConflict(seen.fingerprint, item.Fingerprint) {
			s.mu.Unlock()
			return Decision{}, ErrRequestConflict
		}
		s.mu.Unlock()
		return Decision{Status: StatusDuplicate, Key: key, Sequence: item.Sequence}, nil
	}
	if pending, ok := s.pending[key]; ok && pending.expiresAt.After(now) {
		if reqFPConflict(pending.fingerprint, item.Fingerprint) {
			s.mu.Unlock()
			return Decision{}, ErrRequestConflict
		}
		s.mu.Unlock()
		return Decision{Status: StatusInFlight, Key: key, Sequence: item.Sequence}, nil
	}
	if item.Sequence > 0 {
		if highest, ok := s.highest[seqScope]; ok && highest.expiresAt.After(now) && highest.sequence >= item.Sequence {
			s.seen[key] = memorySeenEntry{fingerprint: item.Fingerprint, expiresAt: now.Add(s.ttl)}
			s.mu.Unlock()
			return Decision{Status: StatusReplay, Key: key, Sequence: item.Sequence}, nil
		}
		if highestPendingSeq(s.pendingSeq[seqScope]) > 0 {
			s.mu.Unlock()
			return Decision{Status: StatusInFlight, Key: key, Sequence: item.Sequence}, nil
		}
	}
	s.pending[key] = pendingMemoryEntry{fingerprint: item.Fingerprint, ownerToken: item.reservationToken, expiresAt: now.Add(s.ttl), seqScope: seqScope, sequence: item.Sequence}
	if item.Sequence > 0 {
		if s.pendingSeq[seqScope] == nil {
			s.pendingSeq[seqScope] = make(map[uint64]int)
		}
		s.pendingSeq[seqScope][item.Sequence]++
	}
	s.mu.Unlock()
	return Decision{Status: StatusAccepted, Key: key, Sequence: item.Sequence, ownerToken: item.reservationToken}, nil
}

func (s *memoryStore) Commit(ctx context.Context, item Request, decision Decision) error {
	return s.commit(ctx, item, decision, nil, false)
}

func (s *memoryStore) CommitResult(ctx context.Context, item Request, decision Decision, payload []byte) error {
	return s.commit(ctx, item, decision, payload, true)
}

func (s *memoryStore) commit(ctx context.Context, item Request, decision Decision, payload []byte, withResult bool) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	key := decision.Key
	if key == "" {
		key = item.Key
	}
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	seqScope := sequenceScope(item.Scope, item.Subject, item.Session)
	s.mu.Lock()
	s.ensureReadyLocked()
	// committed/result TTL 从真实提交时刻起算，不使用请求入口时间缩短重放窗口。
	now := s.now().UTC()
	s.pruneLocked(now)
	pending, pendingExists := s.pending[key]
	if !resvOwnerMatch(pendingExists, pending.ownerToken, decision.ownerToken) {
		s.mu.Unlock()
		return ErrReservationLost
	}
	if seen, ok := s.seen[key]; ok && reqFPConflict(seen.fingerprint, item.Fingerprint) {
		s.mu.Unlock()
		return ErrRequestConflict
	}
	if pendingExists && reqFPConflict(pending.fingerprint, item.Fingerprint) {
		s.mu.Unlock()
		return ErrRequestConflict
	}
	// Commit 先移除 pending，再写 seen 和最高 sequence 水位；重试包会被 duplicate/replay 拦住。
	s.removePendingLocked(key)
	s.seen[key] = memorySeenEntry{fingerprint: item.Fingerprint, expiresAt: now.Add(s.ttl)}
	if withResult {
		s.results[key] = memoryResult{payload: append([]byte(nil), payload...), expiresAt: now.Add(s.ttl)}
	} else {
		delete(s.results, key)
	}
	if item.Sequence > 0 {
		current, ok := s.highest[seqScope]
		expiresAt := now.Add(s.ttl)
		if !ok || !current.expiresAt.After(now) || current.sequence < item.Sequence {
			s.highest[seqScope] = memSeqWatermark{sequence: item.Sequence, expiresAt: expiresAt}
		} else {
			current.expiresAt = expiresAt
			s.highest[seqScope] = current
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *memoryStore) LookupResult(ctx context.Context, decision Decision) ([]byte, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, false, err
	}
	if s == nil {
		return nil, false, ErrStoreRequired
	}
	key := decision.Key
	if key == "" {
		return nil, false, nil
	}
	s.mu.Lock()
	s.ensureReadyLocked()
	now := s.now().UTC()
	s.pruneLocked(now)
	result, ok := s.results[key]
	s.mu.Unlock()
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), result.payload...), true, nil
}

func (s *memoryStore) Abort(ctx context.Context, item Request, decision Decision) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	key := decision.Key
	if key == "" {
		key = item.Key
	}
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	s.mu.Lock()
	s.ensureReadyLocked()
	now := s.now().UTC()
	s.pruneLocked(now)
	pending, pendingExists := s.pending[key]
	if !resvOwnerMatch(pendingExists, pending.ownerToken, decision.ownerToken) {
		s.mu.Unlock()
		return ErrReservationLost
	}
	if pendingExists && reqFPConflict(pending.fingerprint, item.Fingerprint) {
		s.mu.Unlock()
		return ErrRequestConflict
	}
	// Abort 只撤销本次 pending，不推进水位，保证 handler 失败后客户端重试仍可重新执行业务。
	s.removePendingLocked(key)
	s.mu.Unlock()
	return nil
}

func (s *memoryStore) Snapshot() map[string]any {
	if s == nil {
		return map[string]any{}
	}
	s.mu.Lock()
	s.ensureReadyLocked()
	now := s.now().UTC()
	s.pruneLocked(now)
	out := map[string]any{
		"backend":     "memory",
		"ttl_seconds": int64(s.ttl / time.Second),
		"seen":        len(s.seen),
		"pending":     len(s.pending),
		"scopes":      len(s.highest),
	}
	s.mu.Unlock()
	return out
}

func (s *memoryStore) pruneLocked(now time.Time) {
	for key, seen := range s.seen {
		if !seen.expiresAt.After(now) {
			delete(s.seen, key)
		}
	}
	for key, result := range s.results {
		if !result.expiresAt.After(now) {
			delete(s.results, key)
		}
	}
	for key, pending := range s.pending {
		if !pending.expiresAt.After(now) {
			s.removePendingLocked(key)
		}
	}
	for seqScope, highest := range s.highest {
		if !highest.expiresAt.After(now) {
			delete(s.highest, seqScope)
		}
	}
}

func (s *memoryStore) removePendingLocked(key string) {
	pending, ok := s.pending[key]
	if !ok {
		return
	}
	// pendingSeq 是按序列反查 in-flight 的辅助索引，删除 pending 时必须同步清理，避免后续序列误判冲突。
	delete(s.pending, key)
	if pending.sequence == 0 {
		return
	}
	sequences := s.pendingSeq[pending.seqScope]
	if sequences == nil {
		return
	}
	if sequences[pending.sequence] <= 1 {
		delete(sequences, pending.sequence)
	} else {
		sequences[pending.sequence]--
	}
	if len(sequences) == 0 {
		delete(s.pendingSeq, pending.seqScope)
	}
}

func highestPendingSeq(sequences map[uint64]int) uint64 {
	var highest uint64
	for sequence, count := range sequences {
		if count > 0 && sequence > highest {
			highest = sequence
		}
	}
	return highest
}

func (s *memoryStore) ensureReadyLocked() {
	if s.ttl <= 0 {
		s.ttl = 10 * time.Minute
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.seen == nil {
		s.seen = make(map[string]memorySeenEntry)
	}
	if s.results == nil {
		s.results = make(map[string]memoryResult)
	}
	if s.pending == nil {
		s.pending = make(map[string]pendingMemoryEntry)
	}
	if s.highest == nil {
		s.highest = make(map[string]memSeqWatermark)
	}
	if s.pendingSeq == nil {
		s.pendingSeq = make(map[string]map[uint64]int)
	}
}
