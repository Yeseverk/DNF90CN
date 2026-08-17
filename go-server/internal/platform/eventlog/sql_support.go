package eventlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

func (s *SQLStore) queryOne(ctx context.Context, query string, args ...any) (Event, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, false, err
	}
	if s == nil || s.db == nil {
		return Event{}, false, ErrNilSQLDB
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Event{}, false, err
	}
	events, err := scanEvents(rows)
	if err != nil {
		return Event{}, false, err
	}
	if len(events) == 0 {
		return Event{}, false, nil
	}
	return events[0], true, nil
}

func (s *SQLStore) reserveEventID(ctx context.Context, event Event) error {
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO "+quoteIdentifier(s.eventIDTable())+" (id, event_created_at, created_at) VALUES (?, ?, ?)",
		event.ID,
		event.CreatedAt,
		createdAt,
	)
	return err
}

func (s *SQLStore) releaseEventID(ctx context.Context, event Event) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM "+quoteIdentifier(s.eventIDTable())+" WHERE id = ? AND event_created_at = ?",
		event.ID,
		event.CreatedAt,
	)
	return err
}

func (s *SQLStore) releaseEventIDSafe(parent context.Context, event Event) {
	cleanupCtx, cancel := sqlCleanupCtx(parent)
	defer cancel()
	_ = s.releaseEventID(cleanupCtx, event)
}

func scanEvents(rows SQLRows) (events []Event, err error) {
	if rows == nil {
		return nil, ErrInvalidSQLRows
	}
	defer closeSQLRowsErr(rows, &err)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func closeSQLRowsErr(rows SQLRows, err *error) {
	if rows == nil || err == nil {
		return
	}
	if closeErr := rows.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

func scanEvent(rows SQLRows) (Event, error) {
	var event Event
	var aggregateID sql.NullString
	var idempotencyKey sql.NullString
	var payload string
	var headers sql.NullString
	var publishedAt sql.NullTime
	var lastError sql.NullString
	if err := rows.Scan(
		&event.ID,
		&event.Stream,
		&event.Type,
		&aggregateID,
		&idempotencyKey,
		&payload,
		&headers,
		&event.Status,
		&event.Attempts,
		&event.AvailableAt,
		&event.CreatedAt,
		&event.UpdatedAt,
		&publishedAt,
		&lastError,
	); err != nil {
		return Event{}, err
	}
	if aggregateID.Valid {
		event.AggregateID = aggregateID.String
	}
	if idempotencyKey.Valid {
		event.IdempotencyKey = idempotencyKey.String
	}
	event.Payload = json.RawMessage(payload)
	if headers.Valid && strings.TrimSpace(headers.String) != "" {
		if err := json.Unmarshal([]byte(headers.String), &event.Headers); err != nil {
			return Event{}, fmt.Errorf("decode event headers: %w", err)
		}
	}
	if publishedAt.Valid {
		event.PublishedAt = publishedAt.Time
	}
	if lastError.Valid {
		event.LastError = lastError.String
	}
	return cloneEvent(event), nil
}

func eventColumns() string {
	return "id, stream, event_type, aggregate_id, idempotency_key, payload_json, headers_json, status, attempts, available_at, created_at, updated_at, published_at, last_error"
}

func prefixedEventColumns(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return eventColumns()
	}
	columns := strings.Split(eventColumns(), ", ")
	for idx, column := range columns {
		columns[idx] = prefix + "." + column
	}
	return strings.Join(columns, ", ")
}

func (s *SQLStore) reserveIdempotency(ctx context.Context, event Event) error {
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO "+quoteIdentifier(s.idempotencyTable())+" (idempotency_key, event_id, event_created_at, created_at) VALUES (?, ?, ?, ?)",
		event.IdempotencyKey,
		event.ID,
		event.CreatedAt,
		createdAt,
	)
	return err
}

func (s *SQLStore) releaseIdempotency(ctx context.Context, event Event) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM "+quoteIdentifier(s.idempotencyTable())+" WHERE idempotency_key = ? AND event_id = ?",
		event.IdempotencyKey,
		event.ID,
	)
	return err
}

func (s *SQLStore) releaseIdemSafe(parent context.Context, event Event) {
	cleanupCtx, cancel := sqlCleanupCtx(parent)
	defer cancel()
	_ = s.releaseIdempotency(cleanupCtx, event)
}

func sqlCleanupCtx(parent context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	return context.WithTimeout(base, defPubStateTimeout)
}

func (s *SQLStore) idempotencyTable() string {
	if s == nil || strings.TrimSpace(s.table) == "" {
		return defaultSQLTable + "_idempotency"
	}
	return s.table + "_idempotency"
}

func (s *SQLStore) eventIDTable() string {
	if s == nil || strings.TrimSpace(s.table) == "" {
		return defaultSQLTable + "_ids"
	}
	return s.table + "_ids"
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func nullableJSON(value map[string]string) (any, error) {
	if len(value) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func checkAffected(result sql.Result) error {
	if result == nil {
		return nil
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrEventNotFound
	}
	return nil
}

func (s *SQLStore) execClaimUpdate(ctx context.Context, claimDeadline time.Time, query string, claimQuery string, args ...any) (sql.Result, error) {
	if claimDeadline.IsZero() {
		return s.db.ExecContext(ctx, query, args...)
	}
	// 带 claimDeadline 的更新只允许当前 claim 持有者写状态；RowsAffected=0 表示 claim 已丢失。
	claimArgs := append(append([]any(nil), args...), StatusProcessing, claimDeadline.UTC())
	return s.db.ExecContext(ctx, claimQuery, claimArgs...)
}

func checkClaimAffected(result sql.Result, claimDeadline time.Time) error {
	if result == nil {
		return nil
	}
	// claim 过期后其他 worker 可能重新领取；此时旧 worker 的状态更新必须被拒绝。
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		if claimDeadline.IsZero() {
			return ErrEventNotFound
		}
		return ErrClaimLost
	}
	return nil
}

func normalizeTable(table string) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		table = defaultSQLTable
	}
	if !sqlTablePattern.MatchString(table) {
		return "", ErrInvalidTable
	}
	return table, nil
}

func quoteIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	if count == 1 {
		return "?"
	}
	var b strings.Builder
	for i := 0; i < count; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('?')
	}
	return b.String()
}

var sqlTablePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func defaultPartStart() time.Time {
	start, err := time.Parse("2006-01-02", defPartStartYMD)
	if err != nil {
		return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	return start.UTC()
}

func monthlyPartitions(start time.Time, months int) string {
	if months <= 0 {
		months = defPartMonths
	}
	start = time.Date(start.UTC().Year(), start.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	var b strings.Builder
	for idx := 0; idx < months; idx++ {
		lessThan := start.AddDate(0, idx+1, 0)
		partitionMonth := start.AddDate(0, idx, 0)
		if idx > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, "  PARTITION p%04d%02d VALUES LESS THAN ('%04d-%02d-01')",
			partitionMonth.Year(), partitionMonth.Month(),
			lessThan.Year(), lessThan.Month(),
		)
	}
	b.WriteString(",\n  PARTITION pmax VALUES LESS THAN (MAXVALUE)")
	return b.String()
}
