package player

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/db"
)

const (
	sumQueueMax      = 4096
	sumFlushTimeout  = 3 * time.Second
	sumRetryInterval = 200 * time.Millisecond
)

var (
	errSumStoreNil = errors.New("player summary store is nil")
	errSumClosed   = errors.New("player summary queue is closed")
	errSumFull     = errors.New("player summary queue is full")
)

// AsyncSummaryStore 把玩家摘要写入合并到后台，避免远端读模型拖慢玩家热路径。
type AsyncSummaryStore struct {
	base    SummaryStore
	max     int
	timeout time.Duration

	mu      sync.RWMutex
	pending map[string]sumPending
	roles   map[string]string
	seq     uint64
	closed  bool

	wake chan struct{}
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// NewAsyncSummaryStore 创建异步玩家摘要读模型存储。
func NewAsyncSummaryStore(base SummaryStore) *AsyncSummaryStore {
	store := &AsyncSummaryStore{
		base:    base,
		max:     sumQueueMax,
		timeout: sumFlushTimeout,
		pending: make(map[string]sumPending),
		roles:   make(map[string]string),
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go store.run()
	return store
}

// SavePlayerSummary 合并玩家摘要并唤醒后台刷新。
func (s *AsyncSummaryStore) SavePlayerSummary(ctx context.Context, summary PlayerSummary) error {
	if s == nil || s.base == nil {
		return errSumStoreNil
	}
	ctx = summaryCtx(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	summary = normPlayerSummaryID(summary)
	if summary.AccountID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errSumClosed
	}
	if _, ok := s.pending[summary.AccountID]; !ok && len(s.pending) >= s.max {
		return errSumFull
	}
	s.putLocked(summary)
	s.wakeLocked()
	return nil
}

// GetPlayerSummary 优先读取尚未落远端的最新摘要。
func (s *AsyncSummaryStore) GetPlayerSummary(ctx context.Context, accountID string) (PlayerSummary, bool, error) {
	if s == nil || s.base == nil {
		return PlayerSummary{}, false, errSumStoreNil
	}
	ctx = summaryCtx(ctx)
	if err := ctx.Err(); err != nil {
		return PlayerSummary{}, false, err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return PlayerSummary{}, false, nil
	}
	if summary, ok := s.pendingByAcct(accountID); ok {
		return summary, true, nil
	}
	return s.base.GetPlayerSummary(ctx, accountID)
}

// GetPlayerSummaryByRoleID 优先读取尚未落远端的角色索引。
func (s *AsyncSummaryStore) GetPlayerSummaryByRoleID(ctx context.Context, roleID string) (PlayerSummary, bool, error) {
	if s == nil || s.base == nil {
		return PlayerSummary{}, false, errSumStoreNil
	}
	ctx = summaryCtx(ctx)
	if err := ctx.Err(); err != nil {
		return PlayerSummary{}, false, err
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return PlayerSummary{}, false, nil
	}
	if summary, ok := s.pendingByRole(roleID); ok {
		return summary, true, nil
	}
	summary, ok, err := s.base.GetPlayerSummaryByRoleID(ctx, roleID)
	if err != nil || !ok {
		return summary, ok, err
	}
	if pending, pendingOK := s.pendingByAcct(summary.AccountID); pendingOK && pending.RoleID != roleID {
		return PlayerSummary{}, false, nil
	}
	return summary, true, nil
}

// ListPlayerSummariesByAccountIDs 批量读取时合并 pending 摘要。
func (s *AsyncSummaryStore) ListPlayerSummariesByAccountIDs(ctx context.Context, accountIDs []string) ([]PlayerSummary, error) {
	if s == nil || s.base == nil {
		return nil, errSumStoreNil
	}
	ctx = summaryCtx(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base, err := s.base.ListPlayerSummariesByAccountIDs(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	ids := normSummaryIDs(accountIDs)
	byAcct := make(map[string]PlayerSummary, len(base)+len(ids))
	for _, summary := range base {
		summary = normPlayerSummaryID(summary)
		if summary.AccountID != "" {
			byAcct[summary.AccountID] = clonePlayerSummary(summary)
		}
	}
	for _, summary := range s.pendingByAccts(ids) {
		byAcct[summary.AccountID] = summary
	}
	out := make([]PlayerSummary, 0, len(ids))
	for _, accountID := range ids {
		if summary, ok := byAcct[accountID]; ok {
			out = append(out, clonePlayerSummary(summary))
		}
	}
	sortPlayerSummaries(out)
	return out, nil
}

// ListByRoleIDs 批量读取角色索引时合并 pending 摘要并屏蔽旧索引。
func (s *AsyncSummaryStore) ListByRoleIDs(ctx context.Context, roleIDs []string) ([]PlayerSummary, error) {
	if s == nil || s.base == nil {
		return nil, errSumStoreNil
	}
	ctx = summaryCtx(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base, err := s.base.ListByRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	ids := normSummaryIDs(roleIDs)
	byRole := make(map[string]PlayerSummary, len(base)+len(ids))
	for _, summary := range base {
		summary = normPlayerSummaryID(summary)
		if summary.RoleID != "" {
			byRole[summary.RoleID] = clonePlayerSummary(summary)
		}
	}
	pending := s.pendingAll()
	pendingByAcct := make(map[string]PlayerSummary, len(pending))
	for _, summary := range pending {
		pendingByAcct[summary.AccountID] = summary
	}
	for roleID, summary := range byRole {
		if pending, ok := pendingByAcct[summary.AccountID]; ok && pending.RoleID != roleID {
			delete(byRole, roleID)
		}
	}
	for _, pending := range pending {
		if pending.RoleID != "" {
			byRole[pending.RoleID] = pending
		}
	}
	out := make([]PlayerSummary, 0, len(ids))
	for _, roleID := range ids {
		if summary, ok := byRole[roleID]; ok {
			out = append(out, clonePlayerSummary(summary))
		}
	}
	sortPlayerSummaries(out)
	return out, nil
}

// SearchPlayerSummaries 搜索时合并 pending 摘要，保证读到最新公开状态。
func (s *AsyncSummaryStore) SearchPlayerSummaries(ctx context.Context, query PlayerSummaryQuery) ([]PlayerSummary, error) {
	if s == nil || s.base == nil {
		return nil, errSumStoreNil
	}
	if len(query.AccountIDs) > 0 {
		summaries, err := s.ListPlayerSummariesByAccountIDs(ctx, query.AccountIDs)
		return filterSummaries(summaries, query), err
	}
	if len(query.RoleIDs) > 0 {
		summaries, err := s.ListByRoleIDs(ctx, query.RoleIDs)
		return filterSummaries(summaries, query), err
	}
	ctx = summaryCtx(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base, err := s.base.SearchPlayerSummaries(ctx, query)
	if err != nil {
		return nil, err
	}
	byAcct := make(map[string]PlayerSummary, len(base))
	for _, summary := range base {
		summary = normPlayerSummaryID(summary)
		if summary.AccountID != "" {
			byAcct[summary.AccountID] = clonePlayerSummary(summary)
		}
	}
	for _, pending := range s.pendingAll() {
		if summaryMatches(pending, query) {
			byAcct[pending.AccountID] = pending
			continue
		}
		delete(byAcct, pending.AccountID)
	}
	out := make([]PlayerSummary, 0, len(byAcct))
	for _, summary := range byAcct {
		out = append(out, clonePlayerSummary(summary))
	}
	sortPlayerSummaries(out)
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out, nil
}

// Flush 同步刷新所有待写摘要。
func (s *AsyncSummaryStore) Flush(ctx context.Context) error {
	if s == nil || s.base == nil {
		return errSumStoreNil
	}
	ctx = summaryCtx(ctx)
	for {
		batch := s.takePending()
		if len(batch) == 0 {
			return nil
		}
		for _, summary := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.base.SavePlayerSummary(ctx, summary.summary); err != nil {
				return err
			}
			s.removeSaved(summary)
		}
	}
}

// Close 停止后台刷新，排空 pending 后关闭底层存储。
func (s *AsyncSummaryStore) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	closeCtx, cancel := s.closeContext(ctx)
	defer cancel()
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		close(s.stop)
	})
	select {
	case <-s.done:
	case <-closeCtx.Done():
		return closeCtx.Err()
	}
	return errors.Join(s.Flush(closeCtx), db.CloseOrFlush(closeCtx, s.base))
}

func (s *AsyncSummaryStore) closeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx = summaryCtx(ctx)
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	timeout := sumFlushTimeout
	if s.timeout > 0 {
		timeout = s.timeout
	}
	return context.WithTimeout(ctx, timeout)
}

// Check 透传底层摘要存储预检。
func (s *AsyncSummaryStore) Check(ctx context.Context) error {
	if s == nil || s.base == nil {
		return errSumStoreNil
	}
	return db.Check(ctx, s.base)
}

func (s *AsyncSummaryStore) run() {
	defer close(s.done)
	retry := time.NewTimer(time.Hour)
	stopSumTimer(retry)
	var retryC <-chan time.Time
	defer stopSumTimer(retry)
	for {
		select {
		case <-s.wake:
			if err := s.flushBackground(); err != nil {
				resetSumTimer(retry, sumRetryInterval)
				retryC = retry.C
			} else {
				retryC = nil
			}
		case <-retryC:
			retryC = nil
			if err := s.flushBackground(); err != nil {
				resetSumTimer(retry, sumRetryInterval)
				retryC = retry.C
			}
		case <-s.stop:
			return
		}
	}
}

func (s *AsyncSummaryStore) flushBackground() error {
	timeout := s.timeout
	if timeout <= 0 {
		timeout = sumFlushTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	err := s.Flush(ctx)
	cancel()
	return err
}

func (s *AsyncSummaryStore) putLocked(summary PlayerSummary) {
	if old, ok := s.pending[summary.AccountID]; ok && old.summary.RoleID != "" && old.summary.RoleID != summary.RoleID {
		delete(s.roles, old.summary.RoleID)
	}
	s.seq++
	s.pending[summary.AccountID] = sumPending{
		summary: clonePlayerSummary(summary),
		seq:     s.seq,
	}
	if summary.RoleID != "" {
		s.roles[summary.RoleID] = summary.AccountID
	}
}

func (s *AsyncSummaryStore) wakeLocked() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *AsyncSummaryStore) pendingByAcct(accountID string) (PlayerSummary, bool) {
	s.mu.RLock()
	pending, ok := s.pending[accountID]
	s.mu.RUnlock()
	return clonePlayerSummary(pending.summary), ok
}

func (s *AsyncSummaryStore) pendingByRole(roleID string) (PlayerSummary, bool) {
	s.mu.RLock()
	accountID := s.roles[roleID]
	pending, ok := s.pending[accountID]
	s.mu.RUnlock()
	if !ok || pending.summary.RoleID != roleID {
		return PlayerSummary{}, false
	}
	return clonePlayerSummary(pending.summary), true
}

func (s *AsyncSummaryStore) pendingByAccts(accountIDs []string) []PlayerSummary {
	if len(accountIDs) == 0 {
		return nil
	}
	out := make([]PlayerSummary, 0, len(accountIDs))
	s.mu.RLock()
	for _, accountID := range accountIDs {
		if pending, ok := s.pending[accountID]; ok {
			out = append(out, clonePlayerSummary(pending.summary))
		}
	}
	s.mu.RUnlock()
	sortPlayerSummaries(out)
	return out
}

func (s *AsyncSummaryStore) pendingAll() []PlayerSummary {
	s.mu.RLock()
	out := make([]PlayerSummary, 0, len(s.pending))
	for _, pending := range s.pending {
		out = append(out, clonePlayerSummary(pending.summary))
	}
	s.mu.RUnlock()
	sortPlayerSummaries(out)
	return out
}

func (s *AsyncSummaryStore) takePending() []sumWrite {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.pending) == 0 {
		return nil
	}
	out := make([]sumWrite, 0, len(s.pending))
	for _, pending := range s.pending {
		out = append(out, sumWrite{
			summary: clonePlayerSummary(pending.summary),
			seq:     pending.seq,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].summary.AccountID == out[j].summary.AccountID {
			return out[i].summary.RoleID < out[j].summary.RoleID
		}
		return out[i].summary.AccountID < out[j].summary.AccountID
	})
	return out
}

func (s *AsyncSummaryStore) removeSaved(saved sumWrite) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.pending[saved.summary.AccountID]
	if !ok || pending.seq != saved.seq {
		return
	}
	delete(s.pending, saved.summary.AccountID)
	if saved.summary.RoleID != "" && s.roles[saved.summary.RoleID] == saved.summary.AccountID {
		delete(s.roles, saved.summary.RoleID)
	}
}

func wrapSummaryStore(store SummaryStore) SummaryStore {
	switch store.(type) {
	case nil, *MemorySummaryStore, *AsyncSummaryStore:
		return store
	default:
		return NewAsyncSummaryStore(store)
	}
}

func normSummaryIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func summaryCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func resetSumTimer(timer *time.Timer, delay time.Duration) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func stopSumTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

type sumPending struct {
	summary PlayerSummary
	seq     uint64
}

type sumWrite struct {
	summary PlayerSummary
	seq     uint64
}
