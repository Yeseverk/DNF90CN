package leaderboard

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func (m *RedisManager) normalizeDefinition(ctx context.Context, definition Definition) (Definition, error) {
	if m == nil {
		return Definition{}, ErrNilRedisExecutor
	}
	definition.ID = strings.TrimSpace(definition.ID)
	if definition.ID == "" {
		id, err := m.nextID(ctx)
		if err != nil {
			return Definition{}, err
		}
		definition.ID = id
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
	now := m.nowUTC()
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

func (m *RedisManager) definitions(ctx context.Context) ([]Definition, error) {
	value, err := m.do(ctx, "SMEMBERS", m.indexKey())
	if err != nil {
		return nil, err
	}
	ids, err := redisStrings(value)
	if err != nil {
		return nil, err
	}
	definitions := make([]Definition, 0, len(ids))
	for _, id := range ids {
		definition, ok, err := m.definition(ctx, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			_, _ = m.do(ctx, "SREM", m.indexKey(), id)
			continue
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions, nil
}

func (m *RedisManager) definition(ctx context.Context, leaderboardID string) (Definition, bool, error) {
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return Definition{}, false, nil
	}
	value, err := m.do(ctx, "GET", m.definitionKey(leaderboardID))
	if err != nil {
		return Definition{}, false, err
	}
	data, ok, err := redisBytes(value)
	if err != nil || !ok {
		if err != nil {
			return Definition{}, false, err
		}
		return Definition{}, false, nil
	}
	var definition Definition
	if err := json.Unmarshal(data, &definition); err != nil {
		return Definition{}, false, fmt.Errorf("decode leaderboard definition: %w", err)
	}
	return cloneDefinition(definition), true, nil
}

func (m *RedisManager) saveRecord(ctx context.Context, definition Definition, record Record) error {
	return m.saveRecordDetails(ctx, definition, record, HistoryActionSubmit, HistoryDetails{})
}

func (m *RedisManager) saveRecordDetails(ctx context.Context, definition Definition, record Record, action string, details HistoryDetails) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	history, err := m.historyData(action, definition.ID, &record, nil, record.UpdatedAt, details)
	if err != nil {
		return err
	}
	_, err = m.redisEvalInt(ctx, redisSaveRecord, []string{
		m.recordsKey(definition.ID),
		m.zsetKey(definition.ID),
		m.historyKey(definition.ID),
	}, record.OwnerID, data, redisRankScore(record, definition.SortOrder), history, m.historyLimitValue())
	if err != nil {
		return err
	}
	return m.appendHistoryData(ctx, history)
}

func (m *RedisManager) record(ctx context.Context, leaderboardID string, ownerID string) (Record, bool, error) {
	leaderboardID = strings.TrimSpace(leaderboardID)
	ownerID = strings.TrimSpace(ownerID)
	if leaderboardID == "" || ownerID == "" {
		return Record{}, false, nil
	}
	value, err := m.do(ctx, "HGET", m.recordsKey(leaderboardID), ownerID)
	if err != nil {
		return Record{}, false, err
	}
	data, ok, err := redisBytes(value)
	if err != nil || !ok {
		if err != nil {
			return Record{}, false, err
		}
		return Record{}, false, nil
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, false, fmt.Errorf("decode leaderboard record: %w", err)
	}
	return cloneRecord(record), true, nil
}

func (m *RedisManager) rankedRecords(ctx context.Context, leaderboardID string) ([]Record, error) {
	definition, ok, err := m.definition(ctx, leaderboardID)
	if err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, ErrLeaderboardNotFound
	}
	return m.rangeRecords(ctx, definition.ID, 0, -1, 1)
}

func (m *RedisManager) rangeRecords(ctx context.Context, leaderboardID string, start int, stop int, rankStart int) ([]Record, error) {
	value, err := m.do(ctx, "ZREVRANGE", m.zsetKey(leaderboardID), start, stop)
	if err != nil {
		return nil, err
	}
	owners, err := redisStrings(value)
	if err != nil {
		return nil, err
	}
	if len(owners) == 0 {
		return []Record{}, nil
	}
	return m.recordsByOwners(ctx, leaderboardID, owners, rankStart)
}

func (m *RedisManager) recordsByOwners(ctx context.Context, leaderboardID string, owners []string, rankStart int) ([]Record, error) {
	if len(owners) == 0 {
		return []Record{}, nil
	}
	args := make([]any, 0, len(owners)+1)
	args = append(args, m.recordsKey(leaderboardID))
	for _, ownerID := range owners {
		args = append(args, ownerID)
	}
	value, err := m.do(ctx, "HMGET", args...)
	if err != nil {
		return nil, err
	}
	items, found, err := redisBulkBytes(value)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(owners))
	for idx, ownerID := range owners {
		if idx >= len(items) || idx >= len(found) || !found[idx] {
			_, _ = m.do(ctx, "ZREM", m.zsetKey(leaderboardID), ownerID)
			continue
		}
		var record Record
		if err := json.Unmarshal(items[idx], &record); err != nil {
			_, _ = m.do(ctx, "HDEL", m.recordsKey(leaderboardID), ownerID)
			_, _ = m.do(ctx, "ZREM", m.zsetKey(leaderboardID), ownerID)
			continue
		}
		record.Rank = rankStart + idx
		out = append(out, cloneRecord(record))
	}
	return out, nil
}

func (m *RedisManager) recordWithRank(ctx context.Context, leaderboardID string, ownerID string) (Record, bool, error) {
	leaderboardID = strings.TrimSpace(leaderboardID)
	ownerID = strings.TrimSpace(ownerID)
	if leaderboardID == "" {
		return Record{}, false, ErrLeaderboardNotFound
	}
	if ownerID == "" {
		return Record{}, false, nil
	}
	rank, ok, err := m.redisRank(ctx, leaderboardID, ownerID)
	if err != nil {
		return Record{}, false, err
	}
	if !ok {
		return Record{}, false, nil
	}
	record, ok, err := m.record(ctx, leaderboardID, ownerID)
	if err != nil || !ok {
		if err != nil {
			return Record{}, false, err
		}
		_, _ = m.do(ctx, "ZREM", m.zsetKey(leaderboardID), ownerID)
		return Record{}, false, nil
	}
	record.Rank = rank
	return cloneRecord(record), true, nil
}

func (m *RedisManager) redisRank(ctx context.Context, leaderboardID string, ownerID string) (int, bool, error) {
	value, err := m.do(ctx, "ZREVRANK", m.zsetKey(leaderboardID), ownerID)
	if err != nil || value == nil {
		if err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	rank, err := redisInt64(value)
	if err != nil {
		return 0, false, err
	}
	return int(rank) + 1, true, nil
}

func (m *RedisManager) trim(ctx context.Context, definition Definition) error {
	if definition.MaxSize <= 0 {
		return nil
	}
	total, err := m.redisInt(ctx, "ZCARD", m.zsetKey(definition.ID))
	if err != nil {
		return err
	}
	if total <= int64(definition.MaxSize) {
		return nil
	}
	trimmed, err := m.rangeRecords(ctx, definition.ID, definition.MaxSize, -1, definition.MaxSize+1)
	if err != nil {
		return err
	}
	return m.deleteRecords(ctx, definition, trimmed, HistoryActionTrimRecord)
}

func (m *RedisManager) deleteRecords(ctx context.Context, definition Definition, records []Record, action string) error {
	return m.delRecordsDetail(ctx, definition, records, action, HistoryDetails{})
}

func (m *RedisManager) delRecordsDetail(ctx context.Context, definition Definition, records []Record, action string, details HistoryDetails) error {
	if len(records) == 0 {
		return nil
	}
	args := make([]any, 0, 2+len(records)*2)
	args = append(args, len(records), m.historyLimitValue())
	histories := make([][]byte, 0, len(records))
	now := m.nowUTC()
	for _, record := range records {
		history, err := m.historyData(action, definition.ID, &record, nil, now, details)
		if err != nil {
			return err
		}
		args = append(args, record.OwnerID, history)
		histories = append(histories, history)
	}
	_, err := m.redisEvalInt(ctx, redisDeleteRecords, []string{
		m.recordsKey(definition.ID),
		m.zsetKey(definition.ID),
		m.historyKey(definition.ID),
	}, args...)
	if err != nil {
		return err
	}
	return m.appendHistoryData(ctx, histories...)
}

func (m *RedisManager) appendHistoryPayload(ctx context.Context, leaderboardID string, history []byte) error {
	if len(history) == 0 {
		return nil
	}
	historyLimit := m.historyLimitValue()
	if historyLimit > 0 {
		if _, err := m.do(ctx, "RPUSH", m.historyKey(leaderboardID), history); err != nil {
			return err
		}
		if _, err := m.do(ctx, "LTRIM", m.historyKey(leaderboardID), -historyLimit, -1); err != nil {
			return err
		}
	}
	return m.appendHistoryData(ctx, history)
}
