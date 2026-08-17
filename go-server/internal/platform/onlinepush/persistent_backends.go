package onlinepush

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

	cachekit "longheng.io/server/internal/platform/cache"
)

var (
	// ErrCacheRequired 表示缓存状态后端缺失。
	ErrCacheRequired = errors.New("online push cache is required")
	// ErrSQLRequired 表示 SQL 状态后端缺失。
	ErrSQLRequired = errors.New("online push sql db is required")
	// ErrInvalidKey 表示状态 key 为空或非法。
	ErrInvalidKey = errors.New("online push state key is invalid")
	// ErrInvalidTable 表示 SQL 表名为空或非法。
	ErrInvalidTable = errors.New("online push table is invalid")
)

// CacheStateStore 使用框架 cache.Store 保存在线推送状态快照。
type CacheStateStore struct {
	Cache cachekit.Store
	Key   string
	TTL   time.Duration
}

// NewCacheStateStore 创建缓存状态后端。
func NewCacheStateStore(cache cachekit.Store, key string, ttl time.Duration) *CacheStateStore {
	return &CacheStateStore{Cache: cache, Key: strings.TrimSpace(key), TTL: ttl}
}

// Load 从缓存读取在线推送状态。
func (s *CacheStateStore) Load(ctx context.Context) (State, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return State{}, false, err
	}
	if s == nil || s.Cache == nil {
		return State{}, false, ErrCacheRequired
	}
	key := strings.TrimSpace(s.Key)
	if key == "" {
		return State{}, false, ErrInvalidKey
	}
	data, ok, err := s.Cache.Get(ctx, key)
	if err != nil || !ok {
		return State{}, ok, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, false, err
	}
	return state, true, nil
}

// Save 将在线推送状态保存到缓存。
func (s *CacheStateStore) Save(ctx context.Context, state State) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.Cache == nil {
		return ErrCacheRequired
	}
	key := strings.TrimSpace(s.Key)
	if key == "" {
		return ErrInvalidKey
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.Cache.Set(ctx, key, data, s.TTL)
}

// Close 关闭缓存状态后端。
func (s *CacheStateStore) Close() error {
	return nil
}

// SQLStateStore 使用 SQL 单行 JSON 保存在线推送状态快照。
type SQLStateStore struct {
	DB     *sql.DB
	Table  string
	Key    string
	Now    func() time.Time
	stmts  SQLStatements
	initMu sync.Mutex
}

// SQLStatements 保存在线推送 SQL 状态后端的语句模板。
type SQLStatements struct {
	Table        string
	EnsureSchema string
	Select       string
	Upsert       string
}

var tableNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// NewSQLStateStore 创建 SQL 状态后端。
func NewSQLStateStore(db *sql.DB, table, key string, now func() time.Time) (*SQLStateStore, error) {
	if db == nil {
		return nil, ErrSQLRequired
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrInvalidKey
	}
	stmts, err := NewSQLStatements(table)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SQLStateStore{DB: db, Table: stmts.Table, Key: key, Now: now, stmts: stmts}, nil
}

// NewSQLStatements 为指定白名单表名生成 SQL 语句。
func NewSQLStatements(table string) (SQLStatements, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		table = "online_push_state"
	}
	if !tableNamePattern.MatchString(table) {
		return SQLStatements{}, ErrInvalidTable
	}
	quoted := "`" + table + "`"
	return SQLStatements{
		Table: table,
		EnsureSchema: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  state_key VARCHAR(191) NOT NULL,
  state_json JSON NOT NULL,
  updated_at TIMESTAMP(6) NOT NULL,
  PRIMARY KEY (state_key)
)`, quoted),
		Select: fmt.Sprintf(`SELECT state_json FROM %s WHERE state_key=?`, quoted),
		Upsert: fmt.Sprintf(`INSERT INTO %s (state_key, state_json, updated_at) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE state_json=VALUES(state_json), updated_at=VALUES(updated_at)`, quoted),
	}, nil
}

func (s *SQLStateStore) ensureReady(requireKey bool) error {
	if s == nil || s.DB == nil {
		return ErrSQLRequired
	}
	s.initMu.Lock()
	defer s.initMu.Unlock()
	table := strings.TrimSpace(s.Table)
	if table == "" {
		table = strings.TrimSpace(s.stmts.Table)
	}
	stmts, err := NewSQLStatements(table)
	if err != nil {
		return err
	}
	if s.stmts.Table != stmts.Table || s.stmts.EnsureSchema == "" || s.stmts.Select == "" || s.stmts.Upsert == "" {
		s.stmts = stmts
	}
	s.Table = s.stmts.Table
	s.Key = strings.TrimSpace(s.Key)
	if requireKey && s.Key == "" {
		return ErrInvalidKey
	}
	if s.Now == nil {
		s.Now = func() time.Time { return time.Now().UTC() }
	}
	return nil
}

// EnsureSchema 确保 SQL 状态表存在。
func (s *SQLStateStore) EnsureSchema(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := s.ensureReady(false); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, s.stmts.EnsureSchema)
	return err
}

// Load 从 SQL 状态表读取在线推送状态。
func (s *SQLStateStore) Load(ctx context.Context) (State, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return State{}, false, err
	}
	if err := s.ensureReady(true); err != nil {
		return State{}, false, err
	}
	var data []byte
	err := s.DB.QueryRowContext(ctx, s.stmts.Select, s.Key).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, false, err
	}
	return state, true, nil
}

// Save 将在线推送状态保存到 SQL 状态表。
func (s *SQLStateStore) Save(ctx context.Context, state State) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := s.ensureReady(true); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, s.stmts.Upsert, s.Key, data, s.Now().UTC()) // #nosec G701 -- Upsert 由 NewSQLStatements 使用白名单表名生成，业务值通过参数绑定。
	return err
}

// Close 关闭 SQL 状态后端。
func (s *SQLStateStore) Close() error {
	return nil
}
