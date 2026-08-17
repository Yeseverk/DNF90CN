package moderation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNilSQLSanctionDB       = errors.New("moderation sql db is required")
	ErrInvalidSanctionTable   = errors.New("moderation sanction table name is invalid")
	ErrUnsupportedSQLSanction = errors.New("moderation sql operation is unsupported")
	ErrSQLSanctionRows        = errors.New("moderation sql rows are required")
)

const defaultSanctionTable = "moderation_sanctions"

type SQLSanctionRows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}

type SQLSanctionDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (SQLSanctionRows, error)
	QueryRowContext(context.Context, string, ...any) SQLSanctionRow
}

type SQLSanctionRow interface {
	Scan(...any) error
}

type SQLSanctionDBAdapter struct {
	DB *sql.DB
}

func (a SQLSanctionDBAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if a.DB == nil {
		return nil, ErrNilSQLSanctionDB
	}
	return a.DB.ExecContext(ctx, query, args...) // #nosec G701 -- 适配层只执行本包已构造的 SQL，业务值仍由调用方参数绑定。
}

func (a SQLSanctionDBAdapter) QueryContext(ctx context.Context, query string, args ...any) (SQLSanctionRows, error) {
	if a.DB == nil {
		return nil, ErrNilSQLSanctionDB
	}
	// #nosec G701 -- SQL 由本包白名单表名构造，业务值仍由调用方参数绑定。
	return a.DB.QueryContext(ctx, query, args...) //nolint:rowserrcheck // 适配层只返回 rows，调用方负责遍历结束后的 Err 检查。
}

func (a SQLSanctionDBAdapter) QueryRowContext(ctx context.Context, query string, args ...any) SQLSanctionRow {
	if a.DB == nil {
		return errSQLRow{err: ErrNilSQLSanctionDB}
	}
	return a.DB.QueryRowContext(ctx, query, args...)
}

type SQLSanctionStoreOptions struct {
	Table string
	Now   func() time.Time
}

type SQLSanctionStore struct {
	db    SQLSanctionDB
	table string
	now   func() time.Time
}

func NewSQLSanctionStore(db SQLSanctionDB, options SQLSanctionStoreOptions) (*SQLSanctionStore, error) {
	if db == nil {
		return nil, ErrNilSQLSanctionDB
	}
	table, err := normSanctionTable(options.Table)
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &SQLSanctionStore{db: db, table: table, now: now}, nil
}

func NewSQLSanctionStoreFromDB(db *sql.DB, options SQLSanctionStoreOptions) (*SQLSanctionStore, error) {
	if db == nil {
		return nil, ErrNilSQLSanctionDB
	}
	return NewSQLSanctionStore(SQLSanctionDBAdapter{DB: db}, options)
}

func (s *SQLSanctionStore) EnsureSchema(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilSQLSanctionDB
	}
	_, err := s.db.ExecContext(ctx, s.Schema())
	return err
}

func (s *SQLSanctionStore) Schema() string {
	table := quoteSanctionID(s.table)
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id VARCHAR(191) NOT NULL,
  subject VARCHAR(191) NOT NULL,
  scope VARCHAR(64) NOT NULL DEFAULT '',
  kind VARCHAR(32) NOT NULL,
  reason VARCHAR(512) NOT NULL DEFAULT '',
  source VARCHAR(128) NOT NULL DEFAULT '',
  until_at DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL,
  sanction_json JSON NOT NULL,
  PRIMARY KEY (id),
  KEY idx_moderation_sanctions_subject_scope (subject, scope, kind),
  KEY idx_moderation_sanctions_until (until_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table)
}

func (s *SQLSanctionStore) Upsert(ctx context.Context, sanction Sanction) (Sanction, error) {
	if err := ctxErr(ctx); err != nil {
		return Sanction{}, err
	}
	if s == nil || s.db == nil {
		return Sanction{}, ErrNilSQLSanctionDB
	}
	item, err := normalizeSanction(sanction, s.nowUTC())
	if err != nil {
		return Sanction{}, err
	}
	data, err := json.Marshal(item)
	if err != nil {
		return Sanction{}, err
	}
	until := nullableTime(item.Until)
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO "+quoteSanctionID(s.table)+" (id, subject, scope, kind, reason, source, until_at, created_at, sanction_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE subject=VALUES(subject), scope=VALUES(scope), kind=VALUES(kind), reason=VALUES(reason), source=VALUES(source), until_at=VALUES(until_at), sanction_json=VALUES(sanction_json)",
		item.ID,
		item.Subject,
		item.Scope,
		string(item.Kind),
		item.Reason,
		item.Source,
		until,
		item.CreatedAt,
		string(data),
	)
	if err != nil {
		return Sanction{}, err
	}
	return item, nil
}

func (s *SQLSanctionStore) Remove(ctx context.Context, id string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilSQLSanctionDB
	}
	id = normalizeToken(id)
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidSanction)
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM "+quoteSanctionID(s.table)+" WHERE id = ?", id)
	if err != nil {
		return err
	}
	if result != nil {
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrSanctionMissing
		}
	}
	return nil
}

func (s *SQLSanctionStore) Active(ctx context.Context, query SanctionQuery) ([]Sanction, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, ErrNilSQLSanctionDB
	}
	query.Subject = normalizeToken(query.Subject)
	query.Scope = normalizeToken(query.Scope)
	if query.Subject == "" {
		return nil, fmt.Errorf("%w: subject is required", ErrInvalidSanction)
	}
	if query.Now.IsZero() {
		query.Now = s.nowUTC()
	} else {
		query.Now = query.Now.UTC()
	}
	sqlText := "SELECT sanction_json FROM " + quoteSanctionID(s.table) + " WHERE subject = ? AND (scope = '' OR scope = 'global' OR scope = ?) AND (until_at IS NULL OR until_at > ?)"
	args := []any{query.Subject, query.Scope, query.Now}
	if len(query.Kinds) > 0 {
		placeholders := make([]string, 0, len(query.Kinds))
		for _, kind := range query.Kinds {
			if kind == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, string(kind))
		}
		if len(placeholders) > 0 {
			sqlText += " AND kind IN (" + strings.Join(placeholders, ",") + ")"
		}
	}
	sqlText += " ORDER BY subject, scope, kind, id"
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, ErrSQLSanctionRows
	}
	items, err := scanSanctionRows(rows)
	if err != nil {
		return nil, err
	}
	out := make([]Sanction, 0, len(items))
	for _, item := range items {
		if item.ActiveAt(query.Now) {
			out = append(out, item)
		}
	}
	sortSanctions(out)
	return out, nil
}

func (s *SQLSanctionStore) Snapshot(ctx context.Context) (SanctionSnapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return SanctionSnapshot{}, err
	}
	if s == nil || s.db == nil {
		return SanctionSnapshot{}, ErrNilSQLSanctionDB
	}
	rows, err := s.db.QueryContext(ctx, "SELECT sanction_json FROM "+quoteSanctionID(s.table)+" ORDER BY subject, scope, kind, id")
	if err != nil {
		return SanctionSnapshot{}, err
	}
	if rows == nil {
		return SanctionSnapshot{}, ErrSQLSanctionRows
	}
	items, err := scanSanctionRows(rows)
	if err != nil {
		return SanctionSnapshot{}, err
	}
	sortSanctions(items)
	return SanctionSnapshot{Items: items}, nil
}

func scanSanctionRows(rows SQLSanctionRows) (items []Sanction, err error) {
	if rows == nil {
		return nil, ErrSQLSanctionRows
	}
	defer closeSanctionRowsErr(rows, &err)
	items = make([]Sanction, 0)
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var item Sanction
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, fmt.Errorf("decode moderation sql sanction: %w", err)
		}
		items = append(items, cloneSanction(item))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func closeSanctionRowsErr(rows SQLSanctionRows, err *error) {
	if rows == nil || err == nil {
		return
	}
	if closeErr := rows.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

func normSanctionTable(table string) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		table = defaultSanctionTable
	}
	if !sanctionTablePattern.MatchString(table) {
		return "", ErrInvalidSanctionTable
	}
	return table, nil
}

func quoteSanctionID(identifier string) string {
	return "`" + identifier + "`"
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func (s *SQLSanctionStore) nowUTC() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

type errSQLRow struct {
	err error
}

func (r errSQLRow) Scan(...any) error {
	return r.err
}

var sanctionTablePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
