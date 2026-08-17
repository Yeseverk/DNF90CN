package leaderboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/db"
	"longheng.io/server/internal/platform/eventlog"
)

var (
	ErrNilRedisExecutor = errors.New("leaderboard redis executor is required")
	ErrRedisLockToken   = errors.New("leaderboard redis lock token generation failed")
)

const (
	defRedisHistory     = defaultHistoryLimit
	defaultRedisLockTTL = 5 * time.Second
	defRedisBoardPrefix = "longheng:leaderboards"
	defRedisBoardMgr    = "redis-leaderboard"
)

type RedisOptions struct {
	Name           string
	Executor       db.RedisExecutor
	KeyPrefix      string
	Now            func() time.Time
	HistoryLimit   int
	HistoryStore   HistoryStore
	HistoryStrict  bool
	EventLog       *eventlog.Log
	EventLogStrict bool
	EventLogStream string
	EventLogType   string
	LockTTL        time.Duration
}

type RedisManager struct {
	name           string
	executor       db.RedisExecutor
	prefix         string
	now            func() time.Time
	historyLimit   int
	historyStore   HistoryStore
	historyStrict  bool
	historyErrors  uint64
	eventLog       *eventlog.Log
	eventHist      HistoryStore
	eventLogStrict bool
	eventLogStream string
	eventLogType   string
	eventLogErrors uint64
	lockTTL        time.Duration

	mu sync.Mutex
}

func NewRedis(options RedisOptions) *RedisManager {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = defRedisBoardMgr
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	historyLimit := options.HistoryLimit
	if historyLimit == 0 {
		historyLimit = defRedisHistory
	}
	lockTTL := options.LockTTL
	if lockTTL <= 0 {
		lockTTL = defaultRedisLockTTL
	}
	historyStore := wrapHistoryStore(options.HistoryStore, options.HistoryStrict)
	eventLog := options.EventLog
	var eventHist HistoryStore
	if eventLog != nil && !options.EventLogStrict {
		eventHist = NewAsyncHistoryStore(newEventHistoryStore(eventLog, options.EventLogStream, options.EventLogType))
		eventLog = nil
	}
	return &RedisManager{
		name:           name,
		executor:       options.Executor,
		prefix:         normRedisBoardPrefix(options.KeyPrefix),
		now:            now,
		historyLimit:   historyLimit,
		historyStore:   historyStore,
		historyStrict:  options.HistoryStrict,
		eventLog:       eventLog,
		eventHist:      eventHist,
		eventLogStrict: options.EventLogStrict,
		eventLogStream: normalizeEventStream(options.EventLogStream),
		eventLogType:   normHistoryEventType(options.EventLogType),
		lockTTL:        lockTTL,
	}
}

func (m *RedisManager) Create(ctx context.Context, definition Definition) (Definition, error) {
	if err := ctxErr(ctx); err != nil {
		return Definition{}, err
	}
	normalized, err := m.normalizeDefinition(ctx, definition)
	if err != nil {
		return Definition{}, err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return Definition{}, err
	}
	history, err := m.historyData(HistoryActionCreate, normalized.ID, nil, &normalized, normalized.CreatedAt)
	if err != nil {
		return Definition{}, err
	}
	result, err := m.redisEvalInt(ctx, redisCreateScript, []string{
		m.definitionKey(normalized.ID),
		m.indexKey(),
		m.historyKey(normalized.ID),
	}, normalized.ID, data, history, m.historyLimitValue())
	if err != nil {
		return Definition{}, err
	}
	if result < 0 {
		return Definition{}, ErrLeaderboardExists
	}
	if err := m.appendHistoryData(ctx, history); err != nil {
		return Definition{}, err
	}
	return cloneDefinition(normalized), nil
}

func (m *RedisManager) Delete(ctx context.Context, leaderboardID string) (Definition, error) {
	if err := ctxErr(ctx); err != nil {
		return Definition{}, err
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Definition{}, ErrLeaderboardNotFound
	}
	var deleted Definition
	err := m.withLeaderboardLock(ctx, leaderboardID, func() error {
		definition, ok, err := m.definition(ctx, leaderboardID)
		if err != nil || !ok {
			if err != nil {
				return err
			}
			return ErrLeaderboardNotFound
		}
		history, err := m.historyData(HistoryDeleteBoard, definition.ID, nil, &definition, m.nowUTC())
		if err != nil {
			return err
		}
		result, err := m.redisEvalInt(ctx, redisDeleteScript, []string{
			m.definitionKey(definition.ID),
			m.recordsKey(definition.ID),
			m.zsetKey(definition.ID),
			m.indexKey(),
			m.historyKey(definition.ID),
		}, definition.ID, history, m.historyLimitValue())
		if err != nil {
			return err
		}
		if result < 0 {
			return ErrLeaderboardNotFound
		}
		if err := m.appendHistoryData(ctx, history); err != nil {
			return err
		}
		deleted = definition
		return nil
	})
	if err != nil {
		return Definition{}, err
	}
	return deleted, nil
}

func (m *RedisManager) Definition(leaderboardID string) (Definition, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	definition, ok, err := m.DefinitionContext(ctx, leaderboardID)
	if err != nil {
		return Definition{}, false
	}
	return definition, ok
}

// DefinitionContext 让生产读路径区分 Redis 故障和榜单不存在，避免把后端抖动误判为业务空结果。
func (m *RedisManager) DefinitionContext(ctx context.Context, leaderboardID string) (Definition, bool, error) {
	return m.definition(ctx, leaderboardID)
}

func (m *RedisManager) Definitions() []Definition {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	definitions, err := m.DefinitionsContext(ctx)
	if err != nil {
		return nil
	}
	return definitions
}

// DefinitionsContext 让管理面在 Redis 故障时返回错误，而不是把榜单列表显示为空。
func (m *RedisManager) DefinitionsContext(ctx context.Context) ([]Definition, error) {
	return m.definitions(ctx)
}

func (m *RedisManager) Submit(ctx context.Context, leaderboardID string, submission Submission) (Record, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, err
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Record{}, ErrLeaderboardNotFound
	}
	normalized, err := normalizeSubmission(submission)
	if err != nil {
		return Record{}, err
	}
	var out Record
	err = m.withLeaderboardLock(ctx, leaderboardID, func() error {
		definition, ok, err := m.definition(ctx, leaderboardID)
		if err != nil || !ok {
			if err != nil {
				return err
			}
			return ErrLeaderboardNotFound
		}
		existing, exists, err := m.record(ctx, definition.ID, normalized.OwnerID)
		if err != nil {
			return err
		}
		now := m.nowUTC()
		record := Record{
			LeaderboardID: definition.ID,
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
				if !betterThan(record, existing, definition.SortOrder) {
					ranked, ok, err := m.recordWithRank(ctx, definition.ID, normalized.OwnerID)
					if err != nil {
						return err
					}
					if !ok {
						return ErrRecordNotFound
					}
					out = ranked
					return nil
				}
			case OperatorIncrement:
				record.Score, err = addScoreValue(existing.Score, normalized.Score)
				if err != nil {
					return err
				}
				record.Subscore, err = addScoreValue(existing.Subscore, normalized.Subscore)
				if err != nil {
					return err
				}
			case OperatorSet:
			default:
				return fmt.Errorf("%w: unsupported operator %q", ErrInvalidDefinition, definition.Operator)
			}
		}
		if err := m.saveRecord(ctx, definition, record); err != nil {
			return err
		}
		if definition.MaxSize > 0 {
			if err := m.trim(ctx, definition); err != nil {
				return err
			}
		}
		ranked, ok, err := m.recordWithRank(ctx, definition.ID, normalized.OwnerID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrRecordTrimmed
		}
		out = ranked
		return nil
	})
	if err != nil {
		return Record{}, err
	}
	return out, nil
}

func (m *RedisManager) DeleteRecord(ctx context.Context, leaderboardID string, ownerID string) (Record, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, err
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Record{}, ErrLeaderboardNotFound
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return Record{}, ErrRecordNotFound
	}
	var deleted Record
	err := m.withLeaderboardLock(ctx, leaderboardID, func() error {
		definition, ok, err := m.definition(ctx, leaderboardID)
		if err != nil || !ok {
			if err != nil {
				return err
			}
			return ErrLeaderboardNotFound
		}
		record, ok, err := m.recordWithRank(ctx, definition.ID, ownerID)
		if err != nil || !ok {
			if err != nil {
				return err
			}
			return ErrRecordNotFound
		}
		if err := m.deleteRecords(ctx, definition, []Record{record}, HistoryActionDeleteRecord); err != nil {
			return err
		}
		deleted = record
		return nil
	})
	if err != nil {
		return Record{}, err
	}
	return deleted, nil
}

func (m *RedisManager) Repair(ctx context.Context, leaderboardID string, request RepairRequest) (RepairReceipt, error) {
	if err := ctxErr(ctx); err != nil {
		return RepairReceipt{}, err
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return RepairReceipt{}, ErrLeaderboardNotFound
	}
	normalized, err := normRepairReq(request)
	if err != nil {
		return RepairReceipt{}, err
	}
	var receipt RepairReceipt
	err = m.withLeaderboardLock(ctx, leaderboardID, func() error {
		definition, ok, err := m.definition(ctx, leaderboardID)
		if err != nil || !ok {
			if err != nil {
				return err
			}
			return ErrLeaderboardNotFound
		}
		now := m.nowUTC()
		receipt = RepairReceipt{
			LeaderboardID: definition.ID,
			OwnerID:       normalized.OwnerID,
			Action:        HistoryActionRepairRecord,
			Reason:        normalized.Reason,
			OperatorID:    normalized.OperatorID,
			RequestID:     normalized.RequestID,
			Metadata:      cloneStringMap(normalized.Metadata),
			At:            now,
		}
		if before, found, err := m.recordWithRank(ctx, definition.ID, normalized.OwnerID); err != nil {
			return err
		} else if found {
			before := before
			receipt.Before = &before
		}
		details := HistoryDetails{
			Reason:     normalized.Reason,
			OperatorID: normalized.OperatorID,
			RequestID:  normalized.RequestID,
			Metadata:   cloneStringMap(normalized.Metadata),
		}
		if normalized.Delete {
			if receipt.Before == nil {
				return ErrRecordNotFound
			}
			if err := m.delRecordsDetail(ctx, definition, []Record{*receipt.Before}, HistoryActionRepairRecord, details); err != nil {
				return err
			}
			return nil
		}
		createdAt := now
		if receipt.Before != nil && !normalized.ResetCreatedAt {
			createdAt = receipt.Before.CreatedAt
		}
		record := Record{
			LeaderboardID: definition.ID,
			OwnerID:       normalized.OwnerID,
			Score:         normalized.Score,
			Subscore:      normalized.Subscore,
			Metadata:      cloneStringMap(normalized.Metadata),
			CreatedAt:     createdAt,
			UpdatedAt:     now,
		}
		if err := m.saveRecordDetails(ctx, definition, record, HistoryActionRepairRecord, details); err != nil {
			return err
		}
		if definition.MaxSize > 0 {
			if err := m.trim(ctx, definition); err != nil {
				return err
			}
		}
		if after, found, err := m.recordWithRank(ctx, definition.ID, normalized.OwnerID); err != nil {
			return err
		} else if found {
			after := after
			receipt.After = &after
		}
		return nil
	})
	if err != nil {
		return RepairReceipt{}, err
	}
	return cloneRepairReceipt(receipt), nil
}

func (m *RedisManager) Record(leaderboardID string, ownerID string) (Record, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	leaderboardID = strings.TrimSpace(leaderboardID)
	ownerID = strings.TrimSpace(ownerID)
	if leaderboardID == "" || ownerID == "" {
		return Record{}, false
	}
	record, found, err := m.recordWithRank(ctx, leaderboardID, ownerID)
	if err != nil {
		return Record{}, false
	}
	return record, found
}

func (m *RedisManager) List(ctx context.Context, leaderboardID string, options ListOptions) ([]Record, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return nil, ErrLeaderboardNotFound
	}
	if _, ok, err := m.definition(ctx, leaderboardID); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, ErrLeaderboardNotFound
	}
	offset, limit := normalizeListOptions(options)
	out, err := m.rangeRecords(ctx, leaderboardID, offset, offset+limit-1, offset+1)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (m *RedisManager) AroundOwner(ctx context.Context, leaderboardID string, ownerID string, limit int) ([]Record, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, ErrRecordNotFound
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return nil, ErrLeaderboardNotFound
	}
	if _, ok, err := m.definition(ctx, leaderboardID); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, ErrLeaderboardNotFound
	}
	rank, ok, err := m.redisRank(ctx, leaderboardID, ownerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrRecordNotFound
	}
	if limit <= 0 {
		limit = defaultAroundLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	total, err := m.redisInt(ctx, "ZCARD", m.zsetKey(leaderboardID))
	if err != nil {
		return nil, err
	}
	if total <= 0 {
		return nil, ErrRecordNotFound
	}
	if limit > int(total) {
		limit = int(total)
	}
	start := rank - 1 - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > int(total) {
		start = int(total) - limit
		if start < 0 {
			start = 0
		}
	}
	out, err := m.rangeRecords(ctx, leaderboardID, start, start+limit-1, start+1)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (m *RedisManager) Rank(ctx context.Context, leaderboardID string, ownerID string) (int, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return 0, false, err
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	ownerID = strings.TrimSpace(ownerID)
	if leaderboardID == "" {
		return 0, false, ErrLeaderboardNotFound
	}
	if ownerID == "" {
		return 0, false, nil
	}
	return m.redisRank(ctx, leaderboardID, ownerID)
}

func (m *RedisManager) Reset(ctx context.Context, leaderboardID string) (Definition, error) {
	if err := ctxErr(ctx); err != nil {
		return Definition{}, err
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Definition{}, ErrLeaderboardNotFound
	}
	var reset Definition
	err := m.withLeaderboardLock(ctx, leaderboardID, func() error {
		definition, ok, err := m.definition(ctx, leaderboardID)
		if err != nil || !ok {
			if err != nil {
				return err
			}
			return ErrLeaderboardNotFound
		}
		definition.UpdatedAt = m.nowUTC()
		data, err := json.Marshal(definition)
		if err != nil {
			return err
		}
		history, err := m.historyData(HistoryActionReset, definition.ID, nil, &definition, definition.UpdatedAt)
		if err != nil {
			return err
		}
		result, err := m.redisEvalInt(ctx, redisResetScript, []string{
			m.definitionKey(definition.ID),
			m.recordsKey(definition.ID),
			m.zsetKey(definition.ID),
			m.historyKey(definition.ID),
		}, data, history, m.historyLimitValue())
		if err != nil {
			return err
		}
		if result < 0 {
			return ErrLeaderboardNotFound
		}
		if err := m.appendHistoryData(ctx, history); err != nil {
			return err
		}
		reset = cloneDefinition(definition)
		return nil
	})
	if err != nil {
		return Definition{}, err
	}
	return reset, nil
}

func (m *RedisManager) Capture(ctx context.Context, leaderboardID string, options CaptureOptions) (Capture, error) {
	if err := ctxErr(ctx); err != nil {
		return Capture{}, err
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Capture{}, ErrLeaderboardNotFound
	}
	normalized := normCaptureOpts(options)
	var capture Capture
	err := m.withLeaderboardLock(ctx, leaderboardID, func() error {
		if _, ok, err := m.definition(ctx, leaderboardID); err != nil || !ok {
			if err != nil {
				return err
			}
			return ErrLeaderboardNotFound
		}
		ranked, err := m.rankedRecords(ctx, leaderboardID)
		if err != nil {
			return err
		}
		limit := normalized.Limit
		if limit > len(ranked) {
			limit = len(ranked)
		}
		capture = Capture{
			LeaderboardID: leaderboardID,
			Records:       cloneRecords(ranked[:limit]),
			RecordCount:   len(ranked),
			Reason:        normalized.Reason,
			OperatorID:    normalized.OperatorID,
			RequestID:     normalized.RequestID,
			Metadata:      cloneStringMap(normalized.Metadata),
			CapturedAt:    m.nowUTC(),
		}
		history, err := m.historyData(HistoryActionCapture, leaderboardID, nil, nil, capture.CapturedAt, HistoryDetails{
			Reason:     capture.Reason,
			OperatorID: capture.OperatorID,
			RequestID:  capture.RequestID,
			Metadata:   capture.Metadata,
			Records:    capture.Records,
		})
		if err != nil {
			return err
		}
		return m.appendHistoryPayload(ctx, leaderboardID, history)
	})
	if err != nil {
		return Capture{}, err
	}
	return cloneCapture(capture), nil
}

func (m *RedisManager) History(ctx context.Context, leaderboardID string, limit int) ([]HistoryEntry, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrNilRedisExecutor
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return nil, ErrLeaderboardNotFound
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	historyLimit := m.historyLimitValue()
	if limit > historyLimit && historyLimit > 0 {
		limit = historyLimit
	}
	if m.historyStore != nil {
		history, err := m.historyStore.List(ctx, leaderboardID, limit)
		if err == nil {
			return history, nil
		}
		if m.historyStrict {
			return nil, err
		}
		atomic.AddUint64(&m.historyErrors, 1)
	}
	value, err := m.do(ctx, "LRANGE", m.historyKey(leaderboardID), -limit, -1)
	if err != nil {
		return nil, err
	}
	items, err := redisStrings(value)
	if err != nil {
		return nil, err
	}
	history := make([]HistoryEntry, 0, len(items))
	for _, item := range items {
		var entry HistoryEntry
		if err := json.Unmarshal([]byte(item), &entry); err != nil {
			return nil, fmt.Errorf("decode leaderboard history: %w", err)
		}
		if entry.Record != nil {
			cloned := cloneRecord(*entry.Record)
			entry.Record = &cloned
		}
		if entry.Definition != nil {
			cloned := cloneDefinition(*entry.Definition)
			entry.Definition = &cloned
		}
		history = append(history, entry)
	}
	return history, nil
}

func (m *RedisManager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	definitions, err := m.definitions(ctx)
	if err != nil {
		return Snapshot{Name: m.redisName()}
	}
	snapshot := Snapshot{
		Name:               m.redisName(),
		LeaderboardCount:   len(definitions),
		RecordsByBoard:     make(map[string]int, len(definitions)),
		HistoryStoreErrors: intFromU64Sat(atomic.LoadUint64(&m.historyErrors) + historyErrors(m.historyStore)),
		EventLogErrors:     intFromU64Sat(atomic.LoadUint64(&m.eventLogErrors) + historyStoreErrors(m.historyStore) + historyErrors(m.eventHist)),
	}
	for _, definition := range definitions {
		count, err := m.redisInt(ctx, "HLEN", m.recordsKey(definition.ID))
		if err != nil || count <= 0 {
			continue
		}
		snapshot.RecordCount += int(count)
		snapshot.RecordsByBoard[definition.ID] = int(count)
	}
	if len(snapshot.RecordsByBoard) == 0 {
		snapshot.RecordsByBoard = nil
	}
	return snapshot
}

// Flush 刷新排行榜外部历史和事件异步队列。
func (m *RedisManager) Flush(ctx context.Context) error {
	if m == nil {
		return nil
	}
	return errors.Join(flushHistory(ctx, m.historyStore), flushHistory(ctx, m.eventHist))
}

// Close 关闭排行榜外部历史和事件异步队列。
func (m *RedisManager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	return errors.Join(closeHistory(ctx, m.historyStore), closeHistory(ctx, m.eventHist))
}

func intFromU64Sat(value uint64) int {
	if value > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(value) //nolint:gosec // G115：前面已按 int 最大值钳制。
}
