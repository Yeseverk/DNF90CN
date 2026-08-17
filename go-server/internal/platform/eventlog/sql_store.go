package eventlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrNilSQLDB 表示 SQL store 缺少可用数据库连接或事务。
	ErrNilSQLDB = errors.New("eventlog sql db is required")

	// ErrInvalidTable 表示配置的 SQL 表名不符合安全标识符规则。
	ErrInvalidTable = errors.New("eventlog table name is invalid")

	// ErrInvalidSQLRows 表示查询返回的 rows 为空或不满足消费契约。
	ErrInvalidSQLRows = errors.New("eventlog sql rows are invalid")
)

const (
	defaultSQLTable = "outbox_events"
	defPartMonths   = 36
	defPartStartYMD = "2026-01-01"
)

// SQLRows 抽象 database/sql.Rows，便于测试替换和 rows.Err 门禁覆盖。
type SQLRows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}

// SQLDB 是 eventlog SQL store 需要的最小数据库执行接口。
type SQLDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (SQLRows, error)
}

// SQLTransaction 是带提交和回滚能力的 SQLDB。
type SQLTransaction interface {
	SQLDB
	Commit() error
	Rollback() error
}

// SQLTransactor 能为 eventlog 追加事件开启事务。
type SQLTransactor interface {
	BeginEventLogTx(context.Context) (SQLTransaction, error)
}

// SQLDBAdapter 把 *sql.DB 适配成 eventlog SQLDB。
type SQLDBAdapter struct {
	DB *sql.DB
}

// SQLTxAdapter 把 *sql.Tx 适配成 eventlog SQLTransaction。
type SQLTxAdapter struct {
	Tx *sql.Tx
}

var (
	_ SQLDB = SQLDBAdapter{}
	_ SQLDB = SQLTxAdapter{}
)

// ExecContext 在底层 *sql.DB 上执行语句。
func (a SQLDBAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if a.DB == nil {
		return nil, ErrNilSQLDB
	}
	return a.DB.ExecContext(ctx, query, args...)
}

// QueryContext 在底层 *sql.DB 上查询 rows，rows 生命周期由调用方负责。
func (a SQLDBAdapter) QueryContext(ctx context.Context, query string, args ...any) (SQLRows, error) { //nolint:rowserrcheck // 适配层只转交 rows，遍历和 Err 检查由上层消费函数负责。
	if a.DB == nil {
		return nil, ErrNilSQLDB
	}
	return a.DB.QueryContext(ctx, query, args...) //nolint:rowserrcheck // 调用方拿到 rows 后统一遍历并检查 Err。
}

// BeginEventLogTx 开启 eventlog 使用的 SQL 事务。
func (a SQLDBAdapter) BeginEventLogTx(ctx context.Context) (SQLTransaction, error) {
	if a.DB == nil {
		return nil, ErrNilSQLDB
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return SQLTxAdapter{Tx: tx}, nil
}

// ExecContext 在底层 *sql.Tx 上执行语句。
func (a SQLTxAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if a.Tx == nil {
		return nil, ErrNilSQLDB
	}
	return a.Tx.ExecContext(ctx, query, args...)
}

// QueryContext 在底层 *sql.Tx 上查询 rows，rows 生命周期由调用方负责。
func (a SQLTxAdapter) QueryContext(ctx context.Context, query string, args ...any) (SQLRows, error) { //nolint:rowserrcheck // 适配层只转交 rows，遍历和 Err 检查由上层消费函数负责。
	if a.Tx == nil {
		return nil, ErrNilSQLDB
	}
	rows, err := a.Tx.QueryContext(ctx, query, args...) //nolint:rowserrcheck // 调用方拿到 rows 后统一遍历并检查 Err。
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Commit 提交底层事务。
func (a SQLTxAdapter) Commit() error {
	if a.Tx == nil {
		return ErrNilSQLDB
	}
	return a.Tx.Commit()
}

// Rollback 回滚底层事务。
func (a SQLTxAdapter) Rollback() error {
	if a.Tx == nil {
		return ErrNilSQLDB
	}
	return a.Tx.Rollback()
}

// SQLStoreOptions 描述 SQL eventlog 的实例名称和主表名。
type SQLStoreOptions struct {
	Name  string
	Table string
}

// SQLStore 使用 SQL 表保存 eventlog/outbox 事件。
type SQLStore struct {
	name   string
	db     SQLDB
	table  string
	closer interface {
		Close() error
	}
}

// NewSQLStore 使用抽象 SQLDB 创建 eventlog store。
func NewSQLStore(db SQLDB, options SQLStoreOptions) (*SQLStore, error) {
	if db == nil {
		return nil, ErrNilSQLDB
	}
	table, err := normalizeTable(options.Table)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "sql-eventlog"
	}
	return &SQLStore{name: name, db: db, table: table}, nil
}

// NewSQLStoreFromDB 使用 *sql.DB 创建 eventlog store，并在 Close 时关闭 DB。
func NewSQLStoreFromDB(db *sql.DB, options SQLStoreOptions) (*SQLStore, error) {
	if db == nil {
		return nil, ErrNilSQLDB
	}
	store, err := NewSQLStore(SQLDBAdapter{DB: db}, options)
	if err != nil {
		return nil, err
	}
	store.closer = db
	return store, nil
}

// NewSQLStoreFromTx 使用现有事务创建 eventlog store，不接管事务提交。
func NewSQLStoreFromTx(tx *sql.Tx, options SQLStoreOptions) (*SQLStore, error) {
	if tx == nil {
		return nil, ErrNilSQLDB
	}
	return NewSQLStore(SQLTxAdapter{Tx: tx}, options)
}

// EnsureSchema 创建 eventlog 主表、事件 ID 表和幂等键表。
func (s *SQLStore) EnsureSchema(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilSQLDB
	}
	for _, statement := range s.SchemaStatements() {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭由 store 接管的底层数据库连接。
func (s *SQLStore) Close() error {
	if s == nil || s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

// Schema 返回完整建表 SQL 文本。
func (s *SQLStore) Schema() string {
	return strings.Join(s.SchemaStatements(), ";\n")
}

// SchemaStatements 返回 eventlog 所需的分表建表语句。
func (s *SQLStore) SchemaStatements() []string {
	table := quoteIdentifier(s.table)
	eventIDTable := quoteIdentifier(s.eventIDTable())
	idempotencyTable := quoteIdentifier(s.idempotencyTable())
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id VARCHAR(128) NOT NULL,
  stream VARCHAR(128) NOT NULL,
  event_type VARCHAR(128) NOT NULL,
  aggregate_id VARCHAR(128) NULL,
  idempotency_key VARCHAR(191) NULL,
  payload_json JSON NOT NULL,
  headers_json JSON NULL,
  status VARCHAR(32) NOT NULL,
  attempts INT NOT NULL DEFAULT 0,
  available_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  published_at DATETIME(6) NULL,
  last_error TEXT NULL,
  PRIMARY KEY (id, created_at),
  KEY idx_outbox_idempotency (idempotency_key, created_at),
  KEY idx_outbox_pending (status, available_at, id, created_at),
  KEY idx_outbox_aggregate_id (aggregate_id, created_at),
  KEY idx_outbox_stream (stream, aggregate_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
PARTITION BY RANGE COLUMNS(created_at) (
%s
)`, table, monthlyPartitions(defaultPartStart(), defPartMonths)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id VARCHAR(128) NOT NULL,
  event_created_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_outbox_event_ids_created_at (event_created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, eventIDTable),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  idempotency_key VARCHAR(191) NOT NULL,
  event_id VARCHAR(128) NOT NULL,
  event_created_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (idempotency_key),
  KEY idx_outbox_idempotency_event (event_id, event_created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, idempotencyTable),
	}
}

// Append 在 SQL 中追加事件，并用辅助表保证事件 ID 与幂等键唯一。
func (s *SQLStore) Append(ctx context.Context, event Event) (Event, error) {
	if err := ctxErr(ctx); err != nil {
		return Event{}, err
	}
	if s == nil || s.db == nil {
		return Event{}, ErrNilSQLDB
	}
	if txDB, ok := s.db.(SQLTransactor); ok {
		return s.appendInTransaction(ctx, txDB, event)
	}
	return s.appendDirect(ctx, event)
}

func (s *SQLStore) appendInTransaction(ctx context.Context, txDB SQLTransactor, event Event) (Event, error) {
	tx, err := txDB.BeginEventLogTx(ctx)
	if err != nil {
		return Event{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	txStore := *s
	txStore.db = tx
	appended, err := txStore.appendDirect(ctx, event)
	if err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		if recovered, ok := s.recoverAppend(ctx, appended); ok {
			committed = true
			return recovered, nil
		}
		return Event{}, err
	}
	committed = true
	return appended, nil
}

func (s *SQLStore) recoverAppend(parent context.Context, event Event) (Event, bool) {
	ctx, cancel := sqlCleanupCtx(parent)
	defer cancel()
	if event.IdempotencyKey != "" {
		existing, ok, err := s.GetByIdempotencyKey(ctx, event.IdempotencyKey)
		if err == nil && ok && sameIdempotentEvent(existing, event) {
			return existing, true
		}
	}
	existing, ok, err := s.Get(ctx, event.ID)
	if err == nil && ok && sameIdempotentEvent(existing, event) {
		return existing, true
	}
	return Event{}, false
}

func (s *SQLStore) appendDirect(ctx context.Context, event Event) (Event, error) {
	// Append 先查幂等键和事件 ID，再用辅助表保留 ID；主表按 created_at 分区，不能单靠分区主键保证 ID 全局唯一。
	if event.IdempotencyKey != "" {
		if existing, ok, err := s.GetByIdempotencyKey(ctx, event.IdempotencyKey); err != nil {
			return Event{}, err
		} else if ok {
			if !sameIdempotentEvent(existing, event) {
				return Event{}, ErrIdempotencyConflict
			}
			return existing, nil
		}
	}
	if _, ok, err := s.Get(ctx, event.ID); err != nil {
		return Event{}, err
	} else if ok {
		return Event{}, ErrEventExists
	}
	reservedID := false
	if err := s.reserveEventID(ctx, event); err != nil {
		if _, ok, getErr := s.Get(ctx, event.ID); getErr != nil {
			return Event{}, getErr
		} else if ok {
			return Event{}, ErrEventExists
		}
		return Event{}, err
	}
	reservedID = true
	reservedIdempotency := false
	if event.IdempotencyKey != "" {
		if err := s.reserveIdempotency(ctx, event); err != nil {
			if reservedID {
				s.releaseEventIDSafe(ctx, event)
			}
			if existing, ok, getErr := s.GetByIdempotencyKey(ctx, event.IdempotencyKey); getErr != nil {
				return Event{}, getErr
			} else if ok {
				if !sameIdempotentEvent(existing, event) {
					return Event{}, ErrIdempotencyConflict
				}
				return existing, nil
			}
			return Event{}, err
		}
		reservedIdempotency = true
	}
	payload := string(event.Payload)
	headers, err := nullableJSON(event.Headers)
	if err != nil {
		if reservedID {
			s.releaseEventIDSafe(ctx, event)
		}
		if reservedIdempotency {
			s.releaseIdemSafe(ctx, event)
		}
		return Event{}, err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO "+quoteIdentifier(s.table)+" (id, stream, event_type, aggregate_id, idempotency_key, payload_json, headers_json, status, attempts, available_at, created_at, updated_at, published_at, last_error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		event.ID,
		event.Stream,
		event.Type,
		nullableString(event.AggregateID),
		nullableString(event.IdempotencyKey),
		payload,
		headers,
		event.Status,
		event.Attempts,
		event.AvailableAt,
		event.CreatedAt,
		event.UpdatedAt,
		nullableTime(event.PublishedAt),
		nullableString(event.LastError),
	)
	if err != nil {
		if reservedID {
			s.releaseEventIDSafe(ctx, event)
		}
		if reservedIdempotency {
			s.releaseIdemSafe(ctx, event)
		}
		if event.IdempotencyKey != "" {
			if existing, ok, getErr := s.GetByIdempotencyKey(ctx, event.IdempotencyKey); getErr != nil {
				return Event{}, getErr
			} else if ok {
				if !sameIdempotentEvent(existing, event) {
					return Event{}, ErrIdempotencyConflict
				}
				return existing, nil
			}
		}
		return Event{}, err
	}
	return cloneEvent(event), nil
}

// Get 按事件 ID 查询 SQL 中的事件。
func (s *SQLStore) Get(ctx context.Context, id string) (Event, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Event{}, false, nil
	}
	return s.queryOne(ctx, "SELECT "+eventColumns()+" FROM "+quoteIdentifier(s.table)+" WHERE id = ? LIMIT 1", id)
}

// GetByIdempotencyKey 按幂等键查询 SQL 中的事件。
func (s *SQLStore) GetByIdempotencyKey(ctx context.Context, key string) (Event, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Event{}, false, nil
	}
	return s.queryOne(ctx,
		"SELECT "+prefixedEventColumns("e")+" FROM "+quoteIdentifier(s.table)+" e JOIN "+quoteIdentifier(s.idempotencyTable())+" i ON e.id = i.event_id AND e.created_at = i.event_created_at WHERE i.idempotency_key = ? LIMIT 1",
		key,
	)
}

// ListByStreamAggregate 按事件流和聚合 ID 查询最近事件。
func (s *SQLStore) ListByStreamAggregate(ctx context.Context, stream, aggregateID string, limit int) ([]Event, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, ErrNilSQLDB
	}
	stream = strings.TrimSpace(stream)
	aggregateID = strings.TrimSpace(aggregateID)
	if stream == "" {
		return nil, fmt.Errorf("%w: stream is required", ErrInvalidEvent)
	}
	if limit <= 0 {
		limit = defaultPublishLimit
	}
	query := "SELECT " + eventColumns() + " FROM " + quoteIdentifier(s.table) + " WHERE stream = ?"
	args := []any{stream}
	if aggregateID == "" {
		query += " AND aggregate_id IS NULL"
	} else {
		query += " AND aggregate_id = ?"
		args = append(args, aggregateID)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// ClaimPending 领取一批 SQL 中到期的待发布事件。
func (s *SQLStore) ClaimPending(ctx context.Context, limit int, now time.Time, claimTimeout time.Duration) ([]Event, error) {
	return s.claimPending(ctx, limit, now, claimTimeout, nil)
}

// ClaimPendingExcept 领取到期事件时排除指定事件流。
func (s *SQLStore) ClaimPendingExcept(ctx context.Context, limit int, now time.Time, claimTimeout time.Duration, excludeStreams []string) ([]Event, error) {
	return s.claimPending(ctx, limit, now, claimTimeout, normalizeStreamList(excludeStreams))
}

func (s *SQLStore) claimPending(ctx context.Context, limit int, now time.Time, claimTimeout time.Duration, excludeStreams []string) ([]Event, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, ErrNilSQLDB
	}
	if limit <= 0 {
		limit = defaultPublishLimit
	}
	now = now.UTC()
	if claimTimeout <= 0 {
		claimTimeout = defaultClaimTimeout
	}
	claimUntil := now.Add(claimTimeout)
	// SQL claim 通过状态和 available_at 条件竞争领取，领取成功后 available_at 变成本次 claim deadline。
	events, err := s.fetchClaimable(ctx, limit, now, excludeStreams)
	if err != nil {
		return nil, err
	}
	claimed := make([]Event, 0, len(events))
	for _, event := range events {
		result, err := s.db.ExecContext(ctx,
			"UPDATE "+quoteIdentifier(s.table)+" SET status = ?, available_at = ?, updated_at = ? WHERE id = ? AND status IN (?, ?, ?) AND available_at <= ?",
			StatusProcessing,
			claimUntil,
			now,
			event.ID,
			StatusPending,
			StatusFailed,
			StatusProcessing,
			now,
		)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			continue
		}
		event.Status = StatusProcessing
		event.AvailableAt = claimUntil
		event.UpdatedAt = now
		claimed = append(claimed, cloneEvent(event))
	}
	return claimed, nil
}

func (s *SQLStore) fetchClaimable(ctx context.Context, limit int, now time.Time, excludeStreams []string) ([]Event, error) {
	query := "SELECT " + eventColumns() + " FROM " + quoteIdentifier(s.table) + " WHERE status IN (?, ?, ?) AND available_at <= ?"
	args := []any{
		StatusPending,
		StatusFailed,
		StatusProcessing,
		now,
	}
	if len(excludeStreams) > 0 {
		query += " AND stream NOT IN (" + placeholders(len(excludeStreams)) + ")"
		for _, stream := range excludeStreams {
			args = append(args, stream)
		}
	}
	query += " ORDER BY available_at ASC, id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// MarkPublished 将当前 claim 持有的 SQL 事件推进为已发布。
func (s *SQLStore) MarkPublished(ctx context.Context, id string, publishedAt time.Time, claimDeadline time.Time) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilSQLDB
	}
	publishedAt = publishedAt.UTC()
	result, err := s.execClaimUpdate(ctx, claimDeadline,
		"UPDATE "+quoteIdentifier(s.table)+" SET status = ?, published_at = ?, updated_at = ?, last_error = NULL WHERE id = ?",
		"UPDATE "+quoteIdentifier(s.table)+" SET status = ?, published_at = ?, updated_at = ?, last_error = NULL WHERE id = ? AND status = ? AND available_at = ?",
		StatusPublished,
		publishedAt,
		publishedAt,
		id,
	)
	if err != nil {
		return err
	}
	return checkClaimAffected(result, claimDeadline)
}

// MarkFailed 将当前 claim 持有的 SQL 事件推进为失败并设置下次重试时间。
func (s *SQLStore) MarkFailed(ctx context.Context, id string, message string, nextAttempt time.Time, updatedAt time.Time, claimDeadline time.Time) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilSQLDB
	}
	result, err := s.execClaimUpdate(ctx, claimDeadline,
		"UPDATE "+quoteIdentifier(s.table)+" SET status = ?, attempts = attempts + 1, available_at = ?, updated_at = ?, last_error = ? WHERE id = ?",
		"UPDATE "+quoteIdentifier(s.table)+" SET status = ?, attempts = attempts + 1, available_at = ?, updated_at = ?, last_error = ? WHERE id = ? AND status = ? AND available_at = ?",
		StatusFailed,
		nextAttempt.UTC(),
		updatedAt.UTC(),
		message,
		id,
	)
	if err != nil {
		return err
	}
	return checkClaimAffected(result, claimDeadline)
}

// MarkDeadLetter 将当前 claim 持有的 SQL 事件推进为死信。
func (s *SQLStore) MarkDeadLetter(ctx context.Context, id string, message string, updatedAt time.Time, claimDeadline time.Time) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilSQLDB
	}
	updatedAt = updatedAt.UTC()
	result, err := s.execClaimUpdate(ctx, claimDeadline,
		"UPDATE "+quoteIdentifier(s.table)+" SET status = ?, attempts = attempts + 1, available_at = ?, updated_at = ?, last_error = ? WHERE id = ?",
		"UPDATE "+quoteIdentifier(s.table)+" SET status = ?, attempts = attempts + 1, available_at = ?, updated_at = ?, last_error = ? WHERE id = ? AND status = ? AND available_at = ?",
		StatusDeadLetter,
		updatedAt,
		updatedAt,
		message,
		id,
	)
	if err != nil {
		return err
	}
	return checkClaimAffected(result, claimDeadline)
}

// ListDeadLetters 查询 SQL 中的死信事件。
func (s *SQLStore) ListDeadLetters(ctx context.Context, limit int) ([]Event, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, ErrNilSQLDB
	}
	if limit < 0 {
		return nil, ErrInvalidLimit
	}
	if limit == 0 {
		limit = defDeadLetterLimit
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+eventColumns()+" FROM "+quoteIdentifier(s.table)+" WHERE status = ? ORDER BY updated_at ASC, id ASC LIMIT ?",
		StatusDeadLetter,
		limit,
	)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// RequeueDeadLetter 把 SQL 中的死信事件重新放回待发布队列。
func (s *SQLStore) RequeueDeadLetter(ctx context.Context, id string, availableAt time.Time, updatedAt time.Time, preserveAttempts bool) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilSQLDB
	}
	availableAt = availableAt.UTC()
	updatedAt = updatedAt.UTC()
	var (
		result sql.Result
		err    error
	)
	if preserveAttempts {
		result, err = s.db.ExecContext(ctx,
			"UPDATE "+quoteIdentifier(s.table)+" SET status = ?, available_at = ?, updated_at = ?, published_at = NULL, last_error = NULL WHERE id = ? AND status = ?",
			StatusPending,
			availableAt,
			updatedAt,
			id,
			StatusDeadLetter,
		)
	} else {
		result, err = s.db.ExecContext(ctx,
			"UPDATE "+quoteIdentifier(s.table)+" SET status = ?, attempts = 0, available_at = ?, updated_at = ?, published_at = NULL, last_error = NULL WHERE id = ? AND status = ?",
			StatusPending,
			availableAt,
			updatedAt,
			id,
			StatusDeadLetter,
		)
	}
	if err != nil {
		return err
	}
	return checkAffected(result)
}

// Snapshot 汇总 SQL store 当前各状态数量和最早到期时间。
func (s *SQLStore) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return Snapshot{}, err
	}
	if s == nil || s.db == nil {
		return Snapshot{}, ErrNilSQLDB
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+eventColumns()+" FROM "+quoteIdentifier(s.table)+" ORDER BY created_at ASC, id ASC")
	if err != nil {
		return Snapshot{}, err
	}
	events, err := scanEvents(rows)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Name: s.name, Total: len(events), ByStatus: make(map[string]int)}
	for _, event := range events {
		snapshot.ByStatus[event.Status]++
		if isDueStatus(event.Status) && (snapshot.OldestDue.IsZero() || event.AvailableAt.Before(snapshot.OldestDue)) {
			snapshot.OldestDue = event.AvailableAt
		}
	}
	if len(snapshot.ByStatus) == 0 {
		snapshot.ByStatus = nil
	}
	return snapshot, nil
}
