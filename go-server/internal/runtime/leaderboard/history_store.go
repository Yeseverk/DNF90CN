package leaderboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrNilHistoryStoreDB           = errors.New("leaderboard history sql db is required")
	ErrInvalidTableName            = errors.New("leaderboard history table name is invalid")
	ErrHistoryAppendRollbackMissed = errors.New("leaderboard history append rollback missed")
	ErrSQLHistoryRows              = errors.New("leaderboard sql history rows are required")
)

const defaultHistoryTable = "leaderboard_history"

type MemoryHistoryStore struct {
	mu      sync.Mutex
	limit   int
	entries map[string][]HistoryEntry
}

func NewMemoryHistoryStore(limit int) *MemoryHistoryStore {
	return &MemoryHistoryStore{
		limit:   limit,
		entries: make(map[string][]HistoryEntry),
	}
}

func (s *MemoryHistoryStore) Append(ctx context.Context, entry HistoryEntry) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return errors.New("leaderboard memory history store is nil")
	}
	entry, err := normHistStore(entry)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.appendLocked(entry)
	s.mu.Unlock()
	return nil
}

func (s *MemoryHistoryStore) appendWithRollback(ctx context.Context, entry HistoryEntry) (histUndo, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("leaderboard memory history store is nil")
	}
	entry, err := normHistStore(entry)
	if err != nil {
		return nil, err
	}
	leaderboardID := entry.LeaderboardID
	s.mu.Lock()
	before := cloneHistoryEntries(s.entries[leaderboardID])
	s.appendLocked(entry)
	s.mu.Unlock()
	return func(ctx context.Context) error {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		s.mu.Lock()
		if len(before) == 0 {
			delete(s.entries, leaderboardID)
		} else {
			s.entries[leaderboardID] = cloneHistoryEntries(before)
		}
		s.mu.Unlock()
		return nil
	}, nil
}

func (s *MemoryHistoryStore) appendLocked(entry HistoryEntry) {
	if s.entries == nil {
		s.entries = make(map[string][]HistoryEntry)
	}
	s.entries[entry.LeaderboardID] = append(s.entries[entry.LeaderboardID], cloneHistoryEntry(entry))
	if s.limit > 0 && len(s.entries[entry.LeaderboardID]) > s.limit {
		s.entries[entry.LeaderboardID] = append([]HistoryEntry(nil), s.entries[entry.LeaderboardID][len(s.entries[entry.LeaderboardID])-s.limit:]...)
	}
}

func (s *MemoryHistoryStore) List(ctx context.Context, leaderboardID string, limit int) ([]HistoryEntry, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("leaderboard memory history store is nil")
	}
	leaderboardID = strings.TrimSpace(leaderboardID)
	if leaderboardID == "" {
		return nil, ErrLeaderboardNotFound
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	s.mu.Lock()
	entries := s.entries[leaderboardID]
	start := 0
	if limit > 0 && len(entries) > limit {
		start = len(entries) - limit
	}
	out := cloneHistoryEntries(entries[start:])
	s.mu.Unlock()
	return out, nil
}

type SQLHistoryRows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}

type SQLHistoryDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryHistoryContext(context.Context, string, ...any) (SQLHistoryRows, error)
}

type SQLHistoryDBAdapter struct {
	DB *sql.DB
}

func (a SQLHistoryDBAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if a.DB == nil {
		return nil, ErrNilHistoryStoreDB
	}
	return a.DB.ExecContext(ctx, query, args...)
}

func (a SQLHistoryDBAdapter) QueryHistoryContext(ctx context.Context, query string, args ...any) (SQLHistoryRows, error) { //nolint:rowserrcheck // 适配层只返回 rows，调用方负责遍历结束后的 Err 检查。
	if a.DB == nil {
		return nil, ErrNilHistoryStoreDB
	}
	return a.DB.QueryContext(ctx, query, args...) //nolint:rowserrcheck // rows 返回后由上层统一关闭并检查 Err。
}

type SQLHistoryStoreOptions struct {
	Table string
}

type SQLHistoryStore struct {
	db    SQLHistoryDB
	table string
}

func NewSQLHistoryStore(db SQLHistoryDB, options SQLHistoryStoreOptions) (*SQLHistoryStore, error) {
	if db == nil {
		return nil, ErrNilHistoryStoreDB
	}
	table, err := normHistoryTable(options.Table)
	if err != nil {
		return nil, err
	}
	return &SQLHistoryStore{db: db, table: table}, nil
}

func NewSQLHistoryStoreFromDB(db *sql.DB, options SQLHistoryStoreOptions) (*SQLHistoryStore, error) {
	if db == nil {
		return nil, ErrNilHistoryStoreDB
	}
	return NewSQLHistoryStore(SQLHistoryDBAdapter{DB: db}, options)
}

func (s *SQLHistoryStore) EnsureSchema(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilHistoryStoreDB
	}
	_, err := s.db.ExecContext(ctx, s.Schema())
	return err
}

func (s *SQLHistoryStore) Schema() string {
	table := quoteHistoryID(s.table)
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  leaderboard_id VARCHAR(128) NOT NULL,
  owner_id VARCHAR(128) NOT NULL DEFAULT '',
  action VARCHAR(64) NOT NULL,
  record_json JSON NULL,
  definition_json JSON NULL,
  entry_json JSON NOT NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_leaderboard_history_board_id (leaderboard_id, id),
  KEY idx_leaderboard_history_owner_id (owner_id, id),
  KEY idx_leaderboard_history_action_id (action, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, table)
}

func (s *SQLHistoryStore) Append(ctx context.Context, entry HistoryEntry) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilHistoryStoreDB
	}
	_, err := s.appendWithRollback(ctx, entry)
	return err
}

func (s *SQLHistoryStore) appendWithRollback(ctx context.Context, entry HistoryEntry) (histUndo, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, ErrNilHistoryStoreDB
	}
	entry, err := normHistStore(entry)
	if err != nil {
		return nil, err
	}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	recordJSON, err := nullableJSON(entry.Record)
	if err != nil {
		return nil, err
	}
	definitionJSON, err := nullableJSON(entry.Definition)
	if err != nil {
		return nil, err
	}
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO "+quoteHistoryID(s.table)+" (leaderboard_id, owner_id, action, record_json, definition_json, entry_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		entry.LeaderboardID,
		entry.OwnerID,
		entry.Action,
		recordJSON,
		definitionJSON,
		string(entryJSON),
		entry.At,
	)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		return s.rollbackAppend(ctx, entry, result)
	}, nil
}

func (s *SQLHistoryStore) rollbackAppend(ctx context.Context, entry HistoryEntry, result sql.Result) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrNilHistoryStoreDB
	}
	table := quoteHistoryID(s.table)
	if result != nil {
		if id, err := result.LastInsertId(); err == nil && id > 0 {
			deleted, err := s.db.ExecContext(ctx,
				"DELETE FROM "+table+" WHERE id = ? AND leaderboard_id = ? AND owner_id = ? AND action = ?",
				id,
				entry.LeaderboardID,
				entry.OwnerID,
				entry.Action,
			)
			if err != nil {
				return err
			}
			return checkHistUndo(deleted)
		}
	}
	deleted, err := s.db.ExecContext(ctx,
		"DELETE FROM "+table+" WHERE id = (SELECT id FROM (SELECT id FROM "+table+" WHERE leaderboard_id = ? AND owner_id = ? AND action = ? AND created_at = ? ORDER BY id DESC LIMIT 1) AS rollback_candidate)",
		entry.LeaderboardID,
		entry.OwnerID,
		entry.Action,
		entry.At,
	)
	if err != nil {
		return err
	}
	return checkHistUndo(deleted)
}

func checkHistUndo(result sql.Result) error {
	if result == nil {
		return nil
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil
	}
	if affected == 0 {
		return ErrHistoryAppendRollbackMissed
	}
	return nil
}

func normHistStore(entry HistoryEntry) (HistoryEntry, error) {
	entry = cloneHistoryEntry(entry)
	entry.LeaderboardID = strings.TrimSpace(entry.LeaderboardID)
	if entry.LeaderboardID == "" {
		return HistoryEntry{}, ErrLeaderboardNotFound
	}
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	} else {
		entry.At = entry.At.UTC()
	}
	return entry, nil
}

func (s *SQLHistoryStore) List(ctx context.Context, leaderboardID string, limit int) (out []HistoryEntry, err error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, ErrNilHistoryStoreDB
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
	rows, err := s.db.QueryHistoryContext(ctx,
		"SELECT entry_json FROM "+quoteHistoryID(s.table)+" WHERE leaderboard_id = ? ORDER BY id DESC LIMIT ?",
		leaderboardID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, ErrSQLHistoryRows
	}
	defer closeHistRowsErr(rows, &err)
	out = make([]HistoryEntry, 0, limit)
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var entry HistoryEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, fmt.Errorf("decode leaderboard sql history: %w", err)
		}
		out = append(out, cloneHistoryEntry(entry))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	reverseHistory(out)
	return out, nil
}

func closeHistRowsErr(rows SQLHistoryRows, err *error) {
	if rows == nil || err == nil {
		return
	}
	if closeErr := rows.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

func cloneHistoryEntry(entry HistoryEntry) HistoryEntry {
	if entry.Record != nil {
		record := cloneRecord(*entry.Record)
		entry.Record = &record
	}
	entry.Records = cloneRecords(entry.Records)
	if entry.Definition != nil {
		definition := cloneDefinition(*entry.Definition)
		entry.Definition = &definition
	}
	entry.Metadata = cloneStringMap(entry.Metadata)
	return entry
}

func cloneHistoryEntries(entries []HistoryEntry) []HistoryEntry {
	out := make([]HistoryEntry, len(entries))
	for i, entry := range entries {
		out[i] = cloneHistoryEntry(entry)
	}
	return out
}

func reverseHistory(entries []HistoryEntry) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}

func nullableJSON(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func normHistoryTable(table string) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		table = defaultHistoryTable
	}
	if !histTablePattern.MatchString(table) {
		return "", ErrInvalidTableName
	}
	return table, nil
}

func quoteHistoryID(identifier string) string {
	return "`" + identifier + "`"
}

var histTablePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
