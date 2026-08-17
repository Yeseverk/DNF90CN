package leaderboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	name string
	now  func() time.Time
	gen  func(prefix string) string
	hist HistoryStore

	historyStrict bool
	historyErrors int

	mu          sync.Mutex
	next        uint64
	definitions map[string]Definition
	records     map[string]map[string]Record
	repairs     map[string]RepairReceipt
}

type managerStateSnapshot struct {
	definitions map[string]Definition
	records     map[string]map[string]Record
	repairs     map[string]RepairReceipt
}

type boardRankData struct {
	definition Definition
	records    map[string]Record
}

func New(options Options) *Manager {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "leaderboard"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	historyStore := options.HistoryStore
	if historyStore == nil && options.HistoryLimit >= 0 {
		historyLimit := options.HistoryLimit
		if historyLimit == 0 {
			historyLimit = defaultHistoryLimit
		}
		historyStore = NewMemoryHistoryStore(historyLimit)
	}
	historyStore = wrapHistoryStore(historyStore, options.HistoryStrict)
	return &Manager{
		name:          name,
		now:           now,
		gen:           options.IDGenerator,
		hist:          historyStore,
		historyStrict: options.HistoryStrict,
		definitions:   make(map[string]Definition),
		records:       make(map[string]map[string]Record),
		repairs:       make(map[string]RepairReceipt),
	}
}

func (m *Manager) Create(ctx context.Context, definition Definition) (Definition, error) {
	if err := ctxErr(ctx); err != nil {
		return Definition{}, err
	}
	if m == nil {
		return Definition{}, ErrManagerRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()

	normalized, err := m.normDefinitionLocked(definition)
	if err != nil {
		return Definition{}, err
	}
	if _, exists := m.definitions[normalized.ID]; exists {
		return Definition{}, ErrLeaderboardExists
	}
	before := m.snapshotStateLocked()
	m.definitions[normalized.ID] = normalized
	m.records[normalized.ID] = make(map[string]Record)
	if err := m.appendHistoryLocked(ctx, HistoryActionCreate, normalized.ID, nil, &normalized, normalized.CreatedAt); err != nil {
		m.restoreStateLocked(before)
		return Definition{}, err
	}
	return cloneDefinition(normalized), nil
}

func (m *Manager) Delete(ctx context.Context, leaderboardID string) (Definition, error) {
	if err := ctxErr(ctx); err != nil {
		return Definition{}, err
	}
	if m == nil {
		return Definition{}, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Definition{}, ErrLeaderboardNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()

	definition, ok := m.definitions[leaderboardID]
	if !ok {
		return Definition{}, ErrLeaderboardNotFound
	}
	before := m.snapshotStateLocked()
	delete(m.definitions, leaderboardID)
	delete(m.records, leaderboardID)
	if err := m.appendHistoryLocked(ctx, HistoryDeleteBoard, definition.ID, nil, &definition, m.now().UTC()); err != nil {
		m.restoreStateLocked(before)
		return Definition{}, err
	}
	return cloneDefinition(definition), nil
}

func (m *Manager) Definition(leaderboardID string) (Definition, bool) {
	leaderboardID = strings.TrimSpace(leaderboardID)
	if m == nil || leaderboardID == "" {
		return Definition{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()
	definition, ok := m.definitions[leaderboardID]
	return cloneDefinition(definition), ok
}

func (m *Manager) Definitions() []Definition {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()

	out := make([]Definition, 0, len(m.definitions))
	for _, definition := range m.definitions {
		out = append(out, cloneDefinition(definition))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) Submit(ctx context.Context, leaderboardID string, submission Submission) (Record, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, err
	}
	if m == nil {
		return Record{}, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Record{}, ErrLeaderboardNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()

	definition, ok := m.definitions[leaderboardID]
	if !ok {
		return Record{}, ErrLeaderboardNotFound
	}
	normalized, err := normalizeSubmission(submission)
	if err != nil {
		return Record{}, err
	}
	now := m.now().UTC()
	recordMap := m.records[leaderboardID]
	existing, exists := recordMap[normalized.OwnerID]
	record := Record{
		LeaderboardID: leaderboardID,
		OwnerID:       normalized.OwnerID,
		Score:         normalized.Score,
		Subscore:      normalized.Subscore,
		Metadata:      cloneStringMap(normalized.Metadata),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if exists {
		record.CreatedAt = existing.CreatedAt
		switch definition.Operator {
		case OperatorBest:
			candidate := record
			if !betterThan(candidate, existing, definition.SortOrder) {
				// Best 模式只接受更优成绩；较差提交返回当前排名，不写历史噪音。
				ranked, ok := m.recordWithRankLocked(leaderboardID, normalized.OwnerID)
				if !ok {
					return Record{}, ErrRecordNotFound
				}
				return ranked, nil
			}
		case OperatorIncrement:
			// Increment 模式用于累计榜，调用方要保证加分来源已经在关键写路径里做幂等。
			record.Score, err = addScoreValue(existing.Score, normalized.Score)
			if err != nil {
				return Record{}, err
			}
			record.Subscore, err = addScoreValue(existing.Subscore, normalized.Subscore)
			if err != nil {
				return Record{}, err
			}
		case OperatorSet:
		default:
			return Record{}, fmt.Errorf("%w: unsupported operator %q", ErrInvalidDefinition, definition.Operator)
		}
	}
	before := m.snapshotStateLocked()
	recordMap[normalized.OwnerID] = record
	var trimmed []Record
	if definition.MaxSize > 0 {
		// 内存实现按 MaxSize 裁剪只保证当前进程视角；跨服榜生产应使用 Redis/SQL 方案。
		trimmed = m.trimLocked(definition)
	}
	if err := m.appendHistoryLocked(ctx, HistoryActionSubmit, definition.ID, &record, nil, record.UpdatedAt); err != nil {
		m.restoreStateLocked(before)
		return Record{}, err
	}
	for _, trimmedRecord := range trimmed {
		if err := m.appendHistoryLocked(ctx, HistoryActionTrimRecord, definition.ID, &trimmedRecord, nil, m.now().UTC()); err != nil {
			m.restoreStateLocked(before)
			return Record{}, err
		}
	}
	ranked, ok := m.recordWithRankLocked(leaderboardID, normalized.OwnerID)
	if !ok {
		return Record{}, ErrRecordTrimmed
	}
	return ranked, nil
}

func (m *Manager) DeleteRecord(ctx context.Context, leaderboardID string, ownerID string) (Record, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, err
	}
	if m == nil {
		return Record{}, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	ownerID = strings.TrimSpace(ownerID)
	if leaderboardID == "" {
		return Record{}, ErrLeaderboardNotFound
	}
	if ownerID == "" {
		return Record{}, ErrRecordNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()

	if _, ok := m.definitions[leaderboardID]; !ok {
		return Record{}, ErrLeaderboardNotFound
	}
	record, ok := m.recordWithRankLocked(leaderboardID, ownerID)
	if !ok {
		return Record{}, ErrRecordNotFound
	}
	before := m.snapshotStateLocked()
	delete(m.records[leaderboardID], ownerID)
	if err := m.appendHistoryLocked(ctx, HistoryActionDeleteRecord, leaderboardID, &record, nil, m.now().UTC()); err != nil {
		m.restoreStateLocked(before)
		return Record{}, err
	}
	return record, nil
}

func (m *Manager) Repair(ctx context.Context, leaderboardID string, request RepairRequest) (RepairReceipt, error) {
	if err := ctxErr(ctx); err != nil {
		return RepairReceipt{}, err
	}
	if m == nil {
		return RepairReceipt{}, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return RepairReceipt{}, ErrLeaderboardNotFound
	}
	normalized, err := normRepairReq(request)
	if err != nil {
		return RepairReceipt{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()

	definition, ok := m.definitions[leaderboardID]
	if !ok {
		return RepairReceipt{}, ErrLeaderboardNotFound
	}
	repairKey := repairIdempotencyKey(leaderboardID, normalized.RequestID)
	if repairKey != "" {
		// GM 修榜必须支持 request_id 重放，避免审批系统或网络重试重复修改成绩。
		if previous, ok := m.repairs[repairKey]; ok {
			return cloneRepairReceipt(previous), nil
		}
	}
	now := m.now().UTC()
	receipt := RepairReceipt{
		LeaderboardID: leaderboardID,
		OwnerID:       normalized.OwnerID,
		Action:        HistoryActionRepairRecord,
		Reason:        normalized.Reason,
		OperatorID:    normalized.OperatorID,
		RequestID:     normalized.RequestID,
		Metadata:      cloneStringMap(normalized.Metadata),
		At:            now,
	}
	if before, found := m.recordWithRankLocked(leaderboardID, normalized.OwnerID); found {
		before := before
		receipt.Before = &before
	}
	stateBefore := m.snapshotStateLocked()
	if normalized.Delete {
		if receipt.Before == nil {
			return RepairReceipt{}, ErrRecordNotFound
		}
		// 删除和改分都写 history，方便后续审计、回滚和跨节点重建。
		delete(m.records[leaderboardID], normalized.OwnerID)
		if err := m.appendHistoryEntry(ctx, repairHistoryEntry(receipt, nil)); err != nil {
			m.restoreStateLocked(stateBefore)
			return RepairReceipt{}, err
		}
		if repairKey != "" {
			m.repairs[repairKey] = cloneRepairReceipt(receipt)
		}
		return cloneRepairReceipt(receipt), nil
	}
	createdAt := now
	if receipt.Before != nil && !normalized.ResetCreatedAt {
		createdAt = receipt.Before.CreatedAt
	}
	record := Record{
		LeaderboardID: leaderboardID,
		OwnerID:       normalized.OwnerID,
		Score:         normalized.Score,
		Subscore:      normalized.Subscore,
		Metadata:      cloneStringMap(normalized.Metadata),
		CreatedAt:     createdAt,
		UpdatedAt:     now,
	}
	m.records[leaderboardID][record.OwnerID] = record
	trimmed := m.trimLocked(definition)
	if after, found := m.recordWithRankLocked(leaderboardID, record.OwnerID); found {
		after := after
		receipt.After = &after
	}
	if err := m.appendHistoryEntry(ctx, repairHistoryEntry(receipt, receipt.After)); err != nil {
		m.restoreStateLocked(stateBefore)
		return RepairReceipt{}, err
	}
	for _, trimmedRecord := range trimmed {
		if err := m.appendHistoryLocked(ctx, HistoryActionTrimRecord, definition.ID, &trimmedRecord, nil, now); err != nil {
			m.restoreStateLocked(stateBefore)
			return RepairReceipt{}, err
		}
	}
	if repairKey != "" {
		m.repairs[repairKey] = cloneRepairReceipt(receipt)
	}
	return cloneRepairReceipt(receipt), nil
}

func (m *Manager) Record(leaderboardID string, ownerID string) (Record, bool) {
	leaderboardID = strings.TrimSpace(leaderboardID)
	ownerID = strings.TrimSpace(ownerID)
	if m == nil || leaderboardID == "" || ownerID == "" {
		return Record{}, false
	}
	m.mu.Lock()
	m.ensureReadyLocked()
	data, ok := m.rankDataLocked(leaderboardID)
	m.mu.Unlock()
	if !ok {
		return Record{}, false
	}
	return recordWithRank(data.definition, data.records, ownerID)
}

func (m *Manager) List(ctx context.Context, leaderboardID string, options ListOptions) ([]Record, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return nil, ErrLeaderboardNotFound
	}
	m.mu.Lock()
	m.ensureReadyLocked()
	data, ok := m.rankDataLocked(leaderboardID)
	m.mu.Unlock()
	if !ok {
		return nil, ErrLeaderboardNotFound
	}
	ranked := rankRecords(data.definition, data.records)
	offset, limit := normalizeListOptions(options)
	if offset >= len(ranked) {
		return []Record{}, nil
	}
	end := offset + limit
	if end > len(ranked) {
		end = len(ranked)
	}
	return cloneRecords(ranked[offset:end]), nil
}

func (m *Manager) AroundOwner(ctx context.Context, leaderboardID string, ownerID string, limit int) ([]Record, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	ownerID = strings.TrimSpace(ownerID)
	if leaderboardID == "" {
		return nil, ErrLeaderboardNotFound
	}
	if ownerID == "" {
		return nil, ErrRecordNotFound
	}
	m.mu.Lock()
	m.ensureReadyLocked()
	data, ok := m.rankDataLocked(leaderboardID)
	m.mu.Unlock()
	if !ok {
		return nil, ErrLeaderboardNotFound
	}
	ranked := rankRecords(data.definition, data.records)
	idx := -1
	for i, record := range ranked {
		if record.OwnerID == ownerID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, ErrRecordNotFound
	}
	if limit <= 0 {
		limit = defaultAroundLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if limit > len(ranked) {
		limit = len(ranked)
	}
	start := idx - limit/2
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(ranked) {
		end = len(ranked)
		start = end - limit
		if start < 0 {
			start = 0
		}
	}
	return cloneRecords(ranked[start:end]), nil
}

func (m *Manager) Rank(ctx context.Context, leaderboardID string, ownerID string) (int, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return 0, false, err
	}
	if m == nil {
		return 0, false, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	ownerID = strings.TrimSpace(ownerID)
	if leaderboardID == "" {
		return 0, false, ErrLeaderboardNotFound
	}
	if ownerID == "" {
		return 0, false, nil
	}
	m.mu.Lock()
	m.ensureReadyLocked()
	data, ok := m.rankDataLocked(leaderboardID)
	m.mu.Unlock()
	if !ok {
		return 0, false, ErrLeaderboardNotFound
	}
	record, ok := recordWithRank(data.definition, data.records, ownerID)
	return record.Rank, ok, nil
}

func (m *Manager) Reset(ctx context.Context, leaderboardID string) (Definition, error) {
	if err := ctxErr(ctx); err != nil {
		return Definition{}, err
	}
	if m == nil {
		return Definition{}, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Definition{}, ErrLeaderboardNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()

	definition, ok := m.definitions[leaderboardID]
	if !ok {
		return Definition{}, ErrLeaderboardNotFound
	}
	before := m.snapshotStateLocked()
	definition.UpdatedAt = m.now().UTC()
	m.definitions[leaderboardID] = definition
	m.records[leaderboardID] = make(map[string]Record)
	if err := m.appendHistoryLocked(ctx, HistoryActionReset, definition.ID, nil, &definition, definition.UpdatedAt); err != nil {
		m.restoreStateLocked(before)
		return Definition{}, err
	}
	return cloneDefinition(definition), nil
}

func (m *Manager) Capture(ctx context.Context, leaderboardID string, options CaptureOptions) (Capture, error) {
	if err := ctxErr(ctx); err != nil {
		return Capture{}, err
	}
	if m == nil {
		return Capture{}, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Capture{}, ErrLeaderboardNotFound
	}
	normalized := normCaptureOpts(options)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()

	if _, ok := m.definitions[leaderboardID]; !ok {
		return Capture{}, ErrLeaderboardNotFound
	}
	ranked := m.rankedRecordsLocked(leaderboardID)
	limit := normalized.Limit
	if limit > len(ranked) {
		limit = len(ranked)
	}
	capture := Capture{
		LeaderboardID: leaderboardID,
		Records:       cloneRecords(ranked[:limit]),
		RecordCount:   len(ranked),
		Reason:        normalized.Reason,
		OperatorID:    normalized.OperatorID,
		RequestID:     normalized.RequestID,
		Metadata:      cloneStringMap(normalized.Metadata),
		CapturedAt:    m.now().UTC(),
	}
	if err := m.appendHistoryEntry(ctx, HistoryEntry{
		Action:        HistoryActionCapture,
		LeaderboardID: leaderboardID,
		Records:       capture.Records,
		Reason:        capture.Reason,
		OperatorID:    capture.OperatorID,
		RequestID:     capture.RequestID,
		Metadata:      capture.Metadata,
		At:            capture.CapturedAt,
	}); err != nil {
		return Capture{}, err
	}
	return cloneCapture(capture), nil
}

func (m *Manager) History(ctx context.Context, leaderboardID string, limit int) ([]HistoryEntry, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrManagerRequired
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return nil, ErrLeaderboardNotFound
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	m.mu.Lock()
	m.ensureReadyLocked()
	store := m.hist
	strict := m.historyStrict
	m.mu.Unlock()
	if store == nil {
		return []HistoryEntry{}, nil
	}
	history, err := store.List(ctx, leaderboardID, limit)
	if err != nil {
		m.mu.Lock()
		m.historyErrors++
		m.mu.Unlock()
		if strict {
			return nil, err
		}
		return []HistoryEntry{}, nil
	}
	return history, nil
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()

	snapshot := Snapshot{
		Name:               m.name,
		LeaderboardCount:   len(m.definitions),
		RecordsByBoard:     make(map[string]int, len(m.records)),
		HistoryStoreErrors: m.historyErrors + intFromU64Sat(historyErrors(m.hist)),
	}
	for id, records := range m.records {
		if len(records) == 0 {
			continue
		}
		snapshot.RecordCount += len(records)
		snapshot.RecordsByBoard[id] = len(records)
	}
	if len(snapshot.RecordsByBoard) == 0 {
		snapshot.RecordsByBoard = nil
	}
	return snapshot
}

// Flush 刷新排行榜历史异步队列。
func (m *Manager) Flush(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.ensureReadyLocked()
	store := m.hist
	m.mu.Unlock()
	return flushHistory(ctx, store)
}

// Close 关闭排行榜历史异步队列。
func (m *Manager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.ensureReadyLocked()
	store := m.hist
	m.mu.Unlock()
	return closeHistory(ctx, store)
}

func (m *Manager) ensureReadyLocked() {
	if m.name == "" {
		m.name = "leaderboard"
	}
	if m.now == nil {
		m.now = time.Now
	}
	if m.definitions == nil {
		m.definitions = make(map[string]Definition)
	}
	if m.records == nil {
		m.records = make(map[string]map[string]Record)
	}
	if m.repairs == nil {
		m.repairs = make(map[string]RepairReceipt)
	}
}

func (m *Manager) snapshotStateLocked() managerStateSnapshot {
	return managerStateSnapshot{
		definitions: cloneDefinitionMap(m.definitions),
		records:     cloneRecordMap(m.records),
		repairs:     cloneRepairMap(m.repairs),
	}
}

func (m *Manager) restoreStateLocked(snapshot managerStateSnapshot) {
	m.definitions = cloneDefinitionMap(snapshot.definitions)
	m.records = cloneRecordMap(snapshot.records)
	m.repairs = cloneRepairMap(snapshot.repairs)
}

func (m *Manager) normDefinitionLocked(definition Definition) (Definition, error) {
	definition.ID = strings.TrimSpace(definition.ID)
	if definition.ID == "" {
		definition.ID = m.nextIDLocked("leaderboard")
	}
	definition.Title = strings.TrimSpace(definition.Title)
	sortOrder, err := normalizeSortOrder(definition.SortOrder)
	if err != nil {
		return Definition{}, err
	}
	operator, err := normalizeOperator(definition.Operator)
	if err != nil {
		return Definition{}, err
	}
	definition.SortOrder = sortOrder
	definition.Operator = operator
	if definition.MaxSize < 0 {
		return Definition{}, fmt.Errorf("%w: max_size must not be negative", ErrInvalidDefinition)
	}
	now := m.now().UTC()
	if definition.CreatedAt.IsZero() {
		definition.CreatedAt = now
	} else {
		definition.CreatedAt = definition.CreatedAt.UTC()
	}
	if definition.UpdatedAt.IsZero() {
		definition.UpdatedAt = definition.CreatedAt
	} else {
		definition.UpdatedAt = definition.UpdatedAt.UTC()
	}
	definition.Metadata = cloneStringMap(definition.Metadata)
	return definition, nil
}

func (m *Manager) rankedRecordsLocked(leaderboardID string) []Record {
	data, ok := m.rankDataLocked(leaderboardID)
	if !ok {
		return nil
	}
	return rankRecords(data.definition, data.records)
}

func (m *Manager) rankDataLocked(leaderboardID string) (boardRankData, bool) {
	definition, ok := m.definitions[leaderboardID]
	if !ok {
		return boardRankData{}, false
	}
	return boardRankData{
		definition: cloneDefinition(definition),
		records:    cloneRecordSet(m.records[leaderboardID]),
	}, true
}

func rankRecords(definition Definition, records map[string]Record) []Record {
	out := make([]Record, 0, len(records))
	for _, record := range records {
		out = append(out, cloneRecord(record))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return compareRecords(out[i], out[j], definition.SortOrder) < 0
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

func (m *Manager) recordWithRankLocked(leaderboardID string, ownerID string) (Record, bool) {
	ranked := m.rankedRecordsLocked(leaderboardID)
	return recordFromRanked(ranked, ownerID)
}

func recordWithRank(definition Definition, records map[string]Record, ownerID string) (Record, bool) {
	return recordFromRanked(rankRecords(definition, records), ownerID)
}

func recordFromRanked(ranked []Record, ownerID string) (Record, bool) {
	for _, record := range ranked {
		if record.OwnerID == ownerID {
			return cloneRecord(record), true
		}
	}
	return Record{}, false
}

func (m *Manager) trimLocked(definition Definition) []Record {
	if definition.MaxSize <= 0 {
		return nil
	}
	ranked := m.rankedRecordsLocked(definition.ID)
	if len(ranked) <= definition.MaxSize {
		return nil
	}
	keep := make(map[string]struct{}, definition.MaxSize)
	for _, record := range ranked[:definition.MaxSize] {
		keep[record.OwnerID] = struct{}{}
	}
	trimmed := cloneRecords(ranked[definition.MaxSize:])
	for ownerID := range m.records[definition.ID] {
		if _, ok := keep[ownerID]; !ok {
			delete(m.records[definition.ID], ownerID)
		}
	}
	return trimmed
}

func (m *Manager) appendHistoryLocked(ctx context.Context, action string, leaderboardID string, record *Record, definition *Definition, at time.Time) error {
	if at.IsZero() {
		at = m.now().UTC()
	} else {
		at = at.UTC()
	}
	entry := HistoryEntry{
		Action:        action,
		LeaderboardID: strings.TrimSpace(leaderboardID),
		At:            at,
	}
	if record != nil {
		cloned := cloneRecord(*record)
		entry.OwnerID = cloned.OwnerID
		entry.Record = &cloned
	}
	if definition != nil {
		cloned := cloneDefinition(*definition)
		entry.Definition = &cloned
	}
	return m.appendHistoryEntry(ctx, entry)
}

func (m *Manager) appendHistoryEntry(ctx context.Context, entry HistoryEntry) error {
	if m.hist == nil {
		return nil
	}
	entry = cloneHistoryEntry(entry)
	entry.LeaderboardID = strings.TrimSpace(entry.LeaderboardID)
	entry.Action = strings.TrimSpace(entry.Action)
	entry.OwnerID = strings.TrimSpace(entry.OwnerID)
	entry.Reason = strings.TrimSpace(entry.Reason)
	entry.OperatorID = strings.TrimSpace(entry.OperatorID)
	entry.RequestID = strings.TrimSpace(entry.RequestID)
	entry.Metadata = cloneStringMap(entry.Metadata)
	if entry.At.IsZero() {
		entry.At = m.now().UTC()
	} else {
		entry.At = entry.At.UTC()
	}
	if err := m.hist.Append(ctx, entry); err != nil {
		m.historyErrors++
		if m.historyStrict {
			return err
		}
	}
	return nil
}

func (m *Manager) nextIDLocked(prefix string) string {
	if m.gen != nil {
		if id := strings.TrimSpace(m.gen(prefix)); id != "" {
			return id
		}
	}
	m.next++
	return fmt.Sprintf("%s-%d", prefix, m.next)
}
