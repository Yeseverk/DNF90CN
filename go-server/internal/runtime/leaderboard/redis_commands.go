package leaderboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/eventlog"
)

func (m *RedisManager) redisInt(ctx context.Context, command string, args ...any) (int64, error) {
	value, err := m.do(ctx, command, args...)
	if err != nil {
		return 0, err
	}
	return redisInt64(value)
}

func (m *RedisManager) redisEvalInt(ctx context.Context, script string, keys []string, args ...any) (int64, error) {
	params := make([]any, 0, 2+len(keys)+len(args))
	params = append(params, script, len(keys))
	for _, key := range keys {
		params = append(params, key)
	}
	params = append(params, args...)
	value, err := m.do(ctx, "EVAL", params...)
	if err != nil {
		return 0, err
	}
	return redisInt64(value)
}

func (m *RedisManager) nextID(ctx context.Context) (string, error) {
	if m == nil {
		return "", ErrNilRedisExecutor
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.redisInt(ctx, "INCR", m.counterKey())
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("leaderboard-%d", n), nil
}

func (m *RedisManager) do(ctx context.Context, command string, args ...any) (any, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if m == nil || m.executor == nil {
		return nil, ErrNilRedisExecutor
	}
	return m.executor.Do(ctx, command, args...)
}

func (m *RedisManager) indexKey() string {
	return m.redisPrefix() + ":index"
}

func (m *RedisManager) counterKey() string {
	return m.redisPrefix() + ":counter"
}

func (m *RedisManager) definitionKey(id string) string {
	return m.redisPrefix() + ":definition:" + strings.TrimSpace(id)
}

func (m *RedisManager) recordsKey(id string) string {
	return m.redisPrefix() + ":records:" + strings.TrimSpace(id)
}

func (m *RedisManager) zsetKey(id string) string {
	return m.redisPrefix() + ":z:" + strings.TrimSpace(id)
}

func (m *RedisManager) historyKey(id string) string {
	return m.redisPrefix() + ":history:" + strings.TrimSpace(id)
}

func (m *RedisManager) lockKey(id string) string {
	return m.redisPrefix() + ":lock:leaderboard:" + strings.TrimSpace(id)
}

func (m *RedisManager) redisPrefix() string {
	if m == nil {
		return defRedisBoardPrefix
	}
	return normRedisBoardPrefix(m.prefix)
}

func normRedisBoardPrefix(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		return defRedisBoardPrefix
	}
	return prefix
}

func (m *RedisManager) redisName() string {
	if m == nil {
		return ""
	}
	name := strings.TrimSpace(m.name)
	if name == "" {
		return defRedisBoardMgr
	}
	return name
}

func (m *RedisManager) nowUTC() time.Time {
	if m == nil || m.now == nil {
		return time.Now().UTC()
	}
	return m.now().UTC()
}

func (m *RedisManager) historyLimitValue() int {
	if m == nil || m.historyLimit == 0 {
		return defRedisHistory
	}
	return m.historyLimit
}

func (m *RedisManager) lockTTLValue() time.Duration {
	if m == nil || m.lockTTL <= 0 {
		return defaultRedisLockTTL
	}
	return m.lockTTL
}

func (m *RedisManager) appendHistoryData(ctx context.Context, payloads ...[]byte) error {
	if len(payloads) == 0 || (m.historyStore == nil && m.eventLog == nil && m.eventHist == nil) {
		return nil
	}
	for _, payload := range payloads {
		var entry HistoryEntry
		if err := json.Unmarshal(payload, &entry); err != nil {
			atomic.AddUint64(&m.historyErrors, 1)
			if m.historyStrict {
				return err
			}
			continue
		}
		if m.historyStore != nil {
			if err := m.historyStore.Append(ctx, entry); err != nil {
				atomic.AddUint64(&m.historyErrors, 1)
				if m.historyStrict {
					return err
				}
			}
		}
		if m.eventHist != nil {
			if err := m.eventHist.Append(ctx, entry); err != nil {
				atomic.AddUint64(&m.eventLogErrors, 1)
				if m.eventLogStrict {
					return err
				}
			}
			continue
		}
		if err := m.appendEventHistory(ctx, entry); err != nil {
			atomic.AddUint64(&m.eventLogErrors, 1)
			if m.eventLogStrict {
				return err
			}
		}
	}
	return nil
}

func (m *RedisManager) appendEventHistory(ctx context.Context, entry HistoryEntry) error {
	if m == nil || m.eventLog == nil {
		return nil
	}
	entry = normHistoryEntry(entry)
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = m.eventLog.Append(ctx, eventlog.Event{
		Stream:         m.eventLogStream,
		Type:           m.eventLogType,
		AggregateID:    entry.LeaderboardID,
		IdempotencyKey: historyIdemKey(entry, payload),
		Payload:        payload,
		Headers: map[string]string{
			"action":         entry.Action,
			"leaderboard_id": entry.LeaderboardID,
			"owner_id":       entry.OwnerID,
		},
	})
	return err
}

func normalizeEventStream(stream string) string {
	stream = strings.TrimSpace(stream)
	if stream == "" {
		return LeaderboardEventStream
	}
	return stream
}

func normHistoryEventType(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return DefaultLeaderboardHistoryEventType
	}
	return eventType
}

func (m *RedisManager) historyData(action string, leaderboardID string, record *Record, definition *Definition, at time.Time, details ...HistoryDetails) ([]byte, error) {
	if at.IsZero() {
		at = m.nowUTC()
	} else {
		at = at.UTC()
	}
	entry := HistoryEntry{
		Action:        action,
		LeaderboardID: strings.TrimSpace(leaderboardID),
		At:            at,
	}
	if len(details) > 0 {
		detail := details[0]
		entry.Reason = strings.TrimSpace(detail.Reason)
		entry.OperatorID = strings.TrimSpace(detail.OperatorID)
		entry.RequestID = strings.TrimSpace(detail.RequestID)
		entry.Metadata = cloneStringMap(detail.Metadata)
		entry.Records = cloneRecords(detail.Records)
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
	return json.Marshal(entry)
}

func redisRankScore(record Record, sortOrder string) float64 {
	score := float64(record.Score)
	if sortOrder == SortAscending {
		return -score
	}
	return score
}

func redisBytes(value any) ([]byte, bool, error) {
	switch v := value.(type) {
	case nil:
		return nil, false, nil
	case []byte:
		return append([]byte(nil), v...), true, nil
	case string:
		return []byte(v), true, nil
	default:
		return nil, false, fmt.Errorf("unexpected redis bulk type %T", value)
	}
}

func redisStrings(value any) ([]string, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return append([]string(nil), v...), nil
	case [][]byte:
		out := make([]string, len(v))
		for i := range v {
			out[i] = string(v[i])
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			switch x := item.(type) {
			case []byte:
				out = append(out, string(x))
			case string:
				out = append(out, x)
			default:
				return nil, fmt.Errorf("unexpected redis string item %T", item)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unexpected redis string slice type %T", value)
	}
}

func redisBulkBytes(value any) ([][]byte, []bool, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil, nil
	case []any:
		data := make([][]byte, len(v))
		found := make([]bool, len(v))
		for i, item := range v {
			bytes, ok, err := redisBytes(item)
			if err != nil {
				return nil, nil, err
			}
			data[i] = bytes
			found[i] = ok
		}
		return data, found, nil
	case [][]byte:
		data := make([][]byte, len(v))
		found := make([]bool, len(v))
		for i := range v {
			data[i] = append([]byte(nil), v[i]...)
			found[i] = true
		}
		return data, found, nil
	default:
		return nil, nil, fmt.Errorf("unexpected redis bulk slice type %T", value)
	}
}

func redisInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case uint64:
		const maxInt64 = uint64(1<<63 - 1)
		if v > maxInt64 {
			return 0, fmt.Errorf("redis integer overflows int64: %d", v)
		}
		return int64(v), nil
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("unexpected redis integer type %T", value)
	}
}
