package storageobject

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrSQLDBRequired  = errors.New("storage object sql db is required")
	ErrInvalidTable   = errors.New("storage object sql table is invalid")
	ErrInvalidSQLRows = errors.New("storage object sql rows are invalid")
)

const defaultSQLTable = "storage_objects"

var sqlTablePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type SQLStatements struct {
	Table             string
	EnsureSchema      string
	EnsureIndexColumn string
	Insert            string
	Upsert            string
	UpdateWithVersion string
	SelectOne         string
	Delete            string
	DeleteWithVersion string
	List              string
}

type SQLStore struct {
	db    *sql.DB
	now   func() time.Time
	stmts SQLStatements
}

type SQLOptions struct {
	DB    *sql.DB
	Table string
	Now   func() time.Time
}

func NewSQLStore(options SQLOptions) (*SQLStore, error) {
	if options.DB == nil {
		return nil, ErrSQLDBRequired
	}
	stmts, err := NewSQLStatements(options.Table)
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &SQLStore{db: options.DB, now: now, stmts: stmts}, nil
}

func NewSQLStatements(table string) (SQLStatements, error) {
	table, err := normalizeSQLTable(table)
	if err != nil {
		return SQLStatements{}, err
	}
	quoted := "`" + table + "`"
	return SQLStatements{
		Table: table,
		EnsureSchema: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  collection VARCHAR(128) NOT NULL,
  object_key VARCHAR(256) NOT NULL,
  user_id VARCHAR(128) NOT NULL DEFAULT '',
  value_json JSON NOT NULL,
  index_json JSON NULL,
  version VARCHAR(64) NOT NULL,
  permission_read INT NOT NULL,
  permission_write INT NOT NULL,
  created_at TIMESTAMP(6) NOT NULL,
  updated_at TIMESTAMP(6) NOT NULL,
  PRIMARY KEY (collection, user_id, object_key),
  KEY idx_storage_object_list (collection, user_id, object_key)
)`, quoted),
		EnsureIndexColumn: fmt.Sprintf(`ALTER TABLE %s ADD COLUMN index_json JSON NULL AFTER value_json`, quoted),
		Insert:            fmt.Sprintf(`INSERT INTO %s (collection, object_key, user_id, value_json, index_json, version, permission_read, permission_write, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, quoted),
		Upsert:            fmt.Sprintf(`INSERT INTO %s (collection, object_key, user_id, value_json, index_json, version, permission_read, permission_write, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE value_json=VALUES(value_json), index_json=VALUES(index_json), version=VALUES(version), permission_read=VALUES(permission_read), permission_write=VALUES(permission_write), updated_at=VALUES(updated_at)`, quoted),
		UpdateWithVersion: fmt.Sprintf(`UPDATE %s SET value_json=?, index_json=?, version=?, permission_read=?, permission_write=?, updated_at=? WHERE collection=? AND user_id=? AND object_key=? AND version=?`, quoted),
		SelectOne:         fmt.Sprintf(`SELECT collection, object_key, user_id, value_json, index_json, version, permission_read, permission_write, created_at, updated_at FROM %s WHERE collection=? AND user_id=? AND object_key=?`, quoted),
		Delete:            fmt.Sprintf(`DELETE FROM %s WHERE collection=? AND user_id=? AND object_key=?`, quoted),
		DeleteWithVersion: fmt.Sprintf(`DELETE FROM %s WHERE collection=? AND user_id=? AND object_key=? AND version=?`, quoted),
		List:              fmt.Sprintf(`SELECT collection, object_key, user_id, value_json, index_json, version, permission_read, permission_write, created_at, updated_at FROM %s WHERE collection=? AND (?='' OR user_id=?) AND CONCAT(collection, '/', user_id, '/', object_key)>? ORDER BY collection, user_id, object_key LIMIT ?`, quoted),
	}, nil
}

func (s *SQLStore) EnsureSchema(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	stmts, err := s.ready()
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, stmts.EnsureSchema); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, stmts.EnsureIndexColumn); err != nil && !isDupColumnErr(err) {
		return err
	}
	return nil
}

func (s *SQLStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLStore) Write(ctx context.Context, request WriteRequest) (Object, error) {
	if err := ctxErr(ctx); err != nil {
		return Object{}, err
	}
	stmts, err := s.ready()
	if err != nil {
		return Object{}, err
	}
	now := s.nowUTC()
	object, err := normalizeObject(request.Object, now)
	if err != nil {
		return Object{}, err
	}
	if request.IfMatchVersion != "" {
		current, ok, err := s.Read(ctx, object.Key)
		if err != nil {
			return Object{}, err
		}
		if !ok || current.Version != strings.TrimSpace(request.IfMatchVersion) {
			return Object{}, ErrVersionConflict
		}
		object.CreatedAt = current.CreatedAt
		object.Version = versionFor(object)
		result, err := s.db.ExecContext(ctx, stmts.UpdateWithVersion,
			[]byte(object.Value), sqlIndexJSON(object), object.Version, object.PermissionRead, object.PermissionWrite, object.UpdatedAt,
			object.Key.Collection, object.Key.UserID, object.Key.Key, strings.TrimSpace(request.IfMatchVersion),
		)
		if err != nil {
			return Object{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return Object{}, err
		}
		if affected == 0 {
			return Object{}, ErrVersionConflict
		}
		return cloneObject(object), nil
	}
	if request.IfNoneMatch {
		object.Version = versionFor(object)
		_, err := s.db.ExecContext(ctx, stmts.Insert, sqlObjectArgs(object)...)
		if err != nil {
			if isDuplicateKeyError(err) {
				return Object{}, ErrVersionConflict
			}
			return Object{}, err
		}
		return cloneObject(object), nil
	}
	if current, ok, err := s.Read(ctx, object.Key); err != nil {
		return Object{}, err
	} else if ok {
		object.CreatedAt = current.CreatedAt
	}
	object.Version = versionFor(object)
	_, err = s.db.ExecContext(ctx, stmts.Upsert, sqlObjectArgs(object)...)
	if err != nil {
		return Object{}, err
	}
	return cloneObject(object), nil
}

func (s *SQLStore) Read(ctx context.Context, key Key) (Object, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Object{}, false, err
	}
	stmts, err := s.ready()
	if err != nil {
		return Object{}, false, err
	}
	key, err = normalizeKey(key)
	if err != nil {
		return Object{}, false, err
	}
	row := s.db.QueryRowContext(ctx, stmts.SelectOne, key.Collection, key.UserID, key.Key)
	object, err := scanObject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Object{}, false, nil
	}
	if err != nil {
		return Object{}, false, err
	}
	return object, true, nil
}

func (s *SQLStore) Delete(ctx context.Context, key Key, ifMatchVersion string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	stmts, err := s.ready()
	if err != nil {
		return err
	}
	key, err = normalizeKey(key)
	if err != nil {
		return err
	}
	ifMatchVersion = strings.TrimSpace(ifMatchVersion)
	stmt := stmts.Delete
	args := []any{key.Collection, key.UserID, key.Key}
	if ifMatchVersion != "" {
		stmt = stmts.DeleteWithVersion
		args = append(args, ifMatchVersion)
	}
	result, err := s.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		if ifMatchVersion != "" {
			return ErrVersionConflict
		}
		return ErrObjectNotFound
	}
	return nil
}

func (s *SQLStore) List(ctx context.Context, request ListRequest) (result ListResult, err error) {
	if err := ctxErr(ctx); err != nil {
		return ListResult{}, err
	}
	stmts, err := s.ready()
	if err != nil {
		return ListResult{}, err
	}
	collection := strings.TrimSpace(request.Collection)
	if collection == "" {
		return ListResult{}, ErrInvalidKey
	}
	userID := strings.TrimSpace(request.UserID)
	limit := normalizeListLimit(request.Limit)
	cursor := strings.TrimSpace(request.Cursor)
	indexFilters := normalizeIndex(request.Index)
	query, indexArgs := listQuery(stmts, indexFilters)
	args := []any{collection, userID, userID, cursor}
	args = append(args, indexArgs...)
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ListResult{}, err
	}
	if rows == nil {
		return ListResult{}, ErrInvalidSQLRows
	}
	defer closeSQLRowsErr(rows, &err)

	var objects []Object
	for rows.Next() {
		object, err := scanObject(rows)
		if err != nil {
			return ListResult{}, err
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	result = ListResult{Objects: objects}
	if len(result.Objects) > limit {
		result.Objects = result.Objects[:limit]
		result.NextCursor = objectID(result.Objects[len(result.Objects)-1].Key)
	}
	return result, nil
}

func (s *SQLStore) ready() (SQLStatements, error) {
	if s == nil || s.db == nil {
		return SQLStatements{}, ErrSQLDBRequired
	}
	if sqlStatementsReady(s.stmts) {
		return s.stmts, nil
	}
	table := strings.TrimSpace(s.stmts.Table)
	if table == "" {
		table = defaultSQLTable
	}
	return NewSQLStatements(table)
}

func (s *SQLStore) nowUTC() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func sqlStatementsReady(stmts SQLStatements) bool {
	return strings.TrimSpace(stmts.Table) != "" &&
		stmts.EnsureSchema != "" &&
		stmts.EnsureIndexColumn != "" &&
		stmts.Insert != "" &&
		stmts.Upsert != "" &&
		stmts.UpdateWithVersion != "" &&
		stmts.SelectOne != "" &&
		stmts.Delete != "" &&
		stmts.DeleteWithVersion != "" &&
		stmts.List != ""
}

func closeSQLRowsErr(rows interface{ Close() error }, err *error) {
	if rows == nil || err == nil {
		return
	}
	if closeErr := rows.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

func listQuery(stmts SQLStatements, indexFilters map[string]string) (string, []any) {
	indexFilters = normalizeIndex(indexFilters)
	if len(indexFilters) == 0 {
		return stmts.List, nil
	}
	keys := make([]string, 0, len(indexFilters))
	for key := range indexFilters {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	quoted := "`" + stmts.Table + "`"
	var builder strings.Builder
	builder.WriteString(`SELECT collection, object_key, user_id, value_json, index_json, version, permission_read, permission_write, created_at, updated_at FROM `)
	builder.WriteString(quoted)
	builder.WriteString(` WHERE collection=? AND (?='' OR user_id=?) AND CONCAT(collection, '/', user_id, '/', object_key)>?`)
	args := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		builder.WriteString(` AND JSON_UNQUOTE(JSON_EXTRACT(COALESCE(index_json, JSON_OBJECT()), ?)) = ?`)
		args = append(args, jsonPathForIndexKey(key), indexFilters[key])
	}
	builder.WriteString(` ORDER BY collection, user_id, object_key LIMIT ?`)
	return builder.String(), args
}

func jsonPathForIndexKey(key string) string {
	return "$." + strconv.Quote(strings.TrimSpace(key))
}

type objectScanner interface {
	Scan(dest ...any) error
}

func scanObject(scanner objectScanner) (Object, error) {
	var object Object
	var value []byte
	var indexJSON []byte
	if err := scanner.Scan(
		&object.Key.Collection,
		&object.Key.Key,
		&object.Key.UserID,
		&value,
		&indexJSON,
		&object.Version,
		&object.PermissionRead,
		&object.PermissionWrite,
		&object.CreatedAt,
		&object.UpdatedAt,
	); err != nil {
		return Object{}, err
	}
	object.Value = append([]byte(nil), value...)
	object.Index = decodeIndexJSON(indexJSON)
	object.CreatedAt = object.CreatedAt.UTC()
	object.UpdatedAt = object.UpdatedAt.UTC()
	return object, nil
}

func sqlObjectArgs(object Object) []any {
	return []any{
		object.Key.Collection,
		object.Key.Key,
		object.Key.UserID,
		[]byte(object.Value),
		sqlIndexJSON(object),
		object.Version,
		object.PermissionRead,
		object.PermissionWrite,
		object.CreatedAt,
		object.UpdatedAt,
	}
}

func sqlIndexJSON(object Object) []byte {
	data, _ := json.Marshal(normalizeIndex(object.Index))
	if string(data) == "null" {
		return []byte(`{}`)
	}
	return data
}

func decodeIndexJSON(data []byte) map[string]string {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return normalizeIndex(out)
}

func normalizeSQLTable(table string) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		table = defaultSQLTable
	}
	if !sqlTablePattern.MatchString(table) {
		return "", ErrInvalidTable
	}
	return table, nil
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate") || strings.Contains(text, "1062")
}

func isDupColumnErr(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate column") || strings.Contains(text, "1060")
}
